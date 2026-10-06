// Package engine executes backup, restore, delete and scan runs.
package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"s3freeze/internal/repo"
	"s3freeze/internal/storage"
	"s3freeze/internal/store"
)

var (
	ErrBusy         = errors.New("another run is already in progress for this job")
	ErrShuttingDown = errors.New("server is shutting down")
)

const maxLogLines = 2000

type Engine struct {
	store  *store.Store
	tmpDir string

	mu        sync.Mutex
	runs      map[int64]*runCtx // active runs by run ID
	jobRuns   map[int64]int64   // job ID -> active run ID
	repoLocks map[string]*sync.Mutex
	closed    bool
	wg        sync.WaitGroup

	cacheMu   sync.Mutex
	manifests []*cachedManifest
	repos     []cachedRepo
}

func New(st *store.Store, tmpDir string) *Engine {
	return &Engine{
		store:     st,
		tmpDir:    tmpDir,
		runs:      map[int64]*runCtx{},
		jobRuns:   map[int64]int64{},
		repoLocks: map[string]*sync.Mutex{},
	}
}

type runCtx struct {
	id     int64
	cancel context.CancelFunc

	objectsTotal, objectsDone, objectsFailed atomic.Int64
	bytesTotal, bytesDone, bytesUploaded     atomic.Int64

	mu         sync.Mutex
	lines      []string
	dropped    int
	snapshotID string
}

func (rc *runCtx) logf(format string, args ...any) {
	line := time.Now().UTC().Format("15:04:05") + "  " + fmt.Sprintf(format, args...)
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if len(rc.lines) >= maxLogLines {
		// Keep the first lines (setup context) and the most recent ones.
		keep := maxLogLines / 4
		rc.lines = append(rc.lines[:keep], rc.lines[keep+1:]...)
		rc.dropped++
	}
	rc.lines = append(rc.lines, line)
}

func (rc *runCtx) logText() string {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if rc.dropped == 0 {
		return strings.Join(rc.lines, "\n")
	}
	keep := maxLogLines / 4
	head := strings.Join(rc.lines[:keep], "\n")
	tail := strings.Join(rc.lines[keep:], "\n")
	return fmt.Sprintf("%s\n... %d lines omitted ...\n%s", head, rc.dropped, tail)
}

func (rc *runCtx) progress() store.Progress {
	return store.Progress{
		ObjectsTotal:  rc.objectsTotal.Load(),
		ObjectsDone:   rc.objectsDone.Load(),
		ObjectsFailed: rc.objectsFailed.Load(),
		BytesTotal:    rc.bytesTotal.Load(),
		BytesDone:     rc.bytesDone.Load(),
		BytesUploaded: rc.bytesUploaded.Load(),
	}
}

// startRun records a new run and executes fn in the background. Only one run
// per job may be active at a time.
func (e *Engine) startRun(jobID int64, kind, detail string, fn func(ctx context.Context, rc *runCtx) error) (int64, error) {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return 0, ErrShuttingDown
	}
	if _, busy := e.jobRuns[jobID]; busy {
		e.mu.Unlock()
		return 0, ErrBusy
	}
	id, err := e.store.CreateRun(jobID, kind, detail)
	if err != nil {
		e.mu.Unlock()
		return 0, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	rc := &runCtx{id: id, cancel: cancel}
	e.runs[id] = rc
	e.jobRuns[jobID] = id
	e.wg.Add(1)
	e.mu.Unlock()

	go func() {
		defer e.wg.Done()
		defer cancel()

		var tick sync.WaitGroup
		done := make(chan struct{})
		tick.Add(1)
		go func() {
			defer tick.Done()
			t := time.NewTicker(2 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-done:
					return
				case <-t.C:
					if err := e.store.UpdateRunProgress(id, rc.progress(), rc.logText()); err != nil {
						slog.Error("update run progress", "run", id, "err", err)
					}
				}
			}
		}()

		start := time.Now()
		err := func() (err error) {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("internal error: %v", r)
				}
			}()
			return fn(ctx, rc)
		}()
		close(done)
		tick.Wait()

		status := store.StatusSuccess
		errMsg := ""
		switch {
		case err != nil && ctx.Err() != nil:
			status, errMsg = store.StatusCancelled, "cancelled"
			rc.logf("Run cancelled")
		case err != nil:
			status, errMsg = store.StatusFailed, err.Error()
			rc.logf("ERROR: %v", err)
		case rc.objectsFailed.Load() > 0:
			status = store.StatusWarning
			errMsg = fmt.Sprintf("%d objects failed", rc.objectsFailed.Load())
		}
		rc.logf("Finished with status %s in %s", status, time.Since(start).Round(time.Millisecond))
		rc.mu.Lock()
		snap := rc.snapshotID
		rc.mu.Unlock()
		if err := e.store.FinishRun(id, status, errMsg, snap, rc.progress(), rc.logText()); err != nil {
			slog.Error("finish run", "run", id, "err", err)
		}
		slog.Info("run finished", "run", id, "kind", kind, "job", jobID, "status", status)

		e.mu.Lock()
		delete(e.runs, id)
		delete(e.jobRuns, jobID)
		e.mu.Unlock()
	}()
	return id, nil
}

// Cancel stops an active run. It returns false if the run is not active.
func (e *Engine) Cancel(runID int64) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	rc, ok := e.runs[runID]
	if ok {
		rc.cancel()
	}
	return ok
}

// ActiveRun returns the active run ID for a job, or 0.
func (e *Engine) ActiveRun(jobID int64) int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.jobRuns[jobID]
}

