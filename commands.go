package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/greaka/discordwvwbot/loglevels"

	"github.com/bwmarrin/discordgo"
)

func messageReceive(s *discordgo.Session, m *discordgo.MessageCreate) {
	if !strings.HasPrefix(m.Content, ".wvw") {
		return
	}

	_ = s.ChannelTyping(m.ChannelID) // nolint: errcheck, gosec

	mes := strings.Trim(m.Content[4:], " ")

	// remember to add new functions to the help doc
	switch {
	case strings.HasPrefix(mes, "help"):
		printHelp(m)
	case strings.HasPrefix(mes, "addkey"):
		key := strings.Trim(mes[6:], " ")
		addKey(m, key)
	case strings.HasPrefix(mes, "purge"):
		relink := strings.Trim(mes[5:], " ")
		purgeGuild(m, relink)
	case strings.HasPrefix(mes, "check"):
		printUserWorlds(m, strings.Trim(mes[5:], " "))
	case strings.HasPrefix(mes, "verify"):
		userID := strings.Trim(mes[6:], " ")
		commandVerifyUser(m, userID)
	case strings.HasPrefix(mes, "kill"):
		if isOwner(m, true) {
			os.Exit(1)
		}
	case strings.HasPrefix(mes, "allow"):
		server := strings.Trim(mes[5:], " ")
		commandAddServer(m, server)
	case strings.HasPrefix(mes, "deletealldata"):
		commandDeleteAllData(m)
	}
}

func printHelp(m *discordgo.MessageCreate) {
	_, err := dg.ChannelMessageSend(m.ChannelID, `available commands:
	> **help**
	prints this message
	
	> **addkey** `+"`API-KEY`"+`
	adds an api key to the bot. Use it like this:
	`+"`.wvw addkey XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXXXXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX`"+`

	> **verify**
	re-verifies you on all servers

	> **deletealldata**
    Deletes all data associated with your Discord account.
    The bot will not know about you anymore after using this command.

__Commands requiring  `+"`Manage Roles`"+` permission__
	
	> **purge**
	removes roles from players that were manually verified

	> **purge** linked
	like purge, but only for linked servers

	> **verify** `+"`discordUserId`"+`
	verifies a user in your discord server

	> **check** `+"`discordUserId`"+`
	shows the worlds, account names and wvw ranks of the user

	> **allow** `+"`serverName`"+`
	Sets a server as an additional linked server for 24h.
    You can add as many servers as you want. The time will reset to 24h for all additional servers.
	`)
	if err != nil {
		loglevels.Errorf("Failed to send help message to user %v: %v", m.Author.ID, err)
	}
}

func addKey(m *discordgo.MessageCreate, key string) {
	_ = dg.ChannelMessageDelete(m.ChannelID, m.Message.ID) // nolint: errcheck, gosec

	err := checkKey(key, m.Author.ID)
	if err != nil {
		_, erro := dg.ChannelMessageSend(m.ChannelID, m.Author.Mention()+fmt.Sprintf(" %v", err))
		if erro != nil {
			loglevels.Errorf("Failed to send key error message to user %v: %v", m.Author.ID, erro)
		}
		return
	}

	err = addUserKey(m.Author.ID, key)
	if err != nil {
		_, erro := dg.ChannelMessageSend(m.ChannelID, m.Author.Mention()+fmt.Sprintf(" %v", err))
		if erro != nil {
			loglevels.Errorf("Failed to send key save failed message to user %v: %v", m.Author.ID, erro)
		}
		return
	}

	sendSuccess(m)
}

// nolint: gocyclo
func purgeGuild(m *discordgo.MessageCreate, relink string) {
	roles, allowed := isManagerOfRoles(m, true)
	if !allowed {
		return
	}

	authRoles, err := getGuildRoles(m.GuildID, roles)
	if err != nil {
		sendError(m)
		return
	}

	if relink == "linked" {
		for _, role := range authRoles {
			if role.Name == "WvW-Linked" {
				authRoles = authRoles[:0]
				authRoles = append(authRoles, role)
				break
			}
		}
	}

	tempMap := make(map[string]*discordgo.Member)
	for k, v := range guildMembers[m.GuildID] {
		for _, role := range authRoles {
			for _, memberRole := range v.Roles {
				if memberRole == role.ID {
					tempMap[k] = v
					break
				}
			}
		}
	}

	redisConn := usersDatabase.Get()
	processValue := func(userID string) {
		if _, ok := tempMap[userID]; ok {
			delete(tempMap, userID)
		}
	}
	_ = iterateDatabase(redisConn, processValue)
	closeConnection(redisConn)

	for userID := range tempMap {
		for _, role := range authRoles {
			erro := dg.GuildMemberRoleRemove(m.GuildID, userID, role.ID)
			if erro != nil {
				err = erro
			}
		}
	}

	if err != nil {
		loglevels.Warningf("Error purging guild %v: %v", m.GuildID, err)
		_, erro := dg.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" Completed with errors.")
		if erro != nil {
			loglevels.Errorf("Failed to send partial success message to user %v: %v", m.Author.ID, erro)
		}
		return
	}

	sendSuccess(m)
}

