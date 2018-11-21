package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/greaka/discordwvwbot/loglevels"

	"github.com/gomodule/redigo/redis"

	"github.com/bwmarrin/discordgo"
)

var (
	// updateUserChannel holds discord user ids to update
	updateUserChannel chan string

	// dg holds the discord bot session
	dg *discordgo.Session

	// currentWorlds holds the currently active worlds
	currentWorlds map[int]*linkInfo

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

// starting up the bot part
func startBot() {
	updateUserChannel = make(chan string)

	var err error

	// add event listener
	dg.AddHandler(guildCreate)
	dg.AddHandler(guildDelete)
	dg.AddHandler(guildMemberAdd)

	// open the connection to listen for events
	err = dg.Open()
	if err != nil {
		loglevels.Errorf("Error opening discord connection: %v\n", err)
		return
	}
	defer func() {
		if err = dg.Close(); err != nil {
			loglevels.Errorf("Error closing discord connection: %v\n", err)
		}
	}()
	loglevels.Info("Bot is now running")

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
		loglevels.Errorf("Error updating discord status: %v\n", statusUpdateError)
	}

	// firing up the update cycle
	go updater()

	for i := 0; i < 4; i++ {
		go updateCycle()
	}
}

func updateCycle() {
	// waiting for userids to update
	for {
		updateUser(<-updateUserChannel)
	}
}

// updater commands updates. it starts world updates and full user updates
func updater() {
	updateCurrentWorlds()
	updateAllUsers() // has to run here to set delayBetweenFullUpdates
	queueUserChannel := setDelay()
	for {
		// reset timer until next wvw reset update
		worldsChannel := resetWorldUpdateTimer()
		select {
		case <-worldsChannel:
			updateCurrentWorlds()
			queueUserChannel = setDelay()
			updateAllUsers()
		case <-queueUserChannel:
			queueUserChannel = setDelay()
			updateAllUsers()
		}
	}
}

func setDelay() (queueUserChannel <-chan time.Time) {
	// wait at least 15min until starting another full update
	fullUpdateDelay := delayBetweenFullUpdates
	if delayBetweenFullUpdates < 15*time.Minute {
		fullUpdateDelay = 15 * time.Minute
	}
	queueUserChannel = time.After(fullUpdateDelay)
	return
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
	loglevels.Info("Updating all users...")
	redisConn := usersDatabase.Get()
	defer closeConnection(redisConn)
	iterateThroughUsers := time.Tick(delayBetweenUsers)
	processValue := func(userID string) {
		<-iterateThroughUsers
		updateUserChannel <- userID
	}

	userCount := iterateDatabase(redisConn, processValue)
	loglevels.Info("Finished updating all users")

	// calculate the delay between full updates based on the user count
	delayBetweenFullUpdates = delayBetweenUsers * time.Duration(userCount+int(float64(userCount)*0.05)) // updatetime per user * (number of users + 5% margin)
}

// guildMemberAdd listens to new users joining a discord server
func guildMemberAdd(s *discordgo.Session, m *discordgo.GuildMemberAdd) {
	updateUserInGuild(m.User.ID, m.GuildID)
}

// guildCreate listens to the bot getting added to discord servers
// upon connecting to discord or after restoring the connection, the bot will receive this event for every server it is currently added to
func guildCreate(s *discordgo.Session, m *discordgo.GuildCreate) {

	redisConn := guildsDatabase.Get()
	// only update when the guild is not already in the database
	alreadyIn, err := redis.Int(redisConn.Do("EXISTS", m.ID))
	if err != nil {
		loglevels.Errorf("Error checking if guild %v is in redis guilds: %v\n", m.ID, err)
		return
	}
	if alreadyIn == 0 {
		err = saveNewGuild(redisConn, m.ID)
		closeConnection(redisConn)
		if err != nil {
			loglevels.Errorf("Error adding guild %v to redis guilds: %v\n", m.ID, err)
			return
		}
		// updateAllUsers()
	} else {
		closeConnection(redisConn)
	}
}

// guildDelete listens to the kick or ban event when the bot gets removed
func guildDelete(s *discordgo.Session, m *discordgo.GuildDelete) {
	redisConn := guildsDatabase.Get()
	_, err := redisConn.Do("DEL", m.ID)
	closeConnection(redisConn)
	if err != nil {
		loglevels.Errorf("Error removing guild %v from redis guilds: %v\n", m.ID, err)
	}
}

// updateCurrentWorlds updates the current world list
func updateCurrentWorlds() {
	loglevels.Info("Updating worlds...")

	matches, err := getCurrentMatches()
	if err != nil {
		return
	}

	// reformat to custom projection
	currentWorlds = make(map[int]*linkInfo)
	for _, match := range matches {
		processMatchColor(match.AllWorlds.Red)
		processMatchColor(match.AllWorlds.Blue)
		processMatchColor(match.AllWorlds.Green)
	}

	worlds, err := getWorlds()
	if err != nil {
		return
	}

	for _, world := range worlds {
		currentWorlds[world.ID].Name = world.Name
	}

	loglevels.Info("Finished updating worlds")
}

