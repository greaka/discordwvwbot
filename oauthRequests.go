package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/gomodule/redigo/redis"
	"github.com/greaka/discordwvwbot/loglevels"
	"golang.org/x/oauth2"
)

func newSession(user string) (code string, err error) {
	key := [32]byte{}
	_, err = rand.Read(key[:])
	if err != nil {
		loglevels.Errorf("error getting random: %v\n", err)
	}
	code = base64.URLEncoding.EncodeToString(key[:])
	c := sessionsDatabase.Get()
	_, err = c.Do("SET", code, user, "EX", "3600")
	closeConnection(c)
	if err != nil {
		loglevels.Errorf("Error setting session for user %v: %v\n", user, err)
		return
	}
	return
}

func checkSession(session string) (user string, err error) {
	c := sessionsDatabase.Get()
	user, err = redis.String(c.Do("GET", session))
	closeConnection(c)
	if err != nil {
		if strings.Contains(err.Error(), "nil returned") {
			loglevels.Infof("Invalid session %v.\n", session)
		} else {
			loglevels.Errorf("Error getting session for user: %v\n", err)
		}
		return
	}
	return
}

func sendDiscordRestRequest(endpoint, token string, result interface{}) (err error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", discordAPIURL+endpoint, nil)
	if err != nil {
		loglevels.Errorf("Error creating a new request: %v\n", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := client.Do(req)
	if err != nil {
		loglevels.Errorf("Error making discord rest call %v: %v\n", endpoint, err)
		return
	}
	defer func() {
		if err = res.Body.Close(); err != nil {
			loglevels.Errorf("Error closing response body: %v\n", err)
		}
	}()

	jsonParser := json.NewDecoder(res.Body)
	err = jsonParser.Decode(&result)
	if err != nil {
		loglevels.Errorf("Error parsing json to %T: %v\n", result, err)
		return
	}
	return
}

func setDiscordUser(token string) (user *discordgo.User, err error) {
	user, err = internalGetDiscordUser(token)
	if err != nil {
		return
	}

	c := sessionsDatabase.Get()
	_, err = c.Do("SET", user.ID, token, "EX", "3600")
	closeConnection(c)
	if err != nil {
		loglevels.Errorf("Error setting session for user %v: %v\n", user.ID, err)
		return
	}

	return
}

// nolint: deadcode, megacheck
func getDiscordUser(userID string) (user *discordgo.User, err error) {
	token, err := getToken(userID)
	if err != nil {
		return
	}

	return internalGetDiscordUser(token)
}

func getToken(userid string) (token string, err error) {
	c := sessionsDatabase.Get()
	token, err = redis.String(c.Do("GET", userid))
	closeConnection(c)
	if err != nil {
		loglevels.Warningf("Error getting token for user %v: %v\n", userid, err)
		return
	}
	return
}

func cacheRequest(endpoint, token, cache string, seconds int, result interface{}) (err error) {
	c := cacheDatabase.Get()
	defer closeConnection(c)

	resultstring, err := redis.String(c.Do("GET", cache+token))
	if err != nil {
		if !strings.Contains(err.Error(), "nil returned") {
			loglevels.Errorf("Error getting cache%v: %v\n", token, err)
		}
	} else {
		err = json.Unmarshal([]byte(resultstring), &result)
		if err != nil {
			loglevels.Errorf("Error unmarshaling cache: %v\n", err)
			return
		}
		return
	}

	err = sendDiscordRestRequest(endpoint, token, &result)
	if err != nil {
		loglevels.Warningf("Error getting %v: %v\n", endpoint, err)
		return
	}

	resultbytes, erro := json.Marshal(result)
	if erro != nil {
		loglevels.Warningf("Error marshaling cache: %v\n", erro)
		return
	}

	_, erro = c.Do("SET", cache+token, string(resultbytes), "EX", seconds)
	if erro != nil {
		loglevels.Warningf("Error setting cache: %v", erro)
		return
	}
	return
}

func internalGetDiscordUser(token string) (user *discordgo.User, err error) {
	err = cacheRequest("/users/@me", token, "usersCache", 300, &user)
	return
}

func getDiscordServers(userID string) (result []discordgo.UserGuild, err error) {
	token, err := getToken(userID)
	if err != nil {
		return
	}

	var guilds []discordgo.UserGuild
	err = cacheRequest("/users/@me/guilds", token, "guildsCache", 900, &guilds)
	if err != nil {
		loglevels.Errorf("Error getting guilds for user %v: %v\n", userID, err)
		return
	}

	c := guildsDatabase.Get()
	keys, err := redis.Values(c.Do("KEYS", "*"))
	closeConnection(c)
	if err != nil {
		loglevels.Errorf("Error getting keys * from redis: %v\n", err)
		return
	}

	// convert returned string to []string
	var guildIds []string
	err = redis.ScanSlice(keys, &guildIds)
	if err != nil {
		loglevels.Errorf("Error converting keys * to []string: %v\n", err)
		return
	}

	for _, gi := range guildIds {
		for _, g := range guilds {
			if gi == g.ID {
				result = append(result, g)
				break
			}
		}
	}

	return
}

func getOAuthToken(r *http.Request, w io.Writer) (token *oauth2.Token, err error) {
	token, err = oauthConfig.Exchange(context.Background(), r.FormValue("code"))
	// err will also be not nil when the user presses Cancel at the oauth request
	if err != nil {
		if strings.Contains(err.Error(), "invalid_grant") {
			loglevels.Warningf("Invalid Grant: %v\n", err)
			if _, erro := fmt.Fprint(w, "Authorization canceled. I did not receive your Discord ID."); erro != nil {
				loglevels.Errorf("Error writing to Responsewriter: %v and %v\n", err, erro)
			}
		} else {
			loglevels.Errorf("Error getting token: %v\n", err)
			if _, erro := fmt.Fprint(w, "Error getting discord authorization token."); erro != nil {
				loglevels.Errorf("Error writing to Responsewriter: %v and %v\n", err, erro)
			}
		}
		return
	}
	return
}
