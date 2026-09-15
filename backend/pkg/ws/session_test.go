package ws

import (
	"context"
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m, goleak.IgnoreCurrent())
}

func TestSessionQueueIsBoundedAndCloses(t *testing.T) {
	session := New(nil, 1)
	if !session.Enqueue("first") {
		t.Fatal("first enqueue failed")
	}
	if session.Enqueue("second") {
		t.Fatal("queue accepted an item after reaching capacity")
	}
	session.Close()
	if session.Enqueue("after close") {
		t.Fatal("queue accepted an item after close")
	}
}

func TestSessionRunStopsOnContextCancellation(t *testing.T) {
	session := New(nil, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := session.Run(ctx, 1, 1); err == nil {
		t.Fatal("expected cancellation error")
	}
}
