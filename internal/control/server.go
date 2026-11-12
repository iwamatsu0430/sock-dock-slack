// Package control serves sockdock's local control API over a Unix domain
// socket. The CLI and the GUI are its clients.
package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
	"github.com/iwamatsu0430/sock-dock-slack/internal/gateway"
)

// Server is the control API server.
type Server struct {
	gw      *gateway.Gateway
	log     *slog.Logger
	logFile string
	srv     *http.Server
}

// NewServer creates a server for gw. logFile is served by /v1/logs.
func NewServer(gw *gateway.Gateway, log *slog.Logger, logFile string) *Server {
	s := &Server{gw: gw, log: log, logFile: logFile}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", s.status)
	mux.HandleFunc("GET /v1/events", s.events)
	mux.HandleFunc("GET /v1/threads", s.threads)
	mux.HandleFunc("GET /v1/turns", s.turns)
	mux.HandleFunc("GET /v1/config", s.getConfig)
	mux.HandleFunc("PUT /v1/config", s.putConfig)
	mux.HandleFunc("POST /v1/config/validate", s.validateConfig)
	mux.HandleFunc("POST /v1/reload", s.reload)
	mux.HandleFunc("POST /v1/match", s.match)
	mux.HandleFunc("POST /v1/routes/{name}/pause", s.pause(true))
	mux.HandleFunc("POST /v1/routes/{name}/resume", s.pause(false))
	mux.HandleFunc("GET /v1/logs", s.logs)
	s.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	return s
}

// Serve listens on the socket path until Shutdown. A stale socket is removed.
func (s *Server) Serve(socketPath string) error {
	_ = os.Remove(socketPath)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		ln.Close()
		return err
	}
	err = s.srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown stops the server.
func (s *Server) Shutdown(ctx context.Context) error { return s.srv.Shutdown(ctx) }

func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.gw.Status(os.Getpid()))
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch, cancel := s.gw.Subscribe()
	defer cancel()
	send := func(e gateway.Event) {
		b, _ := json.Marshal(e)
		fmt.Fprintf(w, "data: %s\n\n", b)
		fl.Flush()
	}
	send(gateway.Event{Type: "status", Data: s.gw.Status(os.Getpid())})
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			send(e)
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

func (s *Server) threads(w http.ResponseWriter, r *http.Request) {
	out, err := s.gw.Threads(r.Context(), limitParam(r, 50))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) turns(w http.ResponseWriter, r *http.Request) {
	out, err := s.gw.Turns(r.Context(), limitParam(r, 50))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// ConfigBody is the payload of GET/PUT /v1/config.
type ConfigBody struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (s *Server) getConfig(w http.ResponseWriter, _ *http.Request) {
	path := s.gw.Config().Path
	b, err := os.ReadFile(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, ConfigBody{Path: path, Content: string(b)})
}

// ValidateResult reports config problems.
type ValidateResult struct {
	OK       bool     `json:"ok"`
	Problems []string `json:"problems,omitempty"`
}

func validate(path string, content []byte) ValidateResult {
	if _, err := config.Parse(content, path); err != nil {
		var ve *config.ValidationError
		if errors.As(err, &ve) {
			return ValidateResult{Problems: ve.Problems}
		}
		return ValidateResult{Problems: []string{err.Error()}}
	}
	return ValidateResult{OK: true}
}

func (s *Server) validateConfig(w http.ResponseWriter, r *http.Request) {
	var body ConfigBody
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, validate(s.gw.Config().Path, []byte(body.Content)))
}

func (s *Server) putConfig(w http.ResponseWriter, r *http.Request) {
	var body ConfigBody
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	path := s.gw.Config().Path
	if res := validate(path, []byte(body.Content)); !res.OK {
		writeJSON(w, http.StatusUnprocessableEntity, res)
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.yaml")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	_, werr := tmp.WriteString(body.Content)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr, os.Chmod(tmp.Name(), 0o600), os.Rename(tmp.Name(), path)); err != nil {
		os.Remove(tmp.Name())
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.gw.Reload(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, ValidateResult{OK: true})
}

func (s *Server) reload(w http.ResponseWriter, _ *http.Request) {
	if err := s.gw.Reload(); err != nil {
		var ve *config.ValidationError
		if errors.As(err, &ve) {
			writeJSON(w, http.StatusUnprocessableEntity, ValidateResult{Problems: ve.Problems})
			return
		}
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	writeJSON(w, http.StatusOK, ValidateResult{OK: true})
}

// MatchRequest asks which route a message would hit.
type MatchRequest struct {
	Channel string `json:"channel"`
	Text    string `json:"text"`
}

// MatchResult is the answer to MatchRequest.
type MatchResult struct {
	Matched bool              `json:"matched"`
	Route   string            `json:"route,omitempty"`
	Groups  map[string]string `json:"groups,omitempty"`
}

// MatchConfig evaluates a message against a config.
func MatchConfig(cfg *config.Config, req MatchRequest) MatchResult {
	r, groups := cfg.Match(req.Channel, req.Text)
	if r == nil {
		return MatchResult{}
	}
	return MatchResult{Matched: true, Route: r.Name, Groups: groups}
}

func (s *Server) match(w http.ResponseWriter, r *http.Request) {
	var req MatchRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, MatchConfig(s.gw.Config(), req))
}

func (s *Server) pause(paused bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := s.gw.SetPaused(r.Context(), r.PathValue("name"), paused); err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"paused": paused})
	}
}

func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	lines, err := tailLines(s.logFile, limitParam(r, 200))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]string{"lines": lines})
}

// tailLines returns the last n lines of a file (reading at most 2 MiB).
func tailLines(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	const maxRead = 2 << 20
	off := max(st.Size()-maxRead, 0)
	buf := make([]byte, st.Size()-off)
	if _, err := f.ReadAt(buf, off); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(buf), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

func limitParam(r *http.Request, def int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 1000 {
		return v
	}
	return def
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}
