package main

import (
	"github.com/gomodule/redigo/redis"
	"github.com/greaka/discordwvwbot/loglevels"
)

// nolint: ineffassign
func migrateRedis() (err error) {
	versionPool := newPool(dbTypeVersion)
	guildsPool := newPool(dbTypeGuilds)
	usersPool := newPool(dbTypeUsers)

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
		version, _ = redis.Int(vc.Do("GET", "version"))
	}
	closeConnection(vc)

	if version == 1 {
		version, err = migrateRedisFrom1To2(usersPool, guildsPool, versionPool)
	}

	vc = versionPool.Get()

	_, err = vc.Do("SET", "version "+string(version))

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
	_, err = redis.String(tc.Do("RESTORE", key+" 0 "+dump))
	if err != nil {
		loglevels.Errorf("Error restoring %v dump to db while trying to migrate: %v COMMAND: %v\n", key, err, key+" 0 "+dump)
		return
	}

	_, err = sc.Do("DEL", key)
	if err != nil {
		loglevels.Errorf("Error deleting %v from db while trying to migrate: %v\n", key, err)
		return
	}

	return
}
