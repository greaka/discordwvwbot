package main

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/greaka/discordwvwbot/loglevels"
)

func getTokenInfo(key string) (token tokenInfo, err error) {
	retries := 0
	err = gw2Request("/tokeninfo?access_token="+key, &token)
	for err != nil && retries < 3 {
		err = gw2Request("/tokeninfo?access_token="+key, &token)
		retries += 1
	}
	return
}

// used by migration
func getCheckedGw2Account(key string, userID struct {
	string
	bool
}) (account gw2Account, err error) {
	retries := 0
	var erro error
	account, erro = getCachedGw2Account(key)
	if erro != nil {
		invalid := func() bool {
			return strings.Contains(erro.Error(), "invalid key") || strings.Contains(erro.Error(), "Invalid access token")
		}

		for (userID.bool || invalid()) && retries < 5 {
			retries++
			<-time.After(delayBetweenUsers)
			account, erro = getCachedGw2Account(key)
			if erro == nil {
				return
			}
		}
		// if the key got revoked, delete it
		if invalid() {
			loglevels.Infof("Encountered invalid key at %v", userID.string)
			redisConn := usersDatabase.Get()
			_, erro = redisConn.Do("SREM", userID.string, key)
			closeConnection(redisConn)
			if erro != nil {
				loglevels.Errorf("Error deleting api key from redis: %v", erro)
			}
			return
		}
		loglevels.Warningf("Error getting account info: %v\n", erro)
		// unexpected error, don't revoke discord roles because of a server error
		err = erro
	}
	return
}

func getGw2Account(key string) (account gw2Account, err error) {
	err = gw2Request("/account?access_token="+key, &account)
	return
}

func getCachedGw2Account(key string) (account gw2Account, err error) {
	expire := int(delayBetweenFullUpdates.Seconds()) // delayBetweenFullUpdates will be set after the first run
	if expire == 0 {
		expire = 15 * 60 // 15 min
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
	var resx *http.Response
	for {
		// get data
		var res *http.Response
		<-bucket
		res, err = http.Get(gw2APIURL + endpoint)
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
			if res.StatusCode == 429 {
				loglevels.Warning("hit rate limit")
				for {
					select {
					case <-bucket:
					default:
						<-bucket
						break
					}
				}
				continue
			}
			err = errors.New(string(errorString))
			return
		}

		resx = res
		break
	}

	// parse
	jsonParser := json.NewDecoder(resx.Body)
	err = jsonParser.Decode(result)
	if err != nil {
		if resx.StatusCode >= 500 {
			loglevels.Warningf("Internal api server error: %v\n", resx.Status)
		} else {
			loglevels.Errorf("Error parsing json to %v data: %v\n", endpoint, err)
		}
		return
	}
	return
}
