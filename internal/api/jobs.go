package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"s3sync/internal/engine"
	"s3sync/internal/scheduler"
	"s3sync/internal/store"
)

const (
	HealthHealthy  = "healthy"
	HealthFailing  = "failing"
	HealthWarning  = "warning"
	HealthStale    = "stale"
	HealthNever    = "never"
	HealthDisabled = "disabled"
	HealthRunning  = "running"
)

type jobView struct {
	*store.Job
	HasPassphrase bool                   `json:"has_passphrase"`
	Health        string                 `json:"health"`
	NextRun       *time.Time             `json:"next_run"`
	LastRun       *store.Run             `json:"last_run"`
	LastSuccess   *time.Time             `json:"last_success"`
	ActiveRunID   int64                  `json:"active_run_id"`
	Snapshots     *store.SnapshotSummary `json:"snapshots"`
}

type jobInput struct {
	Name            string `json:"name"`
	Enabled         bool   `json:"enabled"`
	Schedule        string `json:"schedule"`
	SourceStorageID int64  `json:"source_storage_id"`
	SourceBucket    string `json:"source_bucket"`
	SourcePrefix    string `json:"source_prefix"`
	DestStorageID   int64  `json:"dest_storage_id"`
	DestBucket      string `json:"dest_bucket"`
	DestPrefix      string `json:"dest_prefix"`
	Compression     bool   `json:"compression"`
	Encryption      bool   `json:"encryption"`
	Passphrase      string `json:"passphrase"`
	Concurrency     int    `json:"concurrency"`
	KeepLast        int    `json:"keep_last"`
	KeepDays        int    `json:"keep_days"`
}

func (s *Server) applyJob(in *jobInput, j *store.Job) error {
	j.Name = strings.TrimSpace(in.Name)
	j.Enabled = in.Enabled
	j.Schedule = strings.TrimSpace(in.Schedule)
	j.SourceStorageID = in.SourceStorageID
	j.SourceBucket = strings.TrimSpace(in.SourceBucket)
	j.SourcePrefix = strings.TrimLeft(strings.TrimSpace(in.SourcePrefix), "/")
	j.DestStorageID = in.DestStorageID
	j.DestBucket = strings.TrimSpace(in.DestBucket)
	j.DestPrefix = strings.Trim(strings.TrimSpace(in.DestPrefix), "/")
	j.Compression = in.Compression
	j.Encryption = in.Encryption
	if in.Passphrase != "" {
		j.Passphrase = in.Passphrase
	}
	if !j.Encryption {
		j.Passphrase = ""
	}
	j.Concurrency = in.Concurrency
	j.KeepLast = in.KeepLast
	j.KeepDays = in.KeepDays

	if j.Name == "" {
		return errors.New("name is required")
	}
	if j.Schedule != "" {
		if _, err := scheduler.Parse(j.Schedule); err != nil {
			return fmt.Errorf("invalid schedule: %v", err)
		}
	}
	// An empty source bucket means the whole storage (every bucket).
	if _, err := s.store.GetStorage(j.SourceStorageID); err != nil {
		return errors.New("source storage not found")
	}
	dst, err := s.store.GetStorage(j.DestStorageID)
	if err != nil {
		return errors.New("destination storage not found")
	}
	if dst.Type == "s3" && j.DestBucket == "" {
		return errors.New("destination bucket is required")
	}
	// A repository inside the source is skipped during backup (see
	// engine.RepoExclusion), but it needs its own folder to be separable.
	if j.SourceStorageID == j.DestStorageID && j.SourceBucket == j.DestBucket && j.DestPrefix == "" {
		return errors.New("the destination is the backed-up location itself; set a destination folder (e.g. s3sync)")
	}
	if j.Encryption && len(j.Passphrase) < 8 {
		return errors.New("encryption passphrase must be at least 8 characters")
	}
	if j.Concurrency < 1 || j.Concurrency > 64 {
		return errors.New("concurrency must be between 1 and 64")
	}
	if j.KeepLast < 0 || j.KeepDays < 0 {
		return errors.New("retention values cannot be negative")
	}
	return nil
}

func (s *Server) jobViews(jobs []*store.Job) ([]jobView, error) {
	lastRuns, err := s.store.LatestBackupRuns()
	if err != nil {
		return nil, err
	}
	successes, err := s.store.LastSuccessfulBackups()
	if err != nil {
		return nil, err
	}
	snaps, err := s.store.SnapshotSummaries()
	if err != nil {
		return nil, err
	}
	out := make([]jobView, len(jobs))
	for i, j := range jobs {
		v := jobView{
			Job:           j,
			HasPassphrase: j.Passphrase != "",
			NextRun:       s.sched.Next(j.ID),
			LastRun:       lastRuns[j.ID],
			ActiveRunID:   s.eng.ActiveRun(j.ID),
			Snapshots:     snaps[j.ID],
		}
		if t, ok := successes[j.ID]; ok {
			v.LastSuccess = &t
		}
		if v.Snapshots == nil {
			v.Snapshots = &store.SnapshotSummary{}
		}
		v.Health = health(j, v.LastRun, v.LastSuccess, v.ActiveRunID != 0)
		out[i] = v
	}
	return out, nil
}

