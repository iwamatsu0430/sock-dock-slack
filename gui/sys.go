package main

import (
	"os/exec"
	"strings"
)

// notify shows a macOS notification. osascript works without code signing.
func notify(title, msg string) {
	esc := func(s string) string {
		return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
	}
	_ = exec.Command("osascript", "-e", `display notification "`+esc(msg)+`" with title "`+esc(title)+`"`).Start()
}
