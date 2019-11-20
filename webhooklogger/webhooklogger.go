package webhooklogger

import (
	"errors"

	"github.com/bwmarrin/discordgo"
)

// WebhookLogger writes to the specified webhook
type WebhookLogger struct {
	id      string
	token   string
	session *discordgo.Session
}

// Write implements io.Writer and performs the write to the webhook
func (w WebhookLogger) Write(p []byte) (n int, err error) {
	content := string(p)
	webhookParams := &discordgo.WebhookParams{
		Content: "`" + content + "`",
	}

	if w.session == nil {
		n = 0
		err = errors.New("discordgo session is nil")
		return
	}

	_, err = w.session.WebhookExecute(w.id, w.token, true, webhookParams)
	// io.Writer specifies that the number of written characters has to be returned
	if err != nil {
		n = 0
	} else {
		n = len(content)
	}
	return
}

// SetOutput sets the webhook to write to
func (w *WebhookLogger) SetOutput(dg *discordgo.Session, webhookID, token string) {
	w.id = webhookID
	w.token = token
	w.session = dg
}
