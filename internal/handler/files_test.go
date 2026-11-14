package handler

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
)

func TestClaudeFilesPermissionAndPrompt(t *testing.T) {
	args := strings.Join(ClaudeArgs(config.Handler{}, ClaudeOptions{FilesDir: "/Users/me/Library/Caches/sockdock/files/7"}), " ")
	if !strings.Contains(args, "--allowedTools Read(//Users/me/Library/Caches/sockdock/files/7/**)") {
		t.Errorf("read permission for the turn's files missing: %s", args)
	}
	if !strings.Contains(args, "--tools Read,Glob,Grep") {
		t.Errorf("tools changed by the files rule: %s", args)
	}
	if args := strings.Join(ClaudeArgs(config.Handler{}, ClaudeOptions{}), " "); strings.Contains(args, "--allowedTools") {
		t.Errorf("no files, no extra rule: %s", args)
	}
	// A route that removed Read keeps its tools.
	args = strings.Join(ClaudeArgs(config.Handler{Tools: []string{"Glob"}}, ClaudeOptions{FilesDir: "/x"}), " ")
	if !strings.Contains(args, "--tools Glob ") {
		t.Errorf("explicit tools must stay: %s", args)
	}

	p := ClaudePrompt(Input{Messages: []InputMessage{{TS: "1.1", User: "U1", Text: "look", Files: []InputFile{
		{Name: "shot.png", Mimetype: "image/png", Size: 10, Path: "/x/shot.png"},
		{Name: "big.zip", Mimetype: "application/zip", Size: 99, Skipped: SkipTooLarge},
	}}}})
	for _, want := range []string{
		`<slack_file name="shot.png" type="image/png" size="10" path="/x/shot.png" />`,
		`<slack_file name="big.zip" type="application/zip" size="99" skipped="too_large" />`,
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %s:\n%s", want, p)
		}
	}
	if strings.Index(p, "<slack_file") > strings.Index(p, "</slack_message>") {
		t.Errorf("files belong inside the message:\n%s", p)
	}
}

func TestCommandJSONFiles(t *testing.T) {
	b, _ := json.Marshal(Input{Messages: []InputMessage{
		{TS: "1", Text: "a"},
		{TS: "2", Text: "b", Files: []InputFile{{Name: "a.log", Mimetype: "text/plain", Size: 3, Path: "/x/a.log"}, {Name: "z", Size: 9, Skipped: SkipExternal}}},
	}})
	s := string(b)
	if strings.Count(s, `"files"`) != 1 {
		t.Errorf("files only on messages that have them: %s", s)
	}
	if !strings.Contains(s, `{"name":"a.log","mimetype":"text/plain","size":3,"path":"/x/a.log"}`) ||
		!strings.Contains(s, `{"name":"z","size":9,"skipped":"external"}`) {
		t.Errorf("file entries: %s", s)
	}
}
