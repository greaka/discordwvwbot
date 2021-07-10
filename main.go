package main

import (
	"encoding/json"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/greaka/discordwvwbot/webhooklogger"

	"github.com/bwmarrin/discordgo"
	"github.com/greaka/discordwvwbot/loglevels"

	"golang.org/x/oauth2"
)

const discordAPIURL string = "https://discordapp.com/api"
const gw2APIURL string = "https://api.guildwars2.com/v2"

var (
	// oauthConfig saves the oauth config for the discord login
	oauthConfig *oauth2.Config

	// mainpage is the page that gets served at / in string format
	mainpage string

	// dbTemplate is the template to render the dashboard
	dbTemplate *template.Template
)

// main is the entry point and fires up everything
// nolint: gocyclo
func main() {

	// open log file to write to it
	f, err := os.OpenFile("botlog", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		log.Fatalf("error opening log file: %v", err)
	}
	defer func() {
		if err = f.Close(); err != nil {
			loglevels.Errorf("Error closing log file: %v\n", err)
		}
	}()

	// set log file
	loglevels.SetWriter(loglevels.LevelInfo, os.Stdout)
	loglevels.SetWriter(loglevels.LevelWarning, os.Stdout)
	loglevels.SetWriter(loglevels.LevelError, os.Stderr)
	loglevels.Info("Starting up...")

	// load config
	conf, err := os.Open("config.json")
	if err != nil {
		loglevels.Errorf("Error opening config file: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if err = conf.Close(); err != nil {
			loglevels.Errorf("Error closing config file: %v\n", err)
		}
	}()
	jsonParser := json.NewDecoder(conf)
	if err = jsonParser.Decode(&config); err != nil {
		loglevels.Errorf("Error parsing config file: %v\n", err)
		os.Exit(1)
	}

	// connect to the discord bot api
	dg, err = discordgo.New("Bot " + config.BotToken)
	if err != nil {
		loglevels.Errorf("Error connecting to discord: %v\n", err)
		os.Exit(1)
	}

	var webhookLoggerInfo webhooklogger.WebhookLogger
	if config.WebhookIDInfo != "" && config.WebhookTokenInfo != "" {
		webhookLoggerInfo = webhooklogger.WebhookLogger{}
		webhookLoggerInfo.SetOutput(dg, config.WebhookIDInfo, config.WebhookTokenInfo)
		w := io.MultiWriter(f, webhookLoggerInfo)
		loglevels.SetWriter(loglevels.LevelInfo, w)
	}

	var webhookLoggerWarning webhooklogger.WebhookLogger
	if config.WebhookIDWarning != "" && config.WebhookTokenWarning != "" {
		webhookLoggerWarning = webhooklogger.WebhookLogger{}
		webhookLoggerWarning.SetOutput(dg, config.WebhookIDWarning, config.WebhookTokenWarning)
		w := io.MultiWriter(f, webhookLoggerWarning)
		loglevels.SetWriter(loglevels.LevelWarning, w)
		log.SetOutput(f)
	}

	var webhookLoggerError webhooklogger.WebhookLogger
	if config.WebhookIDError != "" && config.WebhookTokenError != "" {
		webhookLoggerError = webhooklogger.WebhookLogger{}
		webhookLoggerError.SetOutput(dg, config.WebhookIDError, config.WebhookTokenError)
		w := io.MultiWriter(f, webhookLoggerError)
		loglevels.SetWriter(loglevels.LevelError, w)
	}

	initializeRedisPools()

	if err = migrateRedis(); err != nil {
		os.Exit(1)
	}

	// starting up the bot part
	go startBot()

	oauthConfig = &oauth2.Config{
		ClientID:     config.DiscordClientID,
		ClientSecret: config.DiscordAuthSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  discordAPIURL + "/oauth2/authorize",
			TokenURL: discordAPIURL + "/oauth2/token",
		},
		RedirectURL: config.HostURL + config.RedirectURL,
		Scopes:      []string{"identify", "guilds"},
	}

	// loading mainpage
	htmlFile, err := ioutil.ReadFile(config.HTMLPath)
	if err != nil {
		loglevels.Errorf("Error opening html file: %v\n", err)
		os.Exit(1)
	}
	mainpage = string(htmlFile)

	// loading dashboard template
	htmlFile, err = ioutil.ReadFile(config.TemplatePath)
	if err != nil {
		loglevels.Errorf("Error opening template file: %v\n", err)
		os.Exit(1)
	}
	dbTemplate, err = template.New("dashboard").Parse(string(htmlFile))

	// setting up https server
	mux := http.NewServeMux()

	mux.HandleFunc("/", handleRootRequest)
	mux.HandleFunc("/login", handleAuthRequest)
	mux.HandleFunc(config.RedirectURL, handleAuthCallback)
	mux.HandleFunc("/invite", handleInvite)
	mux.HandleFunc("/dashboard", handleDashboard)
	mux.HandleFunc("/submit", handleSubmitDashboard)
	
	srv := &http.Server{
		Addr:         ":4040",
		Handler:      mux,
	}

	loglevels.Info("starting up https listener...")
	loglevels.Error(srv.ListenAndServe())
	os.Exit(1)
}
