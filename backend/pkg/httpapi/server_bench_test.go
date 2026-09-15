package httpapi

import (
	"encoding/json"
	"testing"
)

func BenchmarkIncomingMessageDecode(b *testing.B) {
	data := []byte(`{"type":"join","name":"Ann","role":"Backend","client_id":"client-1","reconnect_token":"token"}`)
	b.ReportAllocs()
	for b.Loop() {
		var message incomingMessage
		if err := json.Unmarshal(data, &message); err != nil {
			b.Fatal(err)
		}
	}
}
