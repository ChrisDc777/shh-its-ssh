package main

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
)

// limiter is a simple per-key sliding-window rate limiter. Used to cap
// connections and guestbook posts per source IP.
type limiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	max    int
	window time.Duration
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{hits: map[string][]time.Time{}, max: max, window: window}
}

// allow records a hit for key and reports whether it's within the limit.
func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-l.window)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, time.Now())
	return true
}

// remoteIP extracts the source IP (without port) from a session.
func remoteIP(s ssh.Session) string {
	if a, ok := s.RemoteAddr().(*net.TCPAddr); ok {
		return a.IP.String()
	}
	if host, _, err := net.SplitHostPort(s.RemoteAddr().String()); err == nil {
		return host
	}
	return s.RemoteAddr().String()
}

// rateLimitMiddleware rejects a session when its source IP has opened too many
// connections in the recent window.
func rateLimitMiddleware(l *limiter) wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(s ssh.Session) {
			if !l.allow(remoteIP(s)) {
				_, _ = fmt.Fprintln(s, "Too many connections from your address — please try again in a minute.")
				_ = s.Exit(1)
				return
			}
			next(s)
		}
	}
}
