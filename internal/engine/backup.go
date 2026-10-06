package engine

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"s3freeze/internal/repo"
	"s3freeze/internal/storage"
	"s3freeze/internal/store"
)

// Objects up to this size are buffered in memory; larger ones are spooled to disk.
const memSpoolLimit = 8 << 20

func (e *Engine) StartBackup(jobID int64) (int64, error) {
	job, err := e.loadJob(jobID)
	if err != nil {
		return 0, err
	}
	src := strings.Trim(path.Join(job.SourceBucket, job.SourcePrefix), "/")
	if src == "" || src == "." {
		src = "(root)"
	}
	detail := "Backup " + src
	return e.startRun(jobID, store.KindBackup, detail, func(ctx context.Context, rc *runCtx) error {
		return e.runBackup(ctx, rc, job)
	})
}

type backupState struct {
	r        *repo.Repo
	src      storage.Bucket
	prev     map[string]repo.Entry
	compress bool

	mu       sync.Mutex
	blobs    map[string]int64
	reused   int64
	deduped  int64
	newBlobs int64
}

func (e *Engine) runBackup(ctx context.Context, rc *runCtx, job *store.Job) error {
	rc.logf("Starting backup of job %q", job.Name)
	src, err := e.openBucket(job.SourceStorageID, job.SourceBucket)
	if err != nil {
		return fmt.Errorf("source: %w", err)
	}

	unlock := e.lockRepo(job)
	defer unlock()

	r, err := e.openRepo(ctx, job)
	if err != nil {
		return fmt.Errorf("destination repository: %w", err)
	}
	rc.logf("Repository %s opened (encrypted: %v)", r.ID, r.Encrypted())

	st := &backupState{r: r, src: src, prev: map[string]repo.Entry{}, compress: job.Compression}
	if last, err := e.store.LatestSnapshot(job.ID); err == nil {
		if m, err := r.LoadManifest(ctx, last.ID); err != nil {
			rc.logf("Warning: cannot load previous snapshot %s, doing a full backup: %v", last.ID, err)
		} else {
			for _, en := range m.Entries {
				st.prev[en.Key] = en
			}
			rc.logf("Previous snapshot %s has %d objects", last.ID, len(m.Entries))
		}
	}

	if st.blobs, err = r.ListBlobs(ctx); err != nil {
		return fmt.Errorf("list repository blobs: %w", err)
	}
	rc.logf("Repository contains %d blobs", len(st.blobs))

	if job.SourceBucket == "" {
		rc.logf("Listing all buckets of the source storage")
	} else {
		rc.logf("Listing source %s/%s", job.SourceBucket, job.SourcePrefix)
	}
	exclude := RepoExclusion(job)
	var objs []storage.ObjectInfo
	var excluded int
	err = src.List(ctx, job.SourcePrefix, func(o storage.ObjectInfo) error {
		if exclude != "" && strings.HasPrefix(o.Key, exclude) {
			excluded++
			return nil
		}
		objs = append(objs, o)
		rc.objectsTotal.Add(1)
		rc.bytesTotal.Add(o.Size)
		return nil
	})
	if err != nil {
		return fmt.Errorf("list source: %w", err)
	}
	rc.logf("Found %d objects (%s)", len(objs), humanBytes(rc.bytesTotal.Load()))
	if excluded > 0 {
		rc.logf("Skipped %d objects of the backup repository itself (%s)", excluded, exclude)
	}

	entries := make([]repo.Entry, len(objs))
	ok := make([]bool, len(objs))
	work := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < max(1, job.Concurrency); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				en, err := e.backupObject(ctx, rc, st, objs[i])
				if err != nil {
					if ctx.Err() == nil {
						rc.objectsFailed.Add(1)
						rc.logf("FAILED %s: %v", objs[i].Key, err)
					}
					continue
				}
				entries[i], ok[i] = en, true
				rc.objectsDone.Add(1)
			}
		}()
	}
	for i := range objs {
		select {
		case work <- i:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
	}
	close(work)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}

	now := time.Now().UTC()
	m := &repo.Manifest{
		ID:           repo.NewSnapshotID(now),
		JobName:      job.Name,
		CreatedAt:    now,
		SourceBucket: job.SourceBucket,
		SourcePrefix: job.SourcePrefix,
		AddedBytes:   rc.bytesUploaded.Load(),
	}
	for i, en := range entries {
		if ok[i] {
			m.Entries = append(m.Entries, en)
			m.Size += en.Size
		}
	}
	m.Objects = int64(len(m.Entries))
	if len(objs) > 0 && m.Objects == 0 {
		return errors.New("no objects could be backed up")
	}
	if err := r.SaveManifest(ctx, m); err != nil {
		return fmt.Errorf("save snapshot manifest: %w", err)
	}
	if _, err := e.store.InsertSnapshot(&store.Snapshot{
		ID: m.ID, JobID: job.ID, RunID: rc.id, CreatedAt: m.CreatedAt, Objects: m.Objects, Size: m.Size, AddedBytes: m.AddedBytes,
	}); err != nil {
		return err
	}
	rc.mu.Lock()
	rc.snapshotID = m.ID
	rc.mu.Unlock()
	rc.logf("Snapshot %s saved: %d objects, %s total; %d unchanged, %d new blobs, %d deduplicated, %s uploaded",
		m.ID, m.Objects, humanBytes(m.Size), st.reused, st.newBlobs, st.deduped, humanBytes(m.AddedBytes))

	return e.applyRetention(ctx, rc, job, r)
}

