// Package storage provides a minimal object-storage abstraction over
// S3-compatible services and the local filesystem.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

var ErrNotFound = errors.New("object not found")

type ObjectInfo struct {
	Key          string            `json:"key"`
	Size         int64             `json:"size"`
	ETag         string            `json:"etag"`
	LastModified time.Time         `json:"last_modified"`
	ContentType  string            `json:"content_type,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type PutOptions struct {
	ContentType string
	Metadata    map[string]string
}

type Bucket interface {
	// List calls fn for every object under prefix, recursively.
	List(ctx context.Context, prefix string, fn func(ObjectInfo) error) error
	Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error)
	// Put stores r under key. size may be -1 when unknown.
	Put(ctx context.Context, key string, r io.Reader, size int64, opts PutOptions) error
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	// ListDirs returns the immediate "subfolders" (keys ending in "/") under
	// prefix, which must be empty or end in "/".
	ListDirs(ctx context.Context, prefix string) ([]string, error)
}

// maxDirs caps folder listings used for autocomplete.
const maxDirs = 1000

type Backend interface {
	Test(ctx context.Context) error
	ListBuckets(ctx context.Context) ([]string, error)
	Bucket(name string) (Bucket, error)
	// EnsureBucket creates the bucket if it does not exist (a no-op for "").
	EnsureBucket(ctx context.Context, name string) error
}

type Config struct {
	Type      string // "s3" or "local"
	Endpoint  string
	Region    string
	AccessKey string
	SecretKey string
	UseSSL    bool
	PathStyle bool
	LocalPath string
}

func New(c Config) (Backend, error) {
	switch c.Type {
	case "s3":
		return newS3(c)
	case "local":
		return newLocal(c)
	default:
		return nil, fmt.Errorf("unknown storage type %q", c.Type)
	}
}

var bucketNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)

// ValidBucketName reports whether name follows the S3 bucket naming rules.
func ValidBucketName(name string) error {
	if !bucketNameRe.MatchString(name) || strings.Contains(name, "..") {
		return fmt.Errorf("invalid bucket name %q: use 3-63 lowercase letters, numbers, dots and hyphens", name)
	}
	return nil
}
