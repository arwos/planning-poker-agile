/*
 *  Copyright (c) 2026 Mikhail Knyazhev. All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

// Package realtime manages room-scoped delivery of already prepared events.
package realtime

import "sync"

type Sender func(payload any)

type Hub struct {
	mu    sync.RWMutex
	rooms map[string]map[string]Sender
}

func NewHub() *Hub { return &Hub{rooms: map[string]map[string]Sender{}} }

func (h *Hub) Add(roomID, participantID string, sender Sender) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.rooms[roomID] == nil {
		h.rooms[roomID] = map[string]Sender{}
	}
	h.rooms[roomID][participantID] = sender
}

func (h *Hub) Remove(roomID, participantID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.rooms[roomID], participantID)
	if len(h.rooms[roomID]) == 0 {
		delete(h.rooms, roomID)
	}
}

func (h *Hub) Broadcast(roomID string, payload any) {
	h.mu.RLock()
	senders := make([]Sender, 0, len(h.rooms[roomID]))
	for _, sender := range h.rooms[roomID] {
		senders = append(senders, sender)
	}
	h.mu.RUnlock()
	for _, sender := range senders {
		sender(payload)
	}
}
