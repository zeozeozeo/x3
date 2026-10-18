package logstream

import (
	"fmt"
	"testing"
	"time"
)

func TestHubSplitsLines(t *testing.T) {
	h := NewHub(10)
	if _, err := h.Write([]byte("one\ntwo\npartial")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := h.Lines(); !equal(got, []string{"one", "two"}) {
		t.Fatalf("after partial write got %v", got)
	}
	if _, err := h.Write([]byte(" is complete\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := h.Lines(); !equal(got, []string{"one", "two", "partial is complete"}) {
		t.Fatalf("after completion got %v", got)
	}
}

func TestHubTrimsCarriageReturn(t *testing.T) {
	h := NewHub(10)
	h.Write([]byte("windows line\r\n"))
	if got := h.Lines(); !equal(got, []string{"windows line"}) {
		t.Fatalf("got %v, want no trailing carriage return", got)
	}
}

func TestHubRetainsOnlyCapacity(t *testing.T) {
	h := NewHub(3)
	for i := range 10 {
		fmt.Fprintf(h, "line%d\n", i)
	}
	if got := h.Lines(); !equal(got, []string{"line7", "line8", "line9"}) {
		t.Fatalf("got %v, want the last three lines", got)
	}
}

func TestHubBoundlessPartialIsFlushed(t *testing.T) {
	h := NewHub(10)
	big := make([]byte, maxPartialBytes+1)
	for i := range big {
		big[i] = 'a'
	}
	h.Write(big)
	if got := h.Lines(); len(got) != 1 {
		t.Fatalf("got %d lines, want the oversized partial to be flushed as one", len(got))
	}
}

func TestHubSubscribeReceivesAndCloses(t *testing.T) {
	h := NewHub(10)
	ch, cancel := h.Subscribe()
	defer cancel()

	h.Write([]byte("hello\n"))
	select {
	case line := <-ch:
		if line != "hello" {
			t.Fatalf("got %q, want hello", line)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for a line")
	}

	cancel()
	if _, ok := <-ch; ok {
		t.Error("channel should be closed after unsubscribe")
	}
}

func TestHubSlowSubscriberDoesNotBlock(t *testing.T) {
	h := NewHub(10)
	_, cancel := h.Subscribe()
	defer cancel()

	done := make(chan struct{})
	go func() {
		for range 2000 {
			h.Write([]byte("spam\n"))
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("logging blocked on a slow subscriber")
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
