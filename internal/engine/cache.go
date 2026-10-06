package engine

import (
	"context"
	"fmt"
	"io"
	"sync"

	"s3sync/internal/repo"
	"s3sync/internal/store"
)

const (
	manifestCacheSize = 4
	repoCacheSize     = 8
)

type cachedManifest struct {
	key string
	m   *repo.Manifest

	once sync.Once
	idx  map[string]int // key -> entry index, built on first lookup
}

type cachedRepo struct {
	key string
	r   *repo.Repo
}

// cachedRepo returns an opened repository for read access, reusing a recent
// one so browsing and previews do not repeat the key derivation each time.
// The cache key includes modification times, so edited jobs or storage
// credentials are picked up immediately.
func (e *Engine) cachedRepo(ctx context.Context, job *store.Job) (*repo.Repo, error) {
	st, err := e.store.GetStorage(job.DestStorageID)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%d|%d|%d", job.ID, job.UpdatedAt.UnixNano(), st.UpdatedAt.UnixNano())
	e.cacheMu.Lock()
	for _, c := range e.repos {
		if c.key == key {
			e.cacheMu.Unlock()
			return c.r, nil
		}
	}
	e.cacheMu.Unlock()

	r, err := e.openRepo(ctx, job)
	if err != nil {
		return nil, err
	}
	e.cacheMu.Lock()
	e.repos = append(e.repos, cachedRepo{key: key, r: r})
	if len(e.repos) > repoCacheSize {
		e.repos = e.repos[1:]
	}
	e.cacheMu.Unlock()
	return r, nil
}

func (e *Engine) cachedManifest(ctx context.Context, jobID int64, snapID string) (*cachedManifest, error) {
	key := fmt.Sprintf("%d/%s", jobID, snapID)
	e.cacheMu.Lock()
	for _, c := range e.manifests {
		if c.key == key {
			e.cacheMu.Unlock()
			return c, nil
		}
	}
	e.cacheMu.Unlock()

	if _, err := e.store.GetSnapshot(jobID, snapID); err != nil {
		return nil, err
	}
	job, err := e.loadJob(jobID)
	if err != nil {
		return nil, err
	}
	r, err := e.cachedRepo(ctx, job)
	if err != nil {
		return nil, err
	}
	m, err := r.LoadManifest(ctx, snapID)
	if err != nil {
		return nil, err
	}
	c := &cachedManifest{key: key, m: m}
	e.cacheMu.Lock()
	e.manifests = append(e.manifests, c)
	if len(e.manifests) > manifestCacheSize {
		e.manifests = e.manifests[1:]
	}
	e.cacheMu.Unlock()
	return c, nil
}

func (e *Engine) manifest(ctx context.Context, jobID int64, snapID string) (*repo.Manifest, error) {
	c, err := e.cachedManifest(ctx, jobID, snapID)
	if err != nil {
		return nil, err
	}
	return c.m, nil
}

func (e *Engine) forgetManifest(jobID int64, snapID string) {
	key := fmt.Sprintf("%d/%s", jobID, snapID)
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	for i, c := range e.manifests {
		if c.key == key {
			e.manifests = append(e.manifests[:i], e.manifests[i+1:]...)
			return
		}
	}
}

// OpenFile returns the content of one object in a snapshot.
func (e *Engine) OpenFile(ctx context.Context, jobID int64, snapID, key string) (io.ReadCloser, repo.Entry, error) {
	c, err := e.cachedManifest(ctx, jobID, snapID)
	if err != nil {
		return nil, repo.Entry{}, err
	}
	c.once.Do(func() {
		c.idx = make(map[string]int, len(c.m.Entries))
		for i, en := range c.m.Entries {
			c.idx[en.Key] = i
		}
	})
	i, ok := c.idx[key]
	if !ok {
		return nil, repo.Entry{}, store.ErrNotFound
	}
	en := c.m.Entries[i]
	job, err := e.loadJob(jobID)
	if err != nil {
		return nil, repo.Entry{}, err
	}
	r, err := e.cachedRepo(ctx, job)
	if err != nil {
		return nil, repo.Entry{}, err
	}
	rd, err := r.OpenBlob(ctx, en.Blob)
	return rd, en, err
}
