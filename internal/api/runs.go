package api

import (
	"net/http"
	"strconv"
	"time"

	"s3sync/internal/store"
)

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.RunFilter{Kind: q.Get("kind"), Status: q.Get("status")}
	f.JobID, _ = strconv.ParseInt(q.Get("job_id"), 10, 64)
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	runs, err := s.store.ListRuns(f)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	run, err := s.store.GetRun(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) cancelRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if !s.eng.Cancel(id) {
		writeError(w, http.StatusConflict, "run is not active")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.store.ListJobs()
	if err != nil {
		writeErr(w, err)
		return
	}
	views, err := s.jobViews(jobs)
	if err != nil {
		writeErr(w, err)
		return
	}
	storages, err := s.store.ListStorages()
	if err != nil {
		writeErr(w, err)
		return
	}
	counts, err := s.store.RunCountsSince(time.Now().Add(-24 * time.Hour))
	if err != nil {
		writeErr(w, err)
		return
	}
	active, err := s.store.ListRuns(store.RunFilter{Status: store.StatusRunning, Limit: 20})
	if err != nil {
		writeErr(w, err)
		return
	}
	recent, err := s.store.ListRuns(store.RunFilter{Limit: 10})
	if err != nil {
		writeErr(w, err)
		return
	}

	health := map[string]int{}
	var protected, objects int64
	snapshots := 0
	for _, v := range views {
		health[v.Health]++
		protected += v.Snapshots.LatestSize
		objects += v.Snapshots.LatestObjs
		snapshots += v.Snapshots.Count
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"jobs":              views,
		"health":            health,
		"storages":          len(storages),
		"protected_bytes":   protected,
		"protected_objects": objects,
		"snapshots":         snapshots,
		"runs_24h":          counts,
		"active_runs":       active,
		"recent_runs":       recent,
	})
}
