package control

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/iwamatsu0430/sock-dock-slack/internal/gateway"
	"github.com/iwamatsu0430/sock-dock-slack/internal/rules"
	"github.com/iwamatsu0430/sock-dock-slack/internal/store"
)

// ErrNotRunning means nothing is listening on the control socket.
var ErrNotRunning = errors.New("sockdock is not running (no control socket)")

// Client talks to a running sockdock over its control socket.
type Client struct {
	hc *http.Client
	// Source is sent as SourceHeader with every request ("mcp" for
	// `sockdock mcp`), so changes are announced to the app.
	Source string
}

// NewClient creates a client for the socket at path.
func NewClient(socketPath string) *Client {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{hc: &http.Client{Transport: tr}}
}

// APIError is a non-2xx response.
type APIError struct {
	Status   int
	Message  string
	Problems []string
}

func (e *APIError) Error() string {
	if len(e.Problems) > 0 {
		return "invalid config:\n  - " + strings.Join(e.Problems, "\n  - ")
	}
	return fmt.Sprintf("control API: %s (HTTP %d)", e.Message, e.Status)
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, "http://sockdock"+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Source != "" {
		req.Header.Set(SourceHeader, c.Source)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		var opErr *net.OpError
		if errors.As(err, &opErr) {
			return ErrNotRunning
		}
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		var e struct {
			Error    string   `json:"error"`
			Problems []string `json:"problems"`
		}
		_ = json.Unmarshal(data, &e)
		return &APIError{Status: resp.StatusCode, Message: e.Error, Problems: e.Problems}
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// Status returns the gateway status.
func (c *Client) Status(ctx context.Context) (gateway.Status, error) {
	var s gateway.Status
	return s, c.do(ctx, http.MethodGet, "/v1/status", nil, &s)
}

// CheckClaude asks whether Claude Code is signed in (nil when unknown). force
// skips the recent answer.
func (c *Client) CheckClaude(ctx context.Context, force bool) (*bool, error) {
	path := "/v1/claude/check"
	if force {
		path += "?force=1"
	}
	var out ClaudeCheck
	err := c.do(ctx, http.MethodPost, path, nil, &out)
	return out.SignedIn, err
}

// CheckAgents asks whether each agent that a rule runs is signed in (by
// handler type; unknown ones are left out). force skips the recent answers.
func (c *Client) CheckAgents(ctx context.Context, force bool) (map[string]bool, error) {
	path := "/v1/claude/check"
	if force {
		path += "?force=1"
	}
	var out ClaudeCheck
	err := c.do(ctx, http.MethodPost, path, nil, &out)
	return out.Agents, err
}

// Reload asks the gateway to re-read its config.
func (c *Client) Reload(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/v1/reload", nil, nil)
}

// Threads lists recent threads.
func (c *Client) Threads(ctx context.Context, limit int) ([]store.Thread, error) {
	var out []store.Thread
	return out, c.do(ctx, http.MethodGet, fmt.Sprintf("/v1/threads?limit=%d", limit), nil, &out)
}

// Turns lists recent turns.
func (c *Client) Turns(ctx context.Context, limit int) ([]store.Turn, error) {
	var out []store.Turn
	return out, c.do(ctx, http.MethodGet, fmt.Sprintf("/v1/turns?limit=%d", limit), nil, &out)
}

// RouteTurns returns recent handler runs of one route.
func (c *Client) RouteTurns(ctx context.Context, route string, limit int) ([]store.Turn, error) {
	var out []store.Turn
	return out, c.do(ctx, http.MethodGet, fmt.Sprintf("/v1/turns?route=%s&limit=%d", url.QueryEscape(route), limit), nil, &out)
}

// StopTurn stops a running handler run.
func (c *Client) StopTurn(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/v1/turns/%d/stop", id), nil, nil)
}

// Match asks which route a message would hit.
func (c *Client) Match(ctx context.Context, req MatchRequest) (MatchResult, error) {
	var out MatchResult
	return out, c.do(ctx, http.MethodPost, "/v1/match", req, &out)
}

// SetPaused pauses or resumes a route.
func (c *Client) SetPaused(ctx context.Context, route string, paused bool) error {
	action := "resume"
	if paused {
		action = "pause"
	}
	return c.do(ctx, http.MethodPost, "/v1/routes/"+url.PathEscape(route)+"/"+action, nil, nil)
}

// Rules lists the rules in the order they are tried.
func (c *Client) Rules(ctx context.Context) ([]RuleInfo, error) {
	var out []RuleInfo
	return out, c.do(ctx, http.MethodGet, "/v1/rules", nil, &out)
}

// Rule returns one rule.
func (c *Client) Rule(ctx context.Context, name string) (RuleInfo, error) {
	var out RuleInfo
	return out, c.do(ctx, http.MethodGet, "/v1/rules/"+url.PathEscape(name), nil, &out)
}

// SaveRule adds (OriginalName == "") or updates a rule.
func (c *Client) SaveRule(ctx context.Context, f rules.Form) (RuleSaveResult, error) {
	var out RuleSaveResult
	return out, c.do(ctx, http.MethodPut, "/v1/rules", f, &out)
}

// DeleteRule removes a rule.
func (c *Client) DeleteRule(ctx context.Context, name string) (RuleSaveResult, error) {
	var out RuleSaveResult
	return out, c.do(ctx, http.MethodDelete, "/v1/rules/"+url.PathEscape(name), nil, &out)
}

// OrderRules orders one channel's rules.
func (c *Client) OrderRules(ctx context.Context, req OrderRequest) (RuleSaveResult, error) {
	var out RuleSaveResult
	return out, c.do(ctx, http.MethodPost, "/v1/rules/order", req, &out)
}

// Channels lists the channels the Slack app is in.
func (c *Client) Channels(ctx context.Context) (ChannelsResult, error) {
	var out ChannelsResult
	return out, c.do(ctx, http.MethodGet, "/v1/channels", nil, &out)
}

// Owner returns the owner and the registration in progress.
func (c *Client) Owner(ctx context.Context) (OwnerInfo, error) {
	var out OwnerInfo
	return out, c.do(ctx, http.MethodGet, "/v1/owner", nil, &out)
}

// ClearOwner removes the owner.
func (c *Client) ClearOwner(ctx context.Context) (OwnerInfo, error) {
	var out OwnerInfo
	return out, c.do(ctx, http.MethodDelete, "/v1/owner", nil, &out)
}

// StartRegistration starts accepting the owner's code by DM.
func (c *Client) StartRegistration(ctx context.Context) (OwnerInfo, error) {
	var out OwnerInfo
	return out, c.do(ctx, http.MethodPost, "/v1/owner/registration", nil, &out)
}

// CancelRegistration stops accepting the owner's code.
func (c *Client) CancelRegistration(ctx context.Context) (OwnerInfo, error) {
	var out OwnerInfo
	return out, c.do(ctx, http.MethodDelete, "/v1/owner/registration", nil, &out)
}

// SetNotify turns a DM notice setting ("notify_dm", "notify_dm_failures") on or off.
func (c *Client) SetNotify(ctx context.Context, key string, on bool) (OwnerInfo, error) {
	var out OwnerInfo
	return out, c.do(ctx, http.MethodPut, "/v1/notify", NotifyRequest{Key: key, On: on}, &out)
}

// Users searches the workspace's people by name (needs users:read).
func (c *Client) Users(ctx context.Context, q string, limit int) (UsersResult, error) {
	var out UsersResult
	v := url.Values{"q": {q}, "limit": {strconv.Itoa(limit)}}
	err := c.do(ctx, http.MethodGet, "/v1/users?"+v.Encode(), nil, &out)
	return out, err
}

// Config returns the config file content.
func (c *Client) Config(ctx context.Context) (ConfigBody, error) {
	var out ConfigBody
	return out, c.do(ctx, http.MethodGet, "/v1/config", nil, &out)
}

// PutConfig validates, writes and reloads a new config.
func (c *Client) PutConfig(ctx context.Context, content string) error {
	return c.do(ctx, http.MethodPut, "/v1/config", ConfigBody{Content: content}, nil)
}

// Logs returns the last lines of the log.
func (c *Client) Logs(ctx context.Context, limit int) ([]string, error) {
	var out struct {
		Lines []string `json:"lines"`
	}
	return out.Lines, c.do(ctx, http.MethodGet, fmt.Sprintf("/v1/logs?limit=%d", limit), nil, &out)
}

// Events streams gateway events until ctx ends.
func (c *Client) Events(ctx context.Context, fn func(gateway.Event)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://sockdock/v1/events", nil)
	if err != nil {
		return err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return ErrNotRunning
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := sc.Text()
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue
		}
		var e gateway.Event
		if json.Unmarshal([]byte(data), &e) == nil {
			fn(e)
		}
	}
	return sc.Err()
}
