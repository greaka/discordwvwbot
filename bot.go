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
	// updateUserChannel holds discord user ids to update
	updateUserChannel chan string

	// dg holds the discord bot session
	dg *discordgo.Session

	// currentWorlds holds the currently active worlds in the format map[id]name
	currentWorlds map[int]string

	// delayBetweenFullUpdates holds the delay betwenn starting a new full user update cycle
	delayBetweenFullUpdates time.Duration
)

const (
	// delayBetweenUsers holds the duration to wait before queueing up the next user to update in a full update cycle
	/* 	gw2 api rate limit: 600 requests per minute
	api keys to check per user (average): 2
	600 / 2 = 300 users per minute
	60/300 = 0.2s per user
	*/
	delayBetweenUsers time.Duration = 200 * time.Millisecond
)

// gw2Account holds the data returned by the gw2 api /account endpoint
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

// worldStruct holds the world data returned by the gw2 api /worlds endpoint
type worldStruct struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// starting up the bot part
func startBot() {
	updateUserChannel = make(chan string)

	// connect to the discord bot api
	var err error
	dg, err = discordgo.New("Bot " + config.BotToken)
	if err != nil {
		log.Printf("Error connecting to discord: %v\n", err)
		return
	}

	// add event listener
	dg.AddHandler(guildCreate)
	dg.AddHandler(guildDelete)
	dg.AddHandler(guildMemberAdd)

	// open the connection to listen for events
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

	// update discord status to "listening to <hosturl>"
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

	// firing up the update cycle
	go updater()

	// waiting for userids to update
	for {
		updateUser(<-updateUserChannel)
	}
}

