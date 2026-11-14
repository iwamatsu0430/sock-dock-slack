package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/iwamatsu0430/sock-dock-slack/internal/event"
	"github.com/iwamatsu0430/sock-dock-slack/internal/handler"
	"github.com/iwamatsu0430/sock-dock-slack/internal/slackio"
	"github.com/iwamatsu0430/sock-dock-slack/internal/store"
)

// turnFiles are the attached files of one turn (docs/design.md §3.6).
type turnFiles struct {
	// dir holds the saved files ("" when none were saved). It is removed
	// when the turn ends.
	dir string
	// byTS lists each message's files, saved or skipped.
	byTS map[string][]handler.InputFile
	// problem is store.FileProblemMissingScope when files:read is missing.
	problem string
}

// filesRoot is where turns keep their attached files.
func (g *Gateway) filesRoot() string {
	if g.opts.FilesDir != "" {
		return g.opts.FilesDir
	}
	return filepath.Join(os.TempDir(), "sockdock-files")
}

// clearFiles removes files left by turns that did not finish (a crash or a
// restart in the middle of a turn).
func (g *Gateway) clearFiles() {
	if err := os.RemoveAll(g.filesRoot()); err != nil {
		g.log.Warn("could not remove leftover attached files", "err", err)
	}
}

// fetchFiles saves the files attached to a batch of messages into a folder
// for this turn. Files that cannot be handed over are listed with the reason.
// Errors never stop the turn: the handler still gets the text.
func (g *Gateway) fetchFiles(ctx context.Context, turnID int64, batch []event.Message, maxMB int, log *slog.Logger) turnFiles {
	tf := turnFiles{byTS: map[string][]handler.InputFile{}}
	var attached bool
	for _, m := range batch {
		if len(m.Files) > 0 {
			attached = true
		}
	}
	if !attached {
		return tf
	}
	fetcher, _ := g.opts.API.(slackio.FileFetcher)
	maxBytes := int64(maxMB) << 20
	used := map[string]bool{}
	var noAccess bool

	for _, m := range batch {
		for _, f := range m.Files {
			in, err := g.fetchFile(ctx, fetcher, turnID, f, maxBytes, used, &tf)
			if errors.Is(err, slackio.ErrNoFileAccess) {
				noAccess = true
			}
			if err != nil {
				log.Warn("could not get an attached file", "file", f.ID, "err", err)
			}
			tf.byTS[m.TS] = append(tf.byTS[m.TS], in)
		}
	}
	if tf.dir != "" && !savedAny(tf.byTS) {
		_ = os.RemoveAll(tf.dir)
		tf.dir = ""
	}
	if noAccess {
		reason := handler.SkipFailed
		if !g.refreshHasScope(ctx, slackio.ScopeFilesRead) {
			reason = handler.SkipNoScope
			tf.problem = store.FileProblemMissingScope
		}
		for ts, files := range tf.byTS {
			for i := range files {
				if files[i].Skipped == skipNoAccess {
					files[i].Skipped = reason
				}
			}
			tf.byTS[ts] = files
		}
	}
	return tf
}

func savedAny(byTS map[string][]handler.InputFile) bool {
	for _, files := range byTS {
		for _, f := range files {
			if f.Path != "" {
				return true
			}
		}
	}
	return false
}

// skipNoAccess marks a file Slack refused until fetchFiles knows whether
// files:read is the reason.
const skipNoAccess = "no_access"

