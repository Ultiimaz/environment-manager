package builder

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueue_SerializesSameEnv(t *testing.T) {
	q := NewQueue()
	var concurrent int32
	var maxConcurrent int32

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release := q.Acquire("env-a")
			defer release()
			c := atomic.AddInt32(&concurrent, 1)
			defer atomic.AddInt32(&concurrent, -1)
			for {
				m := atomic.LoadInt32(&maxConcurrent)
				if c <= m || atomic.CompareAndSwapInt32(&maxConcurrent, m, c) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
		}()
	}
	wg.Wait()

	if maxConcurrent != 1 {
		t.Errorf("maxConcurrent for same env = %d, want 1", maxConcurrent)
	}
}

func TestQueue_ParallelDifferentEnvs(t *testing.T) {
	q := NewQueue()
	var concurrent int32
	var maxConcurrent int32

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		envID := "env-" + string(rune('a'+i))
		wg.Add(1)
		go func() {
			defer wg.Done()
			release := q.Acquire(envID)
			defer release()
			c := atomic.AddInt32(&concurrent, 1)
			defer atomic.AddInt32(&concurrent, -1)
			for {
				m := atomic.LoadInt32(&maxConcurrent)
				if c <= m || atomic.CompareAndSwapInt32(&maxConcurrent, m, c) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
		}()
	}
	wg.Wait()

	if maxConcurrent < 2 {
		t.Errorf("maxConcurrent across 4 envs = %d, expected >= 2", maxConcurrent)
	}
}