// RepoExclusion returns the key prefix, in the source's key space, under which
// the job's own repository lives when the repository is inside the source
// (same storage). Such keys are skipped so backups never include themselves.
func RepoExclusion(job *store.Job) string {
	if job.SourceStorageID != job.DestStorageID {
		return ""
	}
	dp := repo.NormalizePrefix(job.DestPrefix)
	switch {
	case job.SourceBucket == job.DestBucket:
		return dp
	case job.SourceBucket == "":
		return job.DestBucket + "/" + dp
	}
	return ""
}

func (e *Engine) backupObject(ctx context.Context, rc *runCtx, st *backupState, o storage.ObjectInfo) (repo.Entry, error) {
	// Unchanged since the previous snapshot: reuse its blob without downloading.
	if p, found := st.prev[o.Key]; found && p.Size == o.Size && p.ETag == o.ETag && p.Mod.Equal(o.LastModified) {
		st.mu.Lock()
		_, exists := st.blobs[p.Blob]
		if exists {
			st.reused++
		}
		st.mu.Unlock()
		if exists {
			rc.bytesDone.Add(o.Size)
			return p, nil
		}
	}

	rd, info, err := st.src.Get(ctx, o.Key)
	if err != nil {
		return repo.Entry{}, err
	}
	defer rd.Close()

	sp, err := newSpool(e.tmpDir, o.Size)
	if err != nil {
		return repo.Entry{}, err
	}
	defer sp.Close()

	h := st.r.NewHasher()
	n, err := io.Copy(io.MultiWriter(h, sp), &progressReader{r: rd, n: &rc.bytesDone})
	if err != nil {
		return repo.Entry{}, fmt.Errorf("download: %w", err)
	}
	id := hex.EncodeToString(h.Sum(nil))

	st.mu.Lock()
	_, exists := st.blobs[id]
	st.mu.Unlock()
	if !exists {
		body, err := sp.Reader()
		if err != nil {
			return repo.Entry{}, err
		}
		stored, err := st.r.PutBlob(ctx, id, body, st.compress)
		if err != nil {
			return repo.Entry{}, fmt.Errorf("upload: %w", err)
		}
		rc.bytesUploaded.Add(stored)
		st.mu.Lock()
		st.blobs[id] = stored
		st.newBlobs++
		st.mu.Unlock()
	} else {
		st.mu.Lock()
		st.deduped++
		st.mu.Unlock()
	}

	return repo.Entry{
		Key:         o.Key,
		Size:        n,
		ETag:        o.ETag,
		Mod:         o.LastModified,
		ContentType: info.ContentType,
		Meta:        info.Metadata,
		Blob:        id,
	}, nil
}

func (e *Engine) applyRetention(ctx context.Context, rc *runCtx, job *store.Job, r *repo.Repo) error {
	if job.KeepLast <= 0 && job.KeepDays <= 0 {
		return nil
	}
	snaps, err := e.store.ListSnapshots(job.ID)
	if err != nil {
		return err
	}
	cutoff := time.Now().AddDate(0, 0, -job.KeepDays)
	var expired []*store.Snapshot
	for i, s := range snaps {
		if i == 0 {
			continue // always keep the newest snapshot
		}
		keep := (job.KeepLast > 0 && i < job.KeepLast) || (job.KeepDays > 0 && s.CreatedAt.After(cutoff))
		if !keep {
			expired = append(expired, s)
		}
	}
	if len(expired) == 0 {
		return nil
	}
	rc.logf("Retention: removing %d expired snapshots", len(expired))
	for _, s := range expired {
		if err := r.DeleteManifest(ctx, s.ID); err != nil {
			return fmt.Errorf("delete snapshot %s: %w", s.ID, err)
		}
		if err := e.store.DeleteSnapshot(job.ID, s.ID); err != nil {
			return err
		}
		e.forgetManifest(job.ID, s.ID)
		rc.logf("Removed snapshot %s", s.ID)
	}
	return e.prune(ctx, rc, r)
}

type progressReader struct {
	r interface{ Read([]byte) (int, error) }
	n interface{ Add(int64) int64 }
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.n.Add(int64(n))
	return n, err
}

// spool holds a downloaded object so it can be hashed first and uploaded after.
type spool struct {
	buf  *bytes.Buffer
	file *os.File
}

func newSpool(dir string, size int64) (*spool, error) {
	if size >= 0 && size <= memSpoolLimit {
		return &spool{buf: bytes.NewBuffer(make([]byte, 0, size))}, nil
	}
	f, err := os.CreateTemp(dir, "spool-*")
	if err != nil {
		return nil, err
	}
	return &spool{file: f}, nil
}

func (s *spool) Write(p []byte) (int, error) {
	if s.file != nil {
		return s.file.Write(p)
	}
	return s.buf.Write(p)
}

func (s *spool) Reader() (io.Reader, error) {
	if s.file != nil {
		if _, err := s.file.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		return s.file, nil
	}
	return bytes.NewReader(s.buf.Bytes()), nil
}

func (s *spool) Close() error {
	if s.file != nil {
		s.file.Close()
		return os.Remove(s.file.Name())
	}
	return nil
}
