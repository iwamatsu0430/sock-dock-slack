package gateway

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
	"github.com/iwamatsu0430/sock-dock-slack/internal/event"
	"github.com/iwamatsu0430/sock-dock-slack/internal/handler"
	"github.com/iwamatsu0430/sock-dock-slack/internal/slackio"
	"github.com/iwamatsu0430/sock-dock-slack/internal/store"
)

// fileAPI adds files (and scopes) to fakeAPI.
type fileAPI struct {
	*fakeAPI
	scopes  []string
	content map[string]string // url -> body
	info    map[string]event.File
}

func (f *fileAPI) Scopes(context.Context) ([]string, error) { return f.scopes, nil }
func (f *fileAPI) FileInfo(_ context.Context, id string) (event.File, error) {
	if !f.hasFilesRead() {
		return event.File{}, slackio.ErrNoFileAccess
	}
	return f.info[id], nil
}
func (f *fileAPI) DownloadFile(_ context.Context, url string, w io.Writer) error {
	if !f.hasFilesRead() {
		return slackio.ErrNoFileAccess
	}
	_, err := io.WriteString(w, f.content[url])
	return err
}
func (f *fileAPI) hasFilesRead() bool {
	return len(slackio.MissingScopes(f.scopes, []string{slackio.ScopeFilesRead})) == 0
}

func newFileHarness(t *testing.T, scopes []string) (*harness, *fileAPI, string) {
	t.Helper()
	filesDir := t.TempDir()
	var seen []string
	h := newHarness(t, func(in handler.Input) (handler.Result, error) {
		// The files exist while the handler runs.
		for _, m := range in.Messages {
			for _, f := range m.Files {
				if f.Path != "" {
					b, err := os.ReadFile(f.Path)
					if err != nil {
						return handler.Result{}, err
					}
					seen = append(seen, string(b))
				}
			}
		}
		return handler.Result{Text: "ok " + strings.Join(seen, ",")}, nil
	})
	api := &fileAPI{fakeAPI: h.api, scopes: scopes, content: map[string]string{
		"https://files.slack.com/a": "log line",
		"https://files.slack.com/b": strings.Repeat("x", 2<<20),
		"https://files.slack.com/c": "connect",
	}, info: map[string]event.File{
		"FC": {ID: "FC", Name: "shared.txt", Mimetype: "text/plain", Size: 7, Mode: "hosted", URL: "https://files.slack.com/c"},
	}}
	h.gw.opts.API = api
	h.gw.opts.FilesDir = filesDir
	h.gw.cfg.Load().Settings.MaxFileMB = 1
	return h, api, filesDir
}

