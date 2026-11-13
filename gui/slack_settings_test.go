package main

import (
	"slices"
	"testing"

	"github.com/goccy/go-yaml"

	"github.com/iwamatsu0430/sock-dock-slack/assets"
	"github.com/iwamatsu0430/sock-dock-slack/internal/slackio"
)

type manifestParts struct {
	OAuthConfig struct {
		Scopes struct {
			Bot []string `yaml:"bot"`
		} `yaml:"scopes"`
	} `yaml:"oauth_config"`
	Settings struct {
		EventSubscriptions struct {
			BotEvents []string `yaml:"bot_events"`
		} `yaml:"event_subscriptions"`
		SocketModeEnabled bool `yaml:"socket_mode_enabled"`
	} `yaml:"settings"`
}

// The settings copied for an existing app must match the bundled manifest,
// and the scopes the app requires must be in it.
func TestSlackSettingsSnippetMatchesManifest(t *testing.T) {
	var snip, man manifestParts
	if err := yaml.Unmarshal([]byte(slackSettingsSnippet), &snip); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal([]byte(assets.SlackAppManifest), &man); err != nil {
		t.Fatal(err)
	}
	sorted := func(s []string) []string { s = slices.Clone(s); slices.Sort(s); return s }
	if !slices.Equal(sorted(snip.OAuthConfig.Scopes.Bot), sorted(man.OAuthConfig.Scopes.Bot)) {
		t.Errorf("scopes: snippet %v, manifest %v", snip.OAuthConfig.Scopes.Bot, man.OAuthConfig.Scopes.Bot)
	}
	if !slices.Equal(sorted(snip.Settings.EventSubscriptions.BotEvents), sorted(man.Settings.EventSubscriptions.BotEvents)) {
		t.Errorf("events: snippet %v, manifest %v", snip.Settings.EventSubscriptions.BotEvents, man.Settings.EventSubscriptions.BotEvents)
	}
	if !snip.Settings.SocketModeEnabled || !man.Settings.SocketModeEnabled {
		t.Error("socket mode must be on in both")
	}
	for _, s := range append(slices.Clone(slackio.RequiredBotScopes), slackio.OptionalBotScopes...) {
		if !slices.Contains(man.OAuthConfig.Scopes.Bot, s) {
			t.Errorf("%s is not in the manifest", s)
		}
	}
}
