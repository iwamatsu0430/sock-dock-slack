package control

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iwamatsu0430/sock-dock-slack/internal/slackio"
)

// channelAPI lists one channel.
type channelAPI struct{ memberAPI }

func (channelAPI) Channels(context.Context) ([]slackio.Channel, error) {
	return []slackio.Channel{{ID: "C0123ABCD", Name: "dev-help"}}, nil
}
func (channelAPI) ChannelInfo(context.Context, string) (slackio.Channel, error) {
	return slackio.Channel{ID: "C0123ABCD", Name: "dev-help"}, nil
}

// scopeAPI also reports Bot Token scopes without im:history.
type scopeAPI struct{ channelAPI }

func (scopeAPI) Scopes(context.Context) ([]string, error) {
	return []string{"chat:write", "channels:history"}, nil
}

func TestOwnerEndpoints(t *testing.T) {
	ctx := context.Background()
	c, dir := startWith(t, channelAPI{})

	info, err := c.Owner(ctx)
	if err != nil || info.Owner != nil || info.Registration.Active || info.Problem != "" {
		t.Fatalf("owner = %+v, %v", info, err)
	}
	info, err = c.StartRegistration(ctx)
	if err != nil || !info.Registration.Active || len(info.Registration.Code) != 6 || info.Registration.NextChange == nil {
		t.Fatalf("registration = %+v, %v", info, err)
	}
	if info, _ = c.CancelRegistration(ctx); info.Registration.Active {
		t.Error("registration should be cancelled")
	}

	if _, err := c.SetNotify(ctx, "notify_dm_failures", true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SetNotify(ctx, "owner", true); err == nil {
		t.Error("only the DM notice settings can be set")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if !strings.Contains(string(data), "notify_dm_failures: true") {
		t.Errorf("setting not written:\n%s", data)
	}
	if info, _ = c.Owner(ctx); !info.NotifyDMFailures {
		t.Errorf("owner info = %+v", info)
	}

	// DMs come first in the channel list.
	chs, err := c.Channels(ctx)
	if err != nil || len(chs.Channels) == 0 || chs.Channels[0].ID != "dm" || chs.Channels[0].Problem != "" {
		t.Fatalf("channels = %+v, %v", chs, err)
	}
}

func TestOwnerWithoutIMHistory(t *testing.T) {
	ctx := context.Background()
	c, _ := startWith(t, scopeAPI{})
	if info, _ := c.Owner(ctx); info.Problem != "missing_scope" {
		t.Errorf("owner problem = %q", info.Problem)
	}
	if chs, _ := c.Channels(ctx); chs.Channels[0].Problem != "missing_scope" {
		t.Errorf("DM entry = %+v", chs.Channels[0])
	}
}

func TestMatchDMAndMention(t *testing.T) {
	ctx := context.Background()
	c, dir := startWith(t, memberAPI{})
	content := "routes:\n" +
		"  - name: todo\n    channel: dm\n    starts_with: todo\n    handler: {type: command, cwd: " + dir + ", command: /bin/echo}\n" +
		"  - name: review\n    channel: C0123ABCD\n    starts_with: review\n    mention_only: true\n    handler: {type: command, cwd: " + dir + ", command: /bin/echo}\n"
	if err := c.PutConfig(ctx, content); err != nil {
		t.Fatal(err)
	}
	if m, _ := c.Match(ctx, MatchRequest{Channel: "dm", Text: "todo milk"}); !m.Matched || m.Route != "todo" {
		t.Errorf("dm: %+v", m)
	}
	if m, _ := c.Match(ctx, MatchRequest{Channel: "C0123ABCD", Text: "review x"}); m.Matched {
		t.Errorf("without a mention: %+v", m)
	}
	if m, _ := c.Match(ctx, MatchRequest{Channel: "C0123ABCD", Text: "review x", Mentioned: true}); !m.Matched {
		t.Errorf("mentioned: %+v", m)
	}
}
