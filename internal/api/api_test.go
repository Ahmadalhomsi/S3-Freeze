package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"s3sync/internal/config"
	"s3sync/internal/engine"
	"s3sync/internal/scheduler"
	"s3sync/internal/secret"
	"s3sync/internal/store"
)

func newTestServer(t *testing.T, cfg *config.Config) (*Server, *httptest.Server) {
	t.Helper()
	box, _ := secret.New([32]byte{9})
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"), box)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := BootstrapAdmin(st, cfg); err != nil {
		t.Fatal(err)
	}
	eng := engine.New(st, t.TempDir())
	s := New(cfg, st, eng, scheduler.New(st, eng))
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return s, srv
}

func post(t *testing.T, c *http.Client, url, body string, hdr ...string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "s3sync")
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

func TestSetupRequiresToken(t *testing.T) {
	s, srv := newTestServer(t, &config.Config{})
	c := srv.Client()
	if r := post(t, c, srv.URL+"/api/auth/setup", `{"username":"a","password":"password123","setup_token":"nope"}`); r.StatusCode != http.StatusForbidden {
		t.Fatalf("setup without token: %d", r.StatusCode)
	}
	body := `{"username":"a","password":"password123","setup_token":"` + s.setup.token + `"}`
	if r := post(t, c, srv.URL+"/api/auth/setup", body); r.StatusCode != http.StatusOK {
		t.Fatalf("setup with token: %d", r.StatusCode)
	}
	if r := post(t, c, srv.URL+"/api/auth/setup", body); r.StatusCode != http.StatusConflict {
		t.Fatalf("second setup: %d", r.StatusCode)
	}
}

func TestLoginRateLimit(t *testing.T) {
	// No trusted proxies: forwarding headers must be ignored entirely.
	_, srv := newTestServer(t, &config.Config{AdminUsername: "admin", AdminPassword: "correct-password", TrustedProxies: []*net.IPNet{}})
	c := srv.Client()
	for i := 0; i < 5; i++ {
		if r := post(t, c, srv.URL+"/api/auth/login", `{"username":"admin","password":"wrong"}`); r.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i, r.StatusCode)
		}
	}
	// Locked out now, even with the right password.
	r := post(t, c, srv.URL+"/api/auth/login", `{"username":"admin","password":"correct-password"}`)
	if r.StatusCode != http.StatusTooManyRequests || r.Header.Get("Retry-After") == "" {
		t.Fatalf("expected lockout, got %d", r.StatusCode)
	}
	// A spoofed X-Forwarded-For must not bypass it.
	r = post(t, c, srv.URL+"/api/auth/login", `{"username":"admin","password":"correct-password"}`, "X-Forwarded-For", "1.2.3.4")
	if r.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("spoofed header bypassed lockout: %d", r.StatusCode)
	}
}

func TestAccountLockoutAcrossIPs(t *testing.T) {
	// Behind a trusted proxy, attempts from many client IPs still lock the account.
	nets, _ := config.ParseCIDRs("127.0.0.0/8")
	_, srv := newTestServer(t, &config.Config{AdminUsername: "admin", AdminPassword: "correct-password", TrustedProxies: nets})
	c := srv.Client()
	for i := 0; i < 10; i++ {
		post(t, c, srv.URL+"/api/auth/login", `{"username":"admin","password":"wrong"}`, "X-Forwarded-For", fmt.Sprintf("5.5.5.%d", i))
	}
	r := post(t, c, srv.URL+"/api/auth/login", `{"username":"admin","password":"correct-password"}`, "X-Forwarded-For", "9.9.9.9")
	if r.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected account lockout, got %d", r.StatusCode)
	}
}

func TestClientIP(t *testing.T) {
	nets, _ := config.ParseCIDRs("10.0.0.0/8")
	s := &Server{cfg: &config.Config{TrustedProxies: nets}}
	cases := []struct{ remote, xff, want string }{
		{"8.8.8.8:1", "1.1.1.1", "8.8.8.8"},            // untrusted peer: header ignored
		{"10.0.0.2:1", "1.1.1.1", "1.1.1.1"},           // trusted proxy
		{"10.0.0.2:1", "6.6.6.6, 1.1.1.1", "1.1.1.1"},  // client-supplied prefix ignored
		{"10.0.0.2:1", "1.1.1.1, 10.0.0.3", "1.1.1.1"}, // chain of trusted proxies
		{"10.0.0.2:1", "", "10.0.0.2"},                 // no header
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := s.clientIP(r); got != c.want {
			t.Errorf("%s %q: got %s want %s", c.remote, c.xff, got, c.want)
		}
	}
}

func TestSessionAndHeaders(t *testing.T) {
	_, srv := newTestServer(t, &config.Config{AdminUsername: "admin", AdminPassword: "correct-password"})
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}

	resp, _ := c.Get(srv.URL + "/api/jobs")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated: %d", resp.StatusCode)
	}
	if r := post(t, c, srv.URL+"/api/auth/login", `{"username":"admin","password":"correct-password"}`); r.StatusCode != 200 {
		t.Fatalf("login: %d", r.StatusCode)
	}
	resp, _ = c.Get(srv.URL + "/api/auth/status")
	var st map[string]any
	json.NewDecoder(resp.Body).Decode(&st)
	resp.Body.Close()
	if st["authenticated"] != true {
		t.Fatalf("status after login: %v", st)
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Fatalf("missing CSP: %q", csp)
	}
	// State-changing requests need the anti-CSRF header.
	req, _ := http.NewRequest("POST", srv.URL+"/api/jobs", strings.NewReader(`{}`))
	resp, _ = c.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("csrf: %d", resp.StatusCode)
	}
}
