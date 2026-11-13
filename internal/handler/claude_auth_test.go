package handler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeAuthFailure(t *testing.T) {
	r, _ := setup(t, "auth")
	_, err := RunClaude(context.Background(), r, input, ClaudeOptions{Command: r.Handler.Command}, Env{Path: os.Getenv("PATH")})
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Reason != ReasonSignedOut {
		t.Fatalf("err = %v, want %q", err, ReasonSignedOut)
	}
}

func TestIsAuthFailure(t *testing.T) {
	for _, s := range []string{
		"Failed to authenticate: OAuth session expired and could not be refreshed",
		"Invalid API key · Please run /login",
		"Not logged in · Please run /login",
		`API Error: 401 {"type":"error","error":{"type":"authentication_error"}}`,
	} {
		if !isAuthFailure(s) {
			t.Errorf("isAuthFailure(%q) = false", s)
		}
	}
	for _, s := range []string{"boom", "rate limited", "the login page has a bug"} {
		if isAuthFailure(s) {
			t.Errorf("isAuthFailure(%q) = true", s)
		}
	}
}

func TestClaudeSignedIn(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "claude")
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(script, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		body         string
		signedIn, ok bool
	}{
		{`[ "$1 $2" = "auth status" ] && echo '{"loggedIn": true, "authMethod": "claude.ai"}'`, true, true},
		{`echo '{"loggedIn": false, "authMethod": "none"}'; exit 1`, false, true},
		{`echo "error: unknown command 'auth'" >&2; exit 1`, false, false},
		{`echo '{"other": 1}'`, false, false},
	}
	for _, c := range cases {
		write(c.body)
		signedIn, ok := ClaudeSignedIn(context.Background(), []string{script}, os.Getenv("PATH"))
		if signedIn != c.signedIn || ok != c.ok {
			t.Errorf("%s: got (%v, %v), want (%v, %v)", c.body, signedIn, ok, c.signedIn, c.ok)
		}
	}
	if _, ok := ClaudeSignedIn(context.Background(), []string{filepath.Join(dir, "missing")}, ""); ok {
		t.Error("a missing command should be unknown")
	}
}