// Shutdown cancels all active runs and waits for them to finish recording.
func (e *Engine) Shutdown(timeout time.Duration) {
	e.mu.Lock()
	e.closed = true
	for _, rc := range e.runs {
		rc.cancel()
	}
	e.mu.Unlock()
	done := make(chan struct{})
	go func() { e.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(timeout):
		slog.Warn("timed out waiting for runs to stop")
	}
}

func (e *Engine) lockRepo(job *store.Job) func() {
	key := fmt.Sprintf("%d|%s|%s", job.DestStorageID, job.DestBucket, repo.NormalizePrefix(job.DestPrefix))
	e.mu.Lock()
	l, ok := e.repoLocks[key]
	if !ok {
		l = &sync.Mutex{}
		e.repoLocks[key] = l
	}
	e.mu.Unlock()
	l.Lock()
	return l.Unlock
}

func (e *Engine) openBucket(storageID int64, bucket string) (storage.Bucket, error) {
	st, err := e.store.GetStorage(storageID)
	if err != nil {
		return nil, fmt.Errorf("load storage %d: %w", storageID, err)
	}
	be, err := storage.New(st.Config())
	if err != nil {
		return nil, fmt.Errorf("storage %q: %w", st.Name, err)
	}
	return be.Bucket(bucket)
}

func (e *Engine) openRepo(ctx context.Context, job *store.Job) (*repo.Repo, error) {
	b, err := e.openBucket(job.DestStorageID, job.DestBucket)
	if err != nil {
		return nil, err
	}
	return repo.Open(ctx, b, job.DestPrefix, repo.Options{Encrypt: job.Encryption, Passphrase: job.Passphrase})
}

func (e *Engine) loadJob(jobID int64) (*store.Job, error) {
	job, err := e.store.GetJob(jobID)
	if err != nil {
		return nil, fmt.Errorf("load job: %w", err)
	}
	return job, nil
}

// StartDeleteSnapshot removes a snapshot and prunes data no longer referenced.
func (e *Engine) StartDeleteSnapshot(jobID int64, snapID string) (int64, error) {
	if _, err := e.store.GetSnapshot(jobID, snapID); err != nil {
		return 0, err
	}
	return e.startRun(jobID, store.KindDelete, "Delete snapshot "+snapID, func(ctx context.Context, rc *runCtx) error {
		job, err := e.loadJob(jobID)
		if err != nil {
			return err
		}
		unlock := e.lockRepo(job)
		defer unlock()
		r, err := e.openRepo(ctx, job)
		if err != nil {
			return err
		}
		if err := r.DeleteManifest(ctx, snapID); err != nil {
			return fmt.Errorf("delete snapshot manifest: %w", err)
		}
		if err := e.store.DeleteSnapshot(jobID, snapID); err != nil {
			return err
		}
		e.forgetManifest(jobID, snapID)
		rc.logf("Deleted snapshot %s", snapID)
		return e.prune(ctx, rc, r)
	})
}

// StartScan imports snapshots found in the destination repository that are
// not yet known to the database (e.g. after losing the app's data volume).
func (e *Engine) StartScan(jobID int64) (int64, error) {
	return e.startRun(jobID, store.KindScan, "Scan repository for snapshots", func(ctx context.Context, rc *runCtx) error {
		job, err := e.loadJob(jobID)
		if err != nil {
			return err
		}
		unlock := e.lockRepo(job)
		defer unlock()
		r, err := e.openRepo(ctx, job)
		if err != nil {
			return err
		}
		ids, err := r.ListManifests(ctx)
		if err != nil {
			return err
		}
		rc.logf("Found %d snapshots in repository", len(ids))
		rc.objectsTotal.Store(int64(len(ids)))
		imported := 0
		for _, id := range ids {
			if _, err := e.store.GetSnapshot(jobID, id); err == nil {
				rc.objectsDone.Add(1)
				continue
			}
			m, err := r.LoadManifest(ctx, id)
			if err != nil {
				rc.objectsFailed.Add(1)
				rc.logf("Cannot read snapshot %s: %v", id, err)
				continue
			}
			ok, err := e.store.InsertSnapshot(&store.Snapshot{
				ID: m.ID, JobID: jobID, CreatedAt: m.CreatedAt, Objects: m.Objects, Size: m.Size, AddedBytes: m.AddedBytes,
			})
			if err != nil {
				return err
			}
			if ok {
				imported++
				rc.logf("Imported snapshot %s (%d objects, from job %q)", m.ID, m.Objects, m.JobName)
			}
			rc.objectsDone.Add(1)
		}
		rc.logf("Imported %d new snapshots", imported)
		return nil
	})
}

func (e *Engine) prune(ctx context.Context, rc *runCtx, r *repo.Repo) error {
	rc.logf("Pruning unreferenced data")
	deleted, freed, err := r.Prune(ctx, rc.logf)
	if err != nil {
		return err
	}
	rc.logf("Prune removed %d blobs, freed %s", deleted, humanBytes(freed))
	return nil
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// ensureBucket creates the bucket on a storage if it does not exist yet, so
// backups and restores can target new buckets.
func (e *Engine) ensureBucket(ctx context.Context, storageID int64, bucket string) error {
	if bucket == "" {
		return nil
	}
	st, err := e.store.GetStorage(storageID)
	if err != nil {
		return err
	}
	be, err := storage.New(st.Config())
	if err != nil {
		return err
	}
	return be.EnsureBucket(ctx, bucket)
}
