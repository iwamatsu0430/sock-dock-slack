package slackio

import (
	"context"
	"strings"

	"github.com/slack-go/slack"

	"github.com/iwamatsu0430/sock-dock-slack/internal/event"
)

// ReactionReader reads what a reaction event leaves out: the post it is on
// and who else reacted. WebAPI implements it; the gateway ignores reactions
// when the API does not.
type ReactionReader interface {
	// Post reads one top-level post or DM (conversations.history). ok is
	// false when there is no such post: it is a reply in a thread, a system
	// message, or gone.
	Post(ctx context.Context, channel, ts string) (m event.Message, ok bool, err error)
	// ReactionUsers lists who has each reaction on a message (reactions.get),
	// by emoji name as Slack gives it (a skin tone is part of the name).
	ReactionUsers(ctx context.Context, channel, ts string) (map[string][]string, error)
}

func (w *WebAPI) Post(ctx context.Context, channel, ts string) (event.Message, bool, error) {
	var resp *slack.GetConversationHistoryResponse
	err := w.retry(ctx, func() error {
		var err error
		resp, err = w.c.GetConversationHistoryContext(ctx, &slack.GetConversationHistoryParameters{
			ChannelID: channel, Oldest: ts, Latest: ts, Inclusive: true, Limit: 1,
		})
		return err
	})
	if err != nil {
		return event.Message{}, false, err
	}
	for _, m := range resp.Messages {
		if m.Timestamp == ts {
			return postOf(channel, m)
		}
	}
	return event.Message{}, false, nil
}

// postOf turns a message from the channel's history into an event. Replies
// shown in the channel and system messages (joins, renames, …) are not posts.
func postOf(channel string, m slack.Message) (event.Message, bool, error) {
	if m.ThreadTimestamp != "" && m.ThreadTimestamp != m.Timestamp {
		return event.Message{}, false, nil
	}
	switch m.SubType {
	case "", "file_share", "bot_message":
	default:
		return event.Message{}, false, nil
	}
	out := event.Message{
		Channel: channel, TS: m.Timestamp, User: m.User, BotID: m.BotID, SubType: m.SubType,
		Text: postText(m),
	}
	for _, f := range m.Files {
		out.Files = append(out.Files, convertFile(f))
	}
	return out, true, nil
}

// postText is the post's text. Posts by other apps often leave text empty
// and put what they say in attachments; then that is the text.
func postText(m slack.Message) string {
	if strings.TrimSpace(m.Text) != "" {
		return m.Text
	}
	var parts []string
	for _, a := range m.Attachments {
		n := len(parts)
		for _, s := range []string{a.Pretext, a.Title, a.Text} {
			if s = strings.TrimSpace(s); s != "" {
				parts = append(parts, s)
			}
		}
		if len(parts) == n && strings.TrimSpace(a.Fallback) != "" {
			parts = append(parts, strings.TrimSpace(a.Fallback))
		}
	}
	return strings.Join(parts, "\n")
}

func (w *WebAPI) ReactionUsers(ctx context.Context, channel, ts string) (map[string][]string, error) {
	var item slack.ReactedItem
	err := w.retry(ctx, func() error {
		var err error
		item, err = w.c.GetReactionsContext(ctx, slack.ItemRef{Channel: channel, Timestamp: ts}, slack.GetReactionsParameters{Full: true})
		return err
	})
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, it := range item.Reactions {
		out[it.Name] = append(out[it.Name], it.Users...)
	}
	return out, nil
}
