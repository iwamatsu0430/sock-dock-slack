package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMCPSetup(t *testing.T) {
	cli := "/Users/me/My Apps/SockDock.app/Contents/MacOS/sockdock"
	want := map[string]string{
		"claude-code": "claude mcp add -s user sockdock -- '" + cli + "' mcp",
		"codex":       "codex mcp add sockdock -- '" + cli + "' mcp",
		"gemini":      "gemini mcp add -s user sockdock '" + cli + "' mcp",
	}
	for client, text := range want {
		s, err := mcpSetupFor(client, cli)
		if err != nil || s.Kind != "command" || s.Text != text {
			t.Errorf("%s = %+v, %v", client, s, err)
		}
	}
	vs, _ := mcpSetupFor("vscode", "/usr/local/bin/sockdock")
	if vs.Text != `code --add-mcp '{"args":["mcp"],"command":"/usr/local/bin/sockdock","name":"sockdock"}'` {
		t.Errorf("vscode = %q", vs.Text)
	}
	for _, client := range []string{"claude-desktop", "cursor", "other"} {
		s, err := mcpSetupFor(client, cli)
		var v struct {
			MCPServers map[string]struct {
				Command string   `json:"command"`
				Args    []string `json:"args"`
			} `json:"mcpServers"`
		}
		if err != nil || s.Kind != "config" || json.Unmarshal([]byte(s.Text), &v) != nil || v.MCPServers["sockdock"].Command != cli {
			t.Errorf("%s = %+v, %v", client, s, err)
		}
		if (client == "other") != (s.File == "") {
			t.Errorf("%s file = %q", client, s.File)
		}
	}
	if _, err := mcpSetupFor("nope", cli); err == nil {
		t.Error("unknown client should fail")
	}
	if q := shellQuote("it's"); !strings.HasPrefix(q, "'it'") {
		t.Errorf("quote = %q", q)
	}
}
