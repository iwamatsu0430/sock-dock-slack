// Package slackio talks to Slack: the Web API calls sockdock needs and the
// Socket Mode connection.
package slackio

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/slack-go/slack"
)

// API is the subset of the Slack Web API sockdock uses.
type API interface {
	PostMessage(ctx context.Context, channel, threadTS, text string) error
	AddReaction(ctx context.Context, channel, ts, name string) error
	RemoveReaction(ctx context.Context, channel, ts, name string) error
	Permalink(ctx context.Context, channel, ts string) (string, error)
	AuthTest(ctx context.Context) (Identity, error)
}

// Identity is who the bot token belongs to.
type Identity struct {
	UserID string `json:"user_id"`
	BotID  string `json:"bot_id"`
	Team   string `json:"team"`
	URL    string `json:"url"`
}

// WebAPI implements API with slack-go.
type WebAPI struct {
	c          *slack.Client
	botToken   string
	maxRetries int
}

// ScopeLister reports the Bot Token's scopes.
type ScopeLister interface {
	Scopes(ctx context.Context) ([]string, error)
}

// Scopes returns the scopes granted to the bot token (nil when Slack does
// not say).
func (w *WebAPI) Scopes(ctx context.Context) ([]string, error) {
	_, scopes, err := AuthTestScopes(ctx, w.botToken)
	return scopes, err
}

// NewWebAPI creates a Web API client for the bot token. The app-level token is
// attached so the same client can open Socket Mode connections.
func NewWebAPI(botToken, appToken string) *WebAPI {
	return &WebAPI{
		c:          slack.New(botToken, slack.OptionAppLevelToken(appToken)),
		botToken:   botToken,
		maxRetries: 3,
	}
}

// Client exposes the underlying slack-go client (for Socket Mode).
func (w *WebAPI) Client() *slack.Client { return w.c }

func (w *WebAPI) PostMessage(ctx context.Context, channel, threadTS, text string) error {
	return w.retry(ctx, func() error {
		opts := []slack.MsgOption{slack.MsgOptionText(text, false), slack.MsgOptionDisableLinkUnfurl()}
		if threadTS != "" {
			opts = append(opts, slack.MsgOptionTS(threadTS))
		}
		_, _, err := w.c.PostMessageContext(ctx, channel, opts...)
		return err
	})
}

func (w *WebAPI) AddReaction(ctx context.Context, channel, ts, name string) error {
	err := w.retry(ctx, func() error {
		return w.c.AddReactionContext(ctx, name, slack.ItemRef{Channel: channel, Timestamp: ts})
	})
	if isSlackError(err, "already_reacted") {
		return nil
	}
	return err
}

func (w *WebAPI) RemoveReaction(ctx context.Context, channel, ts, name string) error {
	err := w.retry(ctx, func() error {
		return w.c.RemoveReactionContext(ctx, name, slack.ItemRef{Channel: channel, Timestamp: ts})
	})
	if isSlackError(err, "no_reaction") || isSlackError(err, "message_not_found") {
		return nil
	}
	return err
}

func (w *WebAPI) Permalink(ctx context.Context, channel, ts string) (string, error) {
	var link string
	err := w.retry(ctx, func() error {
		var err error
		link, err = w.c.GetPermalinkContext(ctx, &slack.PermalinkParameters{Channel: channel, Ts: ts})
		return err
	})
	return link, err
}

func (w *WebAPI) AuthTest(ctx context.Context) (Identity, error) {
	resp, err := w.c.AuthTestContext(ctx)
	if err != nil {
		return Identity{}, err
	}
	return Identity{UserID: resp.UserID, BotID: resp.BotID, Team: resp.Team, URL: resp.URL}, nil
}

// TestAppToken checks an app-level token by requesting a Socket Mode
// connection URL (this also fails when Socket Mode is disabled for the app).
func TestAppToken(ctx context.Context, appToken string) error {
	_, _, err := slack.New("", slack.OptionAppLevelToken(appToken)).StartSocketModeContext(ctx)
	return err
}

// retry retries rate-limited calls, honoring Retry-After.
func (w *WebAPI) retry(ctx context.Context, f func() error) error {
	var err error
	for attempt := 0; attempt <= w.maxRetries; attempt++ {
		err = f()
		var rl *slack.RateLimitedError
		if !errors.As(err, &rl) {
			return err
		}
		wait := rl.RetryAfter
		if wait <= 0 {
			wait = time.Second
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	return err
}

func isSlackError(err error, code string) bool {
	return err != nil && strings.Contains(err.Error(), code)
}
