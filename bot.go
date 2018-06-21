package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gomodule/redigo/redis"

	"github.com/bwmarrin/discordgo"
)

var (
	updateUserChannel       chan string
	dg                      *discordgo.Session
	currentWorlds           map[int]string
	delayBetweenFullUpdates time.Duration
)

const (
	/* 	gw2 api rate limit: 600 requests per minute
	api keys to check per user (average): 2
	600 / 2 = 300 users per minute
	60/300 = 0.2s per user
	*/
	delayBetweenUsers time.Duration = 200 * time.Millisecond
)

type gw2Account struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	World   int      `json:"world"`
	Guilds  []string `json:"guilds"`
	Access  []string `json:"access"`
	Created string   `json:"created"`

	FractalLevel int `json:"fractal_level"`
	DailyAP      int `json:"daily_ap"`
	MonthlyAP    int `json:"monthly_ap"`
	WvWRank      int `json:"wvw_rank"`
}

type worldStruct struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func startBot() {
	updateUserChannel = make(chan string)

	var err error
	dg, err = discordgo.New("Bot " + config.BotToken)
	if err != nil {
		log.Printf("Error connecting to discord: %v\n", err)
		return
	}

	dg.AddHandler(guildCreate)
	dg.AddHandler(guildDelete)
	dg.AddHandler(guildMemberAdd)

	err = dg.Open()
	if err != nil {
		log.Printf("Error opening discord connection: %v\n", err)
		return
	}
	defer func() {
		if err = dg.Close(); err != nil {
			log.Printf("Error closing discord connection: %v\n", err)
		}
	}()

	log.Println("Bot is now running")

	status := discordgo.UpdateStatusData{
		Status:    string(discordgo.StatusOnline),
		AFK:       false,
		IdleSince: nil,
		Game: &discordgo.Game{
			Name: config.HostURL,
			Type: 2,
		},
	}
	statusUpdateError := dg.UpdateStatusComplex(status)
	if statusUpdateError != nil {
		log.Printf("Error updating discord status: %v\n", statusUpdateError)
	}

	go updater()

	for {
		updateUser(<-updateUserChannel)
	}
}

func updater() {
	updateCurrentWorlds()
	updateAllUsers() // has to run here to set delayBetweenFullUpdates
	for {
		fullUpdateDelay := delayBetweenFullUpdates
		if delayBetweenFullUpdates < 15*time.Minute {
			fullUpdateDelay = 15 * time.Minute
		}
		queueUserChannel := time.After(fullUpdateDelay)
		worldsChannel := resetWorldUpdateTimer()
		select {
		case <-queueUserChannel:
			updateAllUsers()
		case <-worldsChannel:
			updateCurrentWorlds()
			updateAllUsers()
		}
	}
}

func resetWorldUpdateTimer() (worldsChannel <-chan time.Time) {
	daysUntilNextFriday := int(time.Friday - time.Now().Weekday())
	if daysUntilNextFriday < 0 {
		daysUntilNextFriday += 7
	}
	daysUntilNextSaturday := int(time.Saturday - time.Now().Weekday())
	if daysUntilNextSaturday < 0 {
		daysUntilNextSaturday += 7
	}
	nextEUReset := time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day()+daysUntilNextFriday, 18, 15, 0, 0, time.UTC)
	nextUSReset := time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day()+daysUntilNextSaturday, 2, 15, 0, 0, time.UTC)
	var nextReset time.Time
	if nextEUReset.Before(nextUSReset) {
		if nextEUReset.Before(time.Now()) {
			nextReset = nextUSReset
		} else {
			nextReset = nextEUReset
		}
	} else {
		if nextUSReset.Before(time.Now()) {
			nextReset = nextEUReset
		} else {
			nextReset = nextUSReset
		}
	}
	worldsChannel = time.After(time.Until(nextReset))
	return
}

