// Package repo implements the on-storage backup repository format.
//
// Layout under the repository prefix:
//
//	config.json             repository ID and (optional) wrapped encryption keys
//	data/<xx>/<blob id>     object contents, content-addressed and deduplicated
//	snapshots/<snapshot id> manifest listing every object of one backup run
//
// Every blob and manifest starts with a 6-byte header ("S3SB", version, flags)
// followed by the payload, optionally zstd-compressed and then encrypted.
package repo

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"path"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/klauspost/compress/zstd"

	"s3freeze/internal/storage"
)

const (
	magic         = "S3SB"
	formatVersion = 1
	flagZstd      = 1 << 0
	flagEncrypted = 1 << 1
	headerSize    = 6
)

type Options struct {
	Encrypt    bool
	Passphrase string
}

type Repo struct {
	b      storage.Bucket
	prefix string
	keys   *keys
	ID     string
}

type repoConfig struct {
	Version    int        `json:"version"`
	ID         string     `json:"id"`
	CreatedAt  time.Time  `json:"created_at"`
	Encryption *encConfig `json:"encryption,omitempty"`
}

type Entry struct {
	Key         string            `json:"k"`
	Size        int64             `json:"s"`
	ETag        string            `json:"e,omitempty"`
	Mod         time.Time         `json:"m"`
	ContentType string            `json:"t,omitempty"`
	Meta        map[string]string `json:"x,omitempty"`
	Blob        string            `json:"b"`
}

type Manifest struct {
	Version      int       `json:"version"`
	ID           string    `json:"id"`
	JobName      string    `json:"job_name"`
	CreatedAt    time.Time `json:"created_at"`
	SourceBucket string    `json:"source_bucket"`
	SourcePrefix string    `json:"source_prefix"`
	Objects      int64     `json:"objects"`
	Size         int64     `json:"size"`
	AddedBytes   int64     `json:"added_bytes"`
	Entries      []Entry   `json:"entries"`
}

// NormalizePrefix returns "" or a prefix ending in exactly one "/".
func NormalizePrefix(p string) string {
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	return p + "/"
}

func NewSnapshotID(t time.Time) string {
	b := make([]byte, 4)
	rand.Read(b)
	return t.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(b)
}

// Open opens the repository at prefix, initializing it if it does not exist.
func Open(ctx context.Context, b storage.Bucket, prefix string, opts Options) (*Repo, error) {
	r := &Repo{b: b, prefix: NormalizePrefix(prefix)}
	cfg, err := r.readConfig(ctx)
	if errors.Is(err, storage.ErrNotFound) {
		return r, r.initialize(ctx, opts)
	}
	if err != nil {
		return nil, fmt.Errorf("read repository config: %w", err)
	}
	if cfg.Version != formatVersion {
		return nil, fmt.Errorf("unsupported repository version %d", cfg.Version)
	}
	r.ID = cfg.ID
	if cfg.Encryption == nil {
		if opts.Encrypt {
			return nil, errors.New("the repository at this location is not encrypted, but encryption is enabled for this job; use a different destination prefix")
		}
		return r, nil
	}
	if opts.Passphrase == "" {
		return nil, errors.New("the repository at this location is encrypted; a passphrase is required")
	}
	if r.keys, err = cfg.Encryption.unwrap(opts.Passphrase); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Repo) Encrypted() bool { return r.keys != nil }