func processMatchColor(worlds []int) {
	for _, world := range worlds {
		if _, ok := currentWorlds[world]; !ok {
			currentWorlds[world] = &linkInfo{
				ID:     world,
				Linked: worlds,
			}
		}
	}
}

// updateUser updates a single user on all discord servers
func updateUser(userID string) {
	redisConn := guildsDatabase.Get()
	defer closeConnection(redisConn)
	name, worlds, err := getAccountData(userID)
	processGuild := func(guild string) {
		_, erro := dg.GuildMember(guild, userID)
		if erro != nil {
			if !strings.Contains(erro.Error(), fmt.Sprintf("%v", discordgo.ErrCodeUnknownMember)) {
				loglevels.Errorf("Error getting member %v of guild %v: %v\n", userID, guild, erro)
			}
		} else {
			updateUserDataInGuild(userID, guild, name, worlds, err == nil)
		}
	}

	iterateDatabase(redisConn, processGuild)
}

// getAccountData gets the gw2 account data for a specific discord user
// nolint: gocyclo
func getAccountData(userID string) (name string, worlds []int, err error) {
	keys, err := getAPIKeys(userID)
	if err != nil {
		return
	}

	// for every api key ...
	for _, key := range keys {

		// get account data
		account, erro := getGw2Account(key)
		if erro != nil {
			// if the key got revoked, delete it
			if strings.Contains(erro.Error(), "invalid key") {
				loglevels.Info("Encountered invalid key")
				redisConn := usersDatabase.Get()
				_, erro = redisConn.Do("SREM", userID, key)
				closeConnection(redisConn)
				if erro != nil {
					loglevels.Errorf("Error deleting api key from redis: %v", erro)
				}
			} else {
				loglevels.Errorf("Error getting account info: %v\n", erro)
				// unexpected error, don't revoke discord roles because of a server error
				err = erro
			}
			continue
		}

		// add the name to the account names
		name += " | " + account.Name

		// add world to users worlds
		worlds = append(worlds, account.World)
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
func updateUserDataInGuild(userID, guildID, name string, worlds []int, removeWorlds bool) {
	options, err := getGuildSettings(guildID)
	if err != nil {
		return
	}
	if options.RenameUsers {
		dg.GuildMemberNickname(guildID, userID, name) // nolint: errcheck, gosec
	}

	switch options.Mode {
	case allServers:
		updateUserToWorldsInGuild(userID, guildID, worlds, removeWorlds, options)
	case oneServer:
		updateUserToVerifyInGuild(userID, guildID, worlds, removeWorlds, options, options.Gw2ServerID)
	case userBased:
		updateUserToUserBasedVerifyInGuild(userID, guildID, worlds, removeWorlds, options)
	}
}

// updateUserToWorldsInGuild updates the world roles for the user in a specific guild
// nolint: gocyclo
func updateUserToWorldsInGuild(userID, guildID string, userWorlds []int, removeWorlds bool, options *guildOptions) {
	member, err := dg.GuildMember(guildID, userID)
	if err != nil {
		loglevels.Errorf("Error getting guild member: %v\n", err)
		return
	}

	guildRoles, err := dg.GuildRoles(guildID)
	if err != nil {
		loglevels.Errorf("Error getting guild roles: %v\n", err)
		return
	}

	// get all role ids based on world names
	guildRolesMap := make(map[string]string)
	var guildRoleNames []string
	for _, role := range guildRoles {
		guildRolesMap[role.ID] = role.Name
		guildRoleNames = append(guildRoleNames, role.Name)
	}

	if options.CreateRoles {
		for _, world := range currentWorlds {
			if indexOfString(world.Name, guildRoleNames) == -1 {
				createRole(guildID, world.Name) // nolint: errcheck, gosec
			}
		}
	}

	var userWorldNames []string
	for _, world := range userWorlds {
		userWorldNames = append(userWorldNames, currentWorlds[world].Name)
	}

	// remove world roles the user is not on (anymore)
	userWorldNames = removeWorldsFromUserInGuild(userID, guildID, member, guildRolesMap, userWorldNames, removeWorlds)

	// create discord roles if needed and add user to these world roles
	for _, role := range userWorldNames {
		var roleID string
		if indexOfString(role, guildRoleNames) == -1 {
			newRole, err := createRole(guildID, role)
			if err != nil {
				continue
			}
			roleID = newRole.ID
		} else {
			roleID = getKeyByValue(role, guildRolesMap)
		}

		erro := dg.GuildMemberRoleAdd(guildID, userID, roleID)
		if erro != nil {
			loglevels.Errorf("Error adding guild role to user: %v\n", erro)
		}
	}
}

func createRole(guildID, name string) (newRole *discordgo.Role, err error) {
	newRole, err = dg.GuildRoleCreate(guildID)
	if err != nil {
		loglevels.Errorf("Error creating guild role: %v\n", err)
		return
	}
	newRole, err = dg.GuildRoleEdit(guildID, newRole.ID, name, newRole.Color, newRole.Hoist, newRole.Permissions, newRole.Mentionable)
	if err != nil {
		loglevels.Errorf("Error editing guild role: %v\n", err)
	}
	return
}

func addRole(guildID, userID, roleID string, member *discordgo.Member) (err error) {
	if indexOfString(roleID, member.Roles) == -1 {
		err = dg.GuildMemberRoleAdd(guildID, userID, roleID)
		if err != nil {
			loglevels.Errorf("Error adding dg role %v of guild %v to user %v: %v\n", roleID, guildID, userID, err)
		}
	}
	return
}

func removeRole(guildID, userID, roleID string, member *discordgo.Member, remove bool) (err error) {
	if !remove {
		return
	}
	if indexOfString(roleID, member.Roles) != -1 {
		err = dg.GuildMemberRoleRemove(guildID, userID, roleID)
		if err != nil {
			loglevels.Errorf("Error removing dg role %v of guild %v to user %v: %v\n", roleID, guildID, userID, err)
		}
	}
	return
}

// removeWorldsFromUserInGuild removes every role from the user that is not part of the users worlds (anymore)
func removeWorldsFromUserInGuild(userID, guildID string, member *discordgo.Member, guildRolesMap map[string]string,
	worldNames []string, removeWorlds bool) (wNames []string) {

	// for every role ...
	for _, role := range member.Roles {
		// if role name is a current world name ...
		if getWorldIDByName(guildRolesMap[role], currentWorlds) != -1 {
			// if role is not part of users worlds ...
			index := indexOfString(guildRolesMap[role], worldNames)
			// ... and if we should remove worlds (can be false if unexpected errors occured while getting account data)
			if index == -1 && removeWorlds {
				// remove role
				erro := dg.GuildMemberRoleRemove(guildID, userID, role)
				if erro != nil {
					loglevels.Errorf("Error removing guild member role: %v\n", erro)
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

// nolint: gocyclo
func updateUserToVerifyInGuild(userID, guildID string, worlds []int, removeWorlds bool, options *guildOptions, verifyWorld int) {
	member, err := dg.GuildMember(guildID, userID)
	if err != nil {
		loglevels.Errorf("Error getting guild member: %v\n", err)
		return
	}

	guildRoles, err := dg.GuildRoles(guildID)
	if err != nil {
		loglevels.Errorf("Error getting guild roles: %v\n", err)
		return
	}

	var verifiedID string
	var linkedID string
	for _, role := range guildRoles {
		if role.Name == "WvW-Verified" {
			verifiedID = role.ID
		}
		if role.Name == "WvW-Linked" {
			linkedID = role.ID
		}
	}
	if verifiedID == "" {
		var role *discordgo.Role
		role, err = createRole(guildID, "WvW-Verified")
		if err != nil {
			return
		}
		verifiedID = role.ID
	}
	if options.AllowLinked && linkedID == "" {
		var role *discordgo.Role
		role, err = createRole(guildID, "WvW-Linked")
		if err != nil {
			return
		}
		linkedID = role.ID
	}

	if indexOfInt(verifyWorld, worlds) != -1 {
		err = addRole(guildID, userID, verifiedID, member)
		if err != nil {
			return
		}
		if options.AllowLinked {
			err = removeRole(guildID, userID, linkedID, member, removeWorlds)
			if err != nil {
				return
			}
		} else {
			if linkedID != "" {
				err = dg.GuildRoleDelete(guildID, linkedID)
				if err != nil {
					loglevels.Errorf("Error deleting role %v of guild %v: %v", linkedID, guildID, err)
					return
				}
			}
		}
	} else {
		if options.AllowLinked {
			linked := false
			for _, world := range currentWorlds[verifyWorld].Linked {
				if indexOfInt(world, worlds) != -1 {
					linked = true
					if options.VerifyOnly {
						err = addRole(guildID, userID, verifiedID, member)
						if err != nil {
							return
						}
						err = removeRole(guildID, userID, linkedID, member, removeWorlds)
						if err != nil {
							return
						}
					} else {
						err = addRole(guildID, userID, linkedID, member)
						if err != nil {
							return
						}
						err = removeRole(guildID, userID, verifiedID, member, removeWorlds)
						if err != nil {
							return
						}
					}
				}
			}
			if !linked {
				err = removeRole(guildID, userID, verifiedID, member, removeWorlds)
				if err != nil {
					return
				}
				if options.AllowLinked {
					err = removeRole(guildID, userID, linkedID, member, removeWorlds)
					if err != nil {
						return
					}
				}
			}
		} else {
			err = removeRole(guildID, userID, verifiedID, member, removeWorlds)
			if err != nil {
				return
			}
			if linkedID != "" {
				err = dg.GuildRoleDelete(guildID, linkedID)
				if err != nil {
					loglevels.Errorf("Error deleting role %v of guild %v: %v", linkedID, guildID, err)
					return
				}
			}
		}
	}
}

func updateUserToUserBasedVerifyInGuild(userID, guildID string, worlds []int, removeWorlds bool, options *guildOptions) {
	owner, err := getCachedGw2Account(options.Gw2AccountKey)
	if err != nil {
		return
	}
	updateUserToVerifyInGuild(userID, guildID, worlds, removeWorlds, options, owner.World)
}