func updateAllUsers() {
	log.Println("Updating all users...")
	keys, err := redis.Values(redisConn.Do("KEYS", "*"))
	if err != nil {
		log.Printf("Error getting keys * from redis: %v\n", err)
		return
	}

	var userIds []string
	err = redis.ScanSlice(keys, &userIds)
	if err != nil {
		log.Printf("Error converting keys * to []string: %v\n", err)
		return
	}

	delayBetweenFullUpdates = delayBetweenUsers * time.Duration((len(userIds) + 50)) // updatetime per user * (number of users + 50 margin)
	iterateThroughUsers := time.Tick(delayBetweenUsers)

	for len(userIds) > 0 {
		if userIds[0] != "guilds" {
			<-iterateThroughUsers
			updateUserChannel <- userIds[0]
		}
		userIds = remove(userIds, 0)
	}
	log.Println("Finished updating all users")
}

func guildMemberAdd(s *discordgo.Session, m *discordgo.GuildMemberAdd) {
	updateUserInGuild(m.User.ID, m.GuildID)
}

func guildCreate(s *discordgo.Session, m *discordgo.GuildCreate) {
	alreadyIn, err := redis.Int(redisConn.Do("SISMEMBER", "guilds", m.ID))
	if err != nil {
		log.Printf("Error checking if guild %v is in redis guilds: %v\n", m.ID, err)
		return
	}
	if alreadyIn == 0 {
		_, err = redisConn.Do("SADD", "guilds", m.ID)
		if err != nil {
			log.Printf("Error adding guild %v to redis guilds: %v\n", m.ID, err)
			return
		}
		updateAllUsers()
	}
}
func guildDelete(s *discordgo.Session, m *discordgo.GuildDelete) {
	_, err := redisConn.Do("SREM", "guilds", m.ID)
	if err != nil {
		log.Printf("Error removing guild %v from redis guilds: %v\n", m.ID, err)
	}
}

func updateCurrentWorlds() {
	log.Println("Updating worlds...")
	res, erro := http.Get("https://api.guildwars2.com/v2/worlds?ids=all")
	if erro != nil {
		log.Printf("Error getting worlds info: %v\n", erro)
		return
	}
	defer func() {
		if err := res.Body.Close(); err != nil {
			log.Printf("Error closing response body: %v\n", err)
		}
	}()
	jsonParser := json.NewDecoder(res.Body)
	var worlds []worldStruct
	erro = jsonParser.Decode(&worlds)
	if erro != nil {
		log.Printf("Error parsing json to world data: %v\n", erro)
		return
	}

	currentWorlds = make(map[int]string)
	for _, world := range worlds {
		currentWorlds[world.ID] = world.Name
	}
	log.Println("Finished updating worlds")
}

func updateUser(userID string) {
	guilds, err := redis.Values(redisConn.Do("SMEMBERS", "guilds"))
	if err != nil {
		log.Printf("Error getting guilds from redis: %v\n", err)
		return
	}

	var guildList []string
	err = redis.ScanSlice(guilds, &guildList)
	if err != nil {
		log.Printf("Error converting guilds to []string: %v\n", err)
		return
	}

	name, worlds, err := getAccountData(userID)

	for _, guild := range guildList {
		_, erro := dg.GuildMember(guild, userID)
		if erro != nil {
			if strings.Contains(erro.Error(), fmt.Sprintf("%v", discordgo.ErrCodeUnknownMember)) {
				continue
			} else {
				log.Printf("Error getting member %v of guild %v: %v\n", userID, guild, erro)
				continue
			}
		}

		updateUserDataInGuild(userID, guild, name, worlds, err == nil)
	}
}

