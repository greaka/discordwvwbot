package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

var config struct {
	CertificatePath   string `json:"certificatePath"`
	PrivateKeyPath    string `json:"privateKeyPath"`
	BotToken          string `json:"botToken"`
	DiscordClientID   string `json:"discordClientId"`
	DiscordAuthSecret string `json:"discordOAuthSecret"`
}

func redirectToTLS(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "https://"+r.Host+r.RequestURI, http.StatusMovedPermanently)
}

func handleRootRequest(w http.ResponseWriter, r *http.Request) {}

func handleAuthCallback(w http.ResponseWriter, r *http.Request) {}

func handleAuthRequest(w http.ResponseWriter, r *http.Request) {}

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
