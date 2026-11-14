package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
)

// CodexOptions are gateway-level inputs for the codex handler
// (docs/design.md §3.8).
type CodexOptions struct {
	// Command is the executable and leading args (e.g. ["codex"]).
	Command []string
	// SystemPromptFile is the gateway-wide instruction file.
	SystemPromptFile string
	// SessionID resumes an earlier conversation (Codex's thread id) when set.
	SessionID string
	// FilesDir is the folder of this turn's attached files ("" when none).
	FilesDir string
}

// ReasonCodexSignedOut is the run error when Codex is not signed in. The GUI
// shows its own text for it, so keep it stable.
const ReasonCodexSignedOut = "Codex is not signed in"

const reasonCodexNoThread = "codex could not resume the conversation"

// RunCodex runs one turn of Codex headless (`codex exec`). If resuming fails,
// it starts a new conversation and reports ResumeFailed.
func RunCodex(ctx context.Context, r *config.Route, in Input, opts CodexOptions, env Env) (Result, error) {
	res, err := runCodexOnce(ctx, r, in, opts, env)
	var exitErr *ExitError
	if opts.SessionID != "" && errors.As(err, &exitErr) && exitErr.Reason == reasonCodexNoThread {
		retry := opts
		retry.SessionID = ""
		res, err = runCodexOnce(ctx, r, in, retry, env)
		res.ResumeFailed = err == nil
	}
	return res, err
}

func runCodexOnce(ctx context.Context, r *config.Route, in Input, opts CodexOptions, env Env) (Result, error) {
	if len(opts.Command) == 0 {
		return Result{}, errors.New("no codex command configured")
	}
	h := r.Handler
	path, err := resolveCommand(opts.Command[0], h.Cwd, env.Path)
	if err != nil {
		return Result{}, err
	}
	instructions, err := codexInstructions(h, opts.SystemPromptFile)
	if err != nil {
		return Result{}, err
	}
	home, _ := os.UserHomeDir()
	readable := codexReadable(path, opts.FilesDir, env.Path, home)
	args := append(append([]string{}, opts.Command[1:]...), CodexArgs(h, opts, instructions, readable, home)...)
	extra := map[string]string{
		"SOCKDOCK_ROUTE":  in.Route,
		"SOCKDOCK_TURN":   itoa(in.Turn),
		"SLACK_CHANNEL":   in.Channel,
		"SLACK_THREAD_TS": in.ThreadTS,
	}
	pr, err := runProc(ctx, procSpec{
		Path:     path,
		Args:     args,
		Dir:      h.Cwd,
		Env:      buildEnv(env.Path, r.Env, extra),
		Stdin:    []byte(ClaudePrompt(in)),
		Timeout:  time.Duration(h.TimeoutSec) * time.Second,
		Graceful: true,
	})
	if err != nil {
		return Result{}, err
	}
	if pr.TimedOut {
		return Result{}, ErrTimeout
	}
	out := parseCodexOutput(pr.Stdout)
	stderr := string(pr.Stderr)
	failed := out.Failed != "" || pr.ExitCode != 0
	if failed && isCodexAuthFailure(out.Failed, stderr) {
		return Result{SessionID: out.ThreadID}, &ExitError{Code: pr.ExitCode, Stderr: tail(stderr+out.Failed, 4000), Reason: ReasonCodexSignedOut}
	}
	if opts.SessionID != "" && failed && strings.Contains(stderr, "no rollout found") {
		return Result{}, &ExitError{Code: pr.ExitCode, Stderr: tail(stderr, 4000), Reason: reasonCodexNoThread}
	}
	if failed {
		reason := "codex reported an error"
		if line := firstLine(out.Failed); line != "" {
			reason += ": " + line
		} else if line := firstLine(lastLine(stderr)); line != "" {
			reason += ": " + line
		}
		return Result{SessionID: out.ThreadID}, &ExitError{Code: pr.ExitCode, Stderr: tail(stderr, 4000), Reason: reason}
	}
	if out.ThreadID == "" {
		return Result{}, &ExitError{Code: pr.ExitCode, Stderr: tail(stderr+string(pr.Stdout), 4000), Reason: "codex produced no result"}
	}
	return Result{Text: out.Text, SessionID: out.ThreadID}, nil
}

