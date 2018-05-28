package main

import (
	"log"

	"github.com/bwmarrin/discordgo"
)

func startBot() {
	dg, err := discordgo.New("Bot " + config.BotToken)
	if err != nil {
		log.Fatalf("Error connecting to discord: ", err)
		return
	}
}
