package main

import (
	b64 "encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/bwmarrin/discordgo"
	"github.com/greaka/discordwvwbot/loglevels"
	"io"
	"net/http"
	"strconv"
)

// redirectToTLS is the handler function for http calls to get redirected to https
func redirectToTLS(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "https://"+r.Host+r.RequestURI, http.StatusMovedPermanently)
}

// handleRootRequest serves the mainpage
func handleRootRequest(w http.ResponseWriter, r *http.Request) {
	addHeaders(w, r)
	if _, err := fmt.Fprint(w, mainpage); err != nil {
		loglevels.Errorf("Error handling root request: %v\n", err)
	}
}

// nolint: gocyclo
func handleDashboard(w http.ResponseWriter, r *http.Request) {
	addHeaders(w, r)
	state := r.FormValue("state")

	if state == "" {
		http.Redirect(w, r, "/login?key=dashboard", http.StatusTemporaryRedirect)
		return
	}

	stateString, err := b64.URLEncoding.DecodeString(state)
	if err != nil {
		loglevels.Errorf("Error decoding base64 %v: %v\n", state, err)
		writeToResponse(w, "Internal error, please try again or contact me.")
		return
	}

	var oauthReason oauthState
	err = json.Unmarshal(stateString, &oauthReason)
	if err != nil {
		loglevels.Errorf("Error deserializing json %v: %v\n", stateString, err)
		writeToResponse(w, "Internal error, please try again or contact me.")
		return
	}

	userid, err := checkSession(oauthReason.Data)
	if err != nil {
		loglevels.Warningf("Invalid session: %v\n", err)
		writeToResponse(w, "Session expired.")
		return
	}

	guild := r.FormValue("guild")
	guilds, err := getDiscordServers(userid) // nolint: vetshadow
	if err != nil {
		loglevels.Errorf("Error getting discord servers for user %v: %v\n", userid, err)
		writeToResponse(w, "The Discord API is currently down. Check back in a few minutes.")
		return
	}
	if guild == "" {
		if len(guilds) < 1 {
			writeToResponse(w, "You share no discord server with this bot.")
			return
		}
		i := 1
		guild = guilds[0].ID
		for !checkUserIsMember(guild, guilds) {
			guild = guilds[i].ID
			i = i + 1;
		}
	} else {
		if !checkUserIsMember(guild, guilds) {
			loglevels.Warningf("User %v tried to access dashboard setting from guild %v while missing the needed permissions.\n", userid, guild)
			writeToResponse(w, "You are missing permissions to manage roles on this server.")
			return
		}
	}

	dashboard, err := getDashboardTemplate(guild, userid, state)
	if err != nil {
		loglevels.Errorf("Error getting dashboard template for user %v and guild %v: %v\n", userid, guild, err)
		writeToResponse(w, "Internal error, please try again or contact me.")
		return
	}

	err = dbTemplate.Execute(w, dashboard)
	if err != nil {
		loglevels.Errorf("Error executing dashboard template for user %v and guild %v: %v\n", userid, guild, err)
		writeToResponse(w, "Internal error, please try again or contact me.")
		return
	}
}

// nolint: gocyclo
func handleSubmitDashboard(w http.ResponseWriter, r *http.Request) {
	addHeaders(w, r)

	state := r.FormValue("state")
	stateString, err := b64.URLEncoding.DecodeString(state)
	if err != nil {
		loglevels.Errorf("Error decoding base64 %v: %v\n", state, err)
		writeToResponse(w, "Internal error, please try again or contact me.")
		return
	}

	var oauthReason oauthState
	err = json.Unmarshal(stateString, &oauthReason)
	if err != nil {
		loglevels.Errorf("Error deserializing json %v: %v\n", stateString, err)
		writeToResponse(w, "Internal error, please try again or contact me.")
		return
	}

	user, err := checkSession(oauthReason.Data)
	if err != nil {
		loglevels.Warningf("Invalid session: %v\n", err)
		writeToResponse(w, "Session expired.")
		return
	}
	guild := r.FormValue("guild")
	loglevels.Infof("saving dashboard from user %v for guild %v...\n", user, guild)

	servers, err := getDiscordServers(user)
	if err != nil {
		writeToResponse(w, "Something went wrong. Try again later or contact me.")
		return
	}

	isMember := checkUserIsMember(guild, servers)
	if isMember {
		err = processSubmitData(r)
		if err != nil {
			writeToResponse(w, "%v", err)
		} else {
			writeToResponse(w, "Success")
			loglevels.Infof("dashboard saved by user %v for guild %v\n", user, guild)
		}
		return
	}

	writeToResponse(w, "You are missing permissions to manage roles. Your settings were not saved.")
}

