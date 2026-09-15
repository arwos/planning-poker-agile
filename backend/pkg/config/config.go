/*
 *  Copyright (c) 2026 Mikhail Knyazhev. All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package config

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	HTTP struct {
		Host                string `yaml:"host"`
		Port                int    `yaml:"port"`
		ReadHeaderTimeout   string `yaml:"read_header_timeout"`
		WriteTimeout        string `yaml:"write_timeout"`
		CreateRatePerMinute int    `yaml:"create_rate_per_minute"`
	} `yaml:"http"`
	Rooms struct {
		MaxConcurrent   int    `yaml:"max_concurrent"`
		MaxRoles        int    `yaml:"max_roles"`
		MaxStoryPoints  int    `yaml:"max_story_points"`
		MaxParticipants int    `yaml:"max_participants"`
		PendingTTL      string `yaml:"pending_ttl"`
	} `yaml:"rooms"`
	WebSocket struct {
		PingInterval         string  `yaml:"ping_interval"`
		MaxMessageBytes      int64   `yaml:"max_message_bytes"`
		MaxConnections       int     `yaml:"max_connections"`
		JoinTimeout          string  `yaml:"join_timeout"`
		MessageRatePerSecond float64 `yaml:"message_rate_per_second"`
		MessageBurst         int     `yaml:"message_burst"`
		OutboundQueueSize    int     `yaml:"outbound_queue_size"`
		WriteTimeout         string  `yaml:"write_timeout"`
	} `yaml:"websocket"`
	CORSOrigins []string `yaml:"cors_origins"`
}

func (c Config) PingInterval() (time.Duration, error) {
	if c.WebSocket.PingInterval == "" {
		return time.Second, nil
	}
	return time.ParseDuration(c.WebSocket.PingInterval)
}

func (c Config) MaxMessageBytes() int64 {
	if c.WebSocket.MaxMessageBytes <= 0 {
		return 32768
	}
	return c.WebSocket.MaxMessageBytes
}

func (c Config) PendingRoomTTL() (time.Duration, error) {
	if c.Rooms.PendingTTL == "" {
		return 10 * time.Minute, nil
	}
	return time.ParseDuration(c.Rooms.PendingTTL)
}

func (c Config) JoinTimeout() (time.Duration, error) {
	if c.WebSocket.JoinTimeout == "" {
		return 10 * time.Second, nil
	}
	return time.ParseDuration(c.WebSocket.JoinTimeout)
}

func (c Config) WebSocketWriteTimeout() (time.Duration, error) {
	if c.WebSocket.WriteTimeout == "" {
		return 5 * time.Second, nil
	}
	return time.ParseDuration(c.WebSocket.WriteTimeout)
}

func (c Config) HTTPTimeouts() (time.Duration, time.Duration, error) {
	read, write := "5s", "10s"
	if c.HTTP.ReadHeaderTimeout != "" {
		read = c.HTTP.ReadHeaderTimeout
	}
	if c.HTTP.WriteTimeout != "" {
		write = c.HTTP.WriteTimeout
	}
	rd, err := time.ParseDuration(read)
	if err != nil {
		return 0, 0, fmt.Errorf("http.read_header_timeout: %w", err)
	}
	wd, err := time.ParseDuration(write)
	if err != nil {
		return 0, 0, fmt.Errorf("http.write_timeout: %w", err)
	}
	return rd, wd, nil
}

func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	var c Config
	if err != nil {
		return c, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(b))
	decoder.KnownFields(true)
	if err = decoder.Decode(&c); err != nil && err != io.EOF {
		return c, err
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return c, fmt.Errorf("configuration must contain one YAML document")
		}
		return c, err
	}
	if err = c.applyEnvironment(); err != nil {
		return c, err
	}
	pingInterval, err := c.PingInterval()
	if err != nil {
		return c, fmt.Errorf("websocket.ping_interval: %w", err)
	}
	if pingInterval <= 0 {
		return c, fmt.Errorf("websocket.ping_interval must be positive")
	}
	readHeaderTimeout, writeHTTPTimeout, err := c.HTTPTimeouts()
	if err != nil {
		return c, err
	}
	if readHeaderTimeout <= 0 || writeHTTPTimeout <= 0 {
		return c, fmt.Errorf("http timeouts must be positive")
	}
	pendingTTL, err := c.PendingRoomTTL()
	if err != nil {
		return c, fmt.Errorf("rooms.pending_ttl: %w", err)
	}
	if pendingTTL <= 0 {
		return c, fmt.Errorf("rooms.pending_ttl must be positive")
	}
	joinTimeout, err := c.JoinTimeout()
	if err != nil {
		return c, fmt.Errorf("websocket.join_timeout: %w", err)
	}
	if joinTimeout <= 0 {
		return c, fmt.Errorf("websocket.join_timeout must be positive")
	}
	writeTimeout, err := c.WebSocketWriteTimeout()
	if err != nil {
		return c, fmt.Errorf("websocket.write_timeout: %w", err)
	}
	if writeTimeout <= 0 {
		return c, fmt.Errorf("websocket.write_timeout must be positive")
	}
	if c.Rooms.MaxConcurrent <= 0 {
		c.Rooms.MaxConcurrent = 100
	}
	if c.Rooms.MaxParticipants <= 0 {
		c.Rooms.MaxParticipants = 32
	}
	if c.WebSocket.MaxConnections <= 0 {
		c.WebSocket.MaxConnections = 1000
	}
	if c.WebSocket.MessageRatePerSecond <= 0 {
		c.WebSocket.MessageRatePerSecond = 20
	}
	if c.WebSocket.MessageBurst <= 0 {
		c.WebSocket.MessageBurst = 40
	}
	if c.WebSocket.OutboundQueueSize <= 0 {
		c.WebSocket.OutboundQueueSize = 32
	}
	if c.HTTP.CreateRatePerMinute <= 0 {
		c.HTTP.CreateRatePerMinute = 10
	}
	if math.IsNaN(c.WebSocket.MessageRatePerSecond) || math.IsInf(c.WebSocket.MessageRatePerSecond, 0) {
		return c, fmt.Errorf("websocket.message_rate_per_second must be finite")
	}
	if c.Rooms.MaxRoles < 0 || c.Rooms.MaxStoryPoints < 0 {
		return c, fmt.Errorf("room limits must not be negative")
	}
	if err := validateOrigins(c.CORSOrigins); err != nil {
		return c, err
	}
	return c, nil
}

func validateOrigins(origins []string) error {
	for _, origin := range origins {
		origin = strings.TrimSpace(origin)
		if origin == "*" {
			continue
		}
		parsed, err := url.Parse(origin)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
			return fmt.Errorf("invalid cors origin %q", origin)
		}
	}
	return nil
}

func (c *Config) applyEnvironment() error {
	if value, ok := os.LookupEnv("PLANNING_POKER_HTTP_HOST"); ok {
		c.HTTP.Host = value
	}
	if value, ok := os.LookupEnv("PLANNING_POKER_HTTP_READ_HEADER_TIMEOUT"); ok {
		c.HTTP.ReadHeaderTimeout = value
	}
	if value, ok := os.LookupEnv("PLANNING_POKER_HTTP_WRITE_TIMEOUT"); ok {
		c.HTTP.WriteTimeout = value
	}
	if value, ok := os.LookupEnv("PLANNING_POKER_HTTP_CREATE_RATE_PER_MINUTE"); ok {
		parsed, parseErr := strconv.Atoi(value)
		if parseErr != nil {
			return fmt.Errorf("PLANNING_POKER_HTTP_CREATE_RATE_PER_MINUTE: %w", parseErr)
		}
		c.HTTP.CreateRatePerMinute = parsed
	}
	if value, ok := os.LookupEnv("PLANNING_POKER_ROOMS_PENDING_TTL"); ok {
		c.Rooms.PendingTTL = value
	}
	if value, ok := os.LookupEnv("PLANNING_POKER_WEBSOCKET_PING_INTERVAL"); ok {
		c.WebSocket.PingInterval = value
	}
	if value, ok := os.LookupEnv("PLANNING_POKER_WEBSOCKET_JOIN_TIMEOUT"); ok {
		c.WebSocket.JoinTimeout = value
	}
	if value, ok := os.LookupEnv("PLANNING_POKER_WEBSOCKET_WRITE_TIMEOUT"); ok {
		c.WebSocket.WriteTimeout = value
	}
	if value, ok := os.LookupEnv("PLANNING_POKER_CORS_ORIGINS"); ok {
		origins := strings.Split(value, ",")
		c.CORSOrigins = c.CORSOrigins[:0]
		for _, origin := range origins {
			if origin = strings.TrimSpace(origin); origin != "" {
				c.CORSOrigins = append(c.CORSOrigins, origin)
			}
		}
	}
	for _, item := range []struct {
		name   string
		target *int
	}{
		{"PLANNING_POKER_HTTP_PORT", &c.HTTP.Port},
		{"PLANNING_POKER_ROOMS_MAX_CONCURRENT", &c.Rooms.MaxConcurrent},
		{"PLANNING_POKER_ROOMS_MAX_ROLES", &c.Rooms.MaxRoles},
		{"PLANNING_POKER_ROOMS_MAX_STORY_POINTS", &c.Rooms.MaxStoryPoints},
		{"PLANNING_POKER_ROOMS_MAX_PARTICIPANTS", &c.Rooms.MaxParticipants},
		{"PLANNING_POKER_WEBSOCKET_MAX_CONNECTIONS", &c.WebSocket.MaxConnections},
		{"PLANNING_POKER_WEBSOCKET_MESSAGE_BURST", &c.WebSocket.MessageBurst},
		{"PLANNING_POKER_WEBSOCKET_OUTBOUND_QUEUE_SIZE", &c.WebSocket.OutboundQueueSize},
	} {
		if value, ok := os.LookupEnv(item.name); ok {
			parsed, parseErr := strconv.Atoi(value)
			if parseErr != nil {
				return fmt.Errorf("%s: %w", item.name, parseErr)
			}
			*item.target = parsed
		}
	}
	if value, ok := os.LookupEnv("PLANNING_POKER_WEBSOCKET_MAX_MESSAGE_BYTES"); ok {
		parsed, parseErr := strconv.ParseInt(value, 10, 64)
		if parseErr != nil {
			return fmt.Errorf("PLANNING_POKER_WEBSOCKET_MAX_MESSAGE_BYTES: %w", parseErr)
		}
		c.WebSocket.MaxMessageBytes = parsed
	}
	if value, ok := os.LookupEnv("PLANNING_POKER_WEBSOCKET_MESSAGE_RATE_PER_SECOND"); ok {
		parsed, parseErr := strconv.ParseFloat(value, 64)
		if parseErr != nil {
			return fmt.Errorf("PLANNING_POKER_WEBSOCKET_MESSAGE_RATE_PER_SECOND: %w", parseErr)
		}
		c.WebSocket.MessageRatePerSecond = parsed
	}
	return nil
}