// updater commands updates. it starts world updates and full user updates
func updater() {
	updateCurrentWorlds()
	updateAllUsers() // has to run here to set delayBetweenFullUpdates
	for {
		// wait at least 15min until starting another full update
		fullUpdateDelay := delayBetweenFullUpdates
		if delayBetweenFullUpdates < 15*time.Minute {
			fullUpdateDelay = 15 * time.Minute
		}
		queueUserChannel := time.After(fullUpdateDelay)
		// reset timer until next wvw reset update
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

// resetWorldUpdateTimer returns a channel that fires when the next weekly wvw reset is done
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
	// we have to double check if we can use the earlier time because the calculations to this point are only day precise
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

// updateAllUsers will send update requests for every user and will wait the set duration between requests
func updateAllUsers() {
	log.Println("Updating all users...")
	// get every key
	/* blocks redis database with O(n)
	 * since this bot will never have millions of updates per second, this is fine
	 */
	keys, err := redis.Values(redisConn.Do("KEYS", "*"))
	if err != nil {
		log.Printf("Error getting keys * from redis: %v\n", err)
		return
	}

	// convert returned string to userids []string
	var userIds []string
	err = redis.ScanSlice(keys, &userIds)
	if err != nil {
		log.Printf("Error converting keys * to []string: %v\n", err)
		return
	}

	// calculate the delay between full updates based on the user count
	delayBetweenFullUpdates = delayBetweenUsers * time.Duration((len(userIds) + 50)) // updatetime per user * (number of users + 50 margin)
	iterateThroughUsers := time.Tick(delayBetweenUsers)

	// fire update while list not empty and not databse key "guilds"
	for len(userIds) > 0 {
		if userIds[0] != "guilds" {
			<-iterateThroughUsers
			updateUserChannel <- userIds[0]
		}
		userIds = remove(userIds, 0)
	}
	log.Println("Finished updating all users")
}

// guildMemberAdd listens to new users joining a discord server
func guildMemberAdd(s *discordgo.Session, m *discordgo.GuildMemberAdd) {
	updateUserInGuild(m.User.ID, m.GuildID)
}

// guildCreate listens to the bot getting added to discord servers
// upon connecting to discord or after restoring the connection, the bot will receive this event for every server it is currently added to
func guildCreate(s *discordgo.Session, m *discordgo.GuildCreate) {

	// only update when the guild is not already in the database
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

// guildDelete listens to the kick or ban event when the bot gets removed
func guildDelete(s *discordgo.Session, m *discordgo.GuildDelete) {
	_, err := redisConn.Do("SREM", "guilds", m.ID)
	if err != nil {
		log.Printf("Error removing guild %v from redis guilds: %v\n", m.ID, err)
	}
}

// updateCurrentWorlds updates the current world list
func updateCurrentWorlds() {

	// get worlds data
	log.Println("Updating worlds...")
	res, erro := http.Get(gw2APIURL + "/worlds?ids=all")
	if erro != nil {
		log.Printf("Error getting worlds info: %v\n", erro)
		return
	}
	defer func() {
		if err := res.Body.Close(); err != nil {
			log.Printf("Error closing response body: %v\n", err)
		}
	}()

	// parse to []worldStruct
	jsonParser := json.NewDecoder(res.Body)
	var worlds []worldStruct
	erro = jsonParser.Decode(&worlds)
	if erro != nil {
		log.Printf("Error parsing json to world data: %v\n", erro)
		return
	}

	// reformat to custom projection
	currentWorlds = make(map[int]string)
	for _, world := range worlds {
		currentWorlds[world.ID] = world.Name
	}
	log.Println("Finished updating worlds")
}

// updateUser updates a single user on all discord servers
func updateUser(userID string) {

	// get discord server list
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

	// get user's gw2 account data
	name, worlds, err := getAccountData(userID)

	// cycle through every server to check if user is present and update the user there
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

// getAccountData gets the gw2 account data for a specific discord user
// nolint: gocyclo
func getAccountData(userID string) (name string, worlds []string, err error) {

	// get all api keys of the user
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

	// for every api key ...
	for _, key := range keys {

		// get account data
		res, erro := http.Get(gw2APIURL + "/account?access_token=" + key)
		if erro != nil {
			// if the key got revoked, delete it
			if strings.Contains(erro.Error(), "invalid key") {
				_, erro = redisConn.Do("SREM", userID, key)
				if erro != nil {
					log.Printf("Error deleting api key from redis: %v", erro)
				}
			} else {
				log.Printf("Error getting account info: %v\n", erro)
				// unexpected error, don't revoke discord roles because of a server error
				err = erro
			}
			continue
		}
		defer func() {
			if erro = res.Body.Close(); erro != nil {
				log.Printf("Error closing response body: %v\n", err)
			}
		}()

		// parse it to account struct
		jsonParser := json.NewDecoder(res.Body)
		var account gw2Account
		erro = jsonParser.Decode(&account)
		if erro != nil {
			if res.StatusCode >= 500 {
				log.Printf("Internal api server error: %v", res.Status)
			} else {
				log.Printf("Error parsing json to account data: %v, user %v\n", erro, userID)
			}
			// unexpected error, don't revoke discord roles because of a server error
			err = erro
			continue
		}

		// add the name to the account names
		name += " | " + account.Name

		// add world to users worlds
		worlds = append(worlds, currentWorlds[account.World])
	}
	// strip the first " | ", on unexpeceted erros the name can still be empty
	if len(name) >= 3 {
		name = name[3:]
	}
	return
}

// updateUserInGuild gets the account data and updates the user on a specific discord server
func updateUserInGuild(userID, guildID string) {
	name, worlds, err := getAccountData(userID)
	updateUserDataInGuild(userID, guildID, name, worlds, err == nil)
}

// updateUserDataInGuild updates the user on a specific discord server
func updateUserDataInGuild(userID, guildID, name string, worlds []string, removeWorlds bool) {
	dg.GuildMemberNickname(guildID, userID, name) // nolint: errcheck
	updateUserToWorldsInGuild(userID, guildID, worlds, removeWorlds)
}

// removeWorldsFromUserInGuild removes every role from the user that is not part of the users worlds (anymore)
func removeWorldsFromUserInGuild(userID, guildID string, member *discordgo.Member, guildRolesMap map[string]string,
	worldNames []string, removeWorlds bool) (wNames []string) {

	// for every role ...
	for _, role := range member.Roles {
		// if role name is a current world name ...
		if getIndexByValue(guildRolesMap[role], currentWorlds) != -1 {
			// if role is not part of users worlds ...
			index := indexOf(guildRolesMap[role], worldNames)
			// ... and if we should remove worlds (can be false if unexpected errors occured while getting account data)
			if index == -1 && removeWorlds {
				// remove role
				erro := dg.GuildMemberRoleRemove(guildID, userID, role)
				if erro != nil {
					log.Printf("Error removing guild member role: %v\n", erro)
				}
			} else {
				// role is still a world name but since the user already has this role, we don't need to add it to him later
				worldNames = remove(worldNames, index)
			}
		}
	}
	wNames = worldNames
	return
}

// updateUserToWorldsInGuild updates the world roles for the user in a specific guild
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

	// get all role ids based on world names
	guildRolesMap := make(map[string]string)
	var guildRoleNames []string
	for _, role := range guildRoles {
		guildRolesMap[role.ID] = role.Name
		guildRoleNames = append(guildRoleNames, role.Name)
	}

	// remove world roles the user is not on (anymore)
	worldNames = removeWorldsFromUserInGuild(userID, guildID, member, guildRolesMap, worldNames, removeWorlds)

	// create discord roles if needed and add user to these world roles
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

// getKeyByValue is a helper function to get a key based on a value in a map[key]value
func getKeyByValue(a string, list map[string]string) string {
	for i, b := range list {
		if b == a {
			return i
		}
	}
	return ""
}

// getIndexByValue is a helper function to get an index based on a value in a map[index]value
func getIndexByValue(a string, list map[int]string) int {
	for i, b := range list {
		if b == a {
			return i
		}
	}
	return -1
}

// indexOf is a helper function to get an index based on a value in a [index]string
func indexOf(a string, list []string) int {
	for i, b := range list {
		if b == a {
			return i
		}
	}
	return -1
}

// remove is a helper function to remove an item from an array at an index. The order will not be kept!
func remove(array []string, index int) []string {
	array[len(array)-1], array[index] = array[index], array[len(array)-1]
	return array[:len(array)-1]
}
