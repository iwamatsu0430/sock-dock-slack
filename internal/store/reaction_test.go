package store

import (
	"context"
	"testing"
)

func TestThreadReactionState(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	fresh, err := s.BindThreadIfNew(ctx, Thread{Channel: "C1", ThreadTS: "1.1", Route: "r", Reaction: "added"})
	if err != nil || !fresh {
		t.Fatalf("first bind: %v %v", fresh, err)
	}
	if fresh, _ := s.BindThreadIfNew(ctx, Thread{Channel: "C1", ThreadTS: "1.1", Route: "other", Reaction: "added"}); fresh {
		t.Error("a second bind reported a new thread")
	}
	// Only one of two events that arrive together moves the state.
	if ok, _ := s.SetThreadReaction(ctx, "C1", "1.1", "removed", "added"); ok {
		t.Error("moved from a state the thread is not in")
	}
	if ok, err := s.SetThreadReaction(ctx, "C1", "1.1", "added", "removed"); err != nil || !ok {
		t.Fatalf("added -> removed: %v %v", ok, err)
	}
	if ok, _ := s.SetThreadReaction(ctx, "C1", "1.1", "added", "removed"); ok {
		t.Error("the same move went through twice")
	}
	th, err := s.GetThread(ctx, "C1", "1.1")
	if err != nil || th.Route != "r" || th.Reaction != "removed" {
		t.Errorf("thread = %+v, %v", th, err)
	}

	id, err := s.StartTurn(ctx, Turn{Channel: "C1", ThreadTS: "1.1", Route: "r", Reaction: "removed"})
	if err != nil {
		t.Fatal(err)
	}
	turns, err := s.RunningTurns(ctx)
	if err != nil || len(turns) != 1 || turns[0].ID != id || turns[0].Reaction != "removed" {
		t.Errorf("turns = %+v, %v", turns, err)
	}
}
