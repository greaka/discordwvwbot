package main

import (
	"errors"

	"github.com/bwmarrin/discordgo"
)

// WebhookLogger writes to to specified webhook
type WebhookLogger struct {
	id    string
	token string
}

// Write implements io.Writer and performs the write to the webhook
func (w WebhookLogger) Write(p []byte) (n int, err error) {
	webhookParams := &discordgo.WebhookParams{
		Content: string(p),
	}

	if dg == nil {
		n = 0
		err = errors.New("discordgo session is nil")
		return
	}

	err = dg.WebhookExecute(w.id, w.token, true, webhookParams)
	return
}

// SetOutput sets the webhook to write to
func (w *WebhookLogger) SetOutput(webhookID, token string) {
	w.id = webhookID
	w.token = token
}
