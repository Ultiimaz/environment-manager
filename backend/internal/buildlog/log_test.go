package buildlog

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLog_WriteAndRead(t *testing.T) {
	dir := t.TempDir()
	log, err := New(filepath.Join(dir, "build.log"), 4096)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	if _, err := log.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := log.Write([]byte("world\n")); err != nil {
		t.Fatal(err)
	}

	got := log.Snapshot()
	if string(got) != "hello\nworld\n" {
		t.Errorf("snapshot = %q, want hello\\nworld\\n", string(got))
	}
}

func TestLog_SubscriberReceivesNewBytes(t *testing.T) {
	dir := t.TempDir()
	log, err := New(filepath.Join(dir, "build.log"), 4096)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	_, _ = log.Write([]byte("before\n"))

	sub := log.Subscribe()
	defer log.Unsubscribe(sub)

	go func() {
		time.Sleep(20 * time.Millisecond)
		_, _ = log.Write([]byte("after\n"))
	}()

	var buf strings.Builder
	deadline := time.After(time.Second)
	for buf.Len() < len("after\n") {
		select {
		case chunk, ok := <-sub:
			if !ok {
				t.Fatal("sub channel closed early")
			}
			buf.Write(chunk)
		case <-deadline:
			t.Fatalf("timeout: buf=%q", buf.String())
		}
	}
	if !strings.Contains(buf.String(), "after\n") {
		t.Errorf("got %q, expected to contain after\\n", buf.String())
	}
}

func TestLog_MultipleSubscribers(t *testing.T) {
	dir := t.TempDir()
	log, _ := New(filepath.Join(dir, "build.log"), 4096)
	defer log.Close()

	sub1 := log.Subscribe()
	sub2 := log.Subscribe()
	defer log.Unsubscribe(sub1)
	defer log.Unsubscribe(sub2)

	go func() { _, _ = log.Write([]byte("data\n")) }()

	got1 := readWithDeadline(t, sub1, len("data\n"))
	got2 := readWithDeadline(t, sub2, len("data\n"))
	if got1 != "data\n" || got2 != "data\n" {
		t.Errorf("got1=%q got2=%q", got1, got2)
	}
}

func TestLog_RingDropsOldBytesUnderPressure(t *testing.T) {
	dir := t.TempDir()
	log, _ := New(filepath.Join(dir, "build.log"), 8)
	defer log.Close()

	_, _ = log.Write([]byte("AAAAAAAA"))
	_, _ = log.Write([]byte("BBBBBBBB"))

	got := log.Snapshot()
	if string(got) != "BBBBBBBB" {
		t.Errorf("snapshot = %q, want BBBBBBBB", string(got))
	}
}

func readWithDeadline(t *testing.T, sub <-chan []byte, n int) string {
	t.Helper()
	var buf strings.Builder
	deadline := time.After(time.Second)
	for buf.Len() < n {
		select {
		case chunk, ok := <-sub:
			if !ok {
				return buf.String()
			}
			buf.Write(chunk)
		case <-deadline:
			t.Fatalf("timeout reading; got %q", buf.String())
		}
	}
	return buf.String()
}

func TestLog_CloseDrainsSubscribers(t *testing.T) {
	dir := t.TempDir()
	log, _ := New(filepath.Join(dir, "build.log"), 4096)
	sub := log.Subscribe()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range sub {
		}
	}()

	_, _ = log.Write([]byte("x\n"))
	log.Close()
	wg.Wait()
}
