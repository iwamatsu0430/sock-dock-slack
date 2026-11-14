package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSeenDedup(t *testing.T) {
	s, ctx := open(t), context.Background()
	if ok, _ := s.MarkSeen(ctx, "C", "1.0"); !ok {
		t.Fatal("first should be fresh")
	}
	if ok, _ := s.MarkSeen(ctx, "C", "1.0"); ok {
		t.Fatal("second should be a duplicate")
	}
	s.now = func() time.Time { return time.Now().Add(8 * 24 * time.Hour) }
	if err := s.Prune(ctx, s.now().Add(-7*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.MarkSeen(ctx, "C", "1.0"); !ok {
		t.Fatal("pruned record should be fresh again")
	}
}

func TestThreadLifecycle(t *testing.T) {
	s, ctx := open(t), context.Background()
	if th, err := s.GetThread(ctx, "C", "1.0"); err != nil || th != nil {
		t.Fatalf("expected no thread, got %v %v", th, err)
	}
	if err := s.BindThread(ctx, Thread{Channel: "C", ThreadTS: "1.0", Route: "r", Groups: map[string]string{"code": "AB12"}}); err != nil {
		t.Fatal(err)
	}
	// Binding again keeps the first route.
	_ = s.BindThread(ctx, Thread{Channel: "C", ThreadTS: "1.0", Route: "other"})
	if err := s.UpdateThreadAfterTurn(ctx, "C", "1.0", "sess-1", "https://x/p1"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateThreadAfterTurn(ctx, "C", "1.0", "", ""); err != nil {
		t.Fatal(err)
	}
	th, err := s.GetThread(ctx, "C", "1.0")
	if err != nil {
		t.Fatal(err)
	}
	if th.Route != "r" || th.Groups["code"] != "AB12" || th.SessionID != "sess-1" || th.Permalink != "https://x/p1" || th.Turns != 2 {
		t.Fatalf("unexpected thread: %+v", th)
	}
	list, _ := s.ListThreads(ctx, 10)
	if len(list) != 1 {
		t.Fatalf("list = %v", list)
	}
}

func TestTurnsAndState(t *testing.T) {
	s, ctx := open(t), context.Background()
	id, err := s.StartTurn(ctx, Turn{Channel: "C", ThreadTS: "1.0", MessageTS: []string{"1.0", "1.1"}, Route: "r"})
	if err != nil {
		t.Fatal(err)
	}
	running, _ := s.RunningTurns(ctx)
	if len(running) != 1 || len(running[0].MessageTS) != 2 {
		t.Fatalf("running = %+v", running)
	}
	if err := s.FinishTurn(ctx, id, StatusOK, "", 0.12); err != nil {
		t.Fatal(err)
	}
	running, _ = s.RunningTurns(ctx)
	if len(running) != 0 {
		t.Fatal("turn still running")
	}
	turns, _ := s.ListTurns(ctx, 5)
	if turns[0].Status != StatusOK || turns[0].FinishedAt == nil || turns[0].CostUSD != 0.12 {
		t.Fatalf("turn = %+v", turns[0])
	}

	var v []string
	if ok, _ := s.GetState(ctx, "k", &v); ok {
		t.Fatal("unexpected state")
	}
	_ = s.SetState(ctx, "k", []string{"a"})
	_ = s.SetState(ctx, "k", []string{"b"})
	if ok, _ := s.GetState(ctx, "k", &v); !ok || len(v) != 1 || v[0] != "b" {
		t.Fatalf("state = %v", v)
	}
}

func TestFileProblemColumnAddedToOldDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	// turns as created before file_problem existed.
	if _, err := db.Exec(`CREATE TABLE turns (id INTEGER PRIMARY KEY AUTOINCREMENT, channel TEXT NOT NULL,
		thread_ts TEXT NOT NULL, message_ts TEXT NOT NULL DEFAULT '[]', route TEXT NOT NULL, status TEXT NOT NULL,
		error TEXT NOT NULL DEFAULT '', started_at INTEGER NOT NULL, finished_at INTEGER, cost_usd REAL NOT NULL DEFAULT 0);
		INSERT INTO turns(channel, thread_ts, route, status, started_at) VALUES ('C1', '1.1', 'r', 'ok', 1);`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.SetFileProblem(ctx, 1, FileProblemMissingScope); err != nil {
		t.Fatal(err)
	}
	turns, err := s.ListTurns(ctx, 1)
	if err != nil || len(turns) != 1 || turns[0].FileProblem != FileProblemMissingScope {
		t.Fatalf("turns = %+v, err %v", turns, err)
	}
	s.Close()
	if s, err = Open(path); err != nil { // opening again must not add it twice
		t.Fatal(err)
	}
}
