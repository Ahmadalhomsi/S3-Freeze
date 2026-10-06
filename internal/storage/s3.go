package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type s3Backend struct {
	client *minio.Client
}

func newS3(c Config) (Backend, error) {
	endpoint := strings.TrimSpace(c.Endpoint)
	if endpoint == "" {
		return nil, errors.New("endpoint is required")
	}
	secure := c.UseSSL
	// Accept full URLs such as "https://s3.example.com" as well as bare hosts.
	if u, err := url.Parse(endpoint); err == nil && u.Scheme != "" && u.Host != "" {
		endpoint = u.Host
		secure = u.Scheme == "https"
	}
	lookup := minio.BucketLookupDNS
	if c.PathStyle {
		lookup = minio.BucketLookupPath
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(c.AccessKey, c.SecretKey, ""),
		Secure:       secure,
		Region:       c.Region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, err
	}
	return &s3Backend{client: client}, nil
}

func (s *s3Backend) Test(ctx context.Context) error {
	_, err := s.client.ListBuckets(ctx)
	return err
}

func (s *s3Backend) ListBuckets(ctx context.Context) ([]string, error) {
	buckets, err := s.client.ListBuckets(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(buckets))
	for i, b := range buckets {
		names[i] = b.Name
	}
	return names, nil
}

// Bucket returns one bucket, or with an empty name a view over all buckets.
func (s *s3Backend) Bucket(name string) (Bucket, error) {
	if name == "" {
		return &s3AllBuckets{s: s, known: map[string]bool{}}, nil
	}
	return &s3Bucket{client: s.client, name: name}, nil
}

type s3Bucket struct {
	client *minio.Client
	name   string
}

func isNotFound(err error) bool {
	code := minio.ToErrorResponse(err).Code
	return code == "NoSuchKey" || code == "NotFound"
}

func (b *s3Bucket) List(ctx context.Context, prefix string, fn func(ObjectInfo) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for obj := range b.client.ListObjects(ctx, b.name, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if obj.Err != nil {
			return fmt.Errorf("list %s/%s: %w", b.name, prefix, obj.Err)
		}
		err := fn(ObjectInfo{
			Key:          obj.Key,
			Size:         obj.Size,
			ETag:         obj.ETag,
			LastModified: obj.LastModified,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (b *s3Bucket) Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	obj, err := b.client.GetObject(ctx, b.name, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	st, err := obj.Stat()
	if err != nil {
		obj.Close()
		if isNotFound(err) {
			return nil, ObjectInfo{}, ErrNotFound
		}
		return nil, ObjectInfo{}, err
	}
	var meta map[string]string
	if len(st.UserMetadata) > 0 {
		meta = make(map[string]string, len(st.UserMetadata))
		for k, v := range st.UserMetadata {
			meta[k] = v
		}
	}
	return obj, ObjectInfo{
		Key:          st.Key,
		Size:         st.Size,
		ETag:         st.ETag,
		LastModified: st.LastModified,
		ContentType:  st.ContentType,
		Metadata:     meta,
	}, nil
}

func (b *s3Bucket) Put(ctx context.Context, key string, r io.Reader, size int64, opts PutOptions) error {
	po := minio.PutObjectOptions{
		ContentType:  opts.ContentType,
		UserMetadata: opts.Metadata,
		// Over plain HTTP, minio-go otherwise streams non-seekable bodies with
		// aws-chunked signing, which some S3-compatible servers store verbatim
		// instead of decoding. Backups and restores verify content hashes anyway.
		DisableContentSha256: true,
	}
	if size < 0 {
		po.PartSize = 16 << 20
	}
	_, err := b.client.PutObject(ctx, b.name, key, r, size, po)
	return err
}

func (b *s3Bucket) ListDirs(ctx context.Context, prefix string) ([]string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var dirs []string
	for obj := range b.client.ListObjects(ctx, b.name, minio.ListObjectsOptions{Prefix: prefix}) {
		if obj.Err != nil {
			return nil, obj.Err
		}
		if strings.HasSuffix(obj.Key, "/") && obj.Key != prefix {
			dirs = append(dirs, obj.Key)
			if len(dirs) >= maxDirs {
				break
			}
		}
	}
	return dirs, nil
}

func (b *s3Bucket) Delete(ctx context.Context, key string) error {
	return b.client.RemoveObject(ctx, b.name, key, minio.RemoveObjectOptions{})
}

func (b *s3Bucket) Exists(ctx context.Context, key string) (bool, error) {
	_, err := b.client.StatObject(ctx, b.name, key, minio.StatObjectOptions{})
	if err == nil {
		return true, nil
	}
	if isNotFound(err) {
		return false, nil
	}
	return false, err
}
