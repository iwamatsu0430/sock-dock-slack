package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExternalTokens(t *testing.T) {
	t.Setenv("SLACK_APP_TOKEN", "")
	t.Setenv("SLACK_BOT_TOKEN", "")
	if externalTokens("") {
		t.Error("no env, no file: want false")
	}
	dir := t.TempDir()
	f := filepath.Join(dir, "secrets.env")
	os.WriteFile(f, []byte("OTHER=1\n"), 0o600)
	if externalTokens(f) {
		t.Error("a file without tokens: want false")
	}
	os.WriteFile(f, []byte("SLACK_BOT_TOKEN=xoxb-test\n"), 0o600)
	if !externalTokens(f) {
		t.Error("a token in secrets_file: want true")
	}
	t.Setenv("SLACK_APP_TOKEN", "xapp-test")
	if !externalTokens("") {
		t.Error("a token in the environment: want true")
	}
}
