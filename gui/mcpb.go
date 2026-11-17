package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/iwamatsu0430/sock-dock-slack/internal/mcpb"
)

// The Claude Desktop extension is built on demand from the CLI bundled with the
// app, so it always matches the running app.

// InstallClaudeDesktopExtension builds SockDock.mcpb and opens it, which shows
// Claude Desktop's install confirmation.
func (a *AppService) InstallClaudeDesktopExtension() error {
	cli := sockdockCLI()
	if !filepath.IsAbs(cli) {
		return errors.New("the sockdock CLI was not found")
	}
	if err := os.MkdirAll(a.dirs.DataDir, 0o700); err != nil {
		return err
	}
	out := filepath.Join(a.dirs.DataDir, "SockDock.mcpb")
	c, cancel := ctx()
	defer cancel()
	if err := mcpb.Build(c, out, cli, a.version); err != nil {
		return err
	}
	if err := exec.Command("open", out).Run(); err != nil {
		return errors.New(a.tr("err.noClaudeDesktop"))
	}
	if p := a.GetPrefs(); p.MCPClient != "claude-desktop" {
		p.MCPClient = "claude-desktop"
		return a.SetPrefs(p)
	}
	return nil
}
