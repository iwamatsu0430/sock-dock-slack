package main

import (
	"strings"
	"testing"
)

func TestUpdateScript(t *testing.T) {
	s := updateScript("SockDock を 0.2.0 に更新します。", "Homebrew が見つかりません。", "/opt/homebrew/bin:/usr/bin")
	for _, want := range []string{
		"export PATH=/opt/homebrew/bin:/usr/bin",
		"echo 'SockDock を 0.2.0 に更新します。'",
		"brew upgrade --cask iwamatsu0430/tap/sockdock && open -a SockDock",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("script lacks %q:\n%s", want, s)
		}
	}
}
