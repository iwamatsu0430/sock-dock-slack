// Package assets embeds files shipped inside the sockdock binary.
package assets

import _ "embed"

// GatewaySystemPrompt is appended to Claude Code's system prompt on every turn.
//
//go:embed gateway_system.md
var GatewaySystemPrompt string

// SlackAppManifest is the manifest for creating the Slack app.
//
//go:embed slack-app-manifest.yaml
var SlackAppManifest string

// ConfigExample is a starter config.yaml.
//
//go:embed config.example.yaml
var ConfigExample string
