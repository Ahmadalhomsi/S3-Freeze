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

	"s3freeze/internal/secret"
	"s3freeze/internal/storage"
	"s3freeze/internal/store"
)

func fakeS3(t *testing.T, st *store.Store, name string, buckets ...string) (*store.Storage, storage.Backend) {
	t.Helper()
	mem := s3mem.New()
	for _, b := range buckets {
		if err := mem.CreateBucket(b); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(gofakes3.New(mem).Server())
	t.Cleanup(srv.Close)
	s := &store.Storage{Name: name, Type: "s3", Endpoint: srv.URL, Region: "us-east-1", AccessKey: "k", SecretKey: "s", PathStyle: true}
	if err := st.CreateStorage(s); err != nil {
		t.Fatal(err)
	}
	be, err := storage.New(s.Config())
	if err != nil {
		t.Fatal(err)
	}
	return s, be
}

// TestWholeStorageBackup backs up every bucket of a storage into one of its
// own buckets, then restores everything onto an empty server.
func TestWholeStorageBackup(t *testing.T) {
	ctx := context.Background()
	box, _ := secret.New([32]byte{3})
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"), box)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	src, srcBe := fakeS3(t, st, "src", "photos", "docs", "backups")
	dst, dstBe := fakeS3(t, st, "empty")

	objects := map[string]string{"photos/2024/a.jpg": "AAA", "photos/b.jpg": "BBB", "docs/readme.md": "# hi"}
	all, _ := srcBe.Bucket("")
	for k, v := range objects {
		if err := all.Put(ctx, k, bytes.NewReader([]byte(v)), int64(len(v)), storage.PutOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	job := &store.Job{Name: "all", Enabled: true, SourceStorageID: src.ID, DestStorageID: src.ID,
		DestBucket: "backups", DestPrefix: "repo", Compression: true, Concurrency: 2}
	if err := st.CreateJob(job); err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, st: st, eng: New(st, t.TempDir()), job: job}
	r := f.backup()
	if r.ObjectsDone != 3 {
		t.Fatalf("expected 3 objects (repository excluded), got %d\n%s", r.ObjectsDone, r.Log)
	}
	// The second run must not pick up the repository written by the first.
	if r = f.backup(); r.ObjectsDone != 3 {
		t.Fatalf("second run backed up %d objects", r.ObjectsDone)
	}

	l, err := f.eng.Browse(ctx, job.ID, r.SnapshotID, "")
	if err != nil || len(l.Dirs) != 2 {
		t.Fatalf("expected bucket folders at the top level: %+v %v", l, err)
	}

	id, err := f.eng.StartRestore(job.ID, r.SnapshotID, RestoreRequest{StorageID: dst.ID})
	if err != nil {
		t.Fatal(err)
	}
	f.wait(id)
	names, _ := dstBe.ListBuckets(ctx)
	if len(names) != 2 {
		t.Fatalf("expected buckets to be recreated, got %v", names)
	}
	out, _ := dstBe.Bucket("")
	for k, v := range objects {
		rd, _, err := out.Get(ctx, k)
		if err != nil {
			t.Fatalf("%s: %v", k, err)
		}
		got, _ := io.ReadAll(rd)
		rd.Close()
		if string(got) != v {
			t.Fatalf("%s: got %q", k, got)
		}
	}
}
