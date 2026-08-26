/*
 *  Copyright (c) 2026 Mikhail Knyazhev. All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	HTTP struct {
		Host              string `yaml:"host"`
		Port              int    `yaml:"port"`
		ReadHeaderTimeout string `yaml:"read_header_timeout"`
		WriteTimeout      string `yaml:"write_timeout"`
	} `yaml:"http"`
	Rooms struct {
		MaxConcurrent  int `yaml:"max_concurrent"`
		MaxRoles       int `yaml:"max_roles"`
		MaxStoryPoints int `yaml:"max_story_points"`
	} `yaml:"rooms"`
	WebSocket struct {
		PingInterval    string `yaml:"ping_interval"`
		MaxMessageBytes int64  `yaml:"max_message_bytes"`
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
	if err = yaml.Unmarshal(b, &c); err != nil {
		return c, err
	}
	if err = c.applyEnvironment(); err != nil {
		return c, err
	}
	if _, err = c.PingInterval(); err != nil {
		return c, fmt.Errorf("websocket.ping_interval: %w", err)
	}
	if _, _, err = c.HTTPTimeouts(); err != nil {
		return c, err
	}
	if c.Rooms.MaxConcurrent < 0 || c.Rooms.MaxRoles < 0 || c.Rooms.MaxStoryPoints < 0 {
		return c, fmt.Errorf("room limits must not be negative")
	}
	return c, nil
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
	if value, ok := os.LookupEnv("PLANNING_POKER_WEBSOCKET_PING_INTERVAL"); ok {
		c.WebSocket.PingInterval = value
	}
	if value, ok := os.LookupEnv("PLANNING_POKER_CORS_ORIGINS"); ok {
		c.CORSOrigins = strings.Split(value, ",")
	}
	for _, item := range []struct {
		name   string
		target *int
	}{
		{"PLANNING_POKER_HTTP_PORT", &c.HTTP.Port},
		{"PLANNING_POKER_ROOMS_MAX_CONCURRENT", &c.Rooms.MaxConcurrent},
		{"PLANNING_POKER_ROOMS_MAX_ROLES", &c.Rooms.MaxRoles},
		{"PLANNING_POKER_ROOMS_MAX_STORY_POINTS", &c.Rooms.MaxStoryPoints},
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
	return nil
}
