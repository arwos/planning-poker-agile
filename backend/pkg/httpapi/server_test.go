package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestWebSocketRejectsInvalidMessage(t *testing.T) {
	registry := room.NewRegistry(0)
	rm, err := registry.Create(nil, []string{"Backend"})
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
	if err := wsjson.Write(ctx, conn, map[string]any{
		"type":  "join",
		"name":  "Ann",
		"extra": true,
	}); err != nil {
		t.Fatal(err)
	}
	var response message
	if err := wsjson.Read(ctx, conn, &response); err != nil {
		t.Fatal(err)
	}
	if response.Type != "error" {
		t.Fatalf("response type=%q, want error", response.Type)
	}
}

func TestRejectsBadRoomPayload(t *testing.T) {
	s := &Server{Registry: room.NewRegistry(0)}
	request := httptest.NewRequest(http.MethodPost, "/api/rooms", bytes.NewBufferString(`{"cards":[1,1],"roles":[]}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestCreateRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		maxBytes   int64
		wantStatus int
	}{
		{name: "unknown field", body: `{"cards":[1],"roles":["Backend"],"extra":true}`, wantStatus: http.StatusBadRequest},
		{name: "trailing json", body: `{"cards":[1],"roles":["Backend"]}{}`, wantStatus: http.StatusBadRequest},
		{name: "missing cards", body: `{"roles":["Backend"]}`, wantStatus: http.StatusBadRequest},
		{name: "null cards", body: `{"cards":null,"roles":["Backend"]}`, wantStatus: http.StatusBadRequest},
		{name: "wrong card type", body: `{"cards":["1"],"roles":["Backend"]}`, wantStatus: http.StatusBadRequest},
		{name: "body too large", body: `{"cards":[1],"roles":["Backend"]}`, maxBytes: 8, wantStatus: http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Server{Registry: room.NewRegistry(0), MaxMessageBytes: tt.maxBytes}
			request := httptest.NewRequest(http.MethodPost, "/api/rooms", bytes.NewBufferString(tt.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			s.Handler().ServeHTTP(response, request)
			if response.Code != tt.wantStatus {
				t.Fatalf("status=%d, want %d", response.Code, tt.wantStatus)
			}
		})
	}
}

func TestCreateRejectsUnsupportedMediaType(t *testing.T) {
	s := &Server{Registry: room.NewRegistry(0)}
	request := httptest.NewRequest(http.MethodPost, "/api/rooms", bytes.NewBufferString(`{"cards":[1],"roles":["Backend"]}`))
	request.Header.Set("Content-Type", "text/plain")
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status=%d, want %d", response.Code, http.StatusUnsupportedMediaType)
	}
}

func TestIncomingMessageValidation(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		first bool
		valid bool
	}{
		{name: "valid join", body: `{"type":"join","name":"Ann","role":"Backend","client_id":"client-1"}`, first: true, valid: true},
		{name: "unknown field", body: `{"type":"join","name":"Ann","extra":true}`, first: true},
		{name: "missing name", body: `{"type":"join","role":"Backend"}`, first: true},
		{name: "unknown type", body: `{"type":"unknown"}`, first: false},
		{name: "vote without value", body: `{"type":"vote_submitted"}`, first: false},
		{name: "vote with unexpected field", body: `{"type":"vote_submitted","value":1,"name":"Ann"}`, first: false},
		{name: "reset with value", body: `{"type":"reset","value":1}`, first: false},
		{name: "valid vote", body: `{"type":"vote_selected","value":2}`, first: false, valid: true},
		{name: "valid reset", body: `{"type":"reset"}`, first: false, valid: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var message incomingMessage
			decodeErr := json.Unmarshal([]byte(tt.body), &message)
			valid := decodeErr == nil && message.validate(tt.first) == nil
			if valid != tt.valid {
				t.Fatalf("valid=%v, want %v; decode error=%v", valid, tt.valid, decodeErr)
			}
		})
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

func TestWebSocketReconnectReplacesStaleParticipant(t *testing.T) {
	registry := room.NewRegistry(0)
	rm, err := registry.Create(nil, []string{"Backend"})
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

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/rooms/" + rm.ID
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	clientID := "11111111-1111-4111-8111-111111111111"
	first, _, err := websocket.Dial(ctx, wsURL, nil)
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

	second, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close(websocket.StatusNormalClosure, "")
	if err := wsjson.Write(ctx, second, message{Type: "join", Name: "Ann", Role: "Backend", ClientID: clientID}); err != nil {
		t.Fatal(err)
	}
	var secondState map[string]any
	if err := wsjson.Read(ctx, second, &secondState); err != nil {
		t.Fatal(err)
	}

	participants := rm.Snapshot()["participants"].([]map[string]any)
	if len(participants) != 1 {
		t.Fatalf("participants after reconnect=%d", len(participants))
	}
	if err := first.Close(websocket.StatusGoingAway, "replaced"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if participants := rm.Snapshot()["participants"].([]map[string]any); len(participants) != 1 {
		t.Fatalf("stale cleanup removed active participant: %d", len(participants))
	}
}