// nolint: gocyclo
func getAccountData(userID string) (name string, worlds []string, err error) {
	apikeys, err := redis.Values(redisConn.Do("SMEMBERS", userID))
	if err != nil {
		log.Printf("Error getting api keys from redis: %v\n", err)
		return
	}

	var keys []string
	err = redis.ScanSlice(apikeys, &keys)
	if err != nil {
		log.Printf("Error converting api keys to []string: %v\n", err)
		return
	}

	for _, key := range keys {
		res, erro := http.Get("https://api.guildwars2.com/v2/account?access_token=" + key)
		if erro != nil {
			if strings.Contains(erro.Error(), "invalid key") {
				_, erro = redisConn.Do("SREM", userID, key)
				if erro != nil {
					log.Printf("Error deleting api key from redis: %v", erro)
				}
			} else {
				log.Printf("Error getting account info: %v\n", erro)
			}
			err = erro
			continue
		}
		defer func() {
			if erro = res.Body.Close(); erro != nil {
				log.Printf("Error closing response body: %v\n", err)
			}
		}()
		jsonParser := json.NewDecoder(res.Body)
		var account gw2Account
		erro = jsonParser.Decode(&account)
		if erro != nil {
			if res.StatusCode >= 500 {
				log.Printf("Internal api server error: %v", res.Status)
			} else {
				log.Printf("Error parsing json to account data: %v, user %v\n", erro, userID)
			}
			err = erro
			continue
		}

		name += " | " + account.Name
		worlds = append(worlds, currentWorlds[account.World])
	}
	if len(name) >= 3 {
		name = name[3:]
	}
	return
}

func updateUserInGuild(userID, guildID string) {
	name, worlds, err := getAccountData(userID)
	updateUserDataInGuild(userID, guildID, name, worlds, err == nil)
}

func updateUserDataInGuild(userID, guildID, name string, worlds []string, removeWorlds bool) {
	dg.GuildMemberNickname(guildID, userID, name) // nolint: errcheck
	updateUserToWorldsInGuild(userID, guildID, worlds, removeWorlds)
}

func removeWorldsFromUserInGuild(userID, guildID string, member *discordgo.Member, guildRolesMap map[string]string,
	worldNames []string, removeWorlds bool) (wNames []string) {

	for _, role := range member.Roles {
		if getIndexByValue(guildRolesMap[role], currentWorlds) != -1 {
			index := indexOf(guildRolesMap[role], worldNames)
			if index == -1 && removeWorlds {
				erro := dg.GuildMemberRoleRemove(guildID, userID, role)
				if erro != nil {
					log.Printf("Error removing guild member role: %v\n", erro)
				}
			} else {
				worldNames = remove(worldNames, index)
			}
		}
	}
	wNames = worldNames
	return
}

func updateUserToWorldsInGuild(userID, guildID string, worldNames []string, removeWorlds bool) {
	member, err := dg.GuildMember(guildID, userID)
	if err != nil {
		log.Printf("Error getting guild member: %v\n", err)
		return
	}

	guildRoles, err := dg.GuildRoles(guildID)
	if err != nil {
		log.Printf("Error getting guild roles: %v\n", err)
		return
	}

	guildRolesMap := make(map[string]string)
	var guildRoleNames []string
	for _, role := range guildRoles {
		guildRolesMap[role.ID] = role.Name
		guildRoleNames = append(guildRoleNames, role.Name)
	}

	worldNames = removeWorldsFromUserInGuild(userID, guildID, member, guildRolesMap, worldNames, removeWorlds)

	for _, role := range worldNames {
		var roleID string
		if indexOf(role, guildRoleNames) == -1 {
			newRole, err := dg.GuildRoleCreate(guildID)
			if err != nil {
				log.Printf("Error creating guild role: %v\n", err)
				continue
			}
			newRole, erro := dg.GuildRoleEdit(guildID, newRole.ID, role, newRole.Color, newRole.Hoist, newRole.Permissions, newRole.Mentionable)
			if erro != nil {
				log.Printf("Error editing guild role: %v\n", erro)
			}

			roleID = newRole.ID
		} else {
			roleID = getKeyByValue(role, guildRolesMap)
		}

		erro := dg.GuildMemberRoleAdd(guildID, userID, roleID)
		if erro != nil {
			log.Printf("Error adding guild role to user: %v\n", erro)
		}
	}
}

func getKeyByValue(a string, list map[string]string) string {
	for i, b := range list {
		if b == a {
			return i
		}
	}
	return ""
}

func getIndexByValue(a string, list map[int]string) int {
	for i, b := range list {
		if b == a {
			return i
		}
	}
	return -1
}

func indexOf(a string, list []string) int {
	for i, b := range list {
		if b == a {
			return i
		}
	}
	return -1
}

func remove(array []string, index int) []string {
	array[len(array)-1], array[index] = array[index], array[len(array)-1]
	return array[:len(array)-1]
}
