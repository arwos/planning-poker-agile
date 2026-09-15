package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arwos/planning-poker-agile/app/room"
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
	var createdRoom map[string]string
	if err := json.Unmarshal(created.Body.Bytes(), &createdRoom); err != nil {
		t.Fatal(err)
	}
	if createdRoom["owner_token"] == "" {
		t.Fatal("expected owner token")
	}
	get := httptest.NewRecorder()
	handler := s.Handler()
	handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/rooms/"+createdRoom["id"], nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), createdRoom["id"]) {
		t.Fatalf("pending room GET status=%d body=%s", get.Code, get.Body.String())
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
		{name: "valid join", body: `{"type":"join","name":"Ann","role":"Backend","client_id":"client-1","owner_token":"owner-1"}`, first: true, valid: true},
		{name: "unknown field", body: `{"type":"join","name":"Ann","extra":true}`, first: true},
		{name: "missing name", body: `{"type":"join","role":"Backend"}`, first: true},
		{name: "unknown type", body: `{"type":"unknown"}`, first: false},
		{name: "vote without value", body: `{"type":"vote_submitted"}`, first: false},
		{name: "vote with unexpected field", body: `{"type":"vote_submitted","value":1,"name":"Ann"}`, first: false},
		{name: "skip with unexpected field", body: `{"type":"vote_skipped","value":1}`, first: false},
		{name: "reset with owner token", body: `{"type":"reset","owner_token":"owner-1"}`, first: false},
		{name: "reset with value", body: `{"type":"reset","value":1}`, first: false},
		{name: "valid vote", body: `{"type":"vote_selected","value":2}`, first: false, valid: true},
		{name: "valid skip", body: `{"type":"vote_skipped"}`, first: false, valid: true},
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
	if response.Header().Get("Vary") != "Origin" {
		t.Fatalf("vary=%q, want Origin", response.Header().Get("Vary"))
	}
	if response.Header().Get("Access-Control-Allow-Methods") != "GET, POST, OPTIONS" {
		t.Fatalf("unexpected allowed methods: %q", response.Header().Get("Access-Control-Allow-Methods"))
	}
}

func TestSecurityHeadersAreSet(t *testing.T) {
	s := &Server{Registry: room.NewRegistry(1)}
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	} {
		if got := response.Header().Get(header); got != want {
			t.Fatalf("%s=%q, want %q", header, got, want)
		}
	}
	if !strings.Contains(response.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("missing CSP frame policy")
	}
}

func TestCreateRateLimitIsBoundedPerIP(t *testing.T) {
	s := &Server{Registry: room.NewRegistry(5), CreateRate: 1}
	handler := s.Handler()
	newRequest := func() *http.Request {
		request := httptest.NewRequest(http.MethodPost, "/api/rooms", bytes.NewBufferString(`{"cards":[1],"roles":["Backend"]}`))
		request.Header.Set("Content-Type", "application/json")
		request.RemoteAddr = "192.0.2.1:1234"
		return request
	}
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, newRequest())
	if first.Code != http.StatusOK {
		t.Fatalf("first status=%d", first.Code)
	}
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, newRequest())
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status=%d, want %d", second.Code, http.StatusTooManyRequests)
	}
}

func FuzzIncomingMessageDoesNotPanic(f *testing.F) {
	f.Add([]byte(`{"type":"join","name":"Ann"}`))
	f.Add([]byte(`not json`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var message incomingMessage
		_ = json.Unmarshal(data, &message)
		_ = message.validate(true)
	})
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
