package main

import (
	"errors"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/iwamatsu0430/sock-dock-slack/internal/mcpserver"
)

func mcpCmd(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Serve MCP over stdio so AI agents (Claude Code, Codex, …) can control the running SockDock",
		Long: "Serve MCP over stdio. Register it with your MCP client, e.g.\n\n" +
			"  claude mcp add -s user sockdock -- " + "sockdock mcp\n\n" +
			"The tools relay to the running SockDock (the menu bar app or `sockdock run`); see docs/mcp.md.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Inside a run started by SockDock (an agent answering a Slack post),
			// refuse: a Slack post must not be able to change the rules.
			if os.Getenv("SOCKDOCK_TURN") != "" {
				return errors.New("sockdock mcp does not run inside a run started by SockDock")
			}
			c := client(g)
			c.Source = "mcp"
			return mcpserver.New(c, version).Run(cmd.Context(), &mcp.StdioTransport{})
		},
	}
}