func checkUserIsMember(id string, servers []discordgo.UserGuild) (isMember bool) {
	for _, server := range servers {
		if server.ID == id {
			if server.Permissions&discordgo.PermissionManageRoles == discordgo.PermissionManageRoles {
				return true
			}
		}
	}
	return false
}

// nolint: gocyclo
func processSubmitData(r *http.Request) (err error) {
	options := &guildOptions{
		RenameUsers:  false,
		CreateRoles:  false,
		AllowLinked:  false,
		VerifyOnly:   false,
		DeleteLinked: false,
	}
	mod, err := strconv.Atoi(r.FormValue("mode"))
	if err != nil {
		loglevels.Errorf("Error converting mode from dashboard submit: %v\n", err)
		return
	}
	options.Mode = mode(mod)

	rank, err := strconv.Atoi(r.FormValue("min-rank"))
	if err != nil {
		loglevels.Warningf("Error converting rank from dashboard submit: %v\n", err)
		return
	}
	options.MinimumRank = rank

	if r.FormValue("rename-users") == "on" {
		options.RenameUsers = true
	}

	if r.FormValue("create-all") == "on" {
		options.CreateRoles = true
	}

	if r.FormValue("allow-linked") == "on" {
		options.AllowLinked = true
	}

	if r.FormValue("squash") == "on" {
		options.VerifyOnly = true
	}

	if r.FormValue("delete-linked") == "on" {
		options.DeleteLinked = true
	}

	options.Gw2AccountKey = r.FormValue("account")
	if options.Mode == userBased {
		if options.Gw2AccountKey == "" {
			err = errors.New("you have to choose an account for user based mode to work")
			return
		}
	}

	serverString := r.FormValue("server")
	if serverString != "" {
		var serv int
		serv, err = strconv.Atoi(serverString)
		if err != nil {
			loglevels.Errorf("Error converting server id %v from dashboard submit: %v\n", serverString, err)
			return
		}

		options.Gw2ServerID = serv
	} else {
		if options.Mode == oneServer {
			err = errors.New("you have to choose a server for server based mode to work")
			return
		}
	}

	// r.FormValue("guild") is not empty because of the permissions check before
	err = saveGuildSettings(r.FormValue("guild"), options)
	if err != nil {
		err = errors.New("unexpected error while saving your settings")
	}
	return
}

