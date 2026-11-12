package store

import (
	"context"
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
