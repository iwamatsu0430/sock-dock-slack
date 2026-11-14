package handler

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"time"
)

// codexAuthFailure matches what Codex says when it is not signed in or its
// sign-in no longer works.
var codexAuthFailure = regexp.MustCompile(`(?i)401 unauthorized|not logged in|please log in|codex login|refresh token`)

func isCodexAuthFailure(texts ...string) bool {
	for _, s := range texts {
		if codexAuthFailure.MatchString(s) {
			return true
		}
	}
	return false
}

// CodexSignedIn runs `<command> login status` and reports whether Codex is
// signed in: it exits 0 when signed in and 1 when not. ok is false when it
// cannot tell (a missing command, a timeout, another exit code).
func CodexSignedIn(ctx context.Context, command []string, pathEnv string) (signedIn, ok bool) {
	if len(command) == 0 {
		return false, false
	}
	path, err := resolveCommand(command[0], "", pathEnv)
	if err != nil {
		return false, false
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	args := append(append([]string{}, command[1:]...), "login", "status")
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = buildEnv(pathEnv, nil, nil)
	if home, err := os.UserHomeDir(); err == nil {
		cmd.Dir = home
	}
	err = cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return true, true
	case errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && ctx.Err() == nil:
		return false, true
	default:
		return false, false
	}
}
