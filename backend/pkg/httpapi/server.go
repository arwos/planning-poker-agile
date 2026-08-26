/*
 *  Copyright (c) 2026 Mikhail Knyazhev. All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
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
	Cards []float64 `json:"cards"`
	Roles []string  `json:"roles"`
}
type message struct {
	Type  string   `json:"type"`
	Name  string   `json:"name,omitempty"`
	Role  string   `json:"role,omitempty"`
	Value *float64 `json:"value,omitempty"`
	Error string   `json:"error,omitempty"`
}

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
		if len(s.CORSOrigins) == 0 {
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
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) originPatterns() []string {
	patterns := make([]string, 0, len(s.CORSOrigins))
	for _, origin := range s.CORSOrigins {
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
	var in createRequest
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		http.Error(w, "invalid json", 400)
		return
	}
	x, e := s.Registry.Create(in.Cards, in.Roles)
	if e != nil {
		if errors.Is(e, room.ErrCapacity) {
			http.Error(w, e.Error(), http.StatusTooManyRequests)
			return
		}
		http.Error(w, e.Error(), 400)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": x.ID, "url": "/room/" + x.ID})
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
	c, e := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: s.originPatterns()})
	if e != nil {
		return
	}
	c.SetReadLimit(s.MaxMessageBytes)
	defer c.Close(websocket.StatusNormalClosure, "")
	var in message
	if wsjson.Read(r.Context(), c, &in) != nil {
		return
	}
	if in.Type != "join" {
		_ = wsjson.Write(r.Context(), c, message{Type: "error", Error: "first message must be join"})
		return
	}
	p := &room.Participant{ID: uuid.NewString(), Name: strings.TrimSpace(in.Name), Role: strings.TrimSpace(in.Role)}
	if e = rm.Add(p); e != nil {
		_ = wsjson.Write(r.Context(), c, message{Type: "error", Error: "name or role is invalid"})
		return
	}
	ss := wstransport.New(c)
	s.Hub.Add(id, p.ID, func(payload any) {
		_ = ss.Write(context.Background(), payload)
	})
	defer func() {
		s.Hub.Remove(id, p.ID)
		if rm.Remove(p.ID) {
			s.Registry.DeleteIfEmpty(id)
		}
		s.broadcast(id, map[string]any{"type": "participant_left", "state": rm.Snapshot()})
	}()
	_ = ss.Write(r.Context(), map[string]any{"type": "room_state", "state": rm.Snapshot(), "self": p.ID})
	s.broadcast(id, map[string]any{"type": "participant_joined", "state": rm.Snapshot()})
	go ss.Ping(r.Context(), s.PingInterval)
	for {
		var m message
		if e := wsjson.Read(r.Context(), c, &m); e != nil {
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
