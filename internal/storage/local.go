package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const tmpPrefix = ".s3freeze-tmp-"

// localBackend stores objects as files under a root directory. "Buckets" are
// first-level subdirectories; an empty bucket name means the root itself.
type localBackend struct {
	root string
}

func newLocal(c Config) (Backend, error) {
	if strings.TrimSpace(c.LocalPath) == "" {
		return nil, errors.New("path is required for local storage")
	}
	return &localBackend{root: filepath.Clean(c.LocalPath)}, nil
}

func (l *localBackend) Test(ctx context.Context) error {
	if err := os.MkdirAll(l.root, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(l.root, tmpPrefix)
	if err != nil {
		return fmt.Errorf("directory is not writable: %w", err)
	}
	f.Close()
	return os.Remove(f.Name())
}

func (l *localBackend) ListBuckets(ctx context.Context) ([]string, error) {
	entries, err := os.ReadDir(l.root)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

func (l *localBackend) Bucket(name string) (Bucket, error) {
	if name == "" {
		return &localBucket{dir: l.root}, nil
	}
	if strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return nil, fmt.Errorf("invalid bucket name %q", name)
	}
	return &localBucket{dir: filepath.Join(l.root, name)}, nil
}

type localBucket struct {
	dir string
}

func (b *localBucket) path(key string) (string, error) {
	clean := path.Clean("/" + key)
	if clean == "/" {
		return "", fmt.Errorf("invalid key %q", key)
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == ".." {
			return "", fmt.Errorf("invalid key %q", key)
		}
	}
	return filepath.Join(b.dir, filepath.FromSlash(clean[1:])), nil
}

func (b *localBucket) List(ctx context.Context, prefix string, fn func(ObjectInfo) error) error {
	// Start walking from the deepest directory implied by the prefix.
	start := b.dir
	if i := strings.LastIndex(prefix, "/"); i > 0 {
		start = filepath.Join(b.dir, filepath.FromSlash(prefix[:i]))
	}
	err := filepath.WalkDir(start, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() || strings.HasPrefix(d.Name(), tmpPrefix) {
			return nil
		}
		rel, err := filepath.Rel(b.dir, p)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if !strings.HasPrefix(key, prefix) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return fn(ObjectInfo{Key: key, Size: info.Size(), LastModified: info.ModTime()})
	})
	return err
}

func (b *localBucket) ListDirs(ctx context.Context, prefix string) ([]string, error) {
	dir := b.dir
	if prefix != "" {
		p, err := b.path(prefix)
		if err != nil {
			return nil, err
		}
		dir = p
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			dirs = append(dirs, prefix+e.Name()+"/")
			if len(dirs) >= maxDirs {
				break
			}
		}
	}
	return dirs, nil
}

func (b *localBucket) Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	p, err := b.path(key)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	f, err := os.Open(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ObjectInfo{}, ErrNotFound
		}
		return nil, ObjectInfo{}, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, ObjectInfo{}, err
	}
	return f, ObjectInfo{
		Key:          key,
		Size:         st.Size(),
		LastModified: st.ModTime(),
		ContentType:  mime.TypeByExtension(path.Ext(key)),
	}, nil
}

func (b *localBucket) Put(ctx context.Context, key string, r io.Reader, size int64, opts PutOptions) error {
	p, err := b.path(key)
	if err != nil {
		return err
	}
	if strings.HasSuffix(key, "/") {
		return os.MkdirAll(p, 0o755) // folder marker
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), tmpPrefix)
	if err != nil {
		return err
	}
	tmp := f.Name()
	n, err := io.Copy(f, &ctxReader{ctx: ctx, r: r})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && size >= 0 && n != size {
		err = fmt.Errorf("short write: got %d bytes, expected %d", n, size)
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, p)
}

func (b *localBucket) Delete(ctx context.Context, key string) error {
	p, err := b.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	// Remove now-empty parent directories, stopping at the bucket root.
	for dir := filepath.Dir(p); dir != b.dir && strings.HasPrefix(dir, b.dir); dir = filepath.Dir(dir) {
		if os.Remove(dir) != nil {
			break
		}
	}
	return nil
}

func (b *localBucket) Exists(ctx context.Context, key string) (bool, error) {
	p, err := b.path(key)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(p)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// EnsureBucket creates the bucket's directory.
func (l *localBackend) EnsureBucket(ctx context.Context, name string) error {
	b, err := l.Bucket(name)
	if err != nil {
		return err
	}
	return os.MkdirAll(b.(*localBucket).dir, 0o755)
}
