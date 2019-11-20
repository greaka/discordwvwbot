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
		_, erro := dg.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" Internal error, please try again or contact me.")
		if erro != nil {
			loglevels.Errorf("Failed to send error message to user %v: %v", m.Author.ID, err)
		}
		return
	}

	// check if api key contains wvwbot
	nameInLower := strings.ToLower(token.Name)
	if !strings.Contains(nameInLower, "wvw") || !strings.Contains(nameInLower, "bot") {
		_, erro := dg.ChannelMessageSend(m.ChannelID, m.Author.Mention()+fmt.Sprintf(" This api key is not valid. Make sure your key name contains 'wvwbot'. This api key is named %v", token.Name))
		if erro != nil {
			loglevels.Errorf("Failed to send invalid key message to user %v: %v", m.Author.ID, err)
		}
		return
	}

	err = addUserKey(m.Author.ID, key)
	if err != nil {
		_, erro := dg.ChannelMessageSend(m.ChannelID, m.Author.Mention()+fmt.Sprintf(" %v", err))
		if erro != nil {
			loglevels.Errorf("Failed to send key save failed message to user %v: %v", m.Author.ID, err)
		}
		return
	}

	_, err = dg.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" Success")
	if err != nil {
		loglevels.Errorf("Failed to send success message to user %v: %v", m.Author.ID, err)
	}
}

// nolint: gocyclo
func purgeGuild(m *discordgo.MessageCreate) {
	member := m.Member
	roles, err := dg.GuildRoles(m.GuildID)
	if err != nil {
		loglevels.Warningf("Error getting roles for guild %v: %v", m.GuildID, err)
		_, erro := dg.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" Internal error, please try again or contact me.")
		if erro != nil {
			loglevels.Errorf("Failed to send error message to user %v: %v", m.Author.ID, err)
		}
		return
	}

	var rolesWithPermissions []*discordgo.Role
	for _, role := range roles {
		if role.Permissions&discordgo.PermissionManageRoles == discordgo.PermissionManageRoles {
			rolesWithPermissions = append(rolesWithPermissions, role)
		}
	}

	found := false
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
			loglevels.Errorf("Failed to send error message to user %v: %v", m.Author.ID, err)
		}
		return
	}

	authRoles, err := getGuildRoles(m.GuildID, roles)
	if err != nil {
		_, erro := dg.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" Internal error, please try again or contact me.")
		if erro != nil {
			loglevels.Errorf("Failed to send error message to user %v: %v", m.Author.ID, err)
		}
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
			loglevels.Errorf("Failed to send partial success message to user %v: %v", m.Author.ID, err)
		}
		return
	}

	_, erro := dg.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" Success.")
	if erro != nil {
		loglevels.Errorf("Failed to send success message to user %v: %v", m.Author.ID, err)
	}
}
