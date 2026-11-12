package lockfile

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestSingleInstance(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.lock")
	l, err := Acquire(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(p); !errors.Is(err, ErrLocked) {
		t.Fatalf("second acquire: %v", err)
	}
	l.Release()
	l2, err := Acquire(p)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	l2.Release()
}