func TestAttachedFiles(t *testing.T) {
	h, _, filesDir := newFileHarness(t, []string{"chat:write", slackio.ScopeFilesRead})
	m := msg("300.1", "", "entry AB12 look")
	m.SubType = "file_share"
	m.Files = []event.File{
		{ID: "FA", Name: "../app.log", Mimetype: "text/plain", Size: 8, Mode: "hosted", URL: "https://files.slack.com/a"},
		{ID: "FA2", Name: "_app.log", Mimetype: "text/plain", Size: 8, Mode: "hosted", URL: "https://files.slack.com/a"},
		{ID: "FB", Name: "big.bin", Size: 2 << 20, Mode: "hosted", URL: "https://files.slack.com/b"},
		{ID: "FB2", Name: "lies.bin", Size: 1, Mode: "hosted", URL: "https://files.slack.com/b"},
		{ID: "FE", Name: "doc", Mode: "external", IsExternal: true},
		{ID: "FP", Name: "post", Mode: "post"},
		{ID: "FC", FileAccess: "check_file_info"},
	}
	h.gw.HandleMessage(m)
	waitFor(t, "reply", func() bool { return len(h.api.postsIn("300.1")) == 1 })

	h.mu.Lock()
	in, o := h.calls[0], h.opts[0]
	h.mu.Unlock()
	got := map[string]handler.InputFile{}
	for _, f := range in.Messages[0].Files {
		got[f.Name] = f
	}
	if len(in.Messages[0].Files) != 7 {
		t.Fatalf("files = %+v", in.Messages[0].Files)
	}
	if f := got["../app.log"]; f.Path == "" || filepath.Base(f.Path) != "_app.log" || filepath.Dir(f.Path) != o.FilesDir {
		t.Errorf("unsafe name not cleaned: %+v (dir %s)", f, o.FilesDir)
	}
	if f := got["_app.log"]; filepath.Base(f.Path) != "_app-2.log" {
		t.Errorf("same name must get a suffix: %+v", f)
	}
	for name, want := range map[string]string{
		"big.bin": handler.SkipTooLarge, "lies.bin": handler.SkipTooLarge,
		"doc": handler.SkipExternal, "post": handler.SkipUnsupported,
	} {
		if got[name].Skipped != want || got[name].Path != "" {
			t.Errorf("%s: %+v, want skipped %s", name, got[name], want)
		}
	}
	if f := got["shared.txt"]; f.Path == "" {
		t.Errorf("check_file_info file not read with files.info: %+v", f)
	}
	if !strings.HasPrefix(o.FilesDir, mustReal(t, filesDir)) {
		t.Errorf("FilesDir = %q, want under %q", o.FilesDir, filesDir)
	}
	if reply := h.api.postsIn("300.1")[0]; reply != "ok log line,log line,connect" {
		t.Errorf("reply = %q", reply)
	}
	waitFor(t, "files removed", func() bool {
		_, err := os.Stat(o.FilesDir)
		return os.IsNotExist(err)
	})
	turns, _ := h.db.ListTurns(context.Background(), 1)
	if turns[0].FileProblem != "" {
		t.Errorf("file problem = %q", turns[0].FileProblem)
	}
}

func TestAttachedFilesWithoutScope(t *testing.T) {
	h, _, filesDir := newFileHarness(t, []string{"chat:write"})
	m := msg("301.1", "", "echo hi")
	m.Files = []event.File{{ID: "FA", Name: "app.log", Size: 8, Mode: "hosted", URL: "https://files.slack.com/a"}}
	var got []handler.InputMessage
	h.gw.opts.RunCommand = func(_ context.Context, _ *config.Route, in handler.Input, _ handler.Env) (handler.Result, error) {
		got = in.Messages
		return handler.Result{Text: "done"}, nil
	}
	h.gw.HandleMessage(m)
	waitFor(t, "reply", func() bool { return len(h.api.postsIn("301.1")) == 1 })
	if len(got) != 1 || len(got[0].Files) != 1 || got[0].Files[0].Skipped != handler.SkipNoScope {
		t.Fatalf("files = %+v", got)
	}
	waitTurnsFinished(t, h.db)
	turns, _ := h.db.ListTurns(context.Background(), 1)
	if turns[0].FileProblem != store.FileProblemMissingScope || turns[0].Status != store.StatusOK {
		t.Errorf("turn = %+v", turns[0])
	}
	if posts := h.api.postsIn("301.1"); len(posts) != 1 || posts[0] != "done" {
		t.Errorf("SockDock must not add its own message: %q", posts)
	}
	if left, _ := os.ReadDir(filesDir); len(left) != 0 {
		t.Errorf("no file was saved, so no folder should be left: %v", left)
	}
}

func TestLeftoverFilesRemovedOnStart(t *testing.T) {
	h, _, filesDir := newFileHarness(t, nil)
	old := filepath.Join(filesDir, "12")
	if err := os.MkdirAll(old, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := h.gw.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("leftover folder kept: %v", err)
	}
}

func TestFileName(t *testing.T) {
	for in, want := range map[string]string{
		"a/b:c.txt": "a_b_c.txt", ".env": "env", "...": "F1", "x\x00y": "x_y", "スクショ.png": "スクショ.png",
	} {
		if got := fileName(event.File{ID: "F1", Name: in}); got != want {
			t.Errorf("fileName(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("あ", 100) + ".png"
	if got := fileName(event.File{Name: long}); len(got) > 200 || !strings.HasSuffix(got, ".png") {
		t.Errorf("long name = %q (%d bytes)", got, len(got))
	}
}

func mustReal(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
