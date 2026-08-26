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
