package slackio

import (
	"context"
	"sort"

	"github.com/slack-go/slack"
)

// Channel is a Slack channel the bot is in.
type Channel struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Private bool   `json:"private"`
}

// ChannelLister lists channels (needs the channels:read and groups:read scopes).
type ChannelLister interface {
	Channels(ctx context.Context) ([]Channel, error)
	ChannelInfo(ctx context.Context, id string) (Channel, error)
}

// Channels returns the channels the bot is a member of, sorted by name.
func (w *WebAPI) Channels(ctx context.Context) ([]Channel, error) {
	out := []Channel{}
	cursor := ""
	for range 10 {
		var (
			chs  []slack.Channel
			next string
		)
		err := w.retry(ctx, func() error {
			var err error
			chs, next, err = w.c.GetConversationsForUserContext(ctx, &slack.GetConversationsForUserParameters{
				Types: []string{"public_channel", "private_channel"}, ExcludeArchived: true, Limit: 200, Cursor: cursor,
			})
			return err
		})
		if err != nil {
			return out, err
		}
		for _, ch := range chs {
			out = append(out, Channel{ID: ch.ID, Name: ch.Name, Private: ch.IsPrivate})
		}
		if next == "" {
			break
		}
		cursor = next
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ChannelInfo looks up one channel by ID.
func (w *WebAPI) ChannelInfo(ctx context.Context, id string) (Channel, error) {
	var info *slack.Channel
	err := w.retry(ctx, func() error {
		var err error
		info, err = w.c.GetConversationInfoContext(ctx, &slack.GetConversationInfoInput{ChannelID: id})
		return err
	})
	if err != nil {
		return Channel{ID: id}, err
	}
	return Channel{ID: id, Name: info.Name, Private: info.IsPrivate}, nil
}
