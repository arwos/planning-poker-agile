package realtime

import "testing"

func TestBroadcastAndRemove(t *testing.T) {
	hub := NewHub()
	var first, second []string
	hub.Add("room", "first", func(payload any) { first = append(first, payload.(string)) })
	hub.Add("room", "second", func(payload any) { second = append(second, payload.(string)) })
	hub.Broadcast("room", "joined")
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("first=%v second=%v", first, second)
	}
	hub.Remove("room", "first")
	hub.Broadcast("room", "voted")
	if len(first) != 1 || len(second) != 2 {
		t.Fatalf("first=%v second=%v", first, second)
	}
}

func TestReplacingConnectionKeepsNewSender(t *testing.T) {
	hub := NewHub()
	oldClosed := false
	oldToken, _ := hub.AddConnection("room", "participant", func(payload any) {
		if payload != "new" {
			t.Errorf("old sender received %v", payload)
		}
	}, func() { oldClosed = true })
	newMessages := 0
	newToken, previousClose := hub.AddConnection("room", "participant", func(payload any) {
		if payload == "new" {
			newMessages++
		}
	}, nil)
	if previousClose == nil {
		t.Fatal("expected previous connection closer")
	}
	previousClose()
	if !oldClosed {
		t.Fatal("previous connection was not closed")
	}

	hub.RemoveConnection("room", "participant", oldToken)
	hub.Broadcast("room", "new")
	if newMessages != 1 {
		t.Fatalf("new sender messages=%d", newMessages)
	}

	hub.RemoveConnection("room", "participant", newToken)
	hub.Broadcast("room", "after close")
	if newMessages != 1 {
		t.Fatalf("new sender received message after close: %d", newMessages)
	}
}
