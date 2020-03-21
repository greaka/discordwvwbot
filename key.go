package main

import (
	"errors"
	"github.com/greaka/discordwvwbot/loglevels"
	"strings"
)

func checkKey(key, user string) (err error) {
	// check if api key is valid
	token, err := getTokenInfo(key)
	if err != nil {
		err = errors.New("error communicating with the gw2api, please try again or wait until the api is working again")
		return
	}

	// check if api key contains wvwbot
	nameInLower := strings.ToLower(token.Name)
	if !strings.Contains(nameInLower, "wvw") || !strings.Contains(nameInLower, "bot") {
		text := "This api key is named " + token.Name
		if token.Name == "" {
			text = "This api key does not have a name."
		}
		err = errors.New("this api key is not valid. Make sure your key name contains 'wvwbot'. " + text + "\nPlease create a new key with a valid name")
		return
	}

	if indexOfString("progression", token.Permissions) == -1 {
		err = errors.New("this api key is not valid. Please give it the permission 'progression'")
		return
	}

	acc, err := getGw2Account(key)
	if err != nil {
		err = errors.New("error communicating with the gw2api, please try again or wait until the api is working again")
		return
	}

	force := strings.Contains(nameInLower, user)
	userID, err := checkUnique(acc.ID, user, force)
	if err != nil {
		if err.Error() == AlreadyTaken {
			err = errors.New(`Key not saved.
The account ` + acc.Name + ` was already registered by <@` + userID + `>.
If you wish to verify this discord account instead, then create a new api key, name it ` + "`wvwbot " + user + "` and add the new key to the bot.")
		} else {
			err = errors.New("internal error, please try again later or contact me")
		}
	} else if user != userID && userID != "" {
		// remove key
		redisConn := usersDatabase.Get()
		_, erro := redisConn.Do("SREM", userID, key)
		closeConnection(redisConn)
		if erro != nil {
			loglevels.Errorf("Error deleting api key from redis: %v", erro)
		}
	}
	return
}
