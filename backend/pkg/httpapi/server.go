/*
 *  Copyright (c) 2026 Mikhail Knyazhev. All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/arwos/planning-poker-agile/app/realtime"
	"github.com/arwos/planning-poker-agile/app/room"
	wstransport "github.com/arwos/planning-poker-agile/pkg/ws"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
)

type Server struct {
	Registry        *room.Registry
	PingInterval    time.Duration
	MaxMessageBytes int64
	CORSOrigins     []string
	Hub             *realtime.Hub
	PendingRoomTTL  time.Duration
	MaxConnections  int
	JoinTimeout     time.Duration
	MessageRate     float64
	MessageBurst    int
	OutboundQueue   int
	WriteTimeout    time.Duration
	CreateRate      int
	connectionSlots chan struct{}
	createLimiter   *keyedLimiter
	initMu          sync.Mutex
}
type createRequest struct {
	Cards *[]float64 `json:"cards"`
	Roles *[]string  `json:"roles"`
}
type message struct {
	Type           string   `json:"type"`
	Name           string   `json:"name,omitempty"`
	Role           string   `json:"role,omitempty"`
	ClientID       string   `json:"client_id,omitempty"`
	Value          *float64 `json:"value,omitempty"`
	Error          string   `json:"error,omitempty"`
	ReconnectToken string   `json:"reconnect_token,omitempty"`
}

type incomingMessage struct {
	Type           string   `json:"type"`
	Name           *string  `json:"name"`
	Role           *string  `json:"role"`
	ClientID       *string  `json:"client_id"`
	ReconnectToken *string  `json:"reconnect_token"`
	OwnerToken     *string  `json:"owner_token"`
	Value          *float64 `json:"value"`

	decodeFailed      bool
	nameSet           bool
	roleSet           bool
	clientIDSet       bool
	reconnectTokenSet bool
	ownerTokenSet     bool
	valueSet          bool
}

func (m *incomingMessage) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	*m = incomingMessage{}
	if len(trimmed) == 0 {
		return errors.New("invalid JSON message")
	}
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	if err := decoder.Decode(&fields); err != nil || fields == nil {
		m.decodeFailed = true
		return nil
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		m.decodeFailed = true
		return nil
	}
	for field := range fields {
		switch field {
		case "type", "name", "role", "client_id", "reconnect_token", "owner_token", "value":
		default:
			m.decodeFailed = true
			return nil
		}
	}
	if raw, ok := fields["type"]; ok {
		if err := json.Unmarshal(raw, &m.Type); err != nil {
			m.decodeFailed = true
			return nil
		}
	}
	var err error
	if m.Name, err = optionalString(fields, "name"); err != nil {
		m.decodeFailed = true
		return nil
	}
	m.nameSet = hasField(fields, "name")
	if m.Role, err = optionalString(fields, "role"); err != nil {
		m.decodeFailed = true
		return nil
	}
	m.roleSet = hasField(fields, "role")
	if m.ClientID, err = optionalString(fields, "client_id"); err != nil {
		m.decodeFailed = true
		return nil
	}
	m.clientIDSet = hasField(fields, "client_id")
	if m.ReconnectToken, err = optionalString(fields, "reconnect_token"); err != nil {
		m.decodeFailed = true
		return nil
	}
	m.reconnectTokenSet = hasField(fields, "reconnect_token")
	if m.OwnerToken, err = optionalString(fields, "owner_token"); err != nil {
		m.decodeFailed = true
		return nil
	}
	m.ownerTokenSet = hasField(fields, "owner_token")
	if m.Value, err = optionalNumber(fields, "value"); err != nil {
		m.decodeFailed = true
		return nil
	}
	m.valueSet = hasField(fields, "value")
	return nil
}

func hasField(fields map[string]json.RawMessage, name string) bool {
	_, ok := fields[name]
	return ok
}

func optionalString(fields map[string]json.RawMessage, name string) (*string, error) {
	raw, ok := fields[name]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return &value, nil
}

func optionalNumber(fields map[string]json.RawMessage, name string) (*float64, error) {
	raw, ok := fields[name]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var value float64
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return &value, nil
}

func (m incomingMessage) validate(first bool) error {
	if m.decodeFailed {
		return errors.New("invalid message")
	}
	if m.nameSet != (m.Name != nil) || m.roleSet != (m.Role != nil) || m.clientIDSet != (m.ClientID != nil) || m.reconnectTokenSet != (m.ReconnectToken != nil) || m.ownerTokenSet != (m.OwnerToken != nil) || m.valueSet != (m.Value != nil) {
		return errors.New("invalid message fields")
	}
	if m.Type == "" {
		return errors.New("message type is required")
	}
	if first && m.Type != "join" {
		return errors.New("first message must be join")
	}
	if !first && m.Type == "join" {
		return errors.New("join is only valid as the first message")
	}
	switch m.Type {
	case "join":
		if m.Name == nil || strings.TrimSpace(*m.Name) == "" || !m.nameSet {
			return errors.New("name is required")
		}
		if m.ClientID == nil || strings.TrimSpace(*m.ClientID) == "" || !m.clientIDSet {
			return errors.New("client_id is required")
		}
		if (m.roleSet && m.Role == nil) || (m.clientIDSet && m.ClientID == nil) || (m.reconnectTokenSet && m.ReconnectToken == nil) || (m.ownerTokenSet && m.OwnerToken == nil) {
			return errors.New("invalid join fields")
		}
		if m.valueSet {
			return errors.New("value is not valid for join")
		}
		if m.ClientID != nil && (strings.TrimSpace(*m.ClientID) == "" || len(strings.TrimSpace(*m.ClientID)) > 128) {
			return errors.New("client_id is too long")
		}
		if m.ReconnectToken != nil && (strings.TrimSpace(*m.ReconnectToken) == "" || len(strings.TrimSpace(*m.ReconnectToken)) > 128) {
			return errors.New("invalid reconnect_token")
		}
		if m.OwnerToken != nil && (strings.TrimSpace(*m.OwnerToken) == "" || len(strings.TrimSpace(*m.OwnerToken)) > 128) {
			return errors.New("invalid owner_token")
		}
	case "vote_selected", "vote_submitted":
		if !m.valueSet || m.Value == nil || math.IsNaN(*m.Value) || math.IsInf(*m.Value, 0) {
			return errors.New("finite value is required")
		}
		if m.nameSet || m.roleSet || m.clientIDSet || m.reconnectTokenSet || m.ownerTokenSet {
			return errors.New("unexpected vote fields")
		}
	case "vote_skipped":
		if m.nameSet || m.roleSet || m.clientIDSet || m.reconnectTokenSet || m.ownerTokenSet || m.valueSet {
			return errors.New("vote_skipped does not accept fields")
		}
	case "reset":
		if m.nameSet || m.roleSet || m.clientIDSet || m.reconnectTokenSet || m.ownerTokenSet || m.valueSet {
			return errors.New("reset does not accept fields")
		}
	default:
		return errors.New("unknown message type")
	}
	return nil
}

func decodeStrictJSON(reader io.Reader, target any) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

var (
	errRequestTooLarge  = errors.New("request body too large")
	errUnsupportedMedia = errors.New("content type must be application/json")
)

func (s *Server) Handler() http.Handler {
	s.initMu.Lock()
	defer s.initMu.Unlock()
	if s.PingInterval <= 0 {
		s.PingInterval = time.Second
	}
	if s.MaxMessageBytes <= 0 {
		s.MaxMessageBytes = 32768
	}
	if s.Hub == nil {
		s.Hub = realtime.NewHub()
	}
	if s.PendingRoomTTL <= 0 {
		s.PendingRoomTTL = 10 * time.Minute
	}
	if s.MaxConnections <= 0 {
		s.MaxConnections = 1000
	}
	if s.JoinTimeout <= 0 {
		s.JoinTimeout = 10 * time.Second
	}
	if s.MessageRate <= 0 {
		s.MessageRate = 20
	}
	if s.MessageBurst <= 0 {
		s.MessageBurst = 40
	}
	if s.OutboundQueue <= 0 {
		s.OutboundQueue = 32
	}
	if s.WriteTimeout <= 0 {
		s.WriteTimeout = 5 * time.Second
	}
	if s.CreateRate <= 0 {
		s.CreateRate = 10
	}
	if s.connectionSlots == nil {
		s.connectionSlots = make(chan struct{}, s.MaxConnections)
	}
	if s.createLimiter == nil {
		s.createLimiter = newKeyedLimiter(float64(s.CreateRate)/60, s.CreateRate, 4096)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/api/rooms", s.create)
	mux.HandleFunc("/api/rooms/", s.getRoom)
	mux.HandleFunc("/ws/rooms/", s.ws)
	mux.Handle("/", s.frontend())
	return s.securityHeaders(s.cors(mux))
}
func (s *Server) getRoom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/rooms/")
	if _, err := uuid.Parse(id); err != nil {
		http.Error(w, "invalid room", http.StatusBadRequest)
		return
	}
	state, err := s.Registry.PublicState(id)
	if err != nil {
		http.Error(w, "room not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(state)
}
func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if s.allowsAnyOrigin() {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			w.Header().Add("Vary", "Origin")
			for _, allowed := range s.CORSOrigins {
				if allowed == origin {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					break
				}
			}
		}
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'self'; object-src 'none'; frame-ancestors 'none'; img-src 'self' data:; script-src 'self'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com; connect-src 'self' ws: wss:")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) allowsAnyOrigin() bool {
	if len(s.CORSOrigins) == 0 {
		return true
	}
	for _, origin := range s.CORSOrigins {
		if origin == "*" {
			return true
		}
	}
	return false
}

func (s *Server) originPatterns() []string {
	patterns := make([]string, 0, len(s.CORSOrigins))
	for _, origin := range s.CORSOrigins {
		if origin == "*" {
			continue
		}
		parsed, err := url.Parse(origin)
		if err == nil && parsed.Host != "" && parsed.Scheme != "" {
			patterns = append(patterns, parsed.Scheme+"://"+parsed.Host)
		}
	}
	return patterns
}
func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	if s.createLimiter != nil && !s.createLimiter.allow(remoteIP(r), time.Now()) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "room creation rate limit exceeded", http.StatusTooManyRequests)
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		http.Error(w, errUnsupportedMedia.Error(), http.StatusUnsupportedMediaType)
		return
	}
	var in createRequest
	if err := s.decodeJSONBody(w, r, &in); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errRequestTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, "invalid request", status)
		return
	}
	if in.Cards == nil || in.Roles == nil {
		http.Error(w, "cards and roles are required", http.StatusBadRequest)
		return
	}
	ownerToken := uuid.NewString()
	x, e := s.Registry.ReserveWithOwner(*in.Cards, *in.Roles, ownerToken, s.PendingRoomTTL)
	if e != nil {
		if errors.Is(e, room.ErrCapacity) {
			http.Error(w, e.Error(), http.StatusTooManyRequests)
			return
		}
		http.Error(w, e.Error(), 400)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"id": x.ID, "url": "/room/" + x.ID, "owner_token": ownerToken})
}

func (s *Server) decodeJSONBody(w http.ResponseWriter, r *http.Request, target any) error {
	limited := http.MaxBytesReader(w, r.Body, s.MaxMessageBytes)
	if err := decodeStrictJSON(limited, target); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			return errRequestTooLarge
		}
		return err
	}
	return nil
}
func (s *Server) broadcast(id string, payload any) {
	s.Hub.Broadcast(id, payload)
}
func (s *Server) ws(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/ws/rooms/")
	if _, e := uuid.Parse(id); e != nil {
		http.Error(w, "invalid room", 400)
		return
	}
	select {
	case s.connectionSlots <- struct{}{}:
		defer func() { <-s.connectionSlots }()
	default:
		http.Error(w, "server is busy", http.StatusServiceUnavailable)
		return
	}
	acceptOptions := &websocket.AcceptOptions{OriginPatterns: s.originPatterns()}
	if s.allowsAnyOrigin() {
		acceptOptions.InsecureSkipVerify = true
	}
	c, e := websocket.Accept(w, r, acceptOptions)
	if e != nil {
		return
	}
	c.SetReadLimit(s.MaxMessageBytes)
	defer c.Close(websocket.StatusNormalClosure, "")
	joinCtx, cancelJoin := context.WithTimeout(context.Background(), s.JoinTimeout)
	defer cancelJoin()
	var in incomingMessage
	if wsjson.Read(joinCtx, c, &in) != nil {
		return
	}
	if err := in.validate(true); err != nil {
		_ = wsjson.Write(joinCtx, c, message{Type: "error", Error: err.Error()})
		return
	}
	name := strings.TrimSpace(*in.Name)
	role := ""
	if in.Role != nil {
		role = strings.TrimSpace(*in.Role)
	}
	clientID := ""
	if in.ClientID != nil {
		clientID = strings.TrimSpace(*in.ClientID)
	}
	reconnectToken := ""
	if in.ReconnectToken != nil {
		reconnectToken = strings.TrimSpace(*in.ReconnectToken)
	}
	ownerToken := ""
	if in.OwnerToken != nil {
		ownerToken = strings.TrimSpace(*in.OwnerToken)
	}
	connectionID := uuid.NewString()
	p := &room.Participant{ID: uuid.NewString(), Name: name, Role: role}
	rm, reconnected, issuedReconnectToken, e := s.Registry.Join(id, p, clientID, reconnectToken, ownerToken, connectionID)
	if e != nil {
		errMessage := "name or role is invalid"
		status := websocket.StatusPolicyViolation
		if errors.Is(e, room.ErrNotFound) {
			errMessage = "room not found"
			status = websocket.StatusNormalClosure
		} else if errors.Is(e, room.ErrCapacity) {
			errMessage = "room is full"
		} else if errors.Is(e, room.ErrInvalidReconnect) {
			errMessage = "invalid reconnect token"
		}
		_ = wsjson.Write(joinCtx, c, message{Type: "error", Error: errMessage})
		_ = c.Close(status, errMessage)
		return
	}
	cancelJoin()
	connectionCtx, cancelConnection := context.WithCancel(context.Background())
	defer cancelConnection()
	ss := wstransport.New(c, s.OutboundQueue)
	messageLimiter := tokenBucket{tokens: float64(s.MessageBurst), last: time.Now()}
	write := func(payload any) bool {
		if ss.Enqueue(payload) {
			return true
		}
		cancelConnection()
		_ = c.Close(websocket.StatusTryAgainLater, "client is too slow")
		return false
	}
	token, previousClose := s.Hub.AddConnection(id, p.ID, func(payload any) {
		write(payload)
	}, func() {
		cancelConnection()
		_ = c.Close(websocket.StatusGoingAway, "replaced by a newer connection")
	})
	if previousClose != nil {
		previousClose()
	}
	defer func() {
		ss.Close()
		s.Hub.RemoveConnection(id, p.ID, token)
		if rm.RemoveConnection(p.ID, connectionID) {
			s.Registry.DeleteIfEmpty(id)
			s.broadcast(id, map[string]any{"type": "participant_left", "state": rm.State()})
		}
	}()
	go func() {
		if err := ss.Run(connectionCtx, s.PingInterval, s.WriteTimeout); err != nil {
			cancelConnection()
			_ = c.Close(websocket.StatusGoingAway, "connection closed")
		}
	}()
	write(map[string]any{"type": "room_state", "state": rm.State(), "self": p.ID, "reconnect_token": issuedReconnectToken})
	if reconnected {
		s.broadcast(id, map[string]any{"type": "room_state", "state": rm.State()})
	} else {
		s.broadcast(id, map[string]any{"type": "participant_joined", "state": rm.State()})
	}
	for {
		var m incomingMessage
		if e := wsjson.Read(connectionCtx, c, &m); e != nil {
			return
		}
		if !messageLimiter.allow(s.MessageRate, s.MessageBurst, time.Now()) {
			write(message{Type: "error", Error: "message rate limit exceeded"})
			return
		}
		if err := m.validate(false); err != nil {
			write(message{Type: "error", Error: err.Error()})
			return
		}
		switch m.Type {
		case "vote_selected", "vote_submitted":
			if m.Value != nil {
				done, e := rm.Vote(p.ID, *m.Value, m.Type == "vote_submitted")
				if e == nil {
					typ := "room_state"
					if done {
						typ = "results_revealed"
					}
					s.broadcast(id, map[string]any{"type": typ, "state": rm.State()})
				}
			}
		case "vote_skipped":
			done, e := rm.SkipVote(p.ID)
			if e == nil {
				typ := "room_state"
				if done {
					typ = "results_revealed"
				}
				s.broadcast(id, map[string]any{"type": typ, "state": rm.State()})
			}
		case "reset":
			if rm.Reset(p.ID) == nil {
				s.broadcast(id, map[string]any{"type": "voting_reset", "state": rm.State()})
			}
		}
	}
}
