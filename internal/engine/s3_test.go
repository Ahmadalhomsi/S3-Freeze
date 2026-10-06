package engine

import (
	"bytes"
	"context"
	"io"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"

	"s3sync/internal/secret"
	"s3sync/internal/storage"
	"s3sync/internal/store"
)

// TestS3BackupRestore runs a backup and restore against an in-process fake S3
// server, exercising the S3 code path (metadata, content type, multipart).
func TestS3BackupRestore(t *testing.T) {
	ctx := context.Background()
	mem := s3mem.New()
	for _, b := range []string{"source", "backups", "restored"} {
		if err := mem.CreateBucket(b); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(gofakes3.New(mem).Server())
	defer srv.Close()

	box, _ := secret.New([32]byte{2})
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"), box)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	s3 := &store.Storage{Name: "fake", Type: "s3", Endpoint: srv.URL, Region: "us-east-1", AccessKey: "key", SecretKey: "secret", PathStyle: true}
	if err := st.CreateStorage(s3); err != nil {
		t.Fatal(err)
	}
	be, err := storage.New(s3.Config())
	if err != nil {
		t.Fatal(err)
	}
	if err := be.Test(ctx); err != nil {
		t.Fatal(err)
	}

	src, _ := be.Bucket("source")
	big := bytes.Repeat([]byte("s3sync!"), 3<<20) // ~21 MiB, multipart upload when encoded
	objects := map[string][]byte{"img/logo.png": []byte("PNGDATA"), "docs/readme.md": []byte("# hi"), "big.bin": big}
	for k, v := range objects {
		err := src.Put(ctx, k, bytes.NewReader(v), int64(len(v)), storage.PutOptions{ContentType: "application/x-test", Metadata: map[string]string{"Owner": "me"}})
		if err != nil {
			t.Fatal(err)
		}
	}

	job := &store.Job{Name: "s3", Enabled: true, SourceStorageID: s3.ID, SourceBucket: "source", DestStorageID: s3.ID,
		DestBucket: "backups", DestPrefix: "repo", Compression: true, Encryption: true, Passphrase: "passphrase!", Concurrency: 4}
	if err := st.CreateJob(job); err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, st: st, eng: New(st, t.TempDir()), job: job}
	r := f.backup()
	if r.ObjectsDone != 3 {
		t.Fatalf("backed up %d objects", r.ObjectsDone)
	}
	r = f.backup()
	if r.BytesUploaded > 64<<10 {
		t.Fatalf("unchanged second run uploaded %d bytes", r.BytesUploaded)
	}

	id, err := f.eng.StartRestore(job.ID, r.SnapshotID, RestoreRequest{StorageID: s3.ID, Bucket: "restored"})
	if err != nil {
		t.Fatal(err)
	}
	f.wait(id)
	dst, _ := be.Bucket("restored")
	for k, v := range objects {
		rd, info, err := dst.Get(ctx, k)
		if err != nil {
			t.Fatalf("%s: %v", k, err)
		}
		got, _ := io.ReadAll(rd)
		rd.Close()
		if !bytes.Equal(got, v) {
			t.Fatalf("%s: content mismatch", k)
		}
		if info.ContentType != "application/x-test" || info.Metadata["Owner"] != "me" {
			t.Fatalf("%s: metadata not restored: %q %v", k, info.ContentType, info.Metadata)
		}
	}
}