// handleAuthCallback is listening to returning oauth requests to discord
// nolint: gocyclo
func handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	addHeaders(w, r)
	loglevels.Info("New auth callback")
	state := r.FormValue("state")
	// request oauth access with the issue data sent by discord
	loglevels.Info("get oauth token...")
	token, err := getOAuthToken(r, w)
	if err != nil {
		loglevels.Info("error in oauth")
		return
	}
	loglevels.Info("got oauth token")

	// get discord id
	loglevels.Info("get discord user id")
	user, err := setDiscordUser(token.AccessToken)
	if err != nil {
		loglevels.Info("error getting discord user id")
		writeToResponse(w, "Internal Error getting your discord ID. If this error persists, then please contact me.")
		return
	}
	loglevels.Infof("got dicsord user id %v", user.ID)

	loglevels.Info("unmarshaling state string...")
	var oauthReason oauthState
	err = json.Unmarshal([]byte(state), &oauthReason)
	if err != nil {
		loglevels.Info("error unmarshaling state string")
		loglevels.Errorf("Error deserializing json %v: %v\n", state, err)
		writeToResponse(w, "Internal error, please try again or contact me.")
		return
	}
	loglevels.Infof("unmarshaled state string. request reason: %v", oauthReason.Reason)
	loglevels.Info("Reasons: 1=addUser	2=syncUser 3=deleteKeys 4=useDashboard")

	switch oauthReason.Reason {

	// delete everything we know about this user
	case deleteKeys:
		loglevels.Infof("Delete all data for user %v", user.ID)
		redisConn := usersDatabase.Get()
		defer closeConnection(redisConn)
		_, err = redisConn.Do("DEL", user.ID)
		if err != nil {
			loglevels.Errorf("Error deleting key from redis: %v\n", err)
			writeToResponse(w, "Internal error, please try again or contact me.")
			return
		}
		_, err = redisConn.Do("SADD", user.ID, "A")
		if err != nil {
			loglevels.Errorf("Error adding temporary key to redis: %v\n", err)
			writeToResponse(w, "Internal error, please try again or contact me.")
			return
		}

	// sync the user on all discord servers
	case syncUser:
		loglevels.Infof("Sync user %v", user.ID)
		updateUserChannel <- struct {
			string
			bool
		}{string: user.ID, bool: true}

	// save api key and update user
	case addUser:
		err = checkKey(oauthReason.Data, user.ID)
		if err != nil {
			writeToResponse(w, fmt.Sprintf("%v", err))
			return
		}
		err = addUserKey(user.ID, oauthReason.Data)
		if err != nil {
			writeToResponse(w, fmt.Sprintf("%v", err))
			return
		}
	case useDashboard:
		loglevels.Infof("Dashboard for user %v requested", user.ID)
		oauthReason.Data, err = newSession(user.ID)
		if err != nil {
			writeToResponse(w, "Something went seriously wrong. If this happens again, please contact me.")
			return
		}
		stateBytes, err := json.Marshal(oauthReason) // nolint: vetshadow
		if err != nil {
			loglevels.Errorf("Error stringifying state: %v", oauthReason)
			writeToResponse(w, "Something went seriously wrong. If this happens again, please contact me.")
			return
		}
		http.Redirect(w, r, "https://"+r.Host+"/dashboard?state="+b64.URLEncoding.EncodeToString(stateBytes), http.StatusTemporaryRedirect)
	default:
		loglevels.Errorf("malformed state: %v", oauthReason)
	}

	loglevels.Infof("write 'Success' for user %v", user.ID)
	if _, err = fmt.Fprint(w, "Success"); err != nil {
		loglevels.Errorf("Error writing to Responsewriter: %v\n", err)
		writeToResponse(w, "Something went seriously wrong. If this happens again, please contact me.")
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// handleAuthRequest forges the oauth request to discord and packs data for the callback
// nolint: gocyclo
func handleAuthRequest(w http.ResponseWriter, r *http.Request) {
	addHeaders(w, r)
	key := r.FormValue("key")

	state := oauthState{}

	// filter if request contains special keywords
	switch key {
	case "deletemydata":
		state.Reason = deleteKeys
	case "syncnow":
		state.Reason = syncUser
	case "dashboard":
		state.Reason = useDashboard
	default:
		state.Reason = addUser
		state.Data = key
	}

	stateString, err := json.Marshal(state)
	if err != nil {
		loglevels.Errorf("Error stringifying state: %v", state)
		writeToResponse(w, "Something went seriously wrong. If this happens again, please contact me.")
		return
	}

	// redirect to discord login
	// we can use the key as state here because we are not vulnerable to csrf (change my mind)
	http.Redirect(w, r, oauthConfig.AuthCodeURL(string(stateString)), http.StatusTemporaryRedirect)
}

// handleInvite responds with a discord URL to invite this bot to a discord server
func handleInvite(w http.ResponseWriter, r *http.Request) {
	addHeaders(w, r)
	http.Redirect(w, r, "https://discordapp.com/oauth2/authorize?client_id="+config.DiscordClientID+"&scope=bot&permissions=402680832", http.StatusPermanentRedirect)
}

// addHeaders adds the standard headers to the http.ResponseWriter
func addHeaders(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
}

func writeToResponse(w io.Writer, message string, a ...interface{}) {
	if _, erro := fmt.Fprintf(w, message, a...); erro != nil {
		loglevels.Errorf("Error writing to Responsewriter: %v\n", erro)
	}
}
