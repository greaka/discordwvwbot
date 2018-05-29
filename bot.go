package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/gomodule/redigo/redis"
	"github.com/yasvisu/gw2api"

	"github.com/bwmarrin/discordgo"
)

var (
	updateChannel chan string
	dg            *discordgo.Session
	currentWorlds map[int]string
)

func startBot() {
	updateChannel = make(chan string)

	var err error
	dg, err = discordgo.New("Bot " + config.BotToken)
	if err != nil {
		log.Printf("Error connecting to discord: %v\n", err)
		return
	}

	dg.AddHandler(guildCreate)
	dg.AddHandler(guildDelete)
	dg.AddHandler(guildMemberAdd)
	dg.AddHandler(guildMemberRemove)

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

	for {
		updateUser(<-updateChannel)
	}
}

func guildMemberAdd(s *discordgo.Session, m *discordgo.GuildMemberAdd)       {}
func guildMemberRemove(s *discordgo.Session, m *discordgo.GuildMemberRemove) {}
func guildCreate(s *discordgo.Session, m *discordgo.GuildCreate)             {}
func guildDelete(s *discordgo.Session, m *discordgo.GuildDelete)             {}

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

	for _, guild := range guildList {
		_, erro := dg.GuildMember(guild, userID)
		if strings.Contains(erro.Error(), string(discordgo.ErrCodeUnknownMember)) {
			continue
		} else if erro != nil {
			log.Printf("Error getting member %v of guild %v: %v\n", userID, guild, erro)
			continue
		}

		updateUserInGuild(userID, guild)
	}
}

func updateCurrentWorlds() {}

func updateUserInGuild(userID string, guildID string) {
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

	var worlds []string

	for _, key := range keys {
		res, erro := http.Get("https://api.guildwars2.com/v2/account?access_token=" + key)
		if erro != nil {
			log.Printf("Error getting account info: %v\n", erro)
			continue
		}
		defer func() {
			if err = res.Body.Close(); err != nil {
				log.Printf("Error closing config file: %v\n", err)
			}
		}()
		jsonParser := json.NewDecoder(res.Body)
		var account gw2api.Account
		erro = jsonParser.Decode(&account)
		if erro != nil {
			log.Printf("Error parsing json to account data: %v\n", erro)
			continue
		}

		worlds = append(worlds, currentWorlds[account.World])
	}

	updateUserToWorldsInGuild(userID, guildID, worlds)
}

// nolint: gocyclo
func updateUserToWorldsInGuild(userID string, guildID string, worldNames []string) {
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

	for _, role := range member.Roles {
		if getIndexByValue(guildRolesMap[role], currentWorlds) != -1 {
			index := indexOf(guildRolesMap[role], worldNames)
			if index == -1 {
				erro := dg.GuildMemberRoleRemove(guildID, userID, role)
				if erro != nil {
					log.Printf("Error removing guild member role: %v\n", erro)
				}
			} else {
				worldNames = remove(worldNames, index)
			}
		}
	}

	for _, role := range worldNames {
		var roleID string
		if indexOf(role, guildRoleNames) == -1 {
			newRole, err := dg.GuildRoleCreate(guildID)
			if err != nil {
				log.Printf("Error creating guild role: %v\n", err)
				return
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
