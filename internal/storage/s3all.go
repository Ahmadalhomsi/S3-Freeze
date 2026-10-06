package storage

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
)

// s3AllBuckets presents every bucket of an S3 service as one key space whose
// first path segment is the bucket name ("bucket/key"). Writing to a bucket
// that does not exist creates it, so whole-storage restores recreate buckets.
type s3AllBuckets struct {
	s *s3Backend

	mu    sync.Mutex
	known map[string]bool
}

func splitBucketKey(key string) (bucket, rest string, err error) {
	i := strings.IndexByte(key, '/')
	if i <= 0 || i == len(key)-1 {
		return "", "", fmt.Errorf("key %q does not start with a bucket name", key)
	}
	return key[:i], key[i+1:], nil
}

func (a *s3AllBuckets) bucket(name string) *s3Bucket {
	return &s3Bucket{client: a.s.client, name: name}
}

func (a *s3AllBuckets) List(ctx context.Context, prefix string, fn func(ObjectInfo) error) error {
	names, err := a.s.ListBuckets(ctx)
	if err != nil {
		return err
	}
	for _, name := range names {
		inner := ""
		switch {
		case prefix == "", strings.HasPrefix(name+"/", prefix):
		case strings.HasPrefix(prefix, name+"/"):
			inner = prefix[len(name)+1:]
		default:
			continue
		}
		err := a.bucket(name).List(ctx, inner, func(o ObjectInfo) error {
			o.Key = name + "/" + o.Key
			return fn(o)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *s3AllBuckets) ListDirs(ctx context.Context, prefix string) ([]string, error) {
	if prefix == "" {
		names, err := a.s.ListBuckets(ctx)
		for i := range names {
			names[i] += "/"
		}
		return names, err
	}
	name, inner, _ := strings.Cut(prefix, "/")
	dirs, err := a.bucket(name).ListDirs(ctx, inner)
	for i := range dirs {
		dirs[i] = name + "/" + dirs[i]
	}
	return dirs, err
}

func (a *s3AllBuckets) Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	name, rest, err := splitBucketKey(key)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	rd, info, err := a.bucket(name).Get(ctx, rest)
	info.Key = key
	return rd, info, err
}

func (a *s3AllBuckets) Put(ctx context.Context, key string, r io.Reader, size int64, opts PutOptions) error {
	name, rest, err := splitBucketKey(key)
	if err != nil {
		return err
	}
	if err := a.ensureBucket(ctx, name); err != nil {
		return err
	}
	return a.bucket(name).Put(ctx, rest, r, size, opts)
}

func (a *s3AllBuckets) ensureBucket(ctx context.Context, name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.known[name] {
		return nil
	}
	if err := a.s.EnsureBucket(ctx, name); err != nil {
		return err
	}
	a.known[name] = true
	return nil
}

func (a *s3AllBuckets) Delete(ctx context.Context, key string) error {
	name, rest, err := splitBucketKey(key)
	if err != nil {
		return err
	}
	return a.bucket(name).Delete(ctx, rest)
}

func (a *s3AllBuckets) Exists(ctx context.Context, key string) (bool, error) {
	name, rest, err := splitBucketKey(key)
	if err != nil {
		return false, err
	}
	return a.bucket(name).Exists(ctx, rest)
}
