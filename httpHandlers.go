package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/greaka/discordwvwbot/loglevels"
)

// redirectToTLS is the handler function for http calls to get redirected to https
func redirectToTLS(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "https://"+r.Host+r.RequestURI, http.StatusMovedPermanently)
}

// handleRootRequest serves the mainpage
func handleRootRequest(w http.ResponseWriter, r *http.Request) {
	addHeaders(w, r)
	if _, err := fmt.Fprintf(w, mainpage); err != nil {
		loglevels.Errorf("Error handling root request: %v\n", err)
	}
}

// handleAuthCallback is listening to returning oauth requests to discord
// nolint: gocyclo
func handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	addHeaders(w, r)
	state := r.FormValue("state")
	// request oauth access with the issue data sent by discord
	token, err := oauthConfig.Exchange(context.Background(), r.FormValue("code"))
	// err will also be not nil when the user presses Cancel at the oauth request
	if err != nil {
		loglevels.Errorf("Error getting token: %v\n", err)
		if _, erro := fmt.Fprint(w, "Error getting discord authorization token."); erro != nil {
			loglevels.Errorf("Error writing to Responsewriter: %v and %v\n", err, erro)
		}
		return
	}

	// get discord id
	client := &http.Client{}
	req, err := http.NewRequest("GET", discordAPIURL+"/users/@me", nil)
	if err != nil {
		loglevels.Errorf("Error creating a new request: %v\n", err)
		if _, erro := fmt.Fprint(w, "Internal error, please try again or contact me."); erro != nil {
			loglevels.Errorf("Error writing to Responsewriter: %v and %v\n", err, erro)
		}
		return
	}
	token.SetAuthHeader(req)
	res, err := client.Do(req)
	if err != nil {
		loglevels.Errorf("Error getting discord id: %v\n", err)
		if _, erro := fmt.Fprint(w, "Error getting discord id. Please contact me."); erro != nil {
			loglevels.Errorf("Error writing to Responsewriter: %v and %v\n", err, erro)
		}
		return
	}
	defer func() {
		if err = res.Body.Close(); err != nil {
			loglevels.Errorf("Error closing response body: %v\n", err)
		}
	}()

	// parse user json to discordgo.User
	jsonParser := json.NewDecoder(res.Body)
	var user discordgo.User
	err = jsonParser.Decode(&user)
	if err != nil {
		loglevels.Errorf("Error parsing json to discordgo.User: %v\n", err)
		if _, erro := fmt.Fprint(w, "Internal error, please try again or contact me."); erro != nil {
			loglevels.Errorf("Error writing to Responsewriter: %v and %v\n", err, erro)
		}
		return
	}

	switch state {

	// delete everything we know about this user
	case "deletemydata":
		redisConn := pool.Get()
		_, err = redisConn.Do("DEL", user.ID)
		closeConnection(redisConn)
		if err != nil {
			loglevels.Errorf("Error deleting key from redis: %v\n", err)
			if _, erro := fmt.Fprint(w, "Internal error, please try again or contact me."); erro != nil {
				loglevels.Errorf("Error writing to Responsewriter: %v and %v\n", err, erro)
			}
			return
		}

	// sync the user on all discord servers
	case "syncnow":
		updateUserChannel <- user.ID

	// state holds api key, save api key and update user
	default:
		redisConn := pool.Get()
		// SADD will ignore the request if the apikey is already saved from this user
		_, err = redisConn.Do("SADD", user.ID, state)
		closeConnection(redisConn)
		if err != nil {
			loglevels.Errorf("Error saving key to redis: %v\n", err)
			if _, erro := fmt.Fprint(w, "Internal error, please try again or contact me."); erro != nil {
				loglevels.Errorf("Error writing to Responsewriter: %v and %v\n", err, erro)
			}
			return
		}
		loglevels.Infof("New user: %v", user.ID)
		updateUserChannel <- user.ID
	}

	if _, err = fmt.Fprint(w, "Success"); err != nil {
		loglevels.Errorf("Error writing to Responsewriter: %v\n", err)
	}
}

// handleAuthRequest forges the oauth request to discord and packs data for the callback
// nolint: gocyclo
func handleAuthRequest(w http.ResponseWriter, r *http.Request) {
	addHeaders(w, r)
	key := r.FormValue("key")
	// filter if request contains special keywords
	switch key {
	case "deletemydata":
	case "syncnow":
	default:
		// check if api key is valid
		res, err := http.Get(gw2APIURL + "/tokeninfo?access_token=" + key)
		if err != nil {
			loglevels.Errorf("Error quering tokeninfo: %v\n", err)
			if _, erro := fmt.Fprint(w, "Internal error, please try again or contact me."); erro != nil {
				loglevels.Errorf("Error writing to Responsewriter: %v and %v\n", err, erro)
			}
			return
		}
		defer func() {
			if err = res.Body.Close(); err != nil {
				loglevels.Errorf("Error closing response body: %v\n", err)
			}
		}()
		// parse tokeninfo
		jsonParser := json.NewDecoder(res.Body)
		var token tokenInfo
		err = jsonParser.Decode(&token)
		if err != nil {
			loglevels.Errorf("Error parsing json to tokeninfo: %v\n", err)
			if _, erro := fmt.Fprint(w, "Internal error, please try again or contact me."); erro != nil {
				loglevels.Errorf("Error writing to Responsewriter: %v and %v\n", err, erro)
			}
			return
		}
		// check if api key contains wvwbot
		nameInLower := strings.ToLower(token.Name)
		if !strings.Contains(nameInLower, "wvw") || !strings.Contains(nameInLower, "bot") {
			if _, erro := fmt.Fprintf(w, "This api key is not valid. Make sure your key name contains 'wvwbot'. This api key is named %v", token.Name); erro != nil {
				loglevels.Errorf("Error writing to Responsewriter: %v and %v\n", err, erro)
			}
			return
		}
	}

	// redirect to discord login
	// we can use the key as state here because we are not vulnerable to csrf (change my mind)
	http.Redirect(w, r, oauthConfig.AuthCodeURL(key), http.StatusTemporaryRedirect)
}

// handleInvite responds with a discord URL to invite this bot to a discord server
func handleInvite(w http.ResponseWriter, r *http.Request) {
	addHeaders(w, r)
	http.Redirect(w, r, "https://discordapp.com/oauth2/authorize?client_id="+config.DiscordClientID+"&scope=bot&permissions=402653184", http.StatusPermanentRedirect)
}

// addHeaders adds the standard headers to the http.ResponseWriter
func addHeaders(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
}
