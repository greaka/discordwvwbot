package main

import (
	"github.com/gomodule/redigo/redis"
	"github.com/greaka/discordwvwbot/loglevels"
	"time"
)

// nolint: ineffassign
func migrateRedis() (err error) {
	versionPool := newPool(dbTypeVersion)
	guildsPool := newPool(dbTypeGuilds)
	usersPool := newPool(dbTypeUsers)
	uniquePool := newPool(dbGw2UsersToDiscordUsers)

	vc := versionPool.Get()
	defer closeConnection(vc)
	version := 0
	versionExists, err := redis.Bool(vc.Do("EXISTS", "version"))
	if err != nil {
		loglevels.Errorf("Error checking for existing version in redis version db while trying to migrate: %v\n", err)
		loglevels.Warning("Exiting to prevent damage on the database. Check the error log!")
		return
	}
	if !versionExists {
		guildsExists, err := redis.Bool(vc.Do("EXISTS", "guilds")) // nolint: vetshadow
		if err != nil {
			loglevels.Errorf("Error checking for existing guilds in redis version db while trying to migrate: %v\n", err)
			return err
		}
		if guildsExists {
			version = 1
		}
	} else {
		version, err = redis.Int(vc.Do("GET", "version"))
		if err != nil {
			return
		}
	}
	closeConnection(vc)

	if version == 1 {
		version, err = migrateRedisFrom1To2(usersPool, guildsPool, versionPool)
		if err != nil {
			return
		}
	}
	if version == 2 {
		version, err = migrateRedisFrom2To3(guildsPool)
		if err != nil {
			return
		}
	}
	if version == 3 {
		version, err = migrateRedisFrom3To4(usersPool, uniquePool)
		if err != nil {
			return
		}
	}
	if version == 4 {
		version, err = migrateRedisFrom4To5(guildsPool)
		if err != nil {
			return
		}
	}

	vc = versionPool.Get()

	_, err = vc.Do("SET", "version", version)
	if err != nil {
		loglevels.Errorf("Error setting version while migrating: %v\n", err)
	}

	return
}

func migrateRedisFrom1To2(up, gp, vp *redis.Pool) (version int, err error) {
	version = 1
	gc := gp.Get()
	defer closeConnection(gc)
	vc := vp.Get()
	defer closeConnection(vc)
	uc := up.Get()
	defer closeConnection(uc)

	err = dumpRestoreAndDEL(vp, gp, "guilds")
	if err != nil {
		return
	}

	// get every key
	/* blocks redis database with O(n)
	 * since this bot will never have millions of updates per second, this is fine
	 */
	keys, err := redis.Values(vc.Do("KEYS", "*"))
	if err != nil {
		loglevels.Errorf("Error getting keys * while trying to migrate: %v\n", err)
		return
	}

	// convert returned string to userids []string
	var userIds []string
	err = redis.ScanSlice(keys, &userIds)
	if err != nil {
		loglevels.Errorf("Error converting keys * to []string while trying to migrate: %v\n", err)
		return
	}

	for len(userIds) > 0 {
		if userIds[0] != "guilds" && userIds[0] != "version" {
			// dump key, restore it on users db and delete it on version db
			err = dumpRestoreAndDEL(vp, up, userIds[0])
			if err != nil {
				return
			}
		}
		userIds = remove(userIds, 0)
	}

	version = 2

	return
}

func migrateRedisFrom2To3(gp *redis.Pool) (version int, err error) {
	version = 2
	gc := gp.Get()
	defer closeConnection(gc)

	values, err := redis.Values(gc.Do("SMEMBERS", "guilds"))
	if err != nil {
		loglevels.Errorf("Error getting guilds while migrating from 2 to 3: %v\n", err)
		return
	}

	var guilds []string
	err = redis.ScanSlice(values, &guilds)
	if err != nil {
		loglevels.Errorf("Error slicing guilds while migrating from 2 to 3: %v\n", err)
		return
	}

	for _, guild := range guilds {
		if err = saveNewGuild(gc, guild); err != nil {
			return
		}
	}

	_, err = gc.Do("DEL", "guilds")
	if err != nil {
		loglevels.Errorf("Error deleting guilds while trying to migrate from 2 to 3: %v\n", err)
		return
	}

	version = 3
	return
}

func migrateRedisFrom3To4(usp, unp *redis.Pool) (version int, err error) {
	version = 3
	userc := usp.Get()
	defer closeConnection(userc)
	uniquec := unp.Get()
	defer closeConnection(uniquec)

	wait := time.Tick(delayBetweenUsers)

	processValue := func(user string, keys []string) {
		<-wait
		i := -1
		// for every api key ...
		for i < len(keys)-1 {
			i++
			key := keys[i]
			acc, erro := getCheckedGw2Account(key, struct {
				string
				bool
			}{string: user, bool: true})
			if erro != nil {
				continue
			}

			userID, erro := checkUnique(acc.ID, user, false)
			if erro != nil && erro.Error() == AlreadyTaken {
				// remove key
				redisConn := usersDatabase.Get()
				_, erro = redisConn.Do("SREM", user, key)
				closeConnection(redisConn)
				if erro != nil {
					loglevels.Errorf("Error deleting api key from redis: %v", erro)
				}
				// notify user
				ch, erro := dg.UserChannelCreate(user)
				if erro != nil {
					loglevels.Errorf("Failed to create dm channel with user %v: %v", user, erro)
					continue
				}
				_, erro = dg.ChannelMessageSend(ch.ID, `
From now on, this bot only allows one discord user to verify with the same gw2 account.
You share the account `+acc.Name+` with <@`+userID+`> and the api key was removed from your discord account.
If you wish to verify this discord account, then create a new api key, name it `+"`wvwbot "+user+"` and add the new key to the bot.")
			}
		}
	}
	smembersallkeys(userc, processValue)
	version = 4
	return
}

func migrateRedisFrom4To5(gp *redis.Pool) (version int, err error) {
	version = 4
	gc := gp.Get()
	defer closeConnection(gc)

	processValue := func(guild string) {
		var settings *guildOptions
		settings, err = getGuildSettings(guild)
		if err != nil {
			loglevels.Errorf("Error getting guild while trying to migrate from 4 to 5: %v\n", err)
			return
		}
		settings.MinimumRank = 0
		err = saveGuildSettings(guild, settings)
		if err != nil {
			loglevels.Errorf("Error saving guild while trying to migrate from 4 to 5: %v\n", err)
			return
		}
	}

	iterateDatabase(gc, processValue)

	if err != nil {
		return
	}

	version = 5
	return
}

func dumpRestoreAndDEL(source, target *redis.Pool, key string) (err error) {
	sc := source.Get()
	defer closeConnection(sc)
	tc := target.Get()
	defer closeConnection(tc)

	dump, err := redis.String(sc.Do("DUMP", key))
	if err != nil {
		loglevels.Errorf("Error getting %v dump from db while trying to migrate: %v\n", key, err)
		return
	}
	_, err = redis.String(tc.Do("RESTORE", key, 0, dump))
	if err != nil {
		loglevels.Errorf("Error restoring %v dump to db while trying to migrate: %v\n", key, err)
		return
	}

	_, err = sc.Do("DEL", key)
	if err != nil {
		loglevels.Errorf("Error deleting %v from db while trying to migrate: %v\n", key, err)
		return
	}

	return
}
