package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/gomodule/redigo/redis"

	"golang.org/x/oauth2"
)

const discordAPIURL string = "https://discordapp.com/api"

type tokenInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

var config struct {
	CertificatePath       string `json:"certificatePath"`
	PrivateKeyPath        string `json:"privateKeyPath"`
	BotToken              string `json:"botToken"`
	DiscordClientID       string `json:"discordClientId"`
	DiscordAuthSecret     string `json:"discordOAuthSecret"`
	HostURL               string `json:"domain"`
	HTMLPath              string `json:"html"`
	RedisConnectionString string `json:"redis"`
	RedirectURL           string `json:"oauthredirect"`
}

var (
	oauthConfig *oauth2.Config
	mainpage    string
	redisConn   redis.Conn
)

func redirectToTLS(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "https://"+r.Host+r.RequestURI, http.StatusMovedPermanently)
}

func handleRootRequest(w http.ResponseWriter, r *http.Request) {
	addHeaders(w, r)
	if _, err := fmt.Fprintf(w, mainpage); err != nil {
		log.Printf("Error handling root request: %v\n", err)
	}
}

// nolint: gocyclo
func handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	addHeaders(w, r)
	state := r.FormValue("state")
	token, err := oauthConfig.Exchange(context.Background(), r.FormValue("code"))
	if err != nil {
		log.Printf("Error getting token: %v\n", err)
		if _, erro := fmt.Fprint(w, "Error getting discord authorization token."); erro != nil {
			log.Printf("Error writing to Responsewriter: %v and %v\n", err, erro)
		}
		return
	}

	client := &http.Client{}
	req, err := http.NewRequest("GET", discordAPIURL+"/users/@me", nil)
	if err != nil {
		log.Printf("Error creating a new request: %v\n", err)
		if _, erro := fmt.Fprint(w, "Internal error, please try again or contact me."); erro != nil {
			log.Printf("Error writing to Responsewriter: %v and %v\n", err, erro)
		}
		return
	}
	token.SetAuthHeader(req)
	res, err := client.Do(req)
	if err != nil {
		log.Printf("Error getting discord id: %v\n", err)
		if _, erro := fmt.Fprint(w, "Error getting discord id. Please contact me."); erro != nil {
			log.Printf("Error writing to Responsewriter: %v and %v\n", err, erro)
		}
		return
	}
	defer func() {
		if err = res.Body.Close(); err != nil {
			log.Printf("Error closing response body: %v\n", err)
		}
	}()

	jsonParser := json.NewDecoder(res.Body)
	var user discordgo.User
	err = jsonParser.Decode(&user)
	if err != nil {
		log.Printf("Error parsing json to discordgo.User: %v\n", err)
		if _, erro := fmt.Fprint(w, "Internal error, please try again or contact me."); erro != nil {
			log.Printf("Error writing to Responsewriter: %v and %v\n", err, erro)
		}
		return
	}

	switch state {
	case "deletemydata":
		_, err = redisConn.Do("DEL", user.ID)
		if err != nil {
			log.Printf("Error deleting key from redis: %v\n", err)
			if _, erro := fmt.Fprint(w, "Internal error, please try again or contact me."); erro != nil {
				log.Printf("Error writing to Responsewriter: %v and %v\n", err, erro)
			}
			return
		}
	case "syncnow":
		updateUserChannel <- user.ID
	default:
		_, err = redisConn.Do("SADD", user.ID, state)
		if err != nil {
			log.Printf("Error saving key to redis: %v\n", err)
			if _, erro := fmt.Fprint(w, "Internal error, please try again or contact me."); erro != nil {
				log.Printf("Error writing to Responsewriter: %v and %v\n", err, erro)
			}
			return
		}
		log.Printf("New user: %v", user.ID)
		updateUserChannel <- user.ID
	}

	if _, err = fmt.Fprint(w, "Success"); err != nil {
		log.Printf("Error writing to Responsewriter: %v\n", err)
	}
}

