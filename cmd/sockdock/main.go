// Command sockdock is SockDock for Slack: a local gateway that receives Slack
// messages over Socket Mode and hands them to handlers you configure.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/iwamatsu0430/sock-dock-slack/internal/paths"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

type globals struct {
	configPath string
	dirs       paths.Dirs
}

// exitError carries an exit code without printing usage.
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

func main() {
	g := &globals{dirs: paths.Default()}
	root := &cobra.Command{
		Use:           "sockdock",
		Short:         "SockDock for Slack — run Slack apps locally: one Socket Mode connection, many handlers",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			if g.configPath != "" {
				g.dirs.ConfigFile = g.configPath
			}
		},
	}
	root.PersistentFlags().StringVar(&g.configPath, "config", "", "config file (default "+g.dirs.ConfigFile+")")

	root.AddCommand(
		runCmd(g),
		initCmd(g),
		checkCmd(g),
		matchCmd(g),
		statusCmd(g),
		reloadCmd(g),
		threadsCmd(g),
		turnsCmd(g),
		pauseCmd(g, true),
		pauseCmd(g, false),
		logsCmd(g),
		authCmd(g),
		serviceCmd(g),
		manifestCmd(),
		mcpCmd(g),
	)

	if err := root.Execute(); err != nil {
		var ee *exitError
		if errors.As(err, &ee) {
			if ee.msg != "" {
				fmt.Fprintln(os.Stderr, ee.msg)
			}
			os.Exit(ee.code)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
