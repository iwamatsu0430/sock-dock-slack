package control

import (
	"encoding/json"
	"io"
	"net/http"
	"os"

	"github.com/iwamatsu0430/sock-dock-slack/internal/gateway"
	"github.com/iwamatsu0430/sock-dock-slack/internal/slackio"
)

// OwnerInfo is the owner ("本人") and the registration in progress.
type OwnerInfo struct {
	// Owner is the registered owner; absent when nobody is registered. Name
	// is "" when users:read is missing or Slack does not know the ID.
	Owner        *slackio.Member      `json:"owner,omitempty"`
	Registration gateway.Registration `json:"registration"`
	// NotifyDM and NotifyDMFailures are the DM notice settings.
	NotifyDM         bool `json:"notify_dm"`
	NotifyDMFailures bool `json:"notify_dm_failures"`
	// Problem is "missing_scope" when DMs cannot reach SockDock (im:history).
	Problem string `json:"problem,omitempty"`
}

// NotifyRequest turns a DM notice setting on or off. Key is "notify_dm" or
// "notify_dm_failures".
type NotifyRequest struct {
	Key string `json:"key"`
	On  bool   `json:"on"`
}

func (s *Server) ownerInfo(r *http.Request) OwnerInfo {
	st := s.gw.Status(os.Getpid())
	info := OwnerInfo{Registration: s.gw.Registration(), NotifyDM: st.NotifyDM, NotifyDMFailures: st.NotifyDMFailures}
	if st.Owner != "" {
		m := slackio.Member{ID: st.Owner}
		if named, ok := s.memberDir().Names(r.Context(), []string{st.Owner})[st.Owner]; ok {
			m = named
		}
		info.Owner = &m
	}
	if !st.HasScope(slackio.ScopeIMHistory) {
		info.Problem = "missing_scope"
	}
	return info
}

func (s *Server) getOwner(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.ownerInfo(r))
}

func (s *Server) clearOwner(w http.ResponseWriter, r *http.Request) {
	if err := s.gw.ClearOwner(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, s.ownerInfo(r))
}

func (s *Server) startRegistration(w http.ResponseWriter, r *http.Request) {
	if _, err := s.gw.StartRegistration(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, s.ownerInfo(r))
}

func (s *Server) cancelRegistration(w http.ResponseWriter, r *http.Request) {
	s.gw.CancelRegistration()
	writeJSON(w, http.StatusOK, s.ownerInfo(r))
}

func (s *Server) setNotify(w http.ResponseWriter, r *http.Request) {
	var req NotifyRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.gw.SetNotifyDM(req.Key, req.On); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, s.ownerInfo(r))
}
