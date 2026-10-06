package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"s3sync/internal/store"
)

const (
	sessionCookie = "s3sync_session"
	sessionTTL    = 30 * 24 * time.Hour
)

type ctxKey struct{}

func currentUser(r *http.Request) *store.User {
	u, _ := r.Context().Value(ctxKey{}).(*store.User)
	return u
}

func (s *Server) sessionUser(r *http.Request) *store.User {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	u, err := s.store.SessionUser(c.Value)
	if err != nil {
		return nil
	}
	return u
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := s.sessionUser(r)
		if u == nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	})
}

func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.UserCount()
	if err != nil {
		writeErr(w, err)
		return
	}
	resp := map[string]any{"setup_required": n == 0, "authenticated": false}
	if u := s.sessionUser(r); u != nil {
		resp["authenticated"] = true
		resp["username"] = u.Username
	}
	writeJSON(w, http.StatusOK, resp)
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (c *credentials) validate() error {
	c.Username = strings.TrimSpace(c.Username)
	if c.Username == "" {
		return errors.New("username is required")
	}
	if len(c.Password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	return nil
}

func (s *Server) authSetup(w http.ResponseWriter, r *http.Request) {
	var req credentials
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := req.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	n, err := s.store.UserCount()
	if err != nil {
		writeErr(w, err)
		return
	}
	if n > 0 {
		writeError(w, http.StatusConflict, "setup already completed")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeErr(w, err)
		return
	}
	id, err := s.store.CreateUser(req.Username, string(hash))
	if err != nil {
		writeErr(w, err)
		return
	}
	s.startSession(w, r, id)
}

func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.login.allow(ip) {
		writeError(w, http.StatusTooManyRequests, "too many failed attempts; try again in a few minutes")
		return
	}
	var req credentials
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	u, err := s.store.UserByName(strings.TrimSpace(req.Username))
	if err != nil || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
		s.login.fail(ip)
		time.Sleep(300 * time.Millisecond)
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	s.login.reset(ip)
	s.startSession(w, r, u.ID)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID int64) {
	token, err := s.store.CreateSession(userID, sessionTTL)
	if err != nil {
		writeErr(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies || r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.store.DeleteSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	u := currentUser(r)
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Current)) != nil {
		writeError(w, http.StatusBadRequest, "current password is incorrect")
		return
	}
	if len(req.New) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.New), bcrypt.DefaultCost)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.store.UpdatePassword(u.ID, string(hash)); err != nil {
		writeErr(w, err)
		return
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.store.DeleteUserSessions(u.ID, c.Value)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// loginLimiter blocks an IP for a while after repeated failed logins.
type loginLimiter struct {
	mu       sync.Mutex
	failures map[string]*attempts
}

type attempts struct {
	count int
	until time.Time
}

const (
	maxFailures = 10
	lockout     = 5 * time.Minute
)

func newLoginLimiter() *loginLimiter { return &loginLimiter{failures: map[string]*attempts{}} }

func (l *loginLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.failures[ip]
	return !ok || a.count < maxFailures || time.Now().After(a.until)
}

func (l *loginLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.failures[ip]
	if !ok || (a.count >= maxFailures && time.Now().After(a.until)) {
		a = &attempts{}
		l.failures[ip] = a
	}
	a.count++
	a.until = time.Now().Add(lockout)
}

func (l *loginLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, ip)
}
