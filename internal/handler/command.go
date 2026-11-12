package handler

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
)

// RunCommand runs a command handler: the Input JSON goes to stdin and stdout
// becomes the reply.
func RunCommand(ctx context.Context, r *config.Route, in Input, env Env) (Result, error) {
	h := r.Handler
	path, err := resolveCommand(h.Command[0], h.Cwd, env.Path)
	if err != nil {
		return Result{}, err
	}
	payload, err := json.Marshal(in)
	if err != nil {
		return Result{}, err
	}
	extra := map[string]string{
		"SLACK_BOT_TOKEN": env.BotToken,
		"SLACK_CHANNEL":   in.Channel,
		"SLACK_THREAD_TS": in.ThreadTS,
		"SOCKDOCK_ROUTE":  in.Route,
		"SOCKDOCK_TURN":   itoa(in.Turn),
	}
	res, err := runProc(ctx, procSpec{
		Path:    path,
		Args:    h.Command[1:],
		Dir:     h.Cwd,
		Env:     buildEnv(env.Path, r.Env, extra),
		Stdin:   payload,
		Timeout: time.Duration(h.TimeoutSec) * time.Second,
	})
	if err != nil {
		return Result{}, err
	}
	if res.TimedOut {
		return Result{}, ErrTimeout
	}
	if res.ExitCode != 0 {
		return Result{}, &ExitError{Code: res.ExitCode, Stderr: tail(string(res.Stderr), 4000)}
	}
	return Result{Text: strings.TrimRight(string(res.Stdout), "\n")}, nil
}

// resolveCommand finds the executable: relative paths are relative to cwd,
// bare names are looked up in pathEnv.
func resolveCommand(name, cwd, pathEnv string) (string, error) {
	if strings.ContainsRune(name, '/') {
		if !filepath.IsAbs(name) {
			name = filepath.Join(cwd, name)
		}
		return name, nil
	}
	return config.LookPath(name, pathEnv)
}

// buildEnv composes the child environment: the gateway's environment, PATH,
// the route's env_file/env, then gateway-provided variables.
func buildEnv(pathEnv string, routeEnv, extra map[string]string) []string {
	m := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	// Never leak the app-level token to handlers.
	delete(m, "SLACK_APP_TOKEN")
	if pathEnv != "" {
		m["PATH"] = pathEnv
	}
	for k, v := range routeEnv {
		m[k] = v
	}
	for k, v := range extra {
		m[k] = v
	}
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}
