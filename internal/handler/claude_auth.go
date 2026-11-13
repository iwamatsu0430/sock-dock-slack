package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"time"
)

// ReasonSignedOut is the run error when Claude Code is not signed in. The GUI
// shows its own text for it, so keep it stable.
const ReasonSignedOut = "Claude Code is not signed in"

// authFailure matches what Claude Code says when it cannot authenticate.
var authFailure = regexp.MustCompile(`(?i)failed to authenticate|not logged in|please run /login|invalid api key|oauth token (has )?expired|authentication_error`)

// isAuthFailure reports whether Claude Code's output says it is not signed in.
func isAuthFailure(texts ...string) bool {
	for _, s := range texts {
		if authFailure.MatchString(s) {
			return true
		}
	}
	return false
}

// ClaudeSignedIn runs `<command> auth status` and reports whether Claude Code
// is signed in. ok is false when it cannot tell (an old Claude Code without
// `auth status`, a missing command, a timeout).
func ClaudeSignedIn(ctx context.Context, command []string, pathEnv string) (signedIn, ok bool) {
	if len(command) == 0 {
		return false, false
	}
	path, err := resolveCommand(command[0], "", pathEnv)
	if err != nil {
		return false, false
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	args := append(append([]string{}, command[1:]...), "auth", "status")
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = buildEnv(pathEnv, nil, nil)
	if home, err := os.UserHomeDir(); err == nil {
		cmd.Dir = home
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	_ = cmd.Run() // it may exit non-zero when signed out; the JSON says which
	return parseAuthStatus(out.Bytes())
}

func parseAuthStatus(b []byte) (signedIn, ok bool) {
	var st struct {
		LoggedIn *bool `json:"loggedIn"`
	}
	if i := bytes.IndexByte(b, '{'); i >= 0 {
		b = b[i:]
	}
	if json.Unmarshal(b, &st) != nil || st.LoggedIn == nil {
		return false, false
	}
	return *st.LoggedIn, true
}
