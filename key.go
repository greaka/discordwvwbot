package main

import (
	"errors"
	"strings"
)

func checkKey(key string) (err error) {
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
		err = errors.New("this api key is not valid. Make sure your key name contains 'wvwbot'. "+text+"\nPlease create a new key with a valid name")
		return
	}

	if indexOfString("progression", token.Permissions) == -1 {
		err = errors.New("this api key is not valid. Please give it the permission 'progression'")
		return
	}
}
