package main

import (
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/greaka/discordwvwbot/loglevels"
)

var (
	// userDatabase holds connections to the redis server
	usersDatabase *redis.Pool
	// guildsDatabase holds connections to the redis server
	guildsDatabase *redis.Pool
	// sessionsDatabase holds connections to the redis server
	sessionsDatabase *redis.Pool
	// cacheDatabase holds connections to the redis server
	cacheDatabase *redis.Pool
	// guildRolesDatabase holds connections to the redis server
	guildRolesDatabase *redis.Pool
)

type redisDatabase int

// only add new namespaces below to not mess up existing databases
const (
	dbTypeVersion redisDatabase = iota
	dbTypeUsers
	dbTypeGuilds
	dbTypeSessions
	dbTypeCache
	dbTypeGuildRoles
)

func initializeRedisPools() {
	usersDatabase = newPool(dbTypeUsers)
	guildsDatabase = newPool(dbTypeGuilds)
	sessionsDatabase = newPool(dbTypeSessions)
	cacheDatabase = newPool(dbTypeCache)
	guildRolesDatabase = newPool(dbTypeGuildRoles)
}

// newPool initializes a new pool
func newPool(db redisDatabase) *redis.Pool {
	return &redis.Pool{
		Dial: func() (red redis.Conn, err error) {
			red, err = redis.DialURL(config.RedisConnectionString)
			if err != nil {
				loglevels.Errorf("Error connecting to redis server: %v\n", err)
			}
			if _, err := red.Do("SELECT", db); err != nil {
				red.Close() // nolint: errcheck, gosec
				loglevels.Errorf("Error connecting to redis database %v: %v\n", db, err)
			}
			return
		},
		MaxIdle:      3,
		IdleTimeout:  10 * time.Minute,
		TestOnBorrow: testConnection,
	}
}

// testConnection tests if the connection is still usable
func testConnection(c redis.Conn, t time.Time) error {
	if time.Since(t) < time.Minute {
		return nil
	}
	_, err := c.Do("PING")
	return err
}

// closeConnection closes the connection if not already done
func closeConnection(c redis.Conn) {
	erro := testConnection(c, time.Now().Add(-time.Hour)) // I know... you are allowed to change it
	if erro != nil {
		return
	}
	if err := c.Close(); err != nil {
		loglevels.Errorf("Error closing redis connection: %v\n", err)
	}
}
