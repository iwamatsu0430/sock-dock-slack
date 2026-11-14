// Package daemon starts and stops the whole gateway: lock, logging, config,
// tokens, database, Slack connection, control API and watchdog. Both
// `sockdock run` and the menu bar app use it.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/iwamatsu0430/sock-dock-slack/assets"
	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
	"github.com/iwamatsu0430/sock-dock-slack/internal/control"
	"github.com/iwamatsu0430/sock-dock-slack/internal/gateway"
	"github.com/iwamatsu0430/sock-dock-slack/internal/lockfile"
	"github.com/iwamatsu0430/sock-dock-slack/internal/paths"
	"github.com/iwamatsu0430/sock-dock-slack/internal/secret"
	"github.com/iwamatsu0430/sock-dock-slack/internal/slackio"
	"github.com/iwamatsu0430/sock-dock-slack/internal/store"
)

// Errors callers may want to tell apart.
var (
	// ErrLocked: another sockdock already runs for this data directory.
	ErrLocked = lockfile.ErrLocked
	// ErrNoConfig: the config file does not exist yet.
	ErrNoConfig = errors.New("no config file")
	// ErrNoTokens: the Slack tokens are missing or malformed.
	ErrNoTokens = errors.New("slack tokens are not set")
)

// Options configure a daemon.
type Options struct {
	Dirs    paths.Dirs
	Version string
	Debug   bool
	// Stderr also receives the log (for `sockdock run` in a terminal).
	Stderr io.Writer
	// Watch tunes the watchdog; zero means gateway.DefaultWatchOptions.
	Watch gateway.WatchOptions
}

// Daemon is a running gateway.
type Daemon struct {
	gw       *gateway.Gateway
	log      *slog.Logger
	cancel   context.CancelFunc
	done     chan struct{}
	stopOnce sync.Once

	mu  sync.Mutex
	err error
}

// Start brings the gateway up and returns once it is serving. The returned
// errors wrap ErrLocked, ErrNoConfig or ErrNoTokens where they apply.
func Start(o Options) (*Daemon, error) {
	d := o.Dirs
	if err := d.Ensure(); err != nil {
		return nil, err
	}
	if o.Watch.Interval == 0 {
		o.Watch = gateway.DefaultWatchOptions
	}

	level := slog.LevelInfo
	if o.Debug {
		level = slog.LevelDebug
	}
	logFile := &lumberjack.Logger{Filename: d.LogFile(), MaxSize: 20, MaxBackups: 5, MaxAge: 30}
	var out io.Writer = logFile
	if o.Stderr != nil {
		out = io.MultiWriter(o.Stderr, logFile)
	}
	log := slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: level}))

	// Everything acquired below is released in reverse order on failure.
	var cleanups []func()
	fail := func(err error) (*Daemon, error) {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
		return nil, err
	}
	cleanups = append(cleanups, func() { logFile.Close() })

	lock, err := lockfile.Acquire(d.LockPath())
	if err != nil {
		return fail(err)
	}
	cleanups = append(cleanups, func() { lock.Release() })

	cfg, err := config.LoadLenient(d.ConfigFile)
	if errors.Is(err, os.ErrNotExist) {
		return fail(fmt.Errorf("%w at %s (create one with `sockdock init`)", ErrNoConfig, d.ConfigFile))
	}
	if err != nil {
		return fail(err)
	}
	appToken, botToken, err := secret.Tokens(cfg.Settings.SecretsFile)
	if err != nil {
		return fail(fmt.Errorf("%w: %v", ErrNoTokens, err))
	}

	db, err := store.Open(d.DBPath())
	if err != nil {
		return fail(err)
	}
	cleanups = append(cleanups, func() { db.Close() })

	promptFile := filepath.Join(d.PromptDir(), "gateway_system.md")
	if err := os.MkdirAll(d.PromptDir(), 0o700); err != nil {
		return fail(err)
	}
	if err := os.WriteFile(promptFile, []byte(assets.GatewaySystemPrompt), 0o600); err != nil {
		return fail(err)
	}

	api := slackio.NewWebAPI(botToken, appToken)
	gw := gateway.New(gateway.Options{
		ConfigPath:       d.ConfigFile,
		Store:            db,
		API:              api,
		Log:              log,
		SystemPromptFile: promptFile,
		FilesDir:         d.FilesDir,
		Version:          o.Version,
	}, cfg)

	startCtx, cancelStart := context.WithTimeout(context.Background(), 30*time.Second)
	err = gw.Start(startCtx)
	cancelStart()
	if err != nil {
		return fail(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	dm := &Daemon{gw: gw, log: log, cancel: cancel, done: make(chan struct{})}

	ctl := control.NewServer(gw, log, d.LogFile())
	go func() {
		if err := ctl.Serve(d.SocketPath()); err != nil {
			log.Error("control API stopped", "err", err)
		}
	}()

	listener := slackio.NewListener(api.Client(), log, gw.HandleMessage, gw.OnConnState)
	listenerDone := make(chan struct{})
	go func() { listener.Run(ctx); close(listenerDone) }()

	go func() {
		if err := gw.Watch(ctx, o.Watch, listener.Reconnect); err != nil {
			log.Error("giving up on the Slack connection", "err", err)
			dm.setErr(err)
			cancel()
		}
	}()

	log.Info("sockdock started", "version", o.Version, "config", d.ConfigFile, "routes", len(cfg.Routes))

	// Shutdown sequence, run once ctx ends (Stop or watchdog).
	go func() {
		<-ctx.Done()
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelShutdown()
		if err := gw.Shutdown(shutdownCtx); err != nil {
			log.Warn("handlers did not finish in time", "err", err)
		}
		_ = ctl.Shutdown(shutdownCtx)
		_ = os.Remove(d.SocketPath())
		<-listenerDone
		log.Info("sockdock stopped")
		db.Close()
		lock.Release()
		logFile.Close()
		close(dm.done)
	}()
	return dm, nil
}

// Reload re-reads the config file.
func (d *Daemon) Reload() error { return d.gw.Reload() }

// Stop shuts the gateway down and waits for it (or ctx).
func (d *Daemon) Stop(ctx context.Context) error {
	d.stopOnce.Do(func() {
		d.log.Info("shutting down")
		d.cancel()
	})
	select {
	case <-d.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Done is closed when the daemon has fully stopped.
func (d *Daemon) Done() <-chan struct{} { return d.done }

// Err is the reason the daemon stopped on its own (nil after Stop).
func (d *Daemon) Err() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.err
}

func (d *Daemon) setErr(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err == nil {
		d.err = err
	}
}
