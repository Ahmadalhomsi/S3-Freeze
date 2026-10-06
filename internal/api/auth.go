package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"s3freeze/internal/config"
	"s3freeze/internal/store"
)

const (
	sessionCookie = "s3freeze_session"
	sessionTTL    = 7 * 24 * time.Hour
	minPassword   = 8
	maxPassword   = 72 // bcrypt limit
)

// dummyHash is compared against when a username does not exist, so failed
// logins take the same time whether or not the user exists.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("s3freeze-timing-equalizer"), bcrypt.DefaultCost)

type ctxKey struct{}

func currentUser(r *http.Request) *store.User {
	u, _ := r.Context().Value(ctxKey{}).(*store.User)
	return u
}

// BootstrapAdmin creates the admin account from ADMIN_USERNAME/ADMIN_PASSWORD
// when none exists, or resets its password when ADMIN_RESET=true. The
// password is only stored as a bcrypt hash.
func BootstrapAdmin(st *store.Store, cfg *config.Config) error {
	if cfg.AdminPassword == "" {
		return nil
	}
	if err := validatePassword(cfg.AdminPassword); err != nil {
		return errors.New("ADMIN_PASSWORD: " + err.Error())
	}
	n, err := st.UserCount()
	if err != nil {
		return err
	}
	if n > 0 && !cfg.AdminReset {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := st.UpsertUser(cfg.AdminUsername, string(hash)); err != nil {
		return err
	}
	if n == 0 {
		slog.Info("admin account created from ADMIN_PASSWORD", "username", cfg.AdminUsername)
	} else {
		slog.Warn("admin password reset from ADMIN_PASSWORD; remove ADMIN_RESET", "username", cfg.AdminUsername)
	}
	return nil
}

// setupGuard holds the one-time token required to create the first account
// through the web UI, so a stranger cannot claim a fresh deployment.
type setupGuard struct {
	mu    sync.Mutex
	token string
}

func newSetupGuard(st *store.Store) *setupGuard {
	g := &setupGuard{}
	if n, err := st.UserCount(); err == nil && n == 0 {
		b := make([]byte, 12)
		rand.Read(b)
		g.token = hex.EncodeToString(b)
		slog.Warn("No admin account yet. Open the web UI and enter this setup token " +
			"(or set ADMIN_PASSWORD to create the account automatically).")
		slog.Warn("setup token", "token", g.token)
	}
	return g
}

func (g *setupGuard) check(token string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.token != "" && subtle.ConstantTimeCompare([]byte(g.token), []byte(strings.TrimSpace(token))) == 1
}

func (g *setupGuard) done() {
	g.mu.Lock()
	g.token = ""
	g.mu.Unlock()
}

func (s *Server) sessionUser(r *http.Request) *store.User {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" || len(c.Value) > 128 {
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
	Username   string `json:"username"`
	Password   string `json:"password"`
	SetupToken string `json:"setup_token,omitempty"`
}

func validatePassword(p string) error {
	if len(p) < minPassword {
		return errors.New("password must be at least 8 characters")
	}
	if len(p) > maxPassword {
		return errors.New("password must be at most 72 bytes")
	}
	return nil
}

// limited rejects the request if the client IP is locked out.
func (s *Server) limited(w http.ResponseWriter, ip string) bool {
	if d := s.ipFails.blocked(ip); d > 0 {
		retryAfter(w, d)
		writeError(w, http.StatusTooManyRequests, "too many failed attempts; try again later")
		return true
	}
	return false
}

func (s *Server) authSetup(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP(r)
	if s.limited(w, ip) {
		return
	}
	var req credentials
	if err := decode(r, &req); err != nil {
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
	if !s.setup.check(req.SetupToken) {
		s.ipFails.fail(ip)
		slog.Warn("setup attempt with invalid token", "ip", ip)
		writeError(w, http.StatusForbidden, "invalid setup token; find it in the server logs")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		writeError(w, http.StatusBadRequest, "username is required")
		return
	}
	if err := validatePassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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
	s.setup.done()
	slog.Info("admin account created", "username", req.Username, "ip", ip)
	s.startSession(w, r, id)
}

func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP(r)
	if s.limited(w, ip) {
		return
	}
	var req credentials
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	username := strings.ToLower(strings.TrimSpace(req.Username))
	if d := s.userFails.blocked(username); d > 0 {
		retryAfter(w, d)
		writeError(w, http.StatusTooManyRequests, "this account is temporarily locked after too many failed attempts")
		return
	}

	u, err := s.store.UserByName(strings.TrimSpace(req.Username))
	ok := false
	if err == nil && len(req.Password) <= maxPassword {
		ok = bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) == nil
	} else {
		bcrypt.CompareHashAndPassword(dummyHash, []byte(req.Password))
	}
	if !ok {
		s.ipFails.fail(ip)
		s.userFails.fail(username)
		slog.Warn("login failed", "ip", ip, "username", req.Username)
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	s.ipFails.reset(ip)
	s.userFails.reset(username)
	slog.Info("login", "ip", ip, "username", u.Username)
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
		Secure:   s.cfg.SecureCookies || s.isHTTPS(r),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.store.DeleteSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP(r)
	if s.limited(w, ip) {
		return
	}
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
		s.ipFails.fail(ip)
		writeError(w, http.StatusBadRequest, "current password is incorrect")
		return
	}
	if err := validatePassword(req.New); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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
	slog.Info("password changed", "username", u.Username, "ip", ip)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
