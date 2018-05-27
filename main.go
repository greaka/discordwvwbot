package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/bwmarrin/discordgo"

	"golang.org/x/oauth2"
)

const discordAPIURL string = "https://discordapp.com/api"

type tokeninfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

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

// nolint: gocyclo
func handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	token, err := oauthConfig.Exchange(context.Background(), r.FormValue("state"))
	if err != nil {
		log.Fatalf("Error getting token: %v", err)
		if _, erro := fmt.Fprint(w, "Error getting discord authorization token."); erro != nil {
			log.Fatalf("Error writing to Responsewriter: %v and %v", err, erro)
		}
		return
	}

	client := &http.Client{}
	req, err := http.NewRequest("GET", discordAPIURL+"/users/@me", nil)
	if err != nil {
		log.Fatalf("Error creating a new request: %v", err)
		if _, erro := fmt.Fprint(w, "Internal error, please try again or contact me."); erro != nil {
			log.Fatalf("Error writing to Responsewriter: %v and %v", err, erro)
		}
		return
	}
	token.SetAuthHeader(req)
	res, err := client.Do(req)
	if err != nil {
		log.Fatalf("Error getting discord id: %v", err)
		if _, erro := fmt.Fprint(w, "Error getting discord id..."); erro != nil {
			log.Fatalf("Error writing to Responsewriter: %v and %v", err, erro)
		}
		return
	}
	defer func() {
		if err = res.Body.Close(); err != nil {
			log.Fatalf("Error closing response body: %v", err)
		}
	}()

	jsonParser := json.NewDecoder(res.Body)
	var user discordgo.User
	err = jsonParser.Decode(&user)
	if err != nil {
		log.Fatalf("Error parsing json to discordgo.User: %v", err)
		if _, erro := fmt.Fprint(w, "Internal error, please try again or contact me."); erro != nil {
			log.Fatalf("Error writing to Responsewriter: %v and %v", err, erro)
		}
		return
	}

	// TODO: save

	if _, erro := fmt.Fprint(w, "Success"); erro != nil {
		log.Fatalf("Error writing to Responsewriter: %v and %v", err, erro)
	}
}

func handleAuthRequest(w http.ResponseWriter, r *http.Request) {
	res, err := http.Get("https://api.guildwars2.com/v2/tokeninfo?access_token=" + r.FormValue("key"))
	if err != nil {
		log.Fatalf("Error quering tokeninfo: %v", err)
		if _, erro := fmt.Fprint(w, "Internal error, please try again or contact me."); erro != nil {
			log.Fatalf("Error writing to Responsewriter: %v and %v", err, erro)
		}
		return
	}
	jsonParser := json.NewDecoder(res.Body)
	var token tokeninfo
	err = jsonParser.Decode(&token)
	if err != nil {
		log.Fatalf("Error parsing json to tokeninfo: %v", err)
		if _, erro := fmt.Fprint(w, "Internal error, please try again or contact me."); erro != nil {
			log.Fatalf("Error writing to Responsewriter: %v and %v", err, erro)
		}
		return
	}
	if !strings.Contains(token.Name, "wvwbot") {
		if _, erro := fmt.Fprintf(w, "This api key is not valid. Make sure your key name contains 'wvwbot'. This api key is named %v", token.Name); erro != nil {
			log.Fatalf("Error writing to Responsewriter: %v and %v", err, erro)
		}
		return
	}

	// we can use the key as state here because we are not vulnerable to csrf (change my mind)
	http.Redirect(w, r, oauthConfig.AuthCodeURL(r.FormValue("key")), http.StatusTemporaryRedirect)
}

func main() {
	conf, err := os.Open("config.json")
	if err != nil {
		log.Fatalf("Error opening config file: %v", err)
		return
	}
	defer func() {
		if err = conf.Close(); err != nil {
			log.Fatalf("Error closing config file: %v", err)
		}
	}()
	jsonParser := json.NewDecoder(conf)
	if err = jsonParser.Decode(&config); err != nil {
		log.Fatalf("Error parsing config file: %v", err)
		return
	}

	oauthConfig = &oauth2.Config{
		ClientID:     config.DiscordClientID,
		ClientSecret: config.DiscordAuthSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  discordAPIURL + "/oauth2/authorize",
			TokenURL: discordAPIURL + "/oauth2/token",
		},
		RedirectURL: config.HostURL,
		Scopes:      []string{"identify"},
	}

	htmlFile, err := ioutil.ReadFile(config.HTMLPath)
	if err != nil {
		log.Fatalf("Error opening config file: %v", err)
		return
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
