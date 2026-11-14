package slackio

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// RequiredBotScopes are the Bot Token scopes SockDock cannot work without;
// OptionalBotScopes only enable the channel list and choosing people by name
// for "who can run it" (docs/ia.md §3).
var (
	RequiredBotScopes = []string{"channels:history", "groups:history", "chat:write", "reactions:write"}
	OptionalBotScopes = []string{"channels:read", "groups:read", "users:read"}
)

// authTestURL is overridden in tests.
var authTestURL = "https://slack.com/api/auth.test"

// AuthTestScopes calls auth.test with a bot token and returns who it is and
// the scopes it was granted (from the X-OAuth-Scopes header). scopes is nil
// when Slack did not send the header.
func AuthTestScopes(ctx context.Context, botToken string) (Identity, []string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, authTestURL, nil)
	if err != nil {
		return Identity{}, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+botToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Identity{}, nil, err
	}
	defer resp.Body.Close()
	var body struct {
		OK     bool   `json:"ok"`
		Error  string `json:"error"`
		UserID string `json:"user_id"`
		BotID  string `json:"bot_id"`
		Team   string `json:"team"`
		URL    string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Identity{}, nil, err
	}
	if !body.OK {
		return Identity{}, nil, errors.New(body.Error)
	}
	var scopes []string
	if h := resp.Header.Get("X-OAuth-Scopes"); h != "" {
		for _, s := range strings.Split(h, ",") {
			if s = strings.TrimSpace(s); s != "" {
				scopes = append(scopes, s)
			}
		}
	}
	return Identity{UserID: body.UserID, BotID: body.BotID, Team: body.Team, URL: body.URL}, scopes, nil
}

// MissingScopes returns the scopes in want that have does not include.
func MissingScopes(have, want []string) []string {
	set := map[string]bool{}
	for _, s := range have {
		set[s] = true
	}
	var out []string
	for _, s := range want {
		if !set[s] {
			out = append(out, s)
		}
	}
	return out
}
