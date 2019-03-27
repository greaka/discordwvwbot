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

	statusListenTo()

	// firing up the update cycle
	go updater()

	for i := 0; i < 4; i++ {
		go updateCycle()
	}

	updateCycle()
}

func updateCycle() {
	// waiting for userids to update
	for {
		updateUser(<-updateUserChannel)
	}
}

func statusListenTo() {
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
}

func statusUpdateWorlds() {
	now := int(time.Now().UnixNano() / int64(time.Millisecond))
	// update discord status to "listening to <hosturl>"
	status := discordgo.UpdateStatusData{
		Status:    string(discordgo.StatusOnline),
		AFK:       false,
		IdleSince: &now,
		Game: &discordgo.Game{
			Name: "updating worlds",
			Type: 0,
		},
	}
	statusUpdateError := dg.UpdateStatusComplex(status)
	if statusUpdateError != nil {
		loglevels.Errorf("Error updating discord status: %v\n", statusUpdateError)
	}
}

func statusUpdateUsers() {
	now := int(time.Now().UnixNano() / int64(time.Millisecond))
	// update discord status to "listening to <hosturl>"
	status := discordgo.UpdateStatusData{
		Status:    string(discordgo.StatusOnline),
		AFK:       false,
		IdleSince: &now,
		Game: &discordgo.Game{
			Name: "updating all users",
			Type: 0,
		},
	}
	statusUpdateError := dg.UpdateStatusComplex(status)
	if statusUpdateError != nil {
		loglevels.Errorf("Error updating discord status: %v\n", statusUpdateError)
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
	statusUpdateUsers()
	redisConn := usersDatabase.Get()
	defer closeConnection(redisConn)
	iterateThroughUsers := time.Tick(delayBetweenUsers)
	processValue := func(userID string) {
		<-iterateThroughUsers
		updateUserChannel <- userID
	}

	userCount := iterateDatabase(redisConn, processValue)
	statusListenTo()
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
	statusUpdateWorlds()

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

	statusListenTo()
	loglevels.Info("Finished updating worlds")

	loglevels.Info("Current Links:")
	for _, world := range currentWorlds {
		loglevels.Infof("%v", world)
	}
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
				loglevels.Warningf("Error getting member %v of guild %v: %v\n", userID, guild, erro)
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
				loglevels.Warningf("Error getting account info: %v\n", erro)
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

	guildRoles, err := dg.GuildRoles(guildID)
	if err != nil {
		loglevels.Errorf("Error getting guild roles: %v\n", err)
		return
	}

	roles, err := getGuildRoles(guildID, guildRoles)
	if err != nil {
		return
	}

	if options.RenameUsers {
		dg.GuildMemberNickname(guildID, userID, name) // nolint: errcheck, gosec
	}

	switch options.Mode {
	case allServers:
		updateUserToWorldsInGuild(userID, guildID, worlds, removeWorlds, options, roles, guildRoles)
	case oneServer:
		updateUserToVerifyInGuild(userID, guildID, worlds, removeWorlds, options, options.Gw2ServerID, roles, guildRoles)
	case userBased:
		updateUserToUserBasedVerifyInGuild(userID, guildID, worlds, removeWorlds, options, roles, guildRoles)
	}
}

// updateUserToWorldsInGuild updates the world roles for the user in a specific guild
// nolint: gocyclo
func updateUserToWorldsInGuild(userID, guildID string, userWorlds []int, removeWorlds bool, options *guildOptions, roles []guildRole, guildRoles []*discordgo.Role) {
	member, err := dg.GuildMember(guildID, userID)
	if err != nil {
		loglevels.Errorf("Error getting guild member: %v\n", err)
		return
	}

	var wantedRoles []string

	for _, world := range userWorlds {
		found := false
		for _, role := range roles {
			if currentWorlds[world].Name == role.Name {
				wantedRoles = append(wantedRoles, role.ID)
				found = true
				break
			}
		}
		if !found {
			for _, role := range guildRoles {
				if role.Name == currentWorlds[world].Name {
					wantedRoles = append(wantedRoles, role.ID)
					found = true
					roleStruct := guildRole{
						ID:   role.ID,
						Name: role.Name,
					}
					addGuildRole(guildID, roleStruct) // nolint: errcheck, gosec
					roles = append(roles, roleStruct)
					break
				}
			}
			if !found {
				_, roleStruct, err := createRoleAndAddToManaged(guildID, currentWorlds[world].Name)
				if err != nil {
					continue
				}
				roles = append(roles, roleStruct)
				wantedRoles = append(wantedRoles, roleStruct.ID)
			}
		}
	}

	if options.CreateRoles {
		for _, world := range currentWorlds {
			found := false
			for _, role := range roles {
				if role.Name == world.Name {
					found = true
					break
				}
			}
			if !found {
				for _, role := range guildRoles {
					if role.Name == world.Name {
						found = true
						roleStruct := guildRole{
							ID:   role.ID,
							Name: role.Name,
						}
						roles = append(roles, roleStruct)
						break
					}
				}
			}
			if !found {
				createRoleAndAddToManaged(guildID, world.Name) // nolint: errcheck, gosec
			}
		}
	}

	assignManagedRoles(member, guildID, roles, wantedRoles, removeWorlds)
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

func createRoleAndAddToManaged(guildID, name string) (newRole *discordgo.Role, roleStruct guildRole, err error) {
	newRole, err = createRole(guildID, name)
	if err != nil {
		return
	}
	roleStruct = guildRole{
		ID:   newRole.ID,
		Name: newRole.Name,
	}
	err = addGuildRole(guildID, roleStruct)
	return
}

func addRole(guildID, roleID string, member *discordgo.Member) (err error) {
	if indexOfString(roleID, member.Roles) == -1 {
		err = dg.GuildMemberRoleAdd(guildID, member.User.ID, roleID)
		if err != nil {
			loglevels.Errorf("Error adding dg role %v of guild %v to user %v: %v\n", roleID, guildID, member.User.ID, err)
		}
	}
	return
}

func removeRole(guildID, roleID string, member *discordgo.Member, remove bool) (err error) {
	if !remove {
		return
	}
	if indexOfString(roleID, member.Roles) != -1 {
		err = dg.GuildMemberRoleRemove(guildID, member.User.ID, roleID)
		if err != nil {
			loglevels.Errorf("Error removing dg role %v of guild %v to user %v: %v\n", roleID, guildID, member.User.ID, err)
		}
	}
	return
}

func assignManagedRoles(member *discordgo.Member, guildID string, managedRoles []guildRole, wantedRoles []string, removeRoles bool) {
	var managedRolesOfUser []string
	for _, role := range member.Roles {
		for _, managedRole := range managedRoles {
			if managedRole.ID == role {
				managedRolesOfUser = append(managedRolesOfUser, role)
				break
			}
		}
	}

	for _, role := range managedRolesOfUser {
		index := indexOfString(role, wantedRoles)
		if index == -1 {
			removeRole(guildID, role, member, removeRoles) // nolint: gosec, errcheck
		} else {
			wantedRoles = remove(wantedRoles, index)
		}
	}

	for _, role := range wantedRoles {
		addRole(guildID, role, member) // nolint: gosec, errcheck
	}
}

// nolint: gocyclo
func updateUserToVerifyInGuild(userID, guildID string, worlds []int, removeWorlds bool, options *guildOptions, verifyWorld int, roles []guildRole, guildRoles []*discordgo.Role) {
	member, err := dg.GuildMember(guildID, userID)
	if err != nil {
		loglevels.Errorf("Error getting guild member: %v\n", err)
		return
	}

	var verifiedID string
	var linkedID string
	var wantedRoles []string

	for _, role := range roles {
		switch role.Name {
		case "WvW-Verified":
			verifiedID = role.ID
		case "WvW-Linked":
			linkedID = role.ID
		}
	}

	if verifiedID == "" || (options.AllowLinked && linkedID == "") {
		for _, role := range guildRoles {
			if role.Name == "WvW-Verified" {
				verifiedID = role.ID
				roleStruct := guildRole{
					ID:   role.ID,
					Name: role.Name,
				}
				roles = append(roles, roleStruct)
				erro := addGuildRole(guildID, roleStruct)
				if erro != nil {
					loglevels.Warningf("Error adding existing verified role %v to managed roles: %v", role.ID, erro)
				}
			}
			if role.Name == "WvW-Linked" {
				linkedID = role.ID
				roleStruct := guildRole{
					ID:   role.ID,
					Name: role.Name,
				}
				roles = append(roles, roleStruct)
				erro := addGuildRole(guildID, roleStruct)
				if erro != nil {
					loglevels.Warningf("Error adding existing linked role %v to managed roles: %v", role.ID, erro)
				}
			}
		}
		if verifiedID == "" {
			_, roleStruct, err := createRoleAndAddToManaged(guildID, "WvW-Verified")
			if err != nil {
				return
			}
			verifiedID = roleStruct.ID
			roles = append(roles, roleStruct)
		}
		if options.AllowLinked && linkedID == "" {
			_, roleStruct, err := createRoleAndAddToManaged(guildID, "WvW-Linked")
			if err != nil {
				return
			}
			linkedID = roleStruct.ID
			roles = append(roles, roleStruct)
		}
	}

	if indexOfInt(verifyWorld, worlds) != -1 {
		wantedRoles = append(wantedRoles, verifiedID)
	} else {
		if options.AllowLinked {
			for _, world := range currentWorlds[verifyWorld].Linked {
				if indexOfInt(world, worlds) != -1 {
					if options.VerifyOnly {
						wantedRoles = append(wantedRoles, verifiedID)
					} else {
						wantedRoles = append(wantedRoles, linkedID)
					}
				}
			}
		}
	}

	assignManagedRoles(member, guildID, roles, wantedRoles, removeWorlds)
}

func updateUserToUserBasedVerifyInGuild(userID, guildID string, worlds []int, removeWorlds bool, options *guildOptions, roles []guildRole, guildRoles []*discordgo.Role) {
	owner, err := getCachedGw2Account(options.Gw2AccountKey)
	if err != nil {
		return
	}
	updateUserToVerifyInGuild(userID, guildID, worlds, removeWorlds, options, owner.World, roles, guildRoles)
}
