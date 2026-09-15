//go:build integration

package httpapi

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arwos/planning-poker-agile/app/room"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func integrationServer(t *testing.T, registry *room.Registry, origins ...string) (*httptest.Server, string) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local listeners unavailable: %v", err)
	}
	server := httptest.NewUnstartedServer((&Server{Registry: registry, CORSOrigins: origins}).Handler())
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return server, "ws" + strings.TrimPrefix(server.URL, "http")
}

func TestWebSocketJoinAndReveal(t *testing.T) {
	registry := room.NewRegistry(0)
	rm, err := registry.Create([]float64{1, 3}, []string{"Backend"})
	if err != nil {
		t.Fatal(err)
	}
	_, wsBase := integrationServer(t, registry)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsBase+"/ws/rooms/"+rm.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	if err := wsjson.Write(ctx, conn, message{Type: "join", Name: "Ann", Role: "Backend", ClientID: "join-client"}); err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := wsjson.Read(ctx, conn, &state); err != nil || state["type"] != "room_state" {
		t.Fatalf("initial event=%v err=%v", state, err)
	}
	if err := wsjson.Read(ctx, conn, &state); err != nil || state["type"] != "participant_joined" {
		t.Fatalf("join event=%v err=%v", state, err)
	}
	value := 3.0
	if err := wsjson.Write(ctx, conn, message{Type: "vote_submitted", Value: &value}); err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Read(ctx, conn, &state); err != nil || state["type"] != "results_revealed" {
		t.Fatalf("result event=%v err=%v", state, err)
	}
}

func TestWebSocketRejectsInvalidMessage(t *testing.T) {
	registry := room.NewRegistry(0)
	rm, err := registry.Create(nil, []string{"Backend"})
	if err != nil {
		t.Fatal(err)
	}
	_, wsBase := integrationServer(t, registry)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsBase+"/ws/rooms/"+rm.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	if err := wsjson.Write(ctx, conn, map[string]any{"type": "join", "name": "Ann", "extra": true}); err != nil {
		t.Fatal(err)
	}
	var response message
	if err := wsjson.Read(ctx, conn, &response); err != nil {
		t.Fatal(err)
	}
	if response.Type != "error" {
		t.Fatalf("response type=%q", response.Type)
	}
}

func TestWebSocketOriginPolicies(t *testing.T) {
	tests := []struct {
		name    string
		origins []string
		origin  string
		want    int
	}{
		{name: "configured", origins: []string{"http://app.example"}, origin: "http://app.example", want: http.StatusSwitchingProtocols},
		{name: "wildcard", origins: []string{"*"}, origin: "https://untrusted.example", want: http.StatusSwitchingProtocols},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := room.NewRegistry(0)
			rm, err := registry.Create(nil, []string{"Backend"})
			if err != nil {
				t.Fatal(err)
			}
			_, wsBase := integrationServer(t, registry, tt.origins...)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			conn, response, err := websocket.Dial(ctx, wsBase+"/ws/rooms/"+rm.ID, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {tt.origin}}})
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close(websocket.StatusNormalClosure, "")
			if response.StatusCode != tt.want {
				t.Fatalf("status=%d, want %d", response.StatusCode, tt.want)
			}
		})
	}
}

func TestWebSocketReconnectReplacesStaleParticipant(t *testing.T) {
	registry := room.NewRegistry(0)
	rm, err := registry.Create(nil, []string{"Backend"})
	if err != nil {
		t.Fatal(err)
	}
	_, wsBase := integrationServer(t, registry)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	clientID := "11111111-1111-4111-8111-111111111111"
	first, _, err := websocket.Dial(ctx, wsBase+"/ws/rooms/"+rm.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close(websocket.StatusNormalClosure, "")
	if err := wsjson.Write(ctx, first, message{Type: "join", Name: "Ann", Role: "Backend", ClientID: clientID}); err != nil {
		t.Fatal(err)
	}
	var firstState map[string]any
	if err := wsjson.Read(ctx, first, &firstState); err != nil {
		t.Fatal(err)
	}
	reconnectToken, ok := firstState["reconnect_token"].(string)
	if !ok || reconnectToken == "" {
		t.Fatalf("missing reconnect token: %#v", firstState)
	}
	second, _, err := websocket.Dial(ctx, wsBase+"/ws/rooms/"+rm.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close(websocket.StatusNormalClosure, "")
	if err := wsjson.Write(ctx, second, message{Type: "join", Name: "Ann", Role: "Backend", ClientID: clientID, ReconnectToken: reconnectToken}); err != nil {
		t.Fatal(err)
	}
	var secondState map[string]any
	if err := wsjson.Read(ctx, second, &secondState); err != nil {
		t.Fatal(err)
	}
	if participants := rm.Snapshot()["participants"].([]map[string]any); len(participants) != 1 {
		t.Fatalf("participants after reconnect=%d", len(participants))
	}
	_ = first.Close(websocket.StatusGoingAway, "replaced")
	time.Sleep(20 * time.Millisecond)
	if participants := rm.Snapshot()["participants"].([]map[string]any); len(participants) != 1 {
		t.Fatalf("stale cleanup removed active participant: %d", len(participants))
	}
}
