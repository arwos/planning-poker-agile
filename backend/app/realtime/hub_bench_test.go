package realtime

import "testing"

func BenchmarkHubBroadcast(b *testing.B) {
	hub := NewHub()
	for i := 0; i < 32; i++ {
		participant := string(rune('a' + i))
		hub.Add("room", participant, func(any) {})
	}
	b.ReportAllocs()
	for b.Loop() {
		hub.Broadcast("room", struct{ Type string }{Type: "room_state"})
	}
}
