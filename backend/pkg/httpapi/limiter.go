/*
 *  Copyright (c) 2026 Mikhail Knyazhev. All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package httpapi

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type tokenBucket struct {
	tokens float64
	last   time.Time
}

func (b *tokenBucket) allow(rate float64, burst int, now time.Time) bool {
	if rate <= 0 {
		rate = 1
	}
	if burst <= 0 {
		burst = 1
	}
	if b.last.IsZero() {
		b.tokens = float64(burst)
		b.last = now
	}
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens = minFloat(float64(burst), b.tokens+elapsed*rate)
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

type keyedLimiter struct {
	mu         sync.Mutex
	entries    map[string]tokenBucket
	rate       float64
	burst      float64
	maxEntries int
}

func newKeyedLimiter(ratePerSecond float64, burst, maxEntries int) *keyedLimiter {
	if ratePerSecond <= 0 {
		ratePerSecond = 1
	}
	if burst <= 0 {
		burst = 1
	}
	if maxEntries <= 0 {
		maxEntries = 4096
	}
	return &keyedLimiter{
		entries:    make(map[string]tokenBucket),
		rate:       ratePerSecond,
		burst:      float64(burst),
		maxEntries: maxEntries,
	}
}

func (l *keyedLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.purgeExpiredLocked(now)
	if bucket, ok := l.entries[key]; ok {
		elapsed := now.Sub(bucket.last).Seconds()
		if elapsed > 0 {
			bucket.tokens = minFloat(l.burst, bucket.tokens+elapsed*l.rate)
			bucket.last = now
		}
		if bucket.tokens < 1 {
			l.entries[key] = bucket
			return false
		}
		bucket.tokens--
		l.entries[key] = bucket
		return true
	}
	if len(l.entries) >= l.maxEntries {
		return false
	}
	l.entries[key] = tokenBucket{tokens: l.burst - 1, last: now}
	return true
}

func (l *keyedLimiter) purgeExpiredLocked(now time.Time) {
	expiry := 2 * time.Minute
	if l.rate > 0 {
		expiry = time.Duration(float64(time.Second) * (2 * l.burst / l.rate))
		if expiry < time.Minute {
			expiry = time.Minute
		}
	}
	for key, bucket := range l.entries {
		if now.Sub(bucket.last) > expiry {
			delete(l.entries, key)
		}
	}
}

func minFloat(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}

func remoteIP(r *http.Request) string {
	address := r.RemoteAddr
	host := address
	if parsedHost, _, err := net.SplitHostPort(address); err == nil {
		host = parsedHost
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); net.ParseIP(forwarded) != nil {
			return forwarded
		}
		if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); net.ParseIP(realIP) != nil {
			return realIP
		}
	}
	if strings.TrimSpace(host) != "" {
		return strings.TrimSpace(host)
	}
	return "unknown"
}
