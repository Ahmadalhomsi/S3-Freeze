package api

import (
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
)

// Types the browser may render inline. Everything else is either shown as
// plain text (?text=1) or downloaded. SVG is allowed because previews load it
// through <img>, and the sandbox CSP below blocks scripts on direct visits.
func inlineType(ct string) bool {
	return strings.HasPrefix(ct, "image/") || strings.HasPrefix(ct, "video/") || strings.HasPrefix(ct, "audio/")
}

const maxTextPreview = 256 << 10

// snapshotFile streams one object out of a snapshot for preview or download.
//
//	?path=<key>    object key (required)
//	?download=1    force a download
//	?text=1        plain-text preview, truncated to 256 KiB
func (s *Server) snapshotFile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	q := r.URL.Query()
	key := q.Get("path")
	if key == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	ctx, cancel := timeoutCtx(r, 30*time.Minute)
	defer cancel()
	rd, en, err := s.eng.OpenFile(ctx, id, r.PathValue("sid"), key)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rd.Close()

	ct, _, _ := mime.ParseMediaType(en.ContentType)
	if ct == "" || ct == "application/octet-stream" || ct == "binary/octet-stream" {
		ct, _, _ = mime.ParseMediaType(mime.TypeByExtension(path.Ext(key)))
	}
	name := path.Base(key)
	h := w.Header()
	// Backed-up content is untrusted: never let it run scripts in our origin.
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self' data:; media-src 'self'; style-src 'unsafe-inline'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, max-age=3600") // snapshots never change

	var body io.Reader = rd
	switch {
	case q.Get("download") == "1":
		h.Set("Content-Type", "application/octet-stream")
		h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
		h.Set("Content-Length", strconv.FormatInt(en.Size, 10))
	case q.Get("text") == "1":
		h.Set("Content-Type", "text/plain; charset=utf-8")
		if en.Size > maxTextPreview {
			h.Set("X-Truncated", "true")
		}
		body = io.LimitReader(rd, maxTextPreview)
	case inlineType(ct):
		h.Set("Content-Type", ct)
		h.Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": name}))
		h.Set("Content-Length", strconv.FormatInt(en.Size, 10))
	default:
		h.Set("Content-Type", "application/octet-stream")
		h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
		h.Set("Content-Length", strconv.FormatInt(en.Size, 10))
	}
	io.Copy(w, body)
}