// CodexArgs builds the flags passed after the configured command.
// instructions is the developer instructions text; readable lists the
// folders outside the working folder that Codex's sandbox may read.
func CodexArgs(h config.Handler, opts CodexOptions, instructions string, readable []string, home string) []string {
	args := []string{"exec"}
	if opts.SessionID != "" {
		args = append(args, "resume")
	}
	args = append(args, "--json", "--skip-git-repo-check")
	if !h.AllowMCP {
		// The user's ~/.codex/config.toml (its MCP servers and plugins) and
		// ChatGPT's apps stay out; sign-in still comes from ~/.codex.
		args = append(args, "--ignore-user-config", "--disable", "apps")
	}
	web := "disabled"
	if h.AllowWeb {
		web = "live"
	}
	args = append(args,
		"-c", `approval_policy="never"`,
		"-c", `default_permissions="sockdock"`,
		"-c", "permissions.sockdock.filesystem="+codexFilesystem(h.AllowEdit, readable, home),
		"-c", "web_search="+tomlString(web),
	)
	if instructions != "" {
		args = append(args, "-c", "developer_instructions="+tomlString(instructions))
	}
	if h.Model != "" {
		args = append(args, "-m", h.Model)
	}
	args = append(args, h.ExtraArgs...)
	if opts.SessionID != "" {
		args = append(args, opts.SessionID)
	}
	return append(args, "-")
}

// codexFilesystem is the sandbox's filesystem table: everything outside the
// home folder may be read (system tools, Homebrew, Xcode), inside it only the
// working folder (written to when edits are allowed) and the listed folders.
func codexFilesystem(allowEdit bool, readable []string, home string) string {
	ws := "read"
	if allowEdit {
		ws = "write"
	}
	entries := []string{tomlString("/") + `="read"`}
	if home != "" && home != "/" {
		entries = append(entries, tomlString(home)+`="deny"`)
	}
	for _, p := range readable {
		mode := "read"
		if strings.HasPrefix(p, "w:") {
			p, mode = p[2:], "write"
		}
		entries = append(entries, tomlString(p)+"="+tomlString(mode))
	}
	entries = append(entries, `":workspace_roots"={"."=`+tomlString(ws)+`}`)
	return "{" + strings.Join(entries, ", ") + "}"
}

// codexReadable lists the folders under home that Codex needs besides the
// working folder: its own installation, the attached files, and the folders
// on PATH (so commands like node or go run). A "w:" prefix marks a folder it
// may write to (mise's cache, which its shims update on every run).
func codexReadable(codexPath, filesDir, pathEnv, home string) []string {
	set := map[string]bool{}
	add := func(p string) {
		if p == "" || home == "" {
			return
		}
		p = filepath.Clean(p)
		if p == home || !strings.HasPrefix(p, home+string(os.PathSeparator)) {
			return // outside home is readable anyway; never open all of home
		}
		set[p] = true
	}
	install := func(p string) {
		// <prefix>/bin/codex → <prefix>, so its libraries are readable too.
		add(filepath.Dir(filepath.Dir(p)))
		if real, err := filepath.EvalSymlinks(p); err == nil && real != p {
			add(filepath.Dir(filepath.Dir(real)))
		}
	}
	install(codexPath)
	if filesDir != "" {
		add(filesDir)
		if real, err := filepath.EvalSymlinks(filesDir); err == nil {
			add(real)
		}
	}
	mise := false
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}
		add(dir)
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			add(real)
		}
		if strings.Contains(dir, "/mise/") {
			mise = true
		}
	}
	if mise {
		add(filepath.Join(home, ".local", "share", "mise"))
		add(filepath.Join(home, ".config", "mise"))
	}
	out := make([]string, 0, len(set)+1)
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	if mise {
		out = append(out, "w:"+filepath.Join(home, "Library", "Caches", "mise"))
	}
	return out
}

// codexInstructions joins the gateway-wide instructions, the route's prompt
// file and its inline instructions (Codex takes them as one string).
func codexInstructions(h config.Handler, systemPromptFile string) (string, error) {
	var parts []string
	for _, f := range []string{systemPromptFile, h.Prompt} {
		if f == "" {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		if s := strings.TrimSpace(string(b)); s != "" {
			parts = append(parts, s)
		}
	}
	if s := strings.TrimSpace(h.Instructions); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, "\n\n"), nil
}

// codexOutput is what we take from `codex exec --json` (JSONL events).
type codexOutput struct {
	ThreadID string
	// Text is the last agent message of the turn.
	Text string
	// Failed is the error message of turn.failed, if any.
	Failed string
}

func parseCodexOutput(out []byte) codexOutput {
	var res codexOutput
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
			Item     struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		switch ev.Type {
		case "thread.started":
			res.ThreadID = ev.ThreadID
		case "turn.started":
			res.Text = ""
		case "item.completed":
			if ev.Item.Type == "agent_message" {
				res.Text = ev.Item.Text
			}
		case "turn.failed":
			res.Failed = ev.Error.Message
			if res.Failed == "" {
				res.Failed = "turn failed"
			}
		}
	}
	return res
}

// tomlString quotes s as a TOML basic string.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}
