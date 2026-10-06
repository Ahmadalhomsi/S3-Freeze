package engine

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"s3freeze/internal/repo"
	"s3freeze/internal/storage"
	"s3freeze/internal/store"
)

type RestoreRequest struct {
	// Paths selects objects to restore: exact keys, or folder prefixes ending
	// in "/". Empty means the whole snapshot.
	Paths     []string `json:"paths"`
	StorageID int64    `json:"storage_id"`
	Bucket    string   `json:"bucket"`
	// Prefix is prepended to every restored key.
	Prefix string `json:"prefix"`
	// StripPrefix removes a leading part of every key before Prefix is added.
	StripPrefix string `json:"strip_prefix"`
	Overwrite   bool   `json:"overwrite"`
}

func (e *Engine) StartRestore(jobID int64, snapID string, req RestoreRequest) (int64, error) {
	if _, err := e.store.GetSnapshot(jobID, snapID); err != nil {
		return 0, err
	}
	target, err := e.store.GetStorage(req.StorageID)
	if err != nil {
		return 0, fmt.Errorf("target storage: %w", err)
	}
	job, err := e.loadJob(jobID)
	if err != nil {
		return 0, err
	}
	// Restoring to an S3 storage without a bucket recreates the original
	// buckets, which only makes sense for whole-storage snapshots.
	if target.Type == "s3" && req.Bucket == "" && job.SourceBucket != "" {
		return 0, errors.New("choose a target bucket")
	}
	if target.Type == "s3" && req.Bucket != "" {
		if err := storage.ValidBucketName(req.Bucket); err != nil {
			return 0, err
		}
	}
	detail := fmt.Sprintf("Restore %s to %s/%s", snapID, req.Bucket, req.Prefix)
	return e.startRun(jobID, store.KindRestore, detail, func(ctx context.Context, rc *runCtx) error {
		return e.runRestore(ctx, rc, jobID, snapID, req)
	})
}

func selectEntries(entries []repo.Entry, paths []string) []repo.Entry {
	if len(paths) == 0 {
		return entries
	}
	var out []repo.Entry
	for _, en := range entries {
		for _, p := range paths {
			if en.Key == p || (strings.HasSuffix(p, "/") && strings.HasPrefix(en.Key, p)) {
				out = append(out, en)
				break
			}
		}
	}
	return out
}

func (e *Engine) runRestore(ctx context.Context, rc *runCtx, jobID int64, snapID string, req RestoreRequest) error {
	job, err := e.loadJob(jobID)
	if err != nil {
		return err
	}
	if err := e.ensureBucket(ctx, req.StorageID, req.Bucket); err != nil {
		return fmt.Errorf("target: %w", err)
	}
	dst, err := e.openBucket(req.StorageID, req.Bucket)
	if err != nil {
		return fmt.Errorf("target: %w", err)
	}

	// Hold the repository lock so a concurrent prune cannot remove blobs mid-restore.
	unlock := e.lockRepo(job)
	defer unlock()

	r, err := e.openRepo(ctx, job)
	if err != nil {
		return fmt.Errorf("repository: %w", err)
	}
	m, err := r.LoadManifest(ctx, snapID)
	if err != nil {
		return err
	}
	entries := selectEntries(m.Entries, req.Paths)
	for _, en := range entries {
		rc.objectsTotal.Add(1)
		rc.bytesTotal.Add(en.Size)
	}
	rc.logf("Restoring %d objects (%s) from snapshot %s to %s/%s",
		len(entries), humanBytes(rc.bytesTotal.Load()), snapID, req.Bucket, req.Prefix)

	var skipped int64
	var mu sync.Mutex
	work := make(chan repo.Entry)
	var wg sync.WaitGroup
	for w := 0; w < max(1, job.Concurrency); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for en := range work {
				target := req.Prefix + strings.TrimPrefix(en.Key, req.StripPrefix)
				if !req.Overwrite {
					exists, err := dst.Exists(ctx, target)
					if err == nil && exists {
						mu.Lock()
						skipped++
						mu.Unlock()
						rc.bytesDone.Add(en.Size)
						rc.objectsDone.Add(1)
						continue
					}
				}
				if err := restoreObject(ctx, rc, r, dst, en, target); err != nil {
					if ctx.Err() == nil {
						rc.objectsFailed.Add(1)
						rc.logf("FAILED %s: %v", en.Key, err)
					}
					continue
				}
				rc.objectsDone.Add(1)
			}
		}()
	}
	for _, en := range entries {
		select {
		case work <- en:
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
	if skipped > 0 {
		rc.logf("Skipped %d objects that already exist at the target", skipped)
	}
	if n := rc.objectsFailed.Load(); n > 0 && n == int64(len(entries)) {
		return errors.New("no objects could be restored")
	}
	return nil
}

func restoreObject(ctx context.Context, rc *runCtx, r *repo.Repo, dst storage.Bucket, en repo.Entry, target string) error {
	rd, err := r.OpenBlob(ctx, en.Blob)
	if err != nil {
		return err
	}
	defer rd.Close()
	h := r.NewHasher()
	body := io.TeeReader(&progressReader{r: rd, n: &rc.bytesDone}, h)
	if err := dst.Put(ctx, target, body, en.Size, storage.PutOptions{ContentType: en.ContentType, Metadata: en.Meta}); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != en.Blob {
		return fmt.Errorf("integrity check failed: content hash mismatch")
	}
	return nil
}

type DirEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Objects int64  `json:"objects"`
	Size    int64  `json:"size"`
}

type FileEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	Modified    string `json:"modified"`
	ContentType string `json:"content_type"`
}

type Listing struct {
	Path  string      `json:"path"`
	Dirs  []DirEntry  `json:"dirs"`
	Files []FileEntry `json:"files"`
}

// Browse lists the folder at dir inside a snapshot, S3-style ("/" delimited).
func (e *Engine) Browse(ctx context.Context, jobID int64, snapID, dir string) (*Listing, error) {
	m, err := e.manifest(ctx, jobID, snapID)
	if err != nil {
		return nil, err
	}
	if dir != "" && !strings.HasSuffix(dir, "/") {
		dir += "/"
	}
	l := &Listing{Path: dir, Dirs: []DirEntry{}, Files: []FileEntry{}}
	dirs := map[string]*DirEntry{}
	for _, en := range m.Entries {
		if !strings.HasPrefix(en.Key, dir) {
			continue
		}
		rest := en.Key[len(dir):]
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			name := rest[:i]
			d, ok := dirs[name]
			if !ok {
				d = &DirEntry{Name: name, Path: dir + name + "/"}
				dirs[name] = d
			}
			d.Objects++
			d.Size += en.Size
		} else if rest != "" {
			l.Files = append(l.Files, FileEntry{
				Name: rest, Path: en.Key, Size: en.Size, ContentType: en.ContentType,
				Modified: en.Mod.UTC().Format("2006-01-02T15:04:05Z"),
			})
		}
	}
	for _, d := range dirs {
		l.Dirs = append(l.Dirs, *d)
	}
	sort.Slice(l.Dirs, func(i, j int) bool { return l.Dirs[i].Name < l.Dirs[j].Name })
	sort.Slice(l.Files, func(i, j int) bool { return l.Files[i].Name < l.Files[j].Name })
	return l, nil
}
