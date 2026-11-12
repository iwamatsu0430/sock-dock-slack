// Package dispatch runs work one item at a time per key (thread) while
// different keys run in parallel, bounded by a global concurrency limit.
package dispatch

import (
	"context"
	"sync"
)

// ProcessFunc handles a batch of items for one key. With coalescing enabled
// the batch holds every item queued since the last run; otherwise one item.
type ProcessFunc[T any] func(key string, batch []T)

// Dispatcher serializes work per key.
type Dispatcher[T any] struct {
	process ProcessFunc[T]
	sem     chan struct{}

	mu     sync.Mutex
	queues map[string]*queue[T]
	active map[string]bool
	closed bool
	wg     sync.WaitGroup
}

type queue[T any] struct {
	items    []T
	coalesce bool
}

// New creates a dispatcher that runs at most maxConcurrent batches at once.
func New[T any](maxConcurrent int, process ProcessFunc[T]) *Dispatcher[T] {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &Dispatcher[T]{
		process: process,
		sem:     make(chan struct{}, maxConcurrent),
		queues:  map[string]*queue[T]{},
		active:  map[string]bool{},
	}
}

// Submit queues an item for key. coalesce controls whether items that pile up
// while the key is busy are handed over together in one batch.
// It returns false after Shutdown.
func (d *Dispatcher[T]) Submit(key string, item T, coalesce bool) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return false
	}
	q, ok := d.queues[key]
	if !ok {
		q = &queue[T]{}
		d.queues[key] = q
	}
	q.coalesce = coalesce
	q.items = append(q.items, item)
	if !ok {
		d.wg.Add(1)
		go d.worker(key, q)
	}
	return true
}

func (d *Dispatcher[T]) worker(key string, q *queue[T]) {
	defer d.wg.Done()
	for {
		d.mu.Lock()
		if len(q.items) == 0 {
			delete(d.queues, key)
			d.mu.Unlock()
			return
		}
		var batch []T
		if q.coalesce {
			batch, q.items = q.items, nil
		} else {
			batch, q.items = q.items[:1:1], q.items[1:]
		}
		d.mu.Unlock()

		d.sem <- struct{}{}
		d.setActive(key, true)
		d.process(key, batch)
		d.setActive(key, false)
		<-d.sem
	}
}

func (d *Dispatcher[T]) setActive(key string, on bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if on {
		d.active[key] = true
	} else {
		delete(d.active, key)
	}
}

// Active returns the keys whose batch is running right now.
func (d *Dispatcher[T]) Active() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]string, 0, len(d.active))
	for k := range d.active {
		out = append(out, k)
	}
	return out
}

// Pending returns the number of queued (not yet running) items.
func (d *Dispatcher[T]) Pending() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, q := range d.queues {
		n += len(q.items)
	}
	return n
}

// Shutdown stops accepting work and waits for queued work to finish or ctx to end.
func (d *Dispatcher[T]) Shutdown(ctx context.Context) error {
	d.mu.Lock()
	d.closed = true
	d.mu.Unlock()
	done := make(chan struct{})
	go func() { d.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
