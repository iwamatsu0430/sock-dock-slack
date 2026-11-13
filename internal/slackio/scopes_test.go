package slackio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestAuthTestScopes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer xoxb-good" {
			w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
			return
		}
		w.Header().Set("X-OAuth-Scopes", "chat:write, channels:history,groups:read")
		w.Write([]byte(`{"ok":true,"team":"acme","user_id":"U1","bot_id":"B1"}`))
	}))
	defer srv.Close()
	authTestURL = srv.URL
	defer func() { authTestURL = "https://slack.com/api/auth.test" }()

	id, scopes, err := AuthTestScopes(context.Background(), "xoxb-good")
	if err != nil || id.Team != "acme" || !slices.Equal(scopes, []string{"chat:write", "channels:history", "groups:read"}) {
		t.Fatalf("got %+v %v %v", id, scopes, err)
	}
	if missing := MissingScopes(scopes, RequiredBotScopes); !slices.Equal(missing, []string{"groups:history", "reactions:write"}) {
		t.Errorf("missing = %v", missing)
	}
	if _, _, err := AuthTestScopes(context.Background(), "xoxb-bad"); err == nil || err.Error() != "invalid_auth" {
		t.Errorf("bad token: %v", err)
	}
}
