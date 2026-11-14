package slackio

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/slack-go/slack"
)

// Member is a person in the workspace, for choosing who may run a rule
// (docs/design.md §3.4). It needs the users:read scope.
type Member struct {
	ID string `json:"id"`
	// Name is the display name, or the real name when there is none.
	Name string `json:"name"`
	// RealName is the full name ("" when it is the same as Name).
	RealName string `json:"real_name,omitempty"`
}

// FetchMembers lists the workspace's people (users.list, all pages), without
// deactivated accounts and bots, sorted by name.
func FetchMembers(ctx context.Context, c *slack.Client) ([]Member, error) {
	users, err := c.GetUsersContext(ctx, slack.GetUsersOptionLimit(200))
	if err != nil {
		return nil, err
	}
	out := make([]Member, 0, len(users))
	for _, u := range users {
		if u.Deleted || u.IsBot || u.ID == "USLACKBOT" {
			continue
		}
		out = append(out, memberOf(u))
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

func memberOf(u slack.User) Member {
	m := Member{ID: u.ID, Name: u.Profile.DisplayName, RealName: u.Profile.RealName}
	if m.RealName == "" {
		m.RealName = u.RealName
	}
	if m.Name == "" {
		m.Name, m.RealName = m.RealName, ""
	}
	if m.Name == "" {
		m.Name = u.Name
	}
	if m.RealName == m.Name {
		m.RealName = ""
	}
	return m
}

// MemberDirectory keeps the workspace's people so the name field does not call
// users.list (rate tier 2) every time it opens. The list is fetched again when
// it is older than an hour or Refresh is called.
type MemberDirectory struct {
	// Fetch lists the people; it is called with the lock released.
	Fetch func(ctx context.Context) ([]Member, error)

	mu      sync.Mutex
	members []Member
	byID    map[string]Member
	fetched time.Time
}

// memberTTL is how long a fetched list is used.
const memberTTL = time.Hour

// Members returns the people, fetching them when the list is missing or old.
func (d *MemberDirectory) Members(ctx context.Context) ([]Member, error) {
	d.mu.Lock()
	if d.members != nil && time.Since(d.fetched) < memberTTL {
		m := d.members
		d.mu.Unlock()
		return m, nil
	}
	d.mu.Unlock()
	members, err := d.Fetch(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]Member, len(members))
	for _, m := range members {
		byID[m.ID] = m
	}
	d.mu.Lock()
	d.members, d.byID, d.fetched = members, byID, time.Now()
	d.mu.Unlock()
	return members, nil
}

// Refresh drops the kept list (after reconnecting, the token may differ).
func (d *MemberDirectory) Refresh() {
	d.mu.Lock()
	d.members, d.byID = nil, nil
	d.mu.Unlock()
}

// Search returns up to limit people whose name, real name or ID contains q
// (case-insensitive). An empty q returns the first limit people.
func (d *MemberDirectory) Search(ctx context.Context, q string, limit int) ([]Member, error) {
	members, err := d.Members(ctx)
	if err != nil {
		return nil, err
	}
	return SearchMembers(members, q, limit), nil
}

// SearchMembers filters people by name, real name or ID.
func SearchMembers(members []Member, q string, limit int) []Member {
	q = strings.ToLower(strings.TrimSpace(q))
	out := []Member{}
	for _, m := range members {
		if limit > 0 && len(out) >= limit {
			break
		}
		if q == "" || strings.Contains(strings.ToLower(m.Name), q) || strings.Contains(strings.ToLower(m.RealName), q) || strings.ToLower(m.ID) == q {
			out = append(out, m)
		}
	}
	return out
}

// Names maps the given member IDs to people. IDs Slack does not know (or all
// of them, when the list cannot be fetched) are left out.
func (d *MemberDirectory) Names(ctx context.Context, ids []string) map[string]Member {
	out := map[string]Member{}
	if len(ids) == 0 {
		return out
	}
	if _, err := d.Members(ctx); err != nil {
		return out
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, id := range ids {
		if m, ok := d.byID[id]; ok {
			out[id] = m
		}
	}
	return out
}

// IsMissingScope reports Slack's missing_scope error.
func IsMissingScope(err error) bool {
	return err != nil && strings.Contains(err.Error(), "missing_scope")
}

// MemberLister lists the workspace's people (needs the users:read scope).
type MemberLister interface {
	Members(ctx context.Context) ([]Member, error)
}

// Members lists the workspace's people (see FetchMembers).
func (w *WebAPI) Members(ctx context.Context) ([]Member, error) { return FetchMembers(ctx, w.c) }
