// Package api exposes the REST API and serves the embedded web UI.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"s3freeze/internal/config"
	"s3freeze/internal/engine"
	"s3freeze/internal/scheduler"
	"s3freeze/internal/store"
	"s3freeze/internal/ui"
)

type Server struct {
	cfg   *config.Config
	store *store.Store
	eng   *engine.Engine
	sched *scheduler.Scheduler
	setup *setupGuard

	ipFails    *failureLimiter // failed logins per client IP
	userFails  *failureLimiter // failed logins per username (distributed attacks)
	apiLimiter *rateLimiter    // overall API request rate per client IP
}

func New(cfg *config.Config, st *store.Store, eng *engine.Engine, sched *scheduler.Scheduler) *Server {
	return &Server{
		cfg: cfg, store: st, eng: eng, sched: sched,
		setup:      newSetupGuard(st),
		ipFails:    newFailureLimiter(5, 15*time.Minute, 15*time.Minute),
		userFails:  newFailureLimiter(10, 15*time.Minute, 15*time.Minute),
		apiLimiter: newRateLimiter(20, 100),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.health)

	mux.HandleFunc("GET /api/auth/status", s.authStatus)
	mux.HandleFunc("POST /api/auth/setup", s.authSetup)
	mux.HandleFunc("POST /api/auth/login", s.authLogin)
	mux.HandleFunc("POST /api/auth/logout", s.authLogout)

	api := http.NewServeMux()
	api.HandleFunc("POST /api/auth/password", s.changePassword)

	api.HandleFunc("GET /api/dashboard", s.dashboard)

	api.HandleFunc("GET /api/storages", s.listStorages)
	api.HandleFunc("POST /api/storages", s.createStorage)
	api.HandleFunc("POST /api/storages/test", s.testStorage)
	api.HandleFunc("PUT /api/storages/{id}", s.updateStorage)
	api.HandleFunc("DELETE /api/storages/{id}", s.deleteStorage)
	api.HandleFunc("GET /api/storages/{id}/buckets", s.listBuckets)
	api.HandleFunc("GET /api/storages/{id}/folders", s.listFolders)
	api.HandleFunc("GET /api/storages/{id}/usage", s.storageUsage)
	api.HandleFunc("GET /api/storages/{id}/disk", s.storageDisk)

	api.HandleFunc("POST /api/backups", s.quickBackup)

	api.HandleFunc("GET /api/jobs", s.listJobs)
	api.HandleFunc("POST /api/jobs", s.createJob)
	api.HandleFunc("GET /api/jobs/{id}", s.getJob)
	api.HandleFunc("PUT /api/jobs/{id}", s.updateJob)
	api.HandleFunc("DELETE /api/jobs/{id}", s.deleteJob)
	api.HandleFunc("POST /api/jobs/{id}/run", s.runJob)
	api.HandleFunc("POST /api/jobs/{id}/scan", s.scanJob)
	api.HandleFunc("GET /api/jobs/{id}/snapshots", s.listSnapshots)
	api.HandleFunc("GET /api/jobs/{id}/snapshots/{sid}/browse", s.browseSnapshot)
	api.HandleFunc("GET /api/jobs/{id}/snapshots/{sid}/file", s.snapshotFile)
	api.HandleFunc("POST /api/jobs/{id}/snapshots/{sid}/restore", s.restoreSnapshot)
	api.HandleFunc("DELETE /api/jobs/{id}/snapshots/{sid}", s.deleteSnapshot)

	api.HandleFunc("GET /api/runs", s.listRuns)
	api.HandleFunc("GET /api/runs/{id}", s.getRun)
	api.HandleFunc("POST /api/runs/{id}/cancel", s.cancelRun)

	api.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	mux.Handle("/api/", s.requireAuth(api))

	mux.Handle("/", spaHandler())
	return s.securityHeaders(s.rateLimit(csrfGuard(mux)))
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// csrfGuard requires a custom header on state-changing API requests. Browsers
// cannot send it cross-origin without a CORS preflight, which we never allow.
func csrfGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodGet && r.Method != http.MethodHead {
			if r.Header.Get("X-Requested-With") != "s3freeze" {
				writeError(w, http.StatusForbidden, "missing X-Requested-With header")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// contentSecurityPolicy only allows the app's own scripts; inline styles are
// needed by the UI components.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; media-src 'self' blob:; connect-src 'self'; font-src 'self'; " +
	"object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		if s.isHTTPS(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// spaHandler serves the built web UI, falling back to index.html for client routes.
func spaHandler() http.Handler {
	dist, err := fs.Sub(ui.Dist, "dist")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(dist, p); err != nil {
			p = "index.html"
			if _, err := fs.Stat(dist, p); err != nil {
				http.Error(w, "web UI not built; run `npm run build` in web/", http.StatusNotFound)
				return
			}
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Debug("write response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeErr maps common errors to HTTP status codes.
func writeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, engine.ErrBusy):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, engine.ErrShuttingDown):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	default:
		slog.Error("request failed", "err", err)
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func decode(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

func timeoutCtx(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}
