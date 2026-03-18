package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"text/template"

	_ "github.com/joho/godotenv/autoload"
	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

func main() {
	var err error
	db, err = sql.Open("sqlite3", "./database.db")
	if err != nil {
		log.Fatal(err)
	}
	db.Exec("CREATE TABLE IF NOT EXISTS submissions (id INTEGER PRIMARY KEY AUTOINCREMENT, userid INTEGER, githublink TEXT, description TEXT, timespent INTEGER, thumbnail TEXT, timestamp DATETIME DEFAULT CURRENT_TIMESTAMP)")
	db.Exec("CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, email TEXT, slackid TEXT UNIQUE, verified BOOLEAN, candoysws BOOLEAN)")

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static/"))))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		tmpl := template.Must(template.ParseFiles("templates/index.html"))
		tmpl.Execute(w, nil)
	})
	http.HandleFunc("/submit", func(w http.ResponseWriter, r *http.Request) {
		tmpl := template.Must(template.ParseFiles("templates/submit.html"))
		tmpl.Execute(w, nil)
	})
	http.HandleFunc("/projects", func(w http.ResponseWriter, r *http.Request) {
		tmpl := template.Must(template.ParseFiles("templates/projects.html"))
		tmpl.Execute(w, nil)
	})
	http.HandleFunc("/guide", func(w http.ResponseWriter, r *http.Request) {
		tmpl := template.Must(template.ParseFiles("templates/guide.html"))
		tmpl.Execute(w, nil)
	})

	http.HandleFunc("/api/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		AuthCallback(w, r, code)
	})

	http.HandleFunc("/api/server/usage", func(w http.ResponseWriter, r *http.Request) {
		// TODO
	})

	fmt.Println("Server is running on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":"+os.Getenv("PORT"), nil))
}

type TokenResponse struct {
	AccessToken string `json:"access_token"`
}

func AuthCallback(w http.ResponseWriter, r *http.Request, code string) {
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	body := url.Values{}
	body.Set("client_id", "421396b4a7e853e891b738766586b76a")
	body.Set("client_secret", os.Getenv("CLIENT_SECRET"))
	body.Set("code", code)
	body.Set("grant_type", "authorization_code")
	body.Set("redirect_uri", "http://localhost:8080/api/auth/callback")

	resp, err := http.PostForm("https://auth.hackclub.com/oauth/token", body)
	if err != nil {
		http.Error(w, "failed to request token", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	// fmt.Printf("Token response body: %s\n", string(bodyBytes))

	var tokenResp TokenResponse
	json.Unmarshal(bodyBytes, &tokenResp)

	if tokenResp.AccessToken == "" {
		// fmt.Printf("Token response: %+v\n", tokenResp)
		http.Error(w, "failed to get access token", http.StatusInternalServerError)
		return
	}

	FetchUserInfo(w, r, tokenResp.AccessToken)
}

func FetchUserInfo(w http.ResponseWriter, r *http.Request, accessToken string) {
	req, err := http.NewRequest("GET", "https://auth.hackclub.com/api/v1/me", nil)
	if err != nil {
		http.Error(w, "failed to create request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "failed to fetch user info", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var user map[string]any
	json.NewDecoder(resp.Body).Decode(&user)

	if user["identity"].(map[string]any)["ysws_eligible"].(bool) != true {
		http.Error(w, "Sorry! You are not eligible for YSWS, please send a message on the slack at #identity-help", http.StatusForbidden)
		return
	}
	// log.Print(user)
	AddUser(
		user["identity"].(map[string]any)["first_name"].(string)+" "+user["identity"].(map[string]any)["last_name"].(string),
		user["identity"].(map[string]any)["primary_email"].(string),
		user["identity"].(map[string]any)["slack_id"].(string),
		user["identity"].(map[string]any)["verification_status"].(string) == "verified",
		user["identity"].(map[string]any)["ysws_eligible"].(bool))

	name := user["identity"].(map[string]any)["first_name"].(string) + " " + user["identity"].(map[string]any)["last_name"].(string)
	slackid := user["identity"].(map[string]any)["slack_id"].(string)

	http.Redirect(w, r, "/?name="+url.QueryEscape(name)+
		"&slackid="+url.QueryEscape(slackid),
		http.StatusSeeOther)
}

func AddUser(name string, email string, slackid string, verified bool, candoysws bool) {
	_, err := db.Exec("INSERT INTO users (name, email, slackid, verified, candoysws) VALUES (?, ?, ?, ?, ?)", name, email, slackid, verified, candoysws)
	if err != nil && err.Error() == "UNIQUE constraint failed: users.slackid" {
		log.Printf("User with slackid %s already exists, skipping insert\n", slackid)
	}
	if err != nil {
		log.Println("Error adding user:", err)
	}
}
