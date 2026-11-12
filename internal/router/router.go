// Package router decides which incoming messages sockdock should look at.
package router

import "github.com/iwamatsu0430/sock-dock-slack/internal/event"

// allowedSubtypes are message subtypes that still count as a user's message.
var allowedSubtypes = map[string]bool{
	"":                 true,
	"file_share":       true,
	"thread_broadcast": true,
}

// Self identifies the bot so its own messages can be ignored.
type Self struct {
	UserID string
	BotID  string
}

// Accept reports whether a message should be considered for routing. It drops
// bot messages (including our own), edits, deletions and other system subtypes.
func Accept(m event.Message, self Self) bool {
	if m.Channel == "" || m.TS == "" {
		return false
	}
	if !allowedSubtypes[m.SubType] {
		return false
	}
	if m.BotID != "" {
		return false
	}
	if self.UserID != "" && m.User == self.UserID {
		return false
	}
	if self.BotID != "" && m.BotID == self.BotID {
		return false
	}
	return true
}

// ThreadKey identifies a thread across channels.
func ThreadKey(channel, threadTS string) string {
	return channel + ":" + threadTS
}
