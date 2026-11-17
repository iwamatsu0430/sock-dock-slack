// Package mcpb builds SockDock's MCP Bundle (.mcpb, binary type): the manifest
// and the sockdock CLI, which serves MCP with `sockdock mcp`. The app builds it
// for Claude Desktop from the CLI it bundles; releases attach one for the MCP
// Registry.
// Spec: github.com/modelcontextprotocol/mcpb MANIFEST.md.
package mcpb

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/iwamatsu0430/sock-dock-slack/internal/brand"
	"github.com/iwamatsu0430/sock-dock-slack/internal/mcpserver"
)

type manifest struct {
	ManifestVersion string               `json:"manifest_version"`
	Name            string               `json:"name"`
	DisplayName     string               `json:"display_name"`
	Version         string               `json:"version"`
	Description     string               `json:"description"`
	Author          map[string]string    `json:"author"`
	Homepage        string               `json:"homepage,omitempty"`
	Icon            string               `json:"icon,omitempty"`
	Server          server               `json:"server"`
	Tools           []mcpserver.ToolInfo `json:"tools,omitempty"`
	Compatibility   map[string]any       `json:"compatibility"`
	License         string               `json:"license,omitempty"`
}

type server struct {
	Type       string    `json:"type"`
	EntryPoint string    `json:"entry_point"`
	MCPConfig  mcpConfig `json:"mcp_config"`
}

type mcpConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

var semverRe = regexp.MustCompile(`^v?(\d+\.\d+\.\d+)(-[0-9A-Za-z.-]+)?$`)

// Version turns the app version ("v0.2.0", "b2ebb3c-dirty", "dev") into
// semver, as the manifest requires.
func Version(v string) string {
	if m := semverRe.FindStringSubmatch(v); m != nil {
		return m[1] + m[2]
	}
	clean := regexp.MustCompile(`[^0-9A-Za-z-]+`).ReplaceAllString(v, "-")
	if clean = strings.Trim(clean, "-"); clean == "" {
		clean = "dev"
	}
	return "0.0.0-" + clean
}

// Build writes an .mcpb (zip) holding the manifest and the CLI at cli.
func Build(ctx context.Context, out, cli, version string) error {
	tools, err := mcpserver.Tools(ctx)
	if err != nil {
		return err
	}
	man := manifest{
		ManifestVersion: "0.3",
		Name:            "sockdock",
		DisplayName:     "SockDock for Slack",
		Version:         Version(version),
		Description:     "Create and check SockDock rules (Slack posts that run Claude Code or a program) while SockDock runs on this Mac.",
		Author:          map[string]string{"name": "SockDock for Slack"},
		Homepage:        "https://github.com/iwamatsu0430/sock-dock-slack",
		Icon:            "icon.png",
		Server: server{
			Type:       "binary",
			EntryPoint: "server/sockdock",
			MCPConfig:  mcpConfig{Command: "${__dirname}/server/sockdock", Args: []string{"mcp"}},
		},
		Tools:         tools,
		Compatibility: map[string]any{"platforms": []string{"darwin"}},
		License:       "MIT",
	}
	manJSON, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return err
	}
	bin, err := os.Open(cli)
	if err != nil {
		return fmt.Errorf("the sockdock CLI was not found: %w", err)
	}
	defer bin.Close()

	tmp, err := os.CreateTemp(filepath.Dir(out), ".sockdock-*.mcpb")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	zw := zip.NewWriter(tmp)
	add := func(name string, mode os.FileMode, r io.Reader) error {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Now()}
		h.SetMode(mode)
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		_, err = io.Copy(w, r)
		return err
	}
	if err := add("manifest.json", 0o644, strings.NewReader(string(manJSON))); err != nil {
		return err
	}
	if err := add("icon.png", 0o644, bytes.NewReader(brand.AppIconPNG(512))); err != nil {
		return err
	}
	if err := add("server/sockdock", 0o755, bin); err != nil {
		return err
	}
	if err := errors.Join(zw.Close(), tmp.Close()); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), out)
}
