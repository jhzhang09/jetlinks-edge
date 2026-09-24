package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

type retryNorth struct {
	attempts int
	done     chan struct{}
}

func (n *retryNorth) OnMessage(context.Context, NorthMessage) error {
	n.attempts++
	if n.attempts < 3 {
		return errors.New("temporary failure")
	}
	select {
	case <-n.done:
	default:
		close(n.done)
	}
	return nil
}

func TestQueuedNorthRetriesWithoutBlockingProducer(t *testing.T) {
	target := &retryNorth{done: make(chan struct{})}
	queued := newQueuedNorth(context.Background(), target)
	t.Cleanup(func() {
		if err := queued.Close(); err != nil {
			t.Errorf("close queued north: %v", err)
		}
	})
	start := time.Now()
	if err := queued.OnMessage(context.Background(), NorthMessage{GroupID: "group-1"}); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("producer waited for north delivery")
	}
	select {
	case <-target.done:
	case <-time.After(2 * time.Second):
		t.Fatal("delivery was not retried")
	}
	state := queued.State()
	if state.Stats["deliverySucceeded"] != 1 || target.attempts != 3 {
		t.Fatalf("unexpected delivery state: %#v attempts=%d", state.Stats, target.attempts)
	}
}
