package main

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
)

func getDashboardTemplate(guildID, userID, state string) (db dashboardTemplate, err error) {
	settings, err := getGuildSettings(guildID)
	if err != nil {
		return
	}

	worlds := getCurrentWorlds(settings.Gw2ServerID)

	guilds, err := getDiscordServersTemplate(userID, guildID, state)
	if err != nil {
		return
	}

	accounts, err := getGw2Accounts(userID, settings.Gw2AccountKey)
	if err != nil {
		return
	}

	return mergeToDashboardTemplate(settings, worlds, guilds, accounts), err
}

// getCurrentWorlds uses currentWorlds and builds a []serversTemplate
func getCurrentWorlds(worldID int) (st []serversTemplate) {
	st = make([]serversTemplate, 0, len(currentWorlds))
	for _, world := range currentWorlds {
		st = append(st, serversTemplate{
			ID:     fmt.Sprintf("%v", world.ID),
			Name:   world.Name,
			Active: world.ID == worldID,
		})
	}
	return
}

func getDiscordServersTemplate(user, guildID, state string) (st []serversTemplate, err error) {
	servers, err := getDiscordServers(user)
	if err != nil {
		return
	}

	st = make([]serversTemplate, 0, len(servers))
	for _, server := range servers {
		if server.Permissions&discordgo.PermissionManageRoles == discordgo.PermissionManageRoles {
			st = append(st, serversTemplate{
				ID:     server.ID,
				Name:   server.Name,
				Active: server.ID == guildID,
				State:  state,
			})
		}
	}

	return
}

func getGw2Accounts(user, activeKey string) (at []accountTemplate, err error) {
	var keys []string
	keys, err = getAPIKeys(user)
	if err != nil {
		return
	}

	at = make([]accountTemplate, 0, len(keys)+1)
	ownsActiveKey := false
	var account gw2Account

	for _, key := range keys {
		if key == activeKey {
			ownsActiveKey = true
		}

		account, err = getGw2Account(key)
		if err != nil {
			return
		}

		at = append(at, accountTemplate{
			APIKey: key,
			Name:   account.Name,
			Active: key == activeKey,
		})
	}

	if !ownsActiveKey && activeKey != "" {
		account, err = getGw2Account(activeKey)
		if err != nil {
			return
		}

		at = append(at, accountTemplate{
			APIKey: activeKey,
			Name:   account.Name,
			Active: true,
		})
	}
	return
}

func mergeToDashboardTemplate(options *guildOptions, worlds, guilds []serversTemplate, accounts []accountTemplate) dashboardTemplate {
	return dashboardTemplate{
		Accounts:       accounts,
		DiscordServers: guilds,
		Gw2Servers:     worlds,
		AllowLinked:    options.AllowLinked,
		CreateRoles:    options.CreateRoles,
		DeleteLinked:   options.DeleteLinked,
		Mode:           options.Mode,
		RenameUsers:    options.RenameUsers,
		VerifyOnly:     options.VerifyOnly,
		MinimumRank:    options.MinimumRank,
	}
}
