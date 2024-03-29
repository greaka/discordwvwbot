package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/greaka/discordwvwbot/loglevels"

	"github.com/gomodule/redigo/redis"

	"github.com/bwmarrin/discordgo"
)

var (
	// updateUserChannel holds discord user ids to update
	updateUserChannel chan struct {
		string
		bool
	}

	bucket chan interface{}

	// dg holds the discord bot session
	dg *discordgo.Session

	// currentWorlds holds the currently active worlds
	currentWorlds map[int]*linkInfo

	// delayBetweenFullUpdates holds the delay betwenn starting a new full user update cycle
	delayBetweenFullUpdates time.Duration

	// userCount holds the current userCount. is uninitialized before first full update cycle
	userCount int

	listenKind bool
)

const (
	// delayBetweenUsers holds the duration to wait before queueing up the next user to update in a full update cycle
	/* 	gw2 api rate limit: 600 requests per minute
	api keys to check per user (average): 3
	600 / 3 = 200 users per minute
	60/200 = 0.3s per user
	*/
	delayBetweenUsers = 300 * time.Millisecond
)

// starting up the bot part
func startBot() {
	updateUserChannel = make(chan struct {
		string
		bool
	}, 1000)
	go fillBucket()

	var err error

	dg.Identify.Intents = discordgo.MakeIntent(discordgo.IntentsGuilds |
		discordgo.IntentsGuildMembers |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsDirectMessages)

	// add event listener
	dg.AddHandler(guildCreate)
	dg.AddHandler(guildMemberAdd)
	dg.AddHandler(messageReceive)

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

func fillBucket() {
	bucket = make(chan interface{}, 600)
	rate := time.Tick(time.Second)
	for i := 0; i < 600; i++ {
		bucket <- nil
	}
	for {
		<-rate
		for i := 0; i < 10; i++ {
			select {
			case bucket <- nil:
			default:
			}
		}
	}
}

func updateCycle() {
	// waiting for userids to update
	for {
		updateUser(<-updateUserChannel)
	}
}

func statusListenTo() {
	text := config.HostURL
	listenKind = !listenKind
	if listenKind {
		text = ".wvw help"
	}
	// update discord status to "listening to <hosturl>"
	status := discordgo.UpdateStatusData{
		Status:    string(discordgo.StatusOnline),
		AFK:       false,
		IdleSince: nil,
		Game: &discordgo.Game{
			Name: text,
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
	text := fmt.Sprintf("updating %v users", userCount)
	if userCount == 0 {
		text = "updating all users"
	}
	// update discord status to "listening to <hosturl>"
	status := discordgo.UpdateStatusData{
		Status:    string(discordgo.StatusOnline),
		AFK:       false,
		IdleSince: &now,
		Game: &discordgo.Game{
			Name: text,
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
	nextWorldUpdate := nextWorldReset()
	// wait at least 15min until starting another full update
	fullUpdateDelay := delayBetweenFullUpdates
	if delayBetweenFullUpdates < 15*time.Minute {
		fullUpdateDelay = 15 * time.Minute
	}
	if fullUpdateDelay*2 < time.Until(nextWorldUpdate) {
		queueUserChannel = time.After(fullUpdateDelay)
	} else {
		queueUserChannel = time.After(fullUpdateDelay * 2)
	}
	return
}

// resetWorldUpdateTimer returns a channel that fires when the next weekly wvw reset is done
func resetWorldUpdateTimer() (worldsChannel <-chan time.Time) {
	nextReset := nextWorldReset()
	worldsChannel = time.After(time.Until(nextReset))
	return
}

func nextWorldReset() (nextReset time.Time) {
	now := time.Now()
	daysUntilNextFriday := int(time.Friday - now.Weekday())
	if daysUntilNextFriday < 0 {
		daysUntilNextFriday += 7
	}
	daysUntilNextSaturday := int(time.Saturday - now.Weekday())
	if daysUntilNextSaturday < 0 {
		daysUntilNextSaturday += 7
	}
	nextEUReset := time.Date(now.Year(), now.Month(), now.Day()+daysUntilNextFriday, 18, 15, 0, 0, time.UTC)
	nextUSReset := time.Date(now.Year(), now.Month(), now.Day()+daysUntilNextSaturday, 2, 15, 0, 0, time.UTC)
	// we have to double check if we can use the earlier time because the calculations to this point are only day precise
	if nextEUReset.Before(nextUSReset) {
		if nextEUReset.Before(now) {
			nextReset = nextUSReset
		} else {
			nextReset = nextEUReset
		}
	} else {
		if nextUSReset.Before(now) {
			nextReset = nextEUReset
		} else {
			nextReset = nextUSReset
		}
	}
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
		for len(updateUserChannel) > 10 {
			<-iterateThroughUsers
		}
		updateUserChannel <- struct {
			string
			bool
		}{string: userID, bool: false}
	}

	userCount = iterateDatabase(redisConn, processValue)
	statusListenTo()
	loglevels.Infof("Finished updating %v users", userCount)

	// calculate the delay between full updates based on the user count
	delayBetweenFullUpdates = delayBetweenUsers * time.Duration(userCount*2+int(float64(userCount)*0.05)) // updatetime per user * 2 * (number of users + 5% margin)
	loglevels.Infof("Delay between full updates: %v", delayBetweenFullUpdates)
}

// guildMemberAdd listens to new users joining a discord server
func guildMemberAdd(_ *discordgo.Session, m *discordgo.GuildMemberAdd) {
	_ = updateUserInGuild(m.Member)
}

// guildCreate listens to the bot getting added to discord servers
// upon connecting to discord or after restoring the connection, the bot will receive this event for every server it is currently added to
func guildCreate(s *discordgo.Session, m *discordgo.GuildCreate) {
	erro := s.RequestGuildMembers(m.ID, "", 0, false)
	if erro != nil {
		loglevels.Errorf("Error requesting members for guild %v: %v", m.ID, erro)
	}

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

// updateCurrentWorlds updates the current world list
func updateCurrentWorlds() {
	loglevels.Info("Updating worlds...")
	statusUpdateWorlds()

	worlds, err := getWorlds()
	if err != nil {
		return
	}

	for {
		matches, err := getCurrentMatches()
		if err != nil {
			loglevels.Errorf("Error fetching current worlds: %v\n", err)
			return
		}

		// reformat to custom projection
		currentWorlds = make(map[int]*linkInfo)
		for _, match := range matches {
			processMatchColor(match.AllWorlds.Red)
			processMatchColor(match.AllWorlds.Blue)
			processMatchColor(match.AllWorlds.Green)
		}

		inconsistent := false
		for _, world := range worlds {
			if world.ID > 10000 {
				continue
			}
			if _, ok := currentWorlds[world.ID]; !ok {
				loglevels.Warningf("World %v not found in match data, trying again...", world.ID)
				inconsistent = true
				break
			}
			currentWorlds[world.ID].Name = world.Name
		}
		if inconsistent {
			delay := time.After(1 * time.Minute)
			<-delay
		} else {
			break
		}
	}

	statusListenTo()
	loglevels.Info("Finished updating worlds")

	loglevels.Info("Current Links:")
	var worldList [2]string
	i := 0
	// don't judge me
	for _, world := range currentWorlds {
		if i < 30 {
			worldList[0] += "\n" + fmt.Sprintf("%v", world)
		} else {
			worldList[1] += "\n" + fmt.Sprintf("%v", world)
		}
		i++
	}
	loglevels.Infof("%v", worldList[0])
	loglevels.Infof("%v", worldList[1])
}

func processMatchColor(worlds []int) {
	for _, world := range worlds {
		if world > 10000 {
			continue
		}
		if _, ok := currentWorlds[world]; !ok {
			currentWorlds[world] = &linkInfo{
				ID:     world,
				Linked: worlds,
			}
		}
	}
}

// updateUser updates a single user on all discord servers
func updateUser(userID struct {
	string
	bool
}) {
	redisConn := guildsDatabase.Get()
	defer closeConnection(redisConn)
	data, err := getAccountData(userID)
	processGuild := func(guild string) {
		member, erro := dg.State.Member(guild, userID.string)
		if erro == nil {
			_ = updateUserDataInGuild(member, data, err == nil, userID.bool)
		}
	}

	iterateDatabase(redisConn, processGuild)
}

// getAccountData gets the gw2 account data for a specific discord user
// nolint: gocyclo
func getAccountData(userID struct {
	string
	bool
}) (data gw2AccountData, err error) {
	data = gw2AccountData{
		Name:   "",
		Worlds: []worldWithRank{},
	}
	keys, err := getAPIKeys(userID.string)
	if err != nil {
		return
	}

	i := -1
	// for every api key ...
	for i < len(keys)-1 {
		i++
		key := keys[i]
		// get account data
		account, erro := getCheckedGw2Account(key, userID)
		if erro != nil {
			err = erro
			continue
		}

		_, erro = checkUnique(account.ID, userID.string, false)
		if erro != nil {
			if erro.Error() == AlreadyTaken {
				redisConn := usersDatabase.Get()
				_, erro := redisConn.Do("SREM", userID.string, key)
				closeConnection(redisConn)
				if erro != nil {
					loglevels.Errorf("Error deleting api key from redis: %v", erro)
				}
			}
			continue
		}

		// add the name to the account names
		data.Name += " | " + account.Name

		// add world to users worlds
		data.Worlds = append(data.Worlds, worldWithRank{
			ID:   account.World,
			rank: account.WvWRank,
		})
	}
	// strip the first " | ", on unexpected errors the name can still be empty
	if len(data.Name) >= 3 {
		data.Name = data.Name[3:]
	}
	return
}

// updateUserInGuild gets the account data and updates the user on a specific discord server
func updateUserInGuild(member *discordgo.Member) (err error) {
	data, err := getAccountData(struct {
		string
		bool
	}{string: member.User.ID, bool: true})

	err = updateUserDataInGuild(member, data, err == nil, true)
	return
}

// updateUserDataInGuild updates the user on a specific discord server
func updateUserDataInGuild(member *discordgo.Member, data gw2AccountData, removeWorlds bool, renameUser bool) (err error) {
	options, err := getGuildSettings(member.GuildID)
	if err != nil {
		return
	}

	guildRoles, err := dg.GuildRoles(member.GuildID)
	if err != nil {
		loglevels.Errorf("Error getting guild roles: %v\n", err)
		return
	}

	roles, err := getGuildRoles(member.GuildID, guildRoles)
	if err != nil {
		return
	}

	var worlds []int
	for _, world := range data.Worlds {
		if world.rank >= options.MinimumRank {
			worlds = append(worlds, world.ID)
		}
	}
	if len(worlds) == 0 {
		err = errors.New(fmt.Sprintf("No account from <@%v> meets the wvw rank requirement of this server", member.User.ID))
		return
	}

	if options.RenameUsers && renameUser {
		_ = dg.GuildMemberNickname(member.GuildID, member.User.ID, data.Name) // nolint: errcheck, gosec
	}

	switch options.Mode {
	case allServers:
		err = updateUserToWorldsInGuild(member, worlds, removeWorlds, options, roles, guildRoles)
	case oneServer:
		err = updateUserToVerifyInGuild(member, worlds, removeWorlds, options, options.Gw2ServerID, roles, guildRoles)
	case userBased:
		err = updateUserToUserBasedVerifyInGuild(member, worlds, removeWorlds, options, roles, guildRoles)
	}
	return
}

// updateUserToWorldsInGuild updates the world roles for the user in a specific guild
// nolint: gocyclo
func updateUserToWorldsInGuild(member *discordgo.Member, userWorlds []int, removeWorlds bool, options *guildOptions, roles []guildRole, guildRoles []*discordgo.Role) (err error) {
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
					_ = addGuildRole(member.GuildID, roleStruct) // nolint: errcheck, gosec
					roles = append(roles, roleStruct)
					break
				}
			}
			if !found {
				_, roleStruct, err := createRoleAndAddToManaged(member.GuildID, currentWorlds[world].Name)
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
				_, _, _ = createRoleAndAddToManaged(member.GuildID, world.Name) // nolint: errcheck, gosec
			}
		}
	}

	err = assignManagedRoles(member, roles, wantedRoles, removeWorlds)
	return
}

func createRole(guildID, name string) (newRole *discordgo.Role, err error) {
	newRole, err = dg.GuildRoleCreate(guildID)
	if err != nil {
		loglevels.Errorf("Error creating guild role in guild %v: %v\n", guildID, err)
		return
	}
	newRole, err = dg.GuildRoleEdit(guildID, newRole.ID, name, newRole.Color, newRole.Hoist, newRole.Permissions, newRole.Mentionable)
	if err != nil {
		loglevels.Errorf("Error editing guild role in guild %v: %v\n", guildID, err)
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

func addRole(roleID string, member *discordgo.Member) (err error) {
	if indexOfString(roleID, member.Roles) == -1 {
		err = dg.GuildMemberRoleAdd(member.GuildID, member.User.ID, roleID)
		if err != nil {
			loglevels.Errorf("Error adding dg role %v of guild %v to user %v: %v\n", roleID, member.GuildID, member.User.ID, err)
		}
	}
	return
}

func removeRole(roleID string, member *discordgo.Member, remove bool) (err error) {
	if !remove {
		return
	}
	if indexOfString(roleID, member.Roles) != -1 {
		err = dg.GuildMemberRoleRemove(member.GuildID, member.User.ID, roleID)
		if err != nil {
			loglevels.Errorf("Error removing dg role %v of guild %v to user %v: %v\n", roleID, member.GuildID, member.User.ID, err)
		}
	}
	return
}

func assignManagedRoles(member *discordgo.Member, managedRoles []guildRole, wantedRoles []string, removeRoles bool) (err error) {
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
			err = removeRole(role, member, removeRoles) // nolint: gosec, errcheck
		} else {
			wantedRoles = remove(wantedRoles, index)
		}
	}

	for _, role := range wantedRoles {
		err = addRole(role, member) // nolint: gosec, errcheck
	}
	return
}

// nolint: gocyclo
func updateUserToVerifyInGuild(member *discordgo.Member, worlds []int, removeWorlds bool, options *guildOptions, verifyWorld int, roles []guildRole, guildRoles []*discordgo.Role) (erro error) {
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
				erro = addGuildRole(member.GuildID, roleStruct)
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
				erro = addGuildRole(member.GuildID, roleStruct)
				if erro != nil {
					loglevels.Warningf("Error adding existing linked role %v to managed roles: %v", role.ID, erro)
				}
			}
		}
		if verifiedID == "" {
			_, roleStruct, err := createRoleAndAddToManaged(member.GuildID, "WvW-Verified")
			if err != nil {
				return err
			}
			verifiedID = roleStruct.ID
			roles = append(roles, roleStruct)
		}
		if options.AllowLinked && linkedID == "" {
			_, roleStruct, err := createRoleAndAddToManaged(member.GuildID, "WvW-Linked")
			if err != nil {
				return err
			}
			linkedID = roleStruct.ID
			roles = append(roles, roleStruct)
		}
	}

	linkedWorlds := currentWorlds[verifyWorld].Linked
	additionalWorlds, err := getAdditionalWorlds(member.GuildID)
	if err != nil {
		removeWorlds = false
	} else {
		linkedWorlds = append(linkedWorlds, additionalWorlds...)
	}

	if indexOfInt(verifyWorld, worlds) != -1 {
		wantedRoles = append(wantedRoles, verifiedID)
	} else {
		if options.AllowLinked {
			for _, world := range linkedWorlds {
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

	erro = assignManagedRoles(member, roles, wantedRoles, removeWorlds)
	return
}

func updateUserToUserBasedVerifyInGuild(member *discordgo.Member, worlds []int, removeWorlds bool, options *guildOptions, roles []guildRole, guildRoles []*discordgo.Role) (err error) {
	owner, err := getCachedGw2Account(options.Gw2AccountKey)
	if err != nil {
		return
	}
	err = updateUserToVerifyInGuild(member, worlds, removeWorlds, options, owner.World, roles, guildRoles)
	return
}
