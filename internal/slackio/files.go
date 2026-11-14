package slackio

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/slack-go/slack"

	"github.com/iwamatsu0430/sock-dock-slack/internal/event"
)

// ScopeFilesRead lets the Slack app read files attached to messages
// (docs/design.md §3.6).
const ScopeFilesRead = "files:read"

// ErrNoFileAccess means Slack would not give the file to the token: the
// download was sent to the sign-in page, or files.info said missing_scope.
// Without files:read this is what happens.
var ErrNoFileAccess = errors.New("the bot token cannot read this file")

// FileFetcher reads files attached to messages. WebAPI implements it; the
// gateway passes no files when the API does not.
type FileFetcher interface {
	// FileInfo reads a file's details (files.info), for files whose event
	// left them out (file_access: check_file_info).
	FileInfo(ctx context.Context, id string) (event.File, error)
	// DownloadFile writes the file at url (url_private_download) to w. The
	// token is sent only to Slack's hosts.
	DownloadFile(ctx context.Context, url string, w io.Writer) error
}

func (w *WebAPI) FileInfo(ctx context.Context, id string) (event.File, error) {
	var f *slack.File
	err := w.retry(ctx, func() error {
		var err error
		f, _, _, err = w.c.GetFileInfoContext(ctx, id, 0, 0)
		return err
	})
	if isSlackError(err, "missing_scope") || isSlackError(err, "not_allowed_token_type") {
		return event.File{}, ErrNoFileAccess
	}
	if err != nil {
		return event.File{}, err
	}
	return convertFile(*f), nil
}

func (w *WebAPI) DownloadFile(ctx context.Context, url string, out io.Writer) error {
	err := w.c.GetFileContext(ctx, url, out)
	if err != nil && strings.Contains(err.Error(), "sign-in page") {
		return ErrNoFileAccess
	}
	return err
}

func convertFile(f slack.File) event.File {
	return event.File{
		ID:         f.ID,
		Name:       f.Name,
		Mimetype:   f.Mimetype,
		Size:       f.Size,
		Mode:       f.Mode,
		IsExternal: f.IsExternal,
		URL:        f.URLPrivateDownload,
		FileAccess: f.FileAccess,
	}
}
