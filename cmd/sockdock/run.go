package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/iwamatsu0430/sock-dock-slack/internal/daemon"
)

func runCmd(g *globals) *cobra.Command {
	var debug bool
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the gateway in the foreground (launchd/systemd call this)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(g, debug)
		},
	}
	cmd.Flags().BoolVar(&debug, "debug", false, "verbose logging")
	return cmd
}

func run(g *globals, debug bool) error {
	d, err := daemon.Start(daemon.Options{Dirs: g.dirs, Version: version, Debug: debug, Stderr: os.Stderr})
	if err != nil {
		return err
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	for {
		select {
		case s := <-sigs:
			if s == syscall.SIGHUP {
				_ = d.Reload()
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
			defer cancel()
			return d.Stop(ctx)
		case <-d.Done():
			// Stopped on its own (watchdog): exit non-zero so the service manager restarts us.
			if err := d.Err(); err != nil {
				return &exitError{code: 1, msg: err.Error()}
			}
			return nil
		}
	}
}