func health(j *store.Job, last *store.Run, lastSuccess *time.Time, running bool) string {
	switch {
	case running && last != nil && last.Status == store.StatusRunning:
		return HealthRunning
	case !j.Enabled:
		return HealthDisabled
	case last == nil:
		return HealthNever
	case last.Status == store.StatusFailed || last.Status == store.StatusCancelled:
		return HealthFailing
	}
	if j.Schedule != "" && lastSuccess != nil {
		if iv := scheduler.Interval(j.Schedule); iv > 0 && time.Since(*lastSuccess) > 2*iv+time.Hour {
			return HealthStale
		}
	}
	if last.Status == store.StatusWarning {
		return HealthWarning
	}
	return HealthHealthy
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, views)
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	j, err := s.store.GetJob(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	views, err := s.jobViews([]*store.Job{j})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, views[0])
}

func (s *Server) createJob(w http.ResponseWriter, r *http.Request) {
	var in jobInput
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	j := &store.Job{}
	if err := s.applyJob(&in, j); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.CreateJob(j); err != nil {
		writeErr(w, err)
		return
	}
	s.sched.Reload()
	writeJSON(w, http.StatusCreated, j)
}

func (s *Server) updateJob(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	j, err := s.store.GetJob(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	var in jobInput
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.applyJob(&in, j); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.UpdateJob(j); err != nil {
		writeErr(w, err)
		return
	}
	s.sched.Reload()
	writeJSON(w, http.StatusOK, j)
}

func (s *Server) deleteJob(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if s.eng.ActiveRun(id) != 0 {
		writeError(w, http.StatusConflict, "job has an active run; cancel it first")
		return
	}
	if err := s.store.DeleteJob(id); err != nil {
		writeErr(w, err)
		return
	}
	s.sched.Reload()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) runJob(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	runID, err := s.eng.StartBackup(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]int64{"run_id": runID})
}

func (s *Server) scanJob(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if _, err := s.store.GetJob(id); err != nil {
		writeErr(w, err)
		return
	}
	runID, err := s.eng.StartScan(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]int64{"run_id": runID})
}

func (s *Server) listSnapshots(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	list, err := s.store.ListSnapshots(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) browseSnapshot(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	ctx, cancel := timeoutCtx(r, 2*time.Minute)
	defer cancel()
	l, err := s.eng.Browse(ctx, id, r.PathValue("sid"), r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (s *Server) restoreSnapshot(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req engine.RestoreRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Prefix = strings.TrimLeft(strings.TrimSpace(req.Prefix), "/")
	req.Bucket = strings.TrimSpace(req.Bucket)
	runID, err := s.eng.StartRestore(id, r.PathValue("sid"), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]int64{"run_id": runID})
}

func (s *Server) deleteSnapshot(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	runID, err := s.eng.StartDeleteSnapshot(id, r.PathValue("sid"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]int64{"run_id": runID})
}

type quickBackupInput struct {
	SourceStorageID int64  `json:"source_storage_id"`
	SourceBucket    string `json:"source_bucket"`
	SourcePrefix    string `json:"source_prefix"`
	DestStorageID   int64  `json:"dest_storage_id"`
	DestBucket      string `json:"dest_bucket"`
	DestPrefix      string `json:"dest_prefix"`
	Encryption      bool   `json:"encryption"`
	Passphrase      string `json:"passphrase"`
}

// quickBackup takes a snapshot right away. Snapshots always belong to a job
// (that is where they are listed and restored from), so it reuses the manual
// job for the same source and destination, or creates one without a schedule.
func (s *Server) quickBackup(w http.ResponseWriter, r *http.Request) {
	var in quickBackupInput
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	src, err := s.store.GetStorage(in.SourceStorageID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "source storage not found")
		return
	}
	name := src.Name + " · "
	switch {
	case in.SourceBucket == "" && src.Type == "s3":
		name += "all buckets"
	case in.SourceBucket == "":
		name += "entire directory"
	default:
		name += in.SourceBucket
	}
	if p := strings.Trim(in.SourcePrefix, "/"); p != "" {
		name += "/" + p
	}
	j := &store.Job{}
	err = s.applyJob(&jobInput{
		Name: name, Enabled: true,
		SourceStorageID: in.SourceStorageID, SourceBucket: in.SourceBucket, SourcePrefix: in.SourcePrefix,
		DestStorageID: in.DestStorageID, DestBucket: in.DestBucket, DestPrefix: in.DestPrefix,
		Compression: true, Encryption: in.Encryption, Passphrase: in.Passphrase, Concurrency: 8,
	}, j)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	jobs, err := s.store.ListJobs()
	if err != nil {
		writeErr(w, err)
		return
	}
	existing := false
	for _, o := range jobs {
		if o.SourceStorageID == j.SourceStorageID && o.SourceBucket == j.SourceBucket && o.SourcePrefix == j.SourcePrefix &&
			o.DestStorageID == j.DestStorageID && o.DestBucket == j.DestBucket && o.DestPrefix == j.DestPrefix {
			j, existing = o, true
			break
		}
	}
	if !existing {
		if err := s.store.CreateJob(j); err != nil {
			writeErr(w, err)
			return
		}
	}
	runID, err := s.eng.StartBackup(j.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": j.ID, "job_name": j.Name, "run_id": runID, "existing_job": existing})
}
