package builder

import "sync"

// Queue serializes operations on the same key while allowing different
// keys to run in parallel. Used to ensure one build per env at a time.
type Queue struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewQueue returns an empty Queue.
func NewQueue() *Queue {
	return &Queue{locks: make(map[string]*sync.Mutex)}
}

// Acquire blocks until the lock for key is available, then returns a
// release function the caller MUST call (typically via defer).
func (q *Queue) Acquire(key string) func() {
	q.mu.Lock()
	m, ok := q.locks[key]
	if !ok {
		m = &sync.Mutex{}
		q.locks[key] = m
	}
	q.mu.Unlock()
	m.Lock()
	return func() { m.Unlock() }
}