func isManagerOfRoles(m *discordgo.MessageCreate, sendOnFailure bool) (roles []*discordgo.Role, found bool) {
	member := m.Member
	roles, err := dg.GuildRoles(m.GuildID)
	if err != nil {
		loglevels.Warningf("Error getting roles for guild %v: %v", m.GuildID, err)
		sendError(m)
		return
	}

	var rolesWithPermissions []*discordgo.Role
	for _, role := range roles {
		if role.Permissions&discordgo.PermissionManageRoles == discordgo.PermissionManageRoles {
			rolesWithPermissions = append(rolesWithPermissions, role)
		}
	}

	found = false
	for _, roleID := range member.Roles {
		for _, role := range rolesWithPermissions {
			if role.ID == roleID {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found && sendOnFailure {
		_, erro := dg.ChannelMessageSend(m.ChannelID, m.Author.Mention()+", you are missing the permission `Manage Roles` to perform this operation.")
		if erro != nil {
			loglevels.Errorf("Failed to send error message to user %v: %v", m.Author.ID, erro)
		}
	}
	return
}

func isOwner(m *discordgo.MessageCreate, sendOnFailure bool) bool {
	if config.Owner != m.Author.ID && sendOnFailure {
		_, erro := dg.ChannelMessageSend(m.ChannelID, m.Author.Mention()+", you need to be bot owner to use this command.")
		if erro != nil {
			loglevels.Errorf("Failed to send error message to user %v: %v", m.Author.ID, erro)
		}
	}
	return config.Owner == m.Author.ID
}

func printUserWorlds(m *discordgo.MessageCreate, userID string) {
	userID = trimMention(userID)

	allowed := false
	if isOwner(m, false) {
		allowed = true
	}
	_, manager := isManagerOfRoles(m, false)
	if manager {
		_, ok := getMember(m.GuildID, userID)
		if ok {
			allowed = true
		} else {
			_, _ = dg.ChannelMessageSend(m.ChannelID, "<@"+userID+"> is not a user in your discord server.")
		}
	}
	if !allowed {
		_, erro := dg.ChannelMessageSend(m.ChannelID, m.Author.Mention()+", you have not enough permissions to use this command.")
		if erro != nil {
			loglevels.Errorf("Failed to send error message to user %v: %v", m.Author.ID, erro)
		}
		return
	}

	data, err := getAccountData(struct {
		string
		bool
	}{string: userID, bool: true})

	worldNames := ""
	worldRanks := ""
	for _, world := range data.Worlds {
		worldNames += " | " + currentWorlds[world.ID].Name
		worldRanks += " | " + fmt.Sprintf("%v", world.rank)
	}
	if len(worldNames) >= 3 {
		worldNames = worldNames[3:]
	}
	if len(worldRanks) >= 3 {
		worldRanks = worldRanks[3:]
	}

	errMes := "nil"
	if err != nil {
		errMes = err.Error()
	}

	us := m.Author
	user, err := dg.User(userID)
	if user != nil {
		us = user
	}

	mention := us.Mention()
	_, erro := dg.ChannelMessageSend(m.ChannelID, mention+"\naccount names: "+data.Name+"\nworlds: "+worldNames+"\nranks: "+worldRanks+"\nerr: "+errMes)
	if erro != nil {
		loglevels.Errorf("Failed to send info message to user %v: %v", m.Author.ID, erro)
	}
}

func commandVerifyUser(m *discordgo.MessageCreate, userID string) {
	if len(userID) == 0 {
		userID = m.Author.ID
	} else {
		_, allowed := isManagerOfRoles(m, true)
		if !allowed {
			return
		}
	}

	userID = trimMention(userID)

	member, err := dg.GuildMember(m.GuildID, userID)
	if err != nil {
		sendError(m)
		return
	}

	member.GuildID = m.GuildID
	err = updateUserInGuild(member)
	if err != nil {
		sendErrorMes(m, err.Error())
		return
	}
	sendSuccess(m)
}

func commandAddServer(m *discordgo.MessageCreate, server string) {
	_, allowed := isManagerOfRoles(m, true)
	if !allowed {
		return
	}

	s := removeSpecial(server)

	world := getWorldByName(s)
	if world == -1 {
		sendErrorMes(m, "Could not find a world with the given name.")
		return
	}

	err := addAdditionalWorld(m.GuildID, world)
	if err != nil {
		sendError(m)
		return
	}

	sendSuccess(m)
}

func commandDeleteAllData(m *discordgo.MessageCreate) {
	err := deleteAllData(m.Author.ID)
	if err != nil {
		sendError(m)
		return
	}
	sendSuccess(m)
}

func sendError(m *discordgo.MessageCreate) {
	_, erro := dg.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" Internal error, please try again or contact me.")
	if erro != nil {
		loglevels.Errorf("Failed to send error message to user %v: %v", m.Author.ID, erro)
	}
}

func sendErrorMes(m *discordgo.MessageCreate, mes string) {
	_, erro := dg.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" "+mes)
	if erro != nil {
		loglevels.Errorf("Failed to send error message to user %v: %v", m.Author.ID, erro)
	}
}

func sendSuccess(m *discordgo.MessageCreate) {
	_, erro := dg.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" Success")
	if erro != nil {
		loglevels.Errorf("Failed to send success message to user %v: %v", m.Author.ID, erro)
	}
}
