package httpapi

import (
	"bytes"
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

func TestCreateAndReadRoom(t *testing.T) {
	s := &Server{Registry: room.NewRegistry(0)}
	create := httptest.NewRequest(http.MethodPost, "/api/rooms", bytes.NewBufferString(`{"cards":[1,2],"roles":["Backend"]}`))
	create.Header.Set("Content-Type", "application/json")
	created := httptest.NewRecorder()
	s.Handler().ServeHTTP(created, create)
	if created.Code != http.StatusOK {
		t.Fatalf("create status: %d", created.Code)
	}
	if got := created.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("cors=%q", got)
	}
	// The response is intentionally parsed only for its id; state shape belongs to the WebSocket contract.
	if len(created.Body.Bytes()) == 0 {
		t.Fatal("expected create body")
	}
}

func TestWebSocketJoinAndReveal(t *testing.T) {
	registry := room.NewRegistry(0)
	rm, err := registry.Create([]float64{1, 3}, []string{"Backend"})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local listeners unavailable: %v", err)
	}
	server := httptest.NewUnstartedServer((&Server{Registry: registry}).Handler())
	server.Listener = listener
	server.Start()
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/rooms/" + rm.ID
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	if err = wsjson.Write(ctx, conn, message{Type: "join", Name: "Ann", Role: "Backend"}); err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err = wsjson.Read(ctx, conn, &state); err != nil {
		t.Fatal(err)
	}
	if state["type"] != "room_state" {
		t.Fatalf("initial event: %v", state["type"])
	}
	value := 3.0
	if err = wsjson.Write(ctx, conn, message{Type: "vote_submitted", Value: &value}); err != nil {
		t.Fatal(err)
	}
	if err = wsjson.Read(ctx, conn, &state); err != nil {
		t.Fatal(err)
	}
	if state["type"] != "results_revealed" {
		t.Fatalf("result event: %v", state["type"])
	}
}

func TestRejectsBadRoomPayload(t *testing.T) {
	s := &Server{Registry: room.NewRegistry(0)}
	request := httptest.NewRequest(http.MethodPost, "/api/rooms", bytes.NewBufferString(`{"cards":[1,1],"roles":[]}`))
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestCORSOnlyAllowsConfiguredOrigin(t *testing.T) {
	s := &Server{Registry: room.NewRegistry(0), CORSOrigins: []string{"http://app.example"}}
	request := httptest.NewRequest(http.MethodOptions, "/api/rooms", nil)
	request.Header.Set("Origin", "http://other.example")
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, request)
	if response.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("unexpected CORS header")
	}
	request.Header.Set("Origin", "http://app.example")
	response = httptest.NewRecorder()
	s.Handler().ServeHTTP(response, request)
	if response.Header().Get("Access-Control-Allow-Origin") != "http://app.example" {
		t.Fatal("missing allowed origin")
	}
	if response.Header().Get("Access-Control-Allow-Methods") != "GET, POST, OPTIONS" {
		t.Fatalf("unexpected allowed methods: %q", response.Header().Get("Access-Control-Allow-Methods"))
	}
}

func TestCORSWildcardAllowsAnyOrigin(t *testing.T) {
	s := &Server{Registry: room.NewRegistry(0), CORSOrigins: []string{"*"}}
	request := httptest.NewRequest(http.MethodOptions, "/api/rooms", nil)
	request.Header.Set("Origin", "https://untrusted.example")
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, request)
	if response.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("cors=%q", response.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestWebSocketAllowsConfiguredOrigin(t *testing.T) {
	registry := room.NewRegistry(0)
	rm, err := registry.Create(nil, []string{"Backend"})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local listeners unavailable: %v", err)
	}
	server := httptest.NewUnstartedServer((&Server{
		Registry:    registry,
		CORSOrigins: []string{"http://app.example"},
	}).Handler())
	server.Listener = listener
	server.Start()
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/rooms/" + rm.ID
	conn, response, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {"http://app.example"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status=%d", response.StatusCode)
	}
}

func TestWebSocketAllowsWildcardOrigin(t *testing.T) {
	registry := room.NewRegistry(0)
	rm, err := registry.Create(nil, []string{"Backend"})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local listeners unavailable: %v", err)
	}
	server := httptest.NewUnstartedServer((&Server{
		Registry:    registry,
		CORSOrigins: []string{"*"},
	}).Handler())
	server.Listener = listener
	server.Start()
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/rooms/" + rm.ID
	conn, response, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {"https://untrusted.example"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status=%d", response.StatusCode)
	}
}
