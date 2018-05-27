package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"golang.org/x/oauth2"
)

var config struct {
	CertificatePath   string `json:"certificatePath"`
	PrivateKeyPath    string `json:"privateKeyPath"`
	BotToken          string `json:"botToken"`
	DiscordClientID   string `json:"discordClientId"`
	DiscordAuthSecret string `json:"discordOAuthSecret"`
	HostURL           string `json:"domain"`
	HTMLPath          string `json:"html"`
}

var oauthConfig *oauth2.Config
var mainpage string

func redirectToTLS(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "https://"+r.Host+r.RequestURI, http.StatusMovedPermanently)
}

func handleRootRequest(w http.ResponseWriter, r *http.Request) {
	if _, err := fmt.Fprintf(w, mainpage); err != nil {
		log.Fatalf("Error handling root request: %v", err)
	}
}

func handleAuthCallback(w http.ResponseWriter, r *http.Request) {}

func handleAuthRequest(w http.ResponseWriter, r *http.Request) {
	// we can use the key as state here because we are not vulnerable to csrf (change my mind)
	http.Redirect(w, r, oauthConfig.AuthCodeURL(r.FormValue("key")), http.StatusTemporaryRedirect)
}

func main() {
	conf, err := os.Open("config.json")
	if err != nil {
		log.Fatalf("Error opening config file: %v", err)
	}
	defer func() {
		if err = conf.Close(); err != nil {
			log.Fatalf("Error closing config file: %v", err)
		}
	}()
	jsonParser := json.NewDecoder(conf)
	if err = jsonParser.Decode(&config); err != nil {
		log.Fatalf("Error parsing config file: %v", err)
	}

	oauthConfig = &oauth2.Config{
		ClientID:     config.DiscordClientID,
		ClientSecret: config.DiscordAuthSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://discordapp.com/api/oauth2/authorize",
			TokenURL: "https://discordapp.com/api/oauth2/token",
		},
		RedirectURL: config.HostURL,
		Scopes:      []string{"identify"},
	}

	htmlFile, err := ioutil.ReadFile(config.HTMLPath)
	if err != nil {
		log.Fatalf("Error opening config file: %v", err)
	}
	mainpage = string(htmlFile)

	http.HandleFunc("/", handleRootRequest)
	http.HandleFunc("/login", handleAuthRequest)
	http.HandleFunc("/oauthcallback", handleAuthCallback)

	go func() {
		if err := http.ListenAndServe(":80", http.HandlerFunc(redirectToTLS)); err != nil {
			log.Fatalf("ListenAndServeError: %v", err)
		}
	}()

	log.Fatal(http.ListenAndServeTLS(":443", config.CertificatePath, config.PrivateKeyPath, nil))
}
