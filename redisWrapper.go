package main

import (
	"encoding/json"

	"github.com/bwmarrin/discordgo"
	"github.com/gomodule/redigo/redis"
	"github.com/greaka/discordwvwbot/loglevels"
)

func getAPIKeys(userID string) (keys []string, err error) {
	redisConn := usersDatabase.Get()
	// get all api keys of the user
	apikeys, err := redis.Values(redisConn.Do("SMEMBERS", userID))
	closeConnection(redisConn)
	if err != nil {
		loglevels.Errorf("Error getting api keys from redis: %v\n", err)
		return
	}

	err = redis.ScanSlice(apikeys, &keys)
	if err != nil {
		loglevels.Errorf("Error converting api keys to []string: %v\n", err)
		return
	}
	return
}

func iterateDatabase(redisConn redis.Conn, processValue func(string)) (valueCount int) {
	cursor := 0
	valueCount = 0

	for ok := true; ok; ok = cursor != 0 {
		scan, err := redis.Values(redisConn.Do("SCAN", cursor))
		if err != nil {
			loglevels.Errorf("Error getting scan %v from redis: %v\n", cursor, err)
			return
		}

		// convert returned string to []string of 1) new cursor and 2) values
		var values []string
		_, err = redis.Scan(scan, &cursor, &values)
		if err != nil {
			loglevels.Errorf("Error converting scan %v: %v\n", cursor, err)
			return
		}

		valueCount += len(values)

		// fire update while list not empty and not databse key "guilds"
		for len(values) > 0 {
			processValue(values[0])
			values = remove(values, 0)
		}
	}
	return
}

// keep in mind that this is called from migration, so look if you break migration if you change something here
func saveNewGuild(gc redis.Conn, guild string) (err error) {
	options := guildOptions{
		Gw2ServerID:   0,
		Gw2AccountKey: "",
		Mode:          allServers,
		RenameUsers:   false,
		CreateRoles:   false,
		AllowLinked:   false,
		VerifyOnly:    false,
		DeleteLinked:  false,
	}

	var stringifiedOptions []byte
	stringifiedOptions, err = json.Marshal(options)
	if err != nil {
		loglevels.Errorf("Error marshaling default options for guild %v: %v\n", guild, err)
		return
	}

	_, err = gc.Do("SET", guild, stringifiedOptions)
	if err != nil {
		loglevels.Errorf("Error saving default options for guild %v: %v\n", guild, err)
		return
	}
	return
}

func saveGuildSettings(guildID string, s *guildOptions) (err error) {
	settingsString, err := json.Marshal(&s)
	if err != nil {
		loglevels.Errorf("Error marshaling guild options for guild %v: %v\n", guildID, err)
		return
	}

	c := guildsDatabase.Get()
	_, err = redis.String(c.Do("SET", guildID, settingsString))
	if err != nil {
		loglevels.Errorf("Error setting options for guild %v: %v\n", guildID, err)
		return
	}
	return
}

func getGuildSettings(guildID string) (s *guildOptions, err error) {
	c := guildsDatabase.Get()
	settingsString, err := redis.String(c.Do("GET", guildID))
	if err != nil {
		loglevels.Errorf("Error getting options for guild %v: %v\n", guildID, err)
		return
	}

	err = json.Unmarshal([]byte(settingsString), &s)
	if err != nil {
		loglevels.Errorf("Error converting guild options for guild %v: %v\n", guildID, err)
		return
	}
	return
}

func getGuildRoles(guildID string, guildRoles []*discordgo.Role) (roleStructs []guildRole, err error) {
	redisConn := guildRolesDatabase.Get()
	// get all managed guild roles
	roleString, err := redis.Values(redisConn.Do("SMEMBERS", guildID))
	closeConnection(redisConn)
	if err != nil {
		loglevels.Errorf("Error getting api keys from redis: %v\n", err)
		return
	}

	var roles []string
	err = redis.ScanSlice(roleString, &roles)
	if err != nil {
		loglevels.Errorf("Error converting api keys to []string: %v\n", err)
		return
	}

	for _, role := range roles {
		var roleStruct guildRole
		err = json.Unmarshal([]byte(role), &roleStruct)
		if err != nil {
			loglevels.Errorf("Error converting guild roles for guild %v: %v\n", guildID, err)
			return
		}

		found := false
		for _, guildRole := range guildRoles {
			if guildRole.ID == roleStruct.ID {
				found = true
				break
			}
		}

		if found {
			roleStructs = append(roleStructs, roleStruct)
		} else {
			removeGuildRole(guildID, roleStruct) // nolint: gosec, errcheck
		}
	}

	return
}

func addGuildRole(guildID string, role guildRole) (err error) {
	roleString, err := json.Marshal(role)
	if err != nil {
		loglevels.Errorf("Error converting guild roles for guild %v: %v\n", guildID, err)
		return
	}

	redisConn := guildRolesDatabase.Get()
	// get all managed guild roles
	_, err = redisConn.Do("SADD", guildID, roleString)
	closeConnection(redisConn)
	if err != nil {
		loglevels.Errorf("Error getting guild role from redis: %v\n", err)
		return
	}

	return
}

func removeGuildRole(guildID string, role guildRole) (err error) {
	roleString, err := json.Marshal(role)
	if err != nil {
		loglevels.Errorf("Error converting guild roles for guild %v: %v\n", guildID, err)
		return
	}

	redisConn := guildRolesDatabase.Get()
	// get all managed guild roles
	_, err = redisConn.Do("SREM", guildID, roleString)
	closeConnection(redisConn)
	if err != nil {
		loglevels.Errorf("Error removing guild role from redis: %v\n", err)
		return
	}

	return
}
