/*
 *  Copyright (c) 2026 Mikhail Knyazhev. All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package ws

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// Session serializes writes because coder/websocket permits one concurrent writer.
type Session struct {
	Conn      *websocket.Conn
	mu        sync.Mutex
	writeMu   sync.Mutex
	queue     chan any
	done      chan struct{}
	closed    bool
	closeOnce sync.Once
}

func New(conn *websocket.Conn, queueSize ...int) *Session {
	size := 32
	if len(queueSize) > 0 && queueSize[0] > 0 {
		size = queueSize[0]
	}
	return &Session{Conn: conn, queue: make(chan any, size), done: make(chan struct{})}
}

func (s *Session) Write(ctx context.Context, payload any) error {
	if s.Conn == nil {
		return errors.New("nil websocket connection")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return wsjson.Write(ctx, s.Conn, payload)
}

func (s *Session) Enqueue(payload any) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	select {
	case s.queue <- payload:
		return true
	default:
		return false
	}
}

func (s *Session) Close() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		close(s.done)
	})
}

func (s *Session) Run(ctx context.Context, interval, writeTimeout time.Duration) error {
	if err := ctx.Err(); err != nil {
		s.Close()
		return err
	}
	if s.Conn == nil {
		s.Close()
		return errors.New("nil websocket connection")
	}
	if interval <= 0 {
		interval = time.Second
	}
	if writeTimeout <= 0 {
		writeTimeout = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.Close()
			return ctx.Err()
		case <-s.done:
			return context.Canceled
		case payload := <-s.queue:
			writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
			s.writeMu.Lock()
			err := wsjson.Write(writeCtx, s.Conn, payload)
			s.writeMu.Unlock()
			cancel()
			if err != nil {
				s.Close()
				return err
			}
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, writeTimeout)
			s.writeMu.Lock()
			err := s.Conn.Ping(pingCtx)
			s.writeMu.Unlock()
			cancel()
			if err != nil {
				s.Close()
				return err
			}
		}
	}
}

// Ping preserves the old transport API for callers outside the HTTP handler.
func (s *Session) Ping(ctx context.Context, interval time.Duration) {
	_ = s.Run(ctx, interval, interval)
}
