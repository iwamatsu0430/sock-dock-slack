// Package store keeps sockdock's state in SQLite: thread bindings, turns,
// processed events and small key/value state.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // driver "sqlite"
)

// Turn statuses.
const (
	StatusRunning     = "running"
	StatusOK          = "ok"
	StatusError       = "error"
	StatusTimeout     = "timeout"
	StatusInterrupted = "interrupted"
	StatusStopped     = "stopped"
)

// FileProblemMissingScope: attached files were not handed over for lack of
// the files:read scope.
const FileProblemMissingScope = "missing_scope"

// Store wraps the database.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// Thread is a Slack thread bound to a route.
type Thread struct {
	Channel      string            `json:"channel"`
	ThreadTS     string            `json:"thread_ts"`
	Route        string            `json:"route"`
	Groups       map[string]string `json:"groups"`
	SessionID    string            `json:"session_id,omitempty"`
	Permalink    string            `json:"permalink,omitempty"`
	Turns        int               `json:"turns"`
	CreatedAt    time.Time         `json:"created_at"`
	LastActiveAt time.Time         `json:"last_active_at"`
}

// Turn is one handler run.
type Turn struct {
	ID         int64      `json:"id"`
	Channel    string     `json:"channel"`
	ThreadTS   string     `json:"thread_ts"`
	MessageTS  []string   `json:"message_ts"`
	Route      string     `json:"route"`
	Status     string     `json:"status"`
	Error      string     `json:"error,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	CostUSD    float64    `json:"cost_usd,omitempty"`
	// FileProblem is FileProblemMissingScope when attached files could not
	// be handed over because the Slack app lacks files:read.
	FileProblem string `json:"file_problem,omitempty"`
	// Permalink is the thread's link (from the threads table).
	Permalink string `json:"permalink,omitempty"`
}

const schema = `
CREATE TABLE IF NOT EXISTS threads (
  channel TEXT NOT NULL,
  thread_ts TEXT NOT NULL,
  route TEXT NOT NULL,
  groups_json TEXT NOT NULL DEFAULT '{}',
  session_id TEXT NOT NULL DEFAULT '',
  permalink TEXT NOT NULL DEFAULT '',
  turns INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  last_active_at INTEGER NOT NULL,
  PRIMARY KEY (channel, thread_ts)
);
CREATE TABLE IF NOT EXISTS turns (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  channel TEXT NOT NULL,
  thread_ts TEXT NOT NULL,
  message_ts TEXT NOT NULL DEFAULT '[]',
  route TEXT NOT NULL,
  status TEXT NOT NULL,
  error TEXT NOT NULL DEFAULT '',
  started_at INTEGER NOT NULL,
  finished_at INTEGER,
  cost_usd REAL NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS turns_status ON turns(status);
CREATE INDEX IF NOT EXISTS turns_thread ON turns(channel, thread_ts);
CREATE TABLE IF NOT EXISTS seen_events (
  channel TEXT NOT NULL,
  ts TEXT NOT NULL,
  seen_at INTEGER NOT NULL,
  PRIMARY KEY (channel, ts)
);
CREATE TABLE IF NOT EXISTS state (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
`

// Open opens (and migrates) the database at path.
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite: serialize writes, avoid SQLITE_BUSY churn
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if err := addColumn(db, "turns", "file_problem", "TEXT NOT NULL DEFAULT ''"); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db, now: time.Now}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// MarkSeen records a message. It returns false if it was already recorded.
func (s *Store) MarkSeen(ctx context.Context, channel, ts string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO seen_events(channel, ts, seen_at) VALUES (?, ?, ?)`,
		channel, ts, s.now().Unix())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// BindThread stores a new thread binding. An existing binding is kept.
func (s *Store) BindThread(ctx context.Context, t Thread) error {
	g, _ := json.Marshal(t.Groups)
	now := s.now().Unix()
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO threads(channel, thread_ts, route, groups_json, created_at, last_active_at)
		 VALUES (?, ?, ?, ?, ?, ?)`, t.Channel, t.ThreadTS, t.Route, string(g), now, now)
	return err
}

// GetThread returns the binding for a thread, or nil if there is none.
func (s *Store) GetThread(ctx context.Context, channel, threadTS string) (*Thread, error) {
	row := s.db.QueryRowContext(ctx, `SELECT channel, thread_ts, route, groups_json, session_id, permalink,
		turns, created_at, last_active_at FROM threads WHERE channel = ? AND thread_ts = ?`, channel, threadTS)
	t, err := scanThread(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

// UpdateThreadAfterTurn bumps the turn counter and stores the session and permalink.
func (s *Store) UpdateThreadAfterTurn(ctx context.Context, channel, threadTS, sessionID, permalink string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE threads SET turns = turns + 1, last_active_at = ?,
		session_id = CASE WHEN ? <> '' THEN ? ELSE session_id END,
		permalink = CASE WHEN ? <> '' THEN ? ELSE permalink END
		WHERE channel = ? AND thread_ts = ?`,
		s.now().Unix(), sessionID, sessionID, permalink, permalink, channel, threadTS)
	return err
}

// ListThreads returns the most recently active threads.
func (s *Store) ListThreads(ctx context.Context, limit int) ([]Thread, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT channel, thread_ts, route, groups_json, session_id, permalink,
		turns, created_at, last_active_at FROM threads ORDER BY last_active_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Thread
	for rows.Next() {
		t, err := scanThread(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// StartTurn records a running turn and returns its id.
func (s *Store) StartTurn(ctx context.Context, t Turn) (int64, error) {
	ts, _ := json.Marshal(t.MessageTS)
	res, err := s.db.ExecContext(ctx, `INSERT INTO turns(channel, thread_ts, message_ts, route, status, started_at)
		VALUES (?, ?, ?, ?, ?, ?)`, t.Channel, t.ThreadTS, string(ts), t.Route, StatusRunning, s.now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FinishTurn sets the final status of a turn.
func (s *Store) FinishTurn(ctx context.Context, id int64, status, errMsg string, costUSD float64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE turns SET status = ?, error = ?, finished_at = ?, cost_usd = ? WHERE id = ?`,
		status, errMsg, s.now().Unix(), costUSD, id)
	return err
}

// SetFileProblem records why a turn's attached files were not handed over.
func (s *Store) SetFileProblem(ctx context.Context, id int64, problem string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE turns SET file_problem = ? WHERE id = ?`, problem, id)
	return err
}

// RunningTurns returns turns still marked as running.
func (s *Store) RunningTurns(ctx context.Context) ([]Turn, error) {
	return s.queryTurns(ctx, `WHERE status = ? ORDER BY id`, StatusRunning)
}

// ListTurns returns the most recent turns.
func (s *Store) ListTurns(ctx context.Context, limit int) ([]Turn, error) {
	return s.queryTurns(ctx, `ORDER BY id DESC LIMIT ?`, limit)
}

// ListRouteTurns returns the most recent turns of one route.
func (s *Store) ListRouteTurns(ctx context.Context, route string, limit int) ([]Turn, error) {
	return s.queryTurns(ctx, `WHERE route = ? ORDER BY id DESC LIMIT ?`, route, limit)
}

func (s *Store) queryTurns(ctx context.Context, tail string, args ...any) ([]Turn, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, channel, thread_ts, message_ts, route, status, error,
		started_at, finished_at, cost_usd, file_problem, COALESCE((SELECT permalink FROM threads th
		WHERE th.channel = turns.channel AND th.thread_ts = turns.thread_ts), '') FROM turns `+tail, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Turn
	for rows.Next() {
		var (
			t        Turn
			msgTS    string
			started  int64
			finished sql.NullInt64
		)
		if err := rows.Scan(&t.ID, &t.Channel, &t.ThreadTS, &msgTS, &t.Route, &t.Status, &t.Error,
			&started, &finished, &t.CostUSD, &t.FileProblem, &t.Permalink); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(msgTS), &t.MessageTS)
		t.StartedAt = time.Unix(started, 0)
		if finished.Valid {
			f := time.Unix(finished.Int64, 0)
			t.FinishedAt = &f
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Prune deletes processed-event records older than the cutoff.
func (s *Store) Prune(ctx context.Context, seenBefore time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM seen_events WHERE seen_at < ?`, seenBefore.Unix())
	return err
}

// GetState reads a JSON value. It returns false if the key is absent.
func (s *Store) GetState(ctx context.Context, key string, v any) (bool, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM state WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal([]byte(raw), v)
}

// SetState writes a JSON value.
func (s *Store) SetState(ctx context.Context, key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO state(key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, string(raw))
	return err
}

type scanner interface{ Scan(...any) error }

func scanThread(sc scanner) (*Thread, error) {
	var (
		t               Thread
		groups          string
		created, active int64
	)
	if err := sc.Scan(&t.Channel, &t.ThreadTS, &t.Route, &groups, &t.SessionID, &t.Permalink,
		&t.Turns, &created, &active); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(groups), &t.Groups)
	t.CreatedAt = time.Unix(created, 0)
	t.LastActiveAt = time.Unix(active, 0)
	return &t, nil
}

// addColumn adds a column to a table made by an earlier version.
func addColumn(db *sql.DB, table, column, def string) error {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	_, err = db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + def)
	return err
}
