/*
 *  Copyright (c) 2026 Mikhail Knyazhev. All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

// Package realtime manages room-scoped delivery of already prepared events.
package realtime

import "sync"

type Sender func(payload any)

type connection struct {
	sender Sender
	close  func()
	token  uint64
}

type Hub struct {
	mu        sync.RWMutex
	rooms     map[string]map[string]connection
	nextToken uint64
}

func NewHub() *Hub { return &Hub{rooms: map[string]map[string]connection{}} }

func (h *Hub) Add(roomID, participantID string, sender Sender) {
	_, previousClose := h.AddConnection(roomID, participantID, sender, nil)
	if previousClose != nil {
		previousClose()
	}
}

func (h *Hub) AddConnection(roomID, participantID string, sender Sender, close func()) (uint64, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextToken++
	token := h.nextToken
	if h.rooms[roomID] == nil {
		h.rooms[roomID] = map[string]connection{}
	}
	previous := h.rooms[roomID][participantID]
	h.rooms[roomID][participantID] = connection{sender: sender, close: close, token: token}
	return token, previous.close
}

func (h *Hub) Remove(roomID, participantID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.rooms[roomID], participantID)
	if len(h.rooms[roomID]) == 0 {
		delete(h.rooms, roomID)
	}
}

func (h *Hub) RemoveConnection(roomID, participantID string, token uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	current, ok := h.rooms[roomID][participantID]
	if !ok || current.token != token {
		return
	}
	delete(h.rooms[roomID], participantID)
	if len(h.rooms[roomID]) == 0 {
		delete(h.rooms, roomID)
	}
}

func (h *Hub) Broadcast(roomID string, payload any) {
	h.mu.RLock()
	senders := make([]Sender, 0, len(h.rooms[roomID]))
	for _, client := range h.rooms[roomID] {
		senders = append(senders, client.sender)
	}
	h.mu.RUnlock()
	for _, sender := range senders {
		sender(payload)
	}
}
