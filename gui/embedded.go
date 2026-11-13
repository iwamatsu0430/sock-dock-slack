package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/iwamatsu0430/sock-dock-slack/internal/daemon"
	"github.com/iwamatsu0430/sock-dock-slack/internal/paths"
)

// Gateway states shown in the UI.
const (
	GatewayRunning  = "running"  // running inside this app
	GatewayExternal = "external" // another sockdock (e.g. the CLI service) is running
	GatewaySetup    = "setup"    // config or tokens missing
	GatewayStopped  = "stopped"  // stopped by the user
	GatewayError    = "error"    // failed to start or stopped unexpectedly
)

// GatewayState is the embedded gateway's state.
type GatewayState struct {
	State string `json:"state"`
	Error string `json:"error,omitempty"`
	// Missing is "config" or "tokens" when State is "setup".
	Missing string `json:"missing,omitempty"`
}

// embedded runs the gateway inside the menu bar app and restarts it when it
// stops on its own.
type embedded struct {
	dirs     paths.Dirs
	version  string
	onChange func()

	mu      sync.Mutex
	d       *daemon.Daemon
	state   GatewayState
	stopped bool // the user asked it to stop; do not restart
	gen     int  // bumps on every start, so stale watchers do nothing
	// stopping is closed when the last stopped daemon has fully shut down
	// (and released its lock); Start waits for it.
	stopping <-chan struct{}
}

// stopWait caps how long Start waits for the previous daemon to finish. Its
// own shutdown is capped at 30 seconds.
const stopWait = 45 * time.Second

const restartDelay = 15 * time.Second

func newEmbedded(dirs paths.Dirs, version string) *embedded {
	return &embedded{dirs: dirs, version: version, state: GatewayState{State: GatewayStopped}}
}

// State returns the current state.
func (e *embedded) State() GatewayState {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state
}

// Start starts the gateway unless it already runs here.
func (e *embedded) Start() GatewayState {
	e.mu.Lock()
	if e.d != nil {
		st := e.state
		e.mu.Unlock()
		return st
	}
	e.stopped = false
	e.gen++
	gen := e.gen
	prev := e.stopping
	e.mu.Unlock()

	// A daemon this app just stopped may still hold the lock; starting before
	// it lets go would look like another SockDock running.
	if prev != nil {
		select {
		case <-prev:
		case <-time.After(stopWait):
		}
	}
	d, err := daemon.Start(daemon.Options{Dirs: e.dirs, Version: e.version})

	e.mu.Lock()
	switch {
	case err == nil:
		e.d = d
		e.state = GatewayState{State: GatewayRunning}
		go e.watch(d, gen)
	case errors.Is(err, daemon.ErrLocked):
		e.state = GatewayState{State: GatewayExternal}
	case errors.Is(err, daemon.ErrNoConfig):
		e.state = GatewayState{State: GatewaySetup, Missing: "config", Error: err.Error()}
	case errors.Is(err, daemon.ErrNoTokens):
		e.state = GatewayState{State: GatewaySetup, Missing: "tokens", Error: err.Error()}
	default:
		e.state = GatewayState{State: GatewayError, Error: err.Error()}
	}
	st := e.state
	e.mu.Unlock()
	e.changed()
	return st
}

// Stop stops the gateway and keeps it stopped.
func (e *embedded) Stop(timeout time.Duration) {
	e.mu.Lock()
	d := e.d
	e.d = nil
	e.stopped = true
	e.gen++
	if e.state.State == GatewayRunning {
		e.state = GatewayState{State: GatewayStopped}
	}
	if d != nil {
		e.stopping = d.Done()
	}
	e.mu.Unlock()
	if d != nil {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		_ = d.Stop(ctx)
	}
	e.changed()
}

// Restart stops (if running) and starts again, e.g. after tokens changed.
func (e *embedded) Restart() GatewayState {
	e.Stop(30 * time.Second)
	return e.Start()
}

// Reload re-reads the config of the embedded gateway.
func (e *embedded) Reload() error {
	e.mu.Lock()
	d := e.d
	e.mu.Unlock()
	if d == nil {
		return errors.New("the gateway is not running in this app")
	}
	return d.Reload()
}

// watch restarts the gateway if it stops on its own (e.g. Slack unreachable
// for too long).
func (e *embedded) watch(d *daemon.Daemon, gen int) {
	<-d.Done()
	e.mu.Lock()
	if e.gen != gen || e.stopped {
		e.mu.Unlock()
		return
	}
	e.d = nil
	msg := "the gateway stopped"
	if err := d.Err(); err != nil {
		msg = err.Error()
	}
	e.state = GatewayState{State: GatewayError, Error: msg + " — restarting shortly"}
	e.mu.Unlock()
	e.changed()

	time.Sleep(restartDelay)
	e.mu.Lock()
	again := e.gen == gen && !e.stopped && e.d == nil
	e.mu.Unlock()
	if again {
		e.Start()
	}
}

func (e *embedded) changed() {
	if e.onChange != nil {
		e.onChange()
	}
}
