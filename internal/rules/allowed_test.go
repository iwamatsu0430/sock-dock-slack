package rules

import (
	"slices"
	"testing"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
)

func TestAllowedUsersForm(t *testing.T) {
	f := Form{Name: "r", Channel: "C0123ABCD", Trigger: "starts_with", Keyword: "x", Type: "claude", Folder: t.TempDir(),
		Restricted: true, AllowedUsers: []string{" U0123ABCD ", "", "U0123ABCD", "W0456EFGH"}}
	r := FromForm(f, nil)
	if !slices.Equal(r.AllowedUsers, []string{"U0123ABCD", "W0456EFGH"}) {
		t.Fatalf("allowed_users = %v", r.AllowedUsers)
	}
	if back := ToForm(r); !back.Restricted || !slices.Equal(back.AllowedUsers, r.AllowedUsers) {
		t.Fatalf("round trip = %+v", back)
	}
	// Back to everyone: the list is dropped even if the form still holds it.
	f.Restricted = false
	if r := FromForm(f, r); r.AllowedUsers != nil {
		t.Fatalf("everyone should write no allowed_users, got %v", r.AllowedUsers)
	}
	if back := ToForm(FromForm(f, nil)); back.Restricted || back.AllowedUsers == nil {
		t.Fatalf("everyone: %+v", back)
	}

	raw := &config.Config{}
	f.Restricted, f.AllowedUsers = true, nil
	if ps := Validate(f, raw); len(ps) != 1 || ps[0].Field != "allowed_users" || ps[0].Code != "required" {
		t.Errorf("no one chosen: %+v", ps)
	}
	f.AllowedUsers = []string{"alex"}
	if ps := Validate(f, raw); len(ps) != 1 || ps[0].Code != "bad_user" || ps[0].Detail != "alex" {
		t.Errorf("bad id: %+v", ps)
	}
}

func TestOpenToAll(t *testing.T) {
	f := Form{Type: "claude", Commands: []string{"npm test"}, AllowEdit: true}
	if OpenToAll(f) != "commands" {
		t.Error("commands for everyone")
	}
	f.Commands = []string{" "}
	if OpenToAll(f) != "edit" {
		t.Error("edit for everyone")
	}
	f.Restricted = true
	if OpenToAll(f) != "" {
		t.Error("restricted rules are not open to all")
	}
	if OpenToAll(Form{Type: "command", AllowEdit: true}) != "" {
		t.Error("programs are not judged")
	}
}