func (g *Gateway) fetchFile(ctx context.Context, fetcher slackio.FileFetcher, turnID int64, f event.File, maxBytes int64, used map[string]bool, tf *turnFiles) (handler.InputFile, error) {
	in := handler.InputFile{Name: f.Name, Mimetype: f.Mimetype, Size: f.Size}
	if fetcher == nil {
		in.Skipped = handler.SkipFailed
		return in, nil
	}
	if f.FileAccess == "check_file_info" || (f.URL == "" && f.Mode == "" && !f.IsExternal) {
		full, err := fetcher.FileInfo(ctx, f.ID)
		if errors.Is(err, slackio.ErrNoFileAccess) {
			in.Skipped = skipNoAccess
			return in, err
		}
		if err != nil {
			in.Skipped = handler.SkipFailed
			return in, err
		}
		f = full
		in = handler.InputFile{Name: f.Name, Mimetype: f.Mimetype, Size: f.Size}
	}
	switch {
	case f.IsExternal || f.Mode == "external":
		in.Skipped = handler.SkipExternal
		return in, nil
	case f.Mode != "hosted" && f.Mode != "snippet" && f.Mode != "":
		in.Skipped = handler.SkipUnsupported
		return in, nil
	case f.URL == "":
		in.Skipped = handler.SkipUnsupported
		return in, nil
	case int64(f.Size) > maxBytes:
		in.Skipped = handler.SkipTooLarge
		return in, nil
	}

	if tf.dir == "" {
		dir := filepath.Join(g.filesRoot(), strconv.FormatInt(turnID, 10))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			in.Skipped = handler.SkipFailed
			return in, err
		}
		// Claude Code compares permission rules with the real path.
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			dir = real
		}
		tf.dir = dir
	}
	path := filepath.Join(tf.dir, uniqueName(fileName(f), used))
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		in.Skipped = handler.SkipFailed
		return in, err
	}
	err = fetcher.DownloadFile(ctx, f.URL, &limitWriter{w: out, left: maxBytes})
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(path)
		switch {
		case errors.Is(err, errTooLarge):
			in.Skipped = handler.SkipTooLarge
			return in, nil
		case errors.Is(err, slackio.ErrNoFileAccess):
			in.Skipped = skipNoAccess
		default:
			in.Skipped = handler.SkipFailed
		}
		return in, err
	}
	if st, err := os.Stat(path); err == nil {
		in.Size = int(st.Size())
	}
	in.Path = path
	return in, nil
}

// refreshHasScope reads the Bot Token's scopes again (the app may have been
// reinstalled with more scopes since SockDock started) and reports whether
// scope is among them. Unknown scopes count as missing.
func (g *Gateway) refreshHasScope(ctx context.Context, scope string) bool {
	if l, ok := g.opts.API.(slackio.ScopeLister); ok {
		if scopes, err := l.Scopes(ctx); err == nil && scopes != nil {
			g.mu.Lock()
			g.scopes = scopes
			g.mu.Unlock()
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.scopes == nil {
		return false
	}
	return len(slackio.MissingScopes(g.scopes, []string{scope})) == 0
}

var errTooLarge = errors.New("file is over the size limit")

// limitWriter fails once more than left bytes are written (Slack's size can
// be missing or wrong).
type limitWriter struct {
	w    io.Writer
	left int64
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > l.left {
		return 0, errTooLarge
	}
	l.left -= int64(len(p))
	return l.w.Write(p)
}

// fileName makes a Slack file name safe to use as a file name in the turn's
// folder: no path separators, no control characters, no leading dot.
func fileName(f event.File) string {
	name := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || unicode.IsControl(r) {
			return '_'
		}
		return r
	}, f.Name)
	name = strings.TrimLeft(strings.TrimSpace(name), ".")
	if len(name) > 200 {
		ext := filepath.Ext(name)
		if len(ext) > 20 {
			ext = ""
		}
		name = truncateUTF8(name, 200-len(ext)) + ext
	}
	if name == "" {
		name = f.ID
	}
	if name == "" {
		name = "file"
	}
	return name
}

func truncateUTF8(s string, n int) string {
	for n > 0 && n < len(s) && !utf8Start(s[n]) {
		n--
	}
	if n < len(s) {
		return s[:n]
	}
	return s
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

// uniqueName adds -2, -3, ... before the extension when a name is taken.
func uniqueName(name string, used map[string]bool) string {
	out := name
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 2; used[strings.ToLower(out)]; i++ {
		out = fmt.Sprintf("%s-%d%s", base, i, ext)
	}
	used[strings.ToLower(out)] = true
	return out
}
