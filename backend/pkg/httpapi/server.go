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
}
type createRequest struct {
	Cards *[]float64 `json:"cards"`
	Roles *[]string  `json:"roles"`
}
type message struct {
	Type     string   `json:"type"`
	Name     string   `json:"name,omitempty"`
	Role     string   `json:"role,omitempty"`
	ClientID string   `json:"client_id,omitempty"`
	Value    *float64 `json:"value,omitempty"`
	Error    string   `json:"error,omitempty"`
}

type incomingMessage struct {
	Type       string   `json:"type"`
	Name       *string  `json:"name"`
	Role       *string  `json:"role"`
	ClientID   *string  `json:"client_id"`
	OwnerToken *string  `json:"owner_token"`
	Value      *float64 `json:"value"`

	decodeFailed  bool
	nameSet       bool
	roleSet       bool
	clientIDSet   bool
	ownerTokenSet bool
	valueSet      bool
}

func (m *incomingMessage) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	*m = incomingMessage{}
	if !json.Valid(trimmed) {
		return errors.New("invalid JSON message")
	}
	if len(trimmed) == 0 || trimmed[0] != '{' {
		m.decodeFailed = true
		return nil
	}
	type plainIncomingMessage incomingMessage
	var decoded plainIncomingMessage
	if err := decodeStrictJSON(bytes.NewReader(trimmed), &decoded); err != nil {
		m.decodeFailed = true
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return err
	}
	*m = incomingMessage(decoded)
	_, m.nameSet = fields["name"]
	_, m.roleSet = fields["role"]
	_, m.clientIDSet = fields["client_id"]
	_, m.ownerTokenSet = fields["owner_token"]
	_, m.valueSet = fields["value"]
	return nil
}

func (m incomingMessage) validate(first bool) error {
	if m.decodeFailed {
		return errors.New("invalid message")
	}
	if m.nameSet != (m.Name != nil) || m.roleSet != (m.Role != nil) || m.clientIDSet != (m.ClientID != nil) || m.ownerTokenSet != (m.OwnerToken != nil) || m.valueSet != (m.Value != nil) {
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
		if (m.roleSet && m.Role == nil) || (m.clientIDSet && m.ClientID == nil) || (m.ownerTokenSet && m.OwnerToken == nil) {
			return errors.New("invalid join fields")
		}
		if m.valueSet {
			return errors.New("value is not valid for join")
		}
		if m.ClientID != nil && len(strings.TrimSpace(*m.ClientID)) > 128 {
			return errors.New("client_id is too long")
		}
		if m.OwnerToken != nil && (strings.TrimSpace(*m.OwnerToken) == "" || len(strings.TrimSpace(*m.OwnerToken)) > 128) {
			return errors.New("invalid owner_token")
		}
	case "vote_selected", "vote_submitted":
		if !m.valueSet || m.Value == nil || math.IsNaN(*m.Value) || math.IsInf(*m.Value, 0) {
			return errors.New("finite value is required")
		}
		if m.nameSet || m.roleSet || m.clientIDSet || m.ownerTokenSet {
			return errors.New("unexpected vote fields")
		}
	case "reset":
		if m.nameSet || m.roleSet || m.clientIDSet || m.ownerTokenSet || m.valueSet {
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
	if s.PingInterval <= 0 {
		s.PingInterval = time.Second
	}
	if s.MaxMessageBytes <= 0 {
		s.MaxMessageBytes = 32768
	}
	if s.Hub == nil {
		s.Hub = realtime.NewHub()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/api/rooms", s.create)
	mux.HandleFunc("/api/rooms/", s.getRoom)
	mux.HandleFunc("/ws/rooms/", s.ws)
	mux.Handle("/", s.frontend())
	return s.cors(mux)
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
	rm, err := s.Registry.Get(id)
	if err != nil {
		http.Error(w, "room not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rm.Snapshot())
}
func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if s.allowsAnyOrigin() {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
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
		if err == nil && parsed.Host != "" {
			patterns = append(patterns, parsed.Host)
		}
	}
	return patterns
}
func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
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
	x, e := s.Registry.CreateWithOwner(*in.Cards, *in.Roles, ownerToken)
	if e != nil {
		if errors.Is(e, room.ErrCapacity) {
			http.Error(w, e.Error(), http.StatusTooManyRequests)
			return
		}
		http.Error(w, e.Error(), 400)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": x.ID, "url": "/room/" + x.ID, "owner_token": ownerToken})
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
	rm, e := s.Registry.Get(id)
	if e != nil {
		http.Error(w, "room not found", 404)
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
	connectionCtx, cancelConnection := context.WithCancel(context.WithoutCancel(r.Context()))
	defer cancelConnection()
	var in incomingMessage
	if wsjson.Read(connectionCtx, c, &in) != nil {
		return
	}
	if err := in.validate(true); err != nil {
		_ = wsjson.Write(connectionCtx, c, message{Type: "error", Error: err.Error()})
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
	if len(clientID) > 128 {
		clientID = ""
	}
	ownerToken := ""
	if in.OwnerToken != nil {
		ownerToken = strings.TrimSpace(*in.OwnerToken)
	}
	connectionID := uuid.NewString()
	p := &room.Participant{ID: uuid.NewString(), Name: name, Role: role}
	reconnected, e := rm.AddConnection(p, clientID, ownerToken, connectionID)
	if e != nil {
		_ = wsjson.Write(connectionCtx, c, message{Type: "error", Error: "name or role is invalid"})
		return
	}
	ss := wstransport.New(c)
	write := func(payload any) {
		writeCtx, cancelWrite := context.WithTimeout(connectionCtx, 5*time.Second)
		defer cancelWrite()
		_ = ss.Write(writeCtx, payload)
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
		s.Hub.RemoveConnection(id, p.ID, token)
		if rm.RemoveConnection(p.ID, connectionID) {
			s.Registry.DeleteIfEmpty(id)
			s.broadcast(id, map[string]any{"type": "participant_left", "state": rm.Snapshot()})
		}
	}()
	write(map[string]any{"type": "room_state", "state": rm.Snapshot(), "self": p.ID})
	if reconnected {
		s.broadcast(id, map[string]any{"type": "room_state", "state": rm.Snapshot()})
	} else {
		s.broadcast(id, map[string]any{"type": "participant_joined", "state": rm.Snapshot()})
	}
	go ss.Ping(connectionCtx, s.PingInterval)
	for {
		var m incomingMessage
		if e := wsjson.Read(connectionCtx, c, &m); e != nil {
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
					s.broadcast(id, map[string]any{"type": typ, "state": rm.Snapshot()})
				}
			}
		case "reset":
			if rm.Reset(p.ID) == nil {
				s.broadcast(id, map[string]any{"type": "voting_reset", "state": rm.Snapshot()})
			}
		}
	}
}
