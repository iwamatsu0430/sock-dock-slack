package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/iwamatsu0430/sock-dock-slack/assets"
	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
	"github.com/iwamatsu0430/sock-dock-slack/internal/control"
	"github.com/iwamatsu0430/sock-dock-slack/internal/gateway"
	"github.com/iwamatsu0430/sock-dock-slack/internal/lockfile"
	"github.com/iwamatsu0430/sock-dock-slack/internal/secret"
	"github.com/iwamatsu0430/sock-dock-slack/internal/slackio"
	"github.com/iwamatsu0430/sock-dock-slack/internal/store"
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
	d := g.dirs
	if err := d.Ensure(); err != nil {
		return err
	}
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	logFile := &lumberjack.Logger{Filename: d.LogFile(), MaxSize: 20, MaxBackups: 5, MaxAge: 30}
	defer logFile.Close()
	log := slog.New(slog.NewTextHandler(io.MultiWriter(os.Stderr, logFile), &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)

	lock, err := lockfile.Acquire(d.LockPath())
	if err != nil {
		return err
	}
	defer lock.Release()

	cfg, err := config.Load(d.ConfigFile)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("no config at %s (create one with `sockdock init`)", d.ConfigFile)
	}
	if err != nil {
		return err
	}
	appToken, botToken, err := secret.Tokens(cfg.Settings.SecretsFile)
	if err != nil {
		return err
	}

	db, err := store.Open(d.DBPath())
	if err != nil {
		return err
	}
	defer db.Close()

	promptFile := filepath.Join(d.PromptDir(), "gateway_system.md")
	if err := os.MkdirAll(d.PromptDir(), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(promptFile, []byte(assets.GatewaySystemPrompt), 0o600); err != nil {
		return err
	}

	api := slackio.NewWebAPI(botToken, appToken)
	gw := gateway.New(gateway.Options{
		ConfigPath:       d.ConfigFile,
		Store:            db,
		API:              api,
		Log:              log,
		BotToken:         botToken,
		SystemPromptFile: promptFile,
		Version:          version,
	}, cfg)

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	startCtx, cancelStart := context.WithTimeout(ctx, 30*time.Second)
	err = gw.Start(startCtx)
	cancelStart()
	if err != nil {
		return err
	}

	ctl := control.NewServer(gw, log, d.LogFile())
	go func() {
		if err := ctl.Serve(d.SocketPath()); err != nil {
			log.Error("control API stopped", "err", err)
		}
	}()

	listener := slackio.NewListener(api.Client(), log, gw.HandleMessage, gw.OnConnState)
	listenerDone := make(chan struct{})
	go func() { listener.Run(ctx); close(listenerDone) }()

	watchErr := make(chan error, 1)
	go func() { watchErr <- gw.Watch(ctx, gateway.DefaultWatchOptions, listener.Reconnect) }()

	log.Info("sockdock started", "version", version, "config", d.ConfigFile, "routes", len(cfg.Routes))

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	var exitErr error
loop:
	for {
		select {
		case s := <-sigs:
			if s == syscall.SIGHUP {
				_ = gw.Reload()
				continue
			}
			log.Info("shutting down", "signal", s.String())
			break loop
		case err := <-watchErr:
			if err != nil {
				log.Error("giving up; exiting so the service manager restarts sockdock", "err", err)
				exitErr = &exitError{code: 1, msg: err.Error()}
			}
			break loop
		}
	}

	stop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := gw.Shutdown(shutdownCtx); err != nil {
		log.Warn("handlers did not finish in time", "err", err)
	}
	_ = ctl.Shutdown(shutdownCtx)
	_ = os.Remove(d.SocketPath())
	<-listenerDone
	log.Info("sockdock stopped")
	return exitErr
}
