package api

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// failureLimiter locks a key (IP or username) after too many failures within
// a window. Successful attempts reset the key.
type failureLimiter struct {
	max     int
	window  time.Duration
	lockout time.Duration

	mu sync.Mutex
	m  map[string]*failures
}

type failures struct {
	count       int
	first       time.Time
	lockedUntil time.Time
}

func newFailureLimiter(max int, window, lockout time.Duration) *failureLimiter {
	return &failureLimiter{max: max, window: window, lockout: lockout, m: map[string]*failures{}}
}

// blocked returns how long the key is still locked out (0 if not locked).
func (l *failureLimiter) blocked(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if f, ok := l.m[key]; ok {
		if d := time.Until(f.lockedUntil); d > 0 {
			return d
		}
	}
	return 0
}

func (l *failureLimiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.m) > 10000 {
		for k, f := range l.m {
			if now.Sub(f.first) > l.window && now.After(f.lockedUntil) {
				delete(l.m, k)
			}
		}
	}
	f, ok := l.m[key]
	if !ok || (now.Sub(f.first) > l.window && now.After(f.lockedUntil)) {
		f = &failures{first: now}
		l.m[key] = f
	}
	f.count++
	if f.count >= l.max {
		f.lockedUntil = now.Add(l.lockout)
		f.count = 0
		f.first = now
	}
}

func (l *failureLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, key)
}

// rateLimiter is a per-key token bucket.
type rateLimiter struct {
	rate  float64 // tokens per second
	burst float64

	mu     sync.Mutex
	m      map[string]*bucket
	lastGC time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter(rate, burst float64) *rateLimiter {
	return &rateLimiter{rate: rate, burst: burst, m: map[string]*bucket{}, lastGC: time.Now()}
}

func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if now.Sub(l.lastGC) > time.Minute {
		for k, b := range l.m {
			if now.Sub(b.last) > 10*time.Minute {
				delete(l.m, k)
			}
		}
		l.lastGC = now
	}
	b, ok := l.m[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.m[key] = b
	}
	b.tokens = min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rate)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// rateLimit throttles API requests per client IP.
func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && !s.apiLimiter.allow(s.clientIP(r)) {
			w.Header().Set("Retry-After", "5")
			writeError(w, http.StatusTooManyRequests, "too many requests")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func retryAfter(w http.ResponseWriter, d time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(d.Seconds())+1))
}

func (s *Server) trusted(ip net.IP) bool {
	for _, n := range s.cfg.TrustedProxies {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// clientIP returns the real client address. Forwarding headers are honoured
// only when the direct peer is a trusted proxy, and the right-most untrusted
// address is used so clients cannot spoof it by sending their own header.
func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer == nil || !s.trusted(peer) {
		return host
	}
	var hops []string
	for _, v := range r.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(v, ",")...)
	}
	for i := len(hops) - 1; i >= 0; i-- {
		ip := net.ParseIP(strings.TrimSpace(hops[i]))
		if ip == nil {
			break
		}
		if !s.trusted(ip) || i == 0 {
			return ip.String()
		}
	}
	if ip := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); ip != nil {
		return ip.String()
	}
	return host
}

// isHTTPS reports whether the client connection is HTTPS, either directly or
// through a trusted TLS-terminating proxy.
func (s *Server) isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip := net.ParseIP(host); ip != nil && s.trusted(ip) {
		return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	}
	return false
}
