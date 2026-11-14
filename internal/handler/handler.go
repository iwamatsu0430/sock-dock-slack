// Package handler runs route handlers: arbitrary commands and Claude Code.
package handler

import (
	"errors"
	"fmt"
)

// Input is what every handler receives (as JSON on stdin for command handlers).
type Input struct {
	Route     string            `json:"route"`
	Turn      int               `json:"turn"`
	Channel   string            `json:"channel"`
	ThreadTS  string            `json:"thread_ts"`
	Permalink string            `json:"permalink"`
	Groups    map[string]string `json:"groups"`
	Messages  []InputMessage    `json:"messages"`
}

// InputMessage is one Slack message handed to a handler.
type InputMessage struct {
	TS   string `json:"ts"`
	User string `json:"user"`
	Text string `json:"text"`
	// Files are the files attached to the message (docs/design.md §3.6).
	Files []InputFile `json:"files,omitempty"`
}

// InputFile is an attached file. Path is set when the file was saved for this
// turn; otherwise Skipped says why it was not.
type InputFile struct {
	Name     string `json:"name"`
	Mimetype string `json:"mimetype,omitempty"`
	Size     int    `json:"size"`
	Path     string `json:"path,omitempty"`
	Skipped  string `json:"skipped,omitempty"`
}

// Reasons an attached file was not handed over (InputFile.Skipped).
const (
	SkipTooLarge    = "too_large"
	SkipExternal    = "external"
	SkipUnsupported = "unsupported"
	SkipNoScope     = "no_scope"
	SkipFailed      = "failed"
)

// Result is a handler's outcome.
type Result struct {
	// Text is posted to the thread (if not empty).
	Text string
	// SessionID is the conversation to resume next turn (claude only).
	SessionID string
	// ResumeFailed is set when the previous conversation could not be resumed
	// and a new one was started.
	ResumeFailed bool
	CostUSD      float64
}

// Env is the gateway-provided environment for handlers.
type Env struct {
	// PATH value for the handler process.
	Path string
}

// ErrTimeout is returned when a handler exceeds its time limit.
var ErrTimeout = errors.New("handler timed out")

// ExitError is returned when a handler fails.
type ExitError struct {
	Code   int
	Stderr string
	Reason string
}

func (e *ExitError) Error() string {
	if e.Reason != "" {
		return e.Reason
	}
	return fmt.Sprintf("handler exited with status %d", e.Code)
}
