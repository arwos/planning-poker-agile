/*
 *  Copyright (c) 2026 Mikhail Knyazhev. All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package ws

import (
	"context"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// Session serializes writes because coder/websocket permits one concurrent writer.
type Session struct {
	Conn *websocket.Conn
	mu   sync.Mutex
}

func New(conn *websocket.Conn) *Session { return &Session{Conn: conn} }

func (s *Session) Write(ctx context.Context, payload any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return wsjson.Write(ctx, s.Conn, payload)
}

func (s *Session) Ping(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			err := s.Conn.Ping(ctx)
			s.mu.Unlock()
			if err != nil {
				_ = s.Conn.Close(websocket.StatusGoingAway, "ping failed")
				return
			}
		}
	}
}