func handleAuthRequest(w http.ResponseWriter, r *http.Request) {
	addHeaders(w, r)
	key := r.FormValue("key")
	if key != "deletemydata" && key != "syncnow" {
		res, err := http.Get("https://api.guildwars2.com/v2/tokeninfo?access_token=" + key)
		if err != nil {
			log.Printf("Error quering tokeninfo: %v\n", err)
			if _, erro := fmt.Fprint(w, "Internal error, please try again or contact me."); erro != nil {
				log.Printf("Error writing to Responsewriter: %v and %v\n", err, erro)
			}
			return
		}
		defer func() {
			if err = res.Body.Close(); err != nil {
				log.Printf("Error closing response body: %v\n", err)
			}
		}()
		jsonParser := json.NewDecoder(res.Body)
		var token tokenInfo
		err = jsonParser.Decode(&token)
		if err != nil {
			log.Printf("Error parsing json to tokeninfo: %v\n", err)
			if _, erro := fmt.Fprint(w, "Internal error, please try again or contact me."); erro != nil {
				log.Printf("Error writing to Responsewriter: %v and %v\n", err, erro)
			}
			return
		}
		if !strings.Contains(token.Name, "wvwbot") {
			if _, erro := fmt.Fprintf(w, "This api key is not valid. Make sure your key name contains 'wvwbot'. This api key is named %v", token.Name); erro != nil {
				log.Printf("Error writing to Responsewriter: %v and %v\n", err, erro)
			}
			return
		}
	}

	// we can use the key as state here because we are not vulnerable to csrf (change my mind)
	http.Redirect(w, r, oauthConfig.AuthCodeURL(key), http.StatusTemporaryRedirect)
}

func handleInvite(w http.ResponseWriter, r *http.Request) {
	addHeaders(w, r)
	http.Redirect(w, r, "https://discordapp.com/oauth2/authorize?client_id="+config.DiscordClientID+"&scope=bot&permissions=402653184", http.StatusPermanentRedirect)
}

func addHeaders(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
}

func main() {
	f, err := os.OpenFile("botlog", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		log.Fatalf("error opening log file: %v", err)
	}
	defer func() {
		if err = f.Close(); err != nil {
			log.Printf("Error closing log file: %v\n", err)
		}
	}()

	log.SetOutput(f)
	log.Println("Starting up...")

	conf, err := os.Open("config.json")
	if err != nil {
		log.Fatalf("Error opening config file: %v\n", err)
	}
	defer func() {
		if err = conf.Close(); err != nil {
			log.Printf("Error closing config file: %v\n", err)
		}
	}()
	jsonParser := json.NewDecoder(conf)
	if err = jsonParser.Decode(&config); err != nil {
		log.Fatalf("Error parsing config file: %v\n", err)
	}

	red, err := redis.DialURL(config.RedisConnectionString)
	if err != nil {
		log.Fatalf("Error connecting to redis server: %v\n", err)
	}
	redisConn = red
	defer func() {
		if err = red.Close(); err != nil {
			log.Printf("Error closing redis connection: %v\n", err)
		}
	}()

	go startBot()

	oauthConfig = &oauth2.Config{
		ClientID:     config.DiscordClientID,
		ClientSecret: config.DiscordAuthSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  discordAPIURL + "/oauth2/authorize",
			TokenURL: discordAPIURL + "/oauth2/token",
		},
		RedirectURL: config.HostURL + config.RedirectURL,
		Scopes:      []string{"identify"},
	}

	htmlFile, err := ioutil.ReadFile(config.HTMLPath)
	if err != nil {
		log.Fatalf("Error opening html file: %v\n", err)
	}
	mainpage = string(htmlFile)

	go func() {
		log.Println("starting up http redirect...")
		if err := http.ListenAndServe(":80", http.HandlerFunc(redirectToTLS)); err != nil {
			log.Printf("ListenAndServeError: %v\n", err)
		}
	}()

	mux := http.NewServeMux()

	mux.HandleFunc("/", handleRootRequest)
	mux.HandleFunc("/login", handleAuthRequest)
	mux.HandleFunc(config.RedirectURL, handleAuthCallback)
	mux.HandleFunc("/invite", handleInvite)
	cfg := &tls.Config{
		MinVersion:               tls.VersionTLS12,
		CurvePreferences:         []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
		PreferServerCipherSuites: true,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
			tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
		},
	}
	srv := &http.Server{
		Addr:         ":443",
		Handler:      mux,
		TLSConfig:    cfg,
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}

	log.Println("starting up https listener...")
	log.Fatal(srv.ListenAndServeTLS(config.CertificatePath, config.PrivateKeyPath))
}
