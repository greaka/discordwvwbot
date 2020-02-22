package main

import (
	"fmt"
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
		purgeGuild(m)
	case strings.HasPrefix(mes, "check"):
		printUserWorlds(m, strings.Trim(mes[5:], " "))
	case strings.HasPrefix(mes, "verify"):
		userID := strings.Trim(mes[6:], " ")
		commandVerifyUser(m, userID)
	}
}

func printHelp(m *discordgo.MessageCreate) {
	_, err := dg.ChannelMessageSend(m.ChannelID, `available commands:
	> **help**
	prints this message
	
	> **addkey**
	adds an api key to the bot. Use it like this:
	`+"`.wvw addkey XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXXXXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX`"+`
	
	> **purge**
	removes roles from players that were manually verified

	> **verify**
	re-verifies you on all servers

	> **verify** `+"`discordUserId`"+`
	verifies a user in your discord server. Needs manage roles permission
	`)
	if err != nil {
		loglevels.Errorf("Failed to send help message to user %v: %v", m.Author.ID, err)
	}
}

func addKey(m *discordgo.MessageCreate, key string) {
	_ = dg.ChannelMessageDelete(m.ChannelID, m.Message.ID) // nolint: errcheck, gosec

	// check if api key is valid
	token, err := getTokenInfo(key)
	if err != nil {
		_, erro := dg.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" error communicating with the gw2api, please try again or wait until the api is working again.")
		if erro != nil {
			loglevels.Errorf("Failed to send error message to user %v: %v", m.Author.ID, erro)
		}
		return
	}

	// check if api key contains wvwbot
	nameInLower := strings.ToLower(token.Name)
	if !strings.Contains(nameInLower, "wvw") || !strings.Contains(nameInLower, "bot") {
		text := "This api key is named " + token.Name
		if token.Name == "" {
			text = "This api key does not have a name."
		}
		_, erro := dg.ChannelMessageSend(m.ChannelID, m.Author.Mention()+fmt.Sprintf(" This api key is not valid. Make sure your key name contains 'wvwbot'. %v\nPlease create a new key with a valid name", text))
		if erro != nil {
			loglevels.Errorf("Failed to send invalid key message to user %v: %v", m.Author.ID, erro)
		}
		return
	}

	if indexOfString("progression", token.Permissions) == -1 {
		_, erro := dg.ChannelMessageSend(m.ChannelID, m.Author.Mention()+fmt.Sprintf(" This api key is not valid. Please give it the permission 'progression'"))
		if erro != nil {
			loglevels.Errorf("Failed to send invalid key message to user %v: %v", m.Author.ID, erro)
		}
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
func purgeGuild(m *discordgo.MessageCreate) {
	roles, allowed := isManagerOfRoles(m)
	if !allowed {
		return
	}

	authRoles, err := getGuildRoles(m.GuildID, roles)
	if err != nil {
		sendError(m)
		return
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

func isManagerOfRoles(m *discordgo.MessageCreate) (roles []*discordgo.Role, found bool) {
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
	if !found {
		_, erro := dg.ChannelMessageSend(m.ChannelID, m.Author.Mention()+", you are missing the permission `Manage Roles` to perform this operation.")
		if erro != nil {
			loglevels.Errorf("Failed to send error message to user %v: %v", m.Author.ID, erro)
		}
	}
	return
}

func isOwner(m *discordgo.MessageCreate) bool {
	if config.Owner != m.Author.ID {
		_, erro := dg.ChannelMessageSend(m.ChannelID, m.Author.Mention()+", you need to be bot owner to use this command.")
		if erro != nil {
			loglevels.Errorf("Failed to send error message to user %v: %v", m.Author.ID, erro)
		}
	}
	return config.Owner == m.Author.ID
}

func printUserWorlds(m *discordgo.MessageCreate, userID string) {
	if !isOwner(m) {
		return
	}

	userName, worlds, err := getAccountData(struct {
		string
		bool
	}{string: userID, bool: true})

	worldNames := ""
	for _, world := range worlds {
		worldNames += " | " + currentWorlds[world].Name
	}
	if len(worldNames) >= 3 {
		worldNames = worldNames[3:]
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
	_, erro := dg.ChannelMessageSend(m.ChannelID, mention+"\naccount names: "+userName+"\nworlds: "+worldNames+"\nerr: "+errMes)
	if erro != nil {
		loglevels.Errorf("Failed to send info message to user %v: %v", m.Author.ID, erro)
	}
}

func commandVerifyUser(m *discordgo.MessageCreate, userID string) {
	if len(userID) == 0 {
		updateUserChannel <- struct {
			string
			bool
		}{string: m.Author.ID, bool: true}
		sendSuccess(m)
		return
	}

	_, allowed := isManagerOfRoles(m)
	if !allowed {
		return
	}

	member, err := dg.GuildMember(m.GuildID, userID)
	if err != nil {
		sendError(m)
		return
	}

	member.GuildID = m.GuildID
	err = updateUserInGuild(member)
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

func sendSuccess(m *discordgo.MessageCreate) {
	_, erro := dg.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" Success")
	if erro != nil {
		loglevels.Errorf("Failed to send success message to user %v: %v", m.Author.ID, erro)
	}
}
