// Package buildlog provides a fan-out writer that tees build output to a
// log file on disk plus an in-memory ring buffer. WS subscribers attach
// after the fact, receive whatever bytes the ring still holds, then live
// stream new writes as they arrive.
package buildlog

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

// Log is one build's output sink. Concurrent writes are serialized.
type Log struct {
	mu          sync.Mutex
	file        *os.File
	ring        []byte
	ringPos     int
	ringFull    bool
	subscribers map[chan []byte]struct{}
	closed      bool
}

// New opens (truncates) the log file at path and returns a Log with the
// requested ring-buffer capacity. ringSize must be > 0.
func New(path string, ringSize int) (*Log, error) {
	if ringSize <= 0 {
		return nil, errors.New("ringSize must be > 0")
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create log file: %w", err)
	}
	return &Log{
		file:        f,
		ring:        make([]byte, 0, ringSize),
		subscribers: make(map[chan []byte]struct{}),
	}, nil
}

// Write writes p to the file and ring, and broadcasts to subscribers.
// Implements io.Writer, so it can be passed as cmd.Stdout / cmd.Stderr.
func (l *Log) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return 0, io.ErrClosedPipe
	}
	n, err := l.file.Write(p)
	if err != nil {
		return n, err
	}
	l.appendToRing(p)
	for sub := range l.subscribers {
		chunk := make([]byte, len(p))
		copy(chunk, p)
		select {
		case sub <- chunk:
		default:
		}
	}
	return n, nil
}

// appendToRing copies p into the ring, wrapping when capacity is reached.
// Caller must hold the mutex.
func (l *Log) appendToRing(p []byte) {
	capacity := cap(l.ring)
	for len(p) > 0 {
		end := l.ringPos + len(p)
		if end > capacity {
			end = capacity
		}
		chunk := p[:end-l.ringPos]
		if l.ringPos == len(l.ring) {
			l.ring = append(l.ring, chunk...)
		} else {
			copy(l.ring[l.ringPos:end], chunk)
		}
		l.ringPos = end
		p = p[len(chunk):]
		if l.ringPos == capacity {
			l.ringPos = 0
			l.ringFull = true
		}
	}
}

// Snapshot returns a copy of the bytes currently in the ring buffer
// (the most recent up-to-ringSize bytes of output).
func (l *Log) Snapshot() []byte {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.ringFull {
		out := make([]byte, len(l.ring))
		copy(out, l.ring)
		return out
	}
	out := make([]byte, cap(l.ring))
	n := copy(out, l.ring[l.ringPos:])
	copy(out[n:], l.ring[:l.ringPos])
	return out
}

// Subscribe returns a channel that receives every chunk written from this
// point forward. Buffered to avoid blocking the writer; if a subscriber
// falls behind, chunks are dropped (slow consumers don't slow the build).
// Caller MUST call Unsubscribe(ch) to release resources.
func (l *Log) Subscribe() <-chan []byte {
	l.mu.Lock()
	defer l.mu.Unlock()
	ch := make(chan []byte, 64)
	l.subscribers[ch] = struct{}{}
	return ch
}

// Unsubscribe removes a subscriber and closes its channel.
// Safe to call multiple times.
func (l *Log) Unsubscribe(ch <-chan []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for sub := range l.subscribers {
		if (<-chan []byte)(sub) == ch {
			delete(l.subscribers, sub)
			close(sub)
			return
		}
	}
}

// Close flushes the file and closes all subscriber channels.
// After Close, Write returns io.ErrClosedPipe.
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	for sub := range l.subscribers {
		close(sub)
		delete(l.subscribers, sub)
	}
	return l.file.Close()
}
