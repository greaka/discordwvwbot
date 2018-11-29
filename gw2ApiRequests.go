package main

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/gomodule/redigo/redis"
	"github.com/greaka/discordwvwbot/loglevels"
)

func getTokenInfo(key string) (token tokenInfo, err error) {
	err = gw2Request("/tokeninfo?access_token="+key, &token)
	return
}

func getGw2Account(key string) (account gw2Account, err error) {
	err = gw2Request("/account?access_token="+key, &account)
	return
}

func getCachedGw2Account(key string) (account gw2Account, err error) {
	expire := int(delayBetweenFullUpdates.Seconds()) // delayBetweenFullUpdates will be set after the first run
	if expire == 0 {
		expire = 15 * 60 // 15 min * 60
	}
	err = cacheGw2Request("/account?access_token="+key, key, "gw2Account", expire, &account)
	return
}

func cacheGw2Request(endpoint, token, cache string, seconds int, result interface{}) (err error) {
	c := cacheDatabase.Get()
	defer closeConnection(c)
	resultstring, err := redis.String(c.Do("GET", cache+token))
	if err != nil {
		if !strings.Contains(err.Error(), "nil returned") {
			loglevels.Errorf("Error getting cache %v: %v\n", token, err)
		}
	} else {
		err = json.Unmarshal([]byte(resultstring), &result)
		if err != nil {
			loglevels.Errorf("Error unmarshaling cache: %v\n", err)
			return
		}
		return
	}

	err = gw2Request(endpoint, &result)
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

func getCurrentMatches() (matches []matchOverview, err error) {
	err = gw2Request("/wvw/matches/overview?ids=all", &matches)
	return
}

func getWorlds() (worlds []worldStruct, err error) {
	err = gw2Request("/worlds?ids=all", &worlds)
	return
}

func gw2Request(endpoint string, result interface{}) (err error) {
	// get data
	res, err := http.Get(gw2APIURL + endpoint)
	if err != nil {
		loglevels.Errorf("Error getting %v: %v\n", endpoint, err)
		return
	}
	defer func() {
		if erro := res.Body.Close(); erro != nil {
			loglevels.Errorf("Error closing response body: %v\n", erro)
		}
	}()

	if res.StatusCode >= 300 {
		errorString, _ := ioutil.ReadAll(res.Body) // nolint: gosec
		err = errors.New(string(errorString))
		return
	}

	// parse
	jsonParser := json.NewDecoder(res.Body)
	err = jsonParser.Decode(result)
	if err != nil {
		if res.StatusCode >= 500 {
			loglevels.Warningf("Internal api server error: %v\n", res.Status)
		} else {
			loglevels.Errorf("Error parsing json to %v data: %v\n", endpoint, err)
		}
		return
	}
	return
}
