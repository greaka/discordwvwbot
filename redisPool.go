package main

import (
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/greaka/discordwvwbot/loglevels"
)

var (
	// pool holds the connections to the redis server
	pool *redis.Pool
)

func newPool() *redis.Pool {
	return &redis.Pool{
		Dial:         newConnection,
		MaxIdle:      3,
		IdleTimeout:  10 * time.Minute,
		TestOnBorrow: testConnection,
	}
}

func newConnection() (red redis.Conn, err error) {
	red, err = redis.DialURL(config.RedisConnectionString)
	if err != nil {
		loglevels.Errorf("Error connecting to redis server: %v\n", err)
	}
	return
}

func testConnection(c redis.Conn, t time.Time) error {
	if time.Since(t) < time.Minute {
		return nil
	}
	_, err := c.Do("PING")
	return err
}

func closeConnection(c redis.Conn) {
	erro := testConnection(c, time.Now().Add(-time.Hour))
	if erro != nil {
		return
	}
	if err := c.Close(); err != nil {
		loglevels.Errorf("Error closing redis connection: %v\n", err)
	}
}