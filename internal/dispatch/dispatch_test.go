package dispatch

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSameKeyIsSerialDifferentKeysParallel(t *testing.T) {
	var (
		mu        sync.Mutex
		perKey    = map[string]int{}
		maxPerKey int
		inFlight  atomic.Int32
		maxFlight atomic.Int32
	)
	d := New(4, func(key string, batch []int) {
		mu.Lock()
		perKey[key]++
		if perKey[key] > maxPerKey {
			maxPerKey = perKey[key]
		}
		mu.Unlock()
		n := inFlight.Add(1)
		for {
			m := maxFlight.Load()
			if n <= m || maxFlight.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		inFlight.Add(-1)
		mu.Lock()
		perKey[key]--
		mu.Unlock()
	})
	for i := range 3 {
		for _, k := range []string{"a", "b", "c"} {
			d.Submit(k, i, false)
		}
	}
	if err := d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if maxPerKey != 1 {
		t.Errorf("a key ran %d batches at once", maxPerKey)
	}
	if maxFlight.Load() < 2 {
		t.Errorf("different keys did not run in parallel (max %d)", maxFlight.Load())
	}
}

func TestConcurrencyLimit(t *testing.T) {
	var inFlight, maxFlight atomic.Int32
	d := New(2, func(string, []int) {
		n := inFlight.Add(1)
		for {
			m := maxFlight.Load()
			if n <= m || maxFlight.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		inFlight.Add(-1)
	})
	for i, k := range []string{"a", "b", "c", "d", "e"} {
		d.Submit(k, i, false)
	}
	_ = d.Shutdown(context.Background())
	if maxFlight.Load() > 2 {
		t.Fatalf("limit exceeded: %d", maxFlight.Load())
	}
}

func TestCoalesce(t *testing.T) {
	release := make(chan struct{})
	var batches [][]int
	var mu sync.Mutex
	d := New(1, func(_ string, batch []int) {
		mu.Lock()
		batches = append(batches, append([]int{}, batch...))
		first := len(batches) == 1
		mu.Unlock()
		if first {
			<-release
		}
	})
	d.Submit("t", 1, true)
	time.Sleep(20 * time.Millisecond) // first batch is now running
	d.Submit("t", 2, true)
	d.Submit("t", 3, true)
	close(release)
	_ = d.Shutdown(context.Background())
	if len(batches) != 2 || len(batches[1]) != 2 || batches[1][0] != 2 || batches[1][1] != 3 {
		t.Fatalf("batches = %v, want [[1] [2 3]]", batches)
	}
}

func TestSubmitAfterShutdown(t *testing.T) {
	d := New(1, func(string, []int) {})
	_ = d.Shutdown(context.Background())
	if d.Submit("a", 1, false) {
		t.Fatal("submit after shutdown should fail")
	}
}
