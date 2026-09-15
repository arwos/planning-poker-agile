/*
 *  Copyright (c) 2026 Mikhail Knyazhev. All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/arwos/planning-poker-agile/app/realtime"
	"github.com/arwos/planning-poker-agile/app/room"
	"github.com/arwos/planning-poker-agile/pkg/config"
	"github.com/arwos/planning-poker-agile/pkg/httpapi"
)

func main() {
	path := "config/config.yaml"
	if x := os.Getenv("CONFIG_PATH"); x != "" {
		path = x
	}
	c, e := config.Load(path)
	if e != nil {
		log.Fatal(e)
	}
	pingInterval, e := c.PingInterval()
	if e != nil {
		log.Fatal(e)
	}
	readHeaderTimeout, writeTimeout, e := c.HTTPTimeouts()
	if e != nil {
		log.Fatal(e)
	}
	pendingRoomTTL, e := c.PendingRoomTTL()
	if e != nil {
		log.Fatal(e)
	}
	joinTimeout, e := c.JoinTimeout()
	if e != nil {
		log.Fatal(e)
	}
	websocketWriteTimeout, e := c.WebSocketWriteTimeout()
	if e != nil {
		log.Fatal(e)
	}
	s := &httpapi.Server{
		Registry:        room.NewRegistry(c.Rooms.MaxConcurrent, c.Rooms.MaxRoles, c.Rooms.MaxStoryPoints, c.Rooms.MaxParticipants),
		Hub:             realtime.NewHub(),
		PingInterval:    pingInterval,
		MaxMessageBytes: c.MaxMessageBytes(),
		CORSOrigins:     c.CORSOrigins,
		PendingRoomTTL:  pendingRoomTTL,
		MaxConnections:  c.WebSocket.MaxConnections,
		JoinTimeout:     joinTimeout,
		MessageRate:     c.WebSocket.MessageRatePerSecond,
		MessageBurst:    c.WebSocket.MessageBurst,
		OutboundQueue:   c.WebSocket.OutboundQueueSize,
		WriteTimeout:    websocketWriteTimeout,
		CreateRate:      c.HTTP.CreateRatePerMinute,
	}
	addr := fmt.Sprintf("%s:%d", c.HTTP.Host, c.HTTP.Port)
	log.Printf("listening on %s", addr)
	log.Fatal((&http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      writeTimeout,
	}).ListenAndServe())
}
