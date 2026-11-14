package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
	"github.com/iwamatsu0430/sock-dock-slack/internal/rules"
)

// MCP clients the settings window can register SockDock with.
// "other" copies a generic mcpServers entry.
// Antigravity replaced Gemini CLI, which stopped serving personal accounts on
// 2026-06-18.
var mcpClients = []string{"claude-code", "codex", "antigravity", "vscode", "claude-desktop", "cursor", "other"}

// MCPSetup is what the settings window copies for one MCP client.
type MCPSetup struct {
	Client string `json:"client"`
	// Kind is "command" (run it in a terminal), "config" (add it to a file) or
	// "extension" (Claude Desktop: build and open SockDock.mcpb).
	Kind string `json:"kind"`
	Text string `json:"text"`
	// File is the config file to add it to (Kind "config"; "" for other).
	File string `json:"file,omitempty"`
}

// sockdockCLI is the absolute path of the sockdock CLI: the one bundled with
// the app, else the one on the login shell's PATH.
func sockdockCLI() string {
	if exe, err := os.Executable(); err == nil {
		if p := filepath.Join(filepath.Dir(exe), "sockdock"); isExec(p) {
			return p
		}
	}
	if p, err := config.LookPath("sockdock", rules.LoginShellPath()); err == nil {
		return p
	}
	return "sockdock"
}

func isExec(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

// shellQuote quotes s for a POSIX shell when needed.
func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"\\$`!*?[]{}()<>|&;#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// mcpSetupFor builds the registration for a client.
func mcpSetupFor(client, cli string) (MCPSetup, error) {
	q := shellQuote(cli)
	entry := map[string]any{"command": cli, "args": []string{"mcp"}}
	servers := func() string {
		b, _ := json.MarshalIndent(map[string]any{"mcpServers": map[string]any{"sockdock": entry}}, "", "  ")
		return string(b)
	}
	switch client {
	case "claude-code":
		return MCPSetup{Client: client, Kind: "command", Text: "claude mcp add -s user sockdock -- " + q + " mcp"}, nil
	case "codex":
		return MCPSetup{Client: client, Kind: "command", Text: "codex mcp add sockdock -- " + q + " mcp"}, nil
	case "antigravity":
		return MCPSetup{Client: client, Kind: "command", Text: "agy mcp add sockdock " + q + " mcp"}, nil
	case "vscode":
		b, _ := json.Marshal(map[string]any{"name": "sockdock", "command": cli, "args": []string{"mcp"}})
		return MCPSetup{Client: client, Kind: "command", Text: "code --add-mcp " + shellQuote(string(b))}, nil
	case "claude-desktop":
		// Installed as a Claude Desktop extension (see mcpb.go).
		return MCPSetup{Client: client, Kind: "extension"}, nil
	case "cursor":
		return MCPSetup{Client: client, Kind: "config", File: "~/.cursor/mcp.json", Text: servers()}, nil
	case "other":
		return MCPSetup{Client: client, Kind: "config", Text: servers()}, nil
	}
	return MCPSetup{}, errors.New("unknown MCP client " + client)
}

// MCPSetup returns what to copy for a client ("" for the last one chosen).
func (a *AppService) MCPSetup(client string) (MCPSetup, error) {
	if client == "" {
		client = a.GetPrefs().MCPClient
	}
	if client == "gemini" {
		client = "antigravity" // chosen before Gemini CLI was replaced
	}
	return mcpSetupFor(client, sockdockCLI())
}

// CopyMCPSetup copies a client's registration to the clipboard and remembers
// the client.
func (a *AppService) CopyMCPSetup(client string) error {
	s, err := a.MCPSetup(client)
	if err != nil {
		return err
	}
	if !a.app.Clipboard.SetText(s.Text) {
		return errors.New("could not copy to the clipboard")
	}
	if p := a.GetPrefs(); p.MCPClient != client {
		p.MCPClient = client
		return a.SetPrefs(p)
	}
	return nil
}
