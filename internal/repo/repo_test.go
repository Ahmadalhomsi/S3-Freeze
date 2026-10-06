package repo

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"testing"

	"s3sync/internal/storage"
)

func TestEncryptRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	for _, size := range []int{0, 1, chunkSize - 1, chunkSize, chunkSize + 1, 3*chunkSize + 17} {
		plain := make([]byte, size)
		rand.Read(plain)
		var buf bytes.Buffer
		w, err := newEncWriter(&buf, key)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(plain[:size/2])
		w.Write(plain[size/2:])
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		enc := buf.Bytes()

		r, err := newDecReader(bytes.NewReader(enc), key)
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("size %d: %v", size, err)
		}
		if !bytes.Equal(got, plain) {
			t.Fatalf("size %d: mismatch", size)
		}

		// Truncating at a chunk boundary must be detected.
		if size > chunkSize {
			cut := prefixSize + chunkSize + tagSize
			r, _ := newDecReader(bytes.NewReader(enc[:cut]), key)
			if _, err := io.ReadAll(r); err == nil {
				t.Fatalf("size %d: truncation not detected", size)
			}
		}
	}
}

func TestRepoBlobsAndPassphrase(t *testing.T) {
	ctx := context.Background()
	be, _ := storage.New(storage.Config{Type: "local", LocalPath: t.TempDir()})
	b, _ := be.Bucket("")

	r, err := Open(ctx, b, "repo", Options{Encrypt: true, Passphrase: "correct horse"})
	if err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("hello world "), 10000)
	h := r.NewHasher()
	h.Write(data)
	id := hex.EncodeToString(h.Sum(nil))
	if _, err := r.PutBlob(ctx, id, bytes.NewReader(data), true); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(ctx, b, "repo", Options{Encrypt: true, Passphrase: "wrong"}); err == nil {
		t.Fatal("expected wrong passphrase error")
	}
	r2, err := Open(ctx, b, "repo/", Options{Encrypt: true, Passphrase: "correct horse"})
	if err != nil {
		t.Fatal(err)
	}
	rd, err := r2.OpenBlob(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(rd)
	rd.Close()
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("blob mismatch: %v", err)
	}
}