func (r *Repo) readConfig(ctx context.Context) (*repoConfig, error) {
	rd, _, err := r.b.Get(ctx, r.prefix+"config.json")
	if err != nil {
		return nil, err
	}
	defer rd.Close()
	var cfg repoConfig
	if err := json.NewDecoder(rd).Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (r *Repo) initialize(ctx context.Context, opts Options) error {
	id := make([]byte, 16)
	rand.Read(id)
	cfg := repoConfig{Version: formatVersion, ID: hex.EncodeToString(id), CreatedAt: time.Now().UTC()}
	if opts.Encrypt {
		if opts.Passphrase == "" {
			return errors.New("a passphrase is required to create an encrypted repository")
		}
		k, err := newKeys()
		if err != nil {
			return err
		}
		if cfg.Encryption, err = wrapKeys(k, opts.Passphrase); err != nil {
			return err
		}
		r.keys = k
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	r.ID = cfg.ID
	return r.b.Put(ctx, r.prefix+"config.json", bytes.NewReader(data), int64(len(data)), storage.PutOptions{ContentType: "application/json"})
}

// NewHasher returns the hash used to derive blob IDs from plaintext content.
// Encrypted repositories use a keyed HMAC so IDs do not reveal content hashes.
func (r *Repo) NewHasher() hash.Hash {
	if r.keys != nil {
		return hmac.New(sha256.New, r.keys.mac[:])
	}
	return sha256.New()
}

func (r *Repo) blobKey(id string) string {
	return r.prefix + "data/" + id[:2] + "/" + id
}

func (r *Repo) manifestKey(id string) string {
	return r.prefix + "snapshots/" + id
}

func validID(id string) bool {
	if len(id) != 64 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

// ListBlobs returns all blob IDs with their stored sizes.
func (r *Repo) ListBlobs(ctx context.Context) (map[string]int64, error) {
	blobs := make(map[string]int64)
	err := r.b.List(ctx, r.prefix+"data/", func(o storage.ObjectInfo) error {
		if id := path.Base(o.Key); validID(id) {
			blobs[id] = o.Size
		}
		return nil
	})
	return blobs, err
}

// PutBlob stores the content read from src under id and returns the stored size.
func (r *Repo) PutBlob(ctx context.Context, id string, src io.Reader, compress bool) (int64, error) {
	if !validID(id) {
		return 0, fmt.Errorf("invalid blob id %q", id)
	}
	return r.putEncoded(ctx, r.blobKey(id), compress, func(w io.Writer) error {
		_, err := io.Copy(w, src)
		return err
	})
}

// OpenBlob returns a reader over the decoded (plaintext) content of a blob.
func (r *Repo) OpenBlob(ctx context.Context, id string) (io.ReadCloser, error) {
	if !validID(id) {
		return nil, fmt.Errorf("invalid blob id %q", id)
	}
	return r.openEncoded(ctx, r.blobKey(id))
}

func (r *Repo) DeleteBlob(ctx context.Context, id string) error {
	return r.b.Delete(ctx, r.blobKey(id))
}

func (r *Repo) SaveManifest(ctx context.Context, m *Manifest) error {
	m.Version = formatVersion
	_, err := r.putEncoded(ctx, r.manifestKey(m.ID), true, func(w io.Writer) error {
		return json.NewEncoder(w).Encode(m)
	})
	return err
}

func (r *Repo) LoadManifest(ctx context.Context, id string) (*Manifest, error) {
	rd, err := r.openEncoded(ctx, r.manifestKey(id))
	if err != nil {
		return nil, err
	}
	defer rd.Close()
	var m Manifest
	if err := json.NewDecoder(rd).Decode(&m); err != nil {
		return nil, fmt.Errorf("decode manifest %s: %w", id, err)
	}
	return &m, nil
}

func (r *Repo) ListManifests(ctx context.Context) ([]string, error) {
	var ids []string
	err := r.b.List(ctx, r.prefix+"snapshots/", func(o storage.ObjectInfo) error {
		ids = append(ids, path.Base(o.Key))
		return nil
	})
	sort.Strings(ids)
	return ids, err
}

func (r *Repo) DeleteManifest(ctx context.Context, id string) error {
	return r.b.Delete(ctx, r.manifestKey(id))
}

// Prune deletes blobs no longer referenced by any manifest in the repository.
// Callers must ensure no backup is writing to the repository concurrently.
func (r *Repo) Prune(ctx context.Context, logf func(string, ...any)) (deleted int, freed int64, err error) {
	ids, err := r.ListManifests(ctx)
	if err != nil {
		return 0, 0, err
	}
	refs := make(map[string]struct{})
	for _, id := range ids {
		m, err := r.LoadManifest(ctx, id)
		if err != nil {
			// Never delete data when the set of referenced blobs is unknown.
			return 0, 0, fmt.Errorf("prune aborted, cannot read snapshot %s: %w", id, err)
		}
		for _, e := range m.Entries {
			refs[e.Blob] = struct{}{}
		}
	}
	blobs, err := r.ListBlobs(ctx)
	if err != nil {
		return 0, 0, err
	}
	logf("Prune: %d snapshots reference %d of %d blobs", len(ids), len(refs), len(blobs))
	for id, size := range blobs {
		if _, ok := refs[id]; ok {
			continue
		}
		if err := r.DeleteBlob(ctx, id); err != nil {
			return deleted, freed, fmt.Errorf("delete blob %s: %w", id, err)
		}
		deleted++
		freed += size
	}
	return deleted, freed, nil
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

type chainCloser struct {
	io.Writer
	closers []io.Closer
}

func (c *chainCloser) Close() error {
	for _, cl := range c.closers {
		if err := cl.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repo) encoder(dst io.Writer, compress bool) (io.WriteCloser, error) {
	var flags byte
	if compress {
		flags |= flagZstd
	}
	if r.keys != nil {
		flags |= flagEncrypted
	}
	if _, err := dst.Write([]byte{magic[0], magic[1], magic[2], magic[3], formatVersion, flags}); err != nil {
		return nil, err
	}
	var w io.WriteCloser = nopWriteCloser{dst}
	if r.keys != nil {
		ew, err := newEncWriter(dst, r.keys.enc[:])
		if err != nil {
			return nil, err
		}
		w = ew
	}
	if compress {
		zw, err := zstd.NewWriter(w, zstd.WithEncoderLevel(zstd.SpeedFastest), zstd.WithEncoderConcurrency(1))
		if err != nil {
			return nil, err
		}
		w = &chainCloser{Writer: zw, closers: []io.Closer{zw, w}}
	}
	return w, nil
}

type countWriter struct {
	w io.Writer
	n atomic.Int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n.Add(int64(n))
	return n, err
}

func (r *Repo) putEncoded(ctx context.Context, key string, compress bool, write func(io.Writer) error) (int64, error) {
	pr, pw := io.Pipe()
	cw := &countWriter{w: pw}
	go func() {
		enc, err := r.encoder(cw, compress)
		if err == nil {
			err = write(enc)
			if cerr := enc.Close(); err == nil {
				err = cerr
			}
		}
		pw.CloseWithError(err)
	}()
	err := r.b.Put(ctx, key, pr, -1, storage.PutOptions{ContentType: "application/octet-stream"})
	pr.CloseWithError(err) // unblock the writer if Put returned early
	return cw.n.Load(), err
}

type readCloser struct {
	io.Reader
	close func() error
}

func (r readCloser) Close() error { return r.close() }

func (r *Repo) openEncoded(ctx context.Context, key string) (io.ReadCloser, error) {
	src, _, err := r.b.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	hdr := make([]byte, headerSize)
	if _, err := io.ReadFull(src, hdr); err != nil {
		src.Close()
		return nil, fmt.Errorf("read header of %s: %w", key, err)
	}
	if string(hdr[:4]) != magic || hdr[4] != formatVersion {
		src.Close()
		return nil, fmt.Errorf("%s: not a valid repository object", key)
	}
	flags := hdr[5]
	var rd io.Reader = src
	if flags&flagEncrypted != 0 {
		if r.keys == nil {
			src.Close()
			return nil, errors.New("object is encrypted but the repository was opened without keys")
		}
		dr, err := newDecReader(rd, r.keys.enc[:])
		if err != nil {
			src.Close()
			return nil, err
		}
		rd = dr
	}
	if flags&flagZstd != 0 {
		zr, err := zstd.NewReader(rd, zstd.WithDecoderConcurrency(1))
		if err != nil {
			src.Close()
			return nil, err
		}
		return readCloser{Reader: zr, close: func() error { zr.Close(); return src.Close() }}, nil
	}
	return readCloser{Reader: rd, close: src.Close}, nil
}
