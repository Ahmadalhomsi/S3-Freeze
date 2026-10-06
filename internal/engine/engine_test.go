package engine

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"s3freeze/internal/secret"
	"s3freeze/internal/store"
)

type fixture struct {
	t      *testing.T
	st     *store.Store
	eng    *Engine
	srcDir string
	dstDir string
	outDir string
	job    *store.Job
	outID  int64
}

func newFixture(t *testing.T, encrypt bool) *fixture {
	t.Helper()
	dir := t.TempDir()
	box, _ := secret.New([32]byte{1})
	st, err := store.Open(filepath.Join(dir, "test.db"), box)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	f := &fixture{t: t, st: st, eng: New(st, t.TempDir()),
		srcDir: filepath.Join(dir, "src"), dstDir: filepath.Join(dir, "dst"), outDir: filepath.Join(dir, "out")}

	ids := []int64{}
	for _, p := range []string{f.srcDir, f.dstDir, f.outDir} {
		s := &store.Storage{Name: filepath.Base(p), Type: "local", LocalPath: p}
		if err := st.CreateStorage(s); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, s.ID)
	}
	f.outID = ids[2]
	f.job = &store.Job{Name: "test", Enabled: true, SourceStorageID: ids[0], DestStorageID: ids[1], DestPrefix: "backups",
		Compression: true, Encryption: encrypt, Concurrency: 3, KeepLast: 2}
	if encrypt {
		f.job.Passphrase = "s3cret-passphrase"
	}
	if err := st.CreateJob(f.job); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) write(key, content string) {
	p := filepath.Join(f.srcDir, filepath.FromSlash(key))
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) wait(runID int64) *store.Run {
	f.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		r, err := f.st.GetRun(runID)
		if err != nil {
			f.t.Fatal(err)
		}
		if r.Status != store.StatusRunning && f.eng.ActiveRun(f.job.ID) == 0 {
			if r.Status != store.StatusSuccess {
				f.t.Fatalf("run %d %s: %s\n%s", runID, r.Status, r.Error, r.Log)
			}
			return r
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.t.Fatal("run did not finish")
	return nil
}

func (f *fixture) backup() *store.Run {
	f.t.Helper()
	id, err := f.eng.StartBackup(f.job.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	return f.wait(id)
}

func TestBackupRestoreCycle(t *testing.T) {
	for _, encrypt := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "encrypted"}[encrypt], func(t *testing.T) {
			f := newFixture(t, encrypt)
			big := string(bytes.Repeat([]byte("0123456789abcdef"), 1<<20)) // 16 MiB, spooled to disk
			f.write("a.txt", "alpha")
			f.write("dir/b.txt", "bravo")
			f.write("dir/sub/c.txt", "charlie")
			f.write("dup.txt", "alpha") // same content as a.txt
			f.write("big.bin", big)

			r1 := f.backup()
			if r1.ObjectsDone != 5 {
				t.Fatalf("expected 5 objects, got %d", r1.ObjectsDone)
			}

			// Second run with one change: only the changed object is uploaded.
			f.write("dir/b.txt", "bravo v2")
			r2 := f.backup()
			if r2.BytesUploaded == 0 || r2.BytesUploaded > 1024 {
				t.Fatalf("incremental run uploaded %d bytes", r2.BytesUploaded)
			}

			// Third run triggers retention (keep_last=2) and prune.
			f.write("new.txt", "new")
			f.backup()
			snaps, _ := f.st.ListSnapshots(f.job.ID)
			if len(snaps) != 2 {
				t.Fatalf("expected 2 snapshots after retention, got %d", len(snaps))
			}

			// Browse latest snapshot.
			l, err := f.eng.Browse(context.Background(), f.job.ID, snaps[0].ID, "dir/")
			if err != nil {
				t.Fatal(err)
			}
			if len(l.Files) != 1 || len(l.Dirs) != 1 || l.Dirs[0].Name != "sub" {
				t.Fatalf("unexpected listing %+v", l)
			}

			// Restore the "dir/" folder of the oldest kept snapshot into another storage.
			id, err := f.eng.StartRestore(f.job.ID, snaps[1].ID, RestoreRequest{
				Paths: []string{"dir/"}, StorageID: f.outID, Prefix: "restored/",
			})
			if err != nil {
				t.Fatal(err)
			}
			rr := f.wait(id)
			if rr.ObjectsDone != 2 {
				t.Fatalf("expected 2 restored objects, got %d", rr.ObjectsDone)
			}
			got, _ := os.ReadFile(filepath.Join(f.outDir, "restored", "dir", "b.txt"))
			if string(got) != "bravo v2" {
				t.Fatalf("restored content %q", got)
			}

			// Full restore and compare the big file.
			id, _ = f.eng.StartRestore(f.job.ID, snaps[0].ID, RestoreRequest{StorageID: f.outID, Prefix: "full/"})
			f.wait(id)
			got, _ = os.ReadFile(filepath.Join(f.outDir, "full", "big.bin"))
			if string(got) != big {
				t.Fatal("big file mismatch after restore")
			}

			// Rebuild snapshot index from the repository after "losing" the DB rows.
			for _, s := range snaps {
				f.st.DeleteSnapshot(f.job.ID, s.ID)
			}
			id, _ = f.eng.StartScan(f.job.ID)
			f.wait(id)
			if again, _ := f.st.ListSnapshots(f.job.ID); len(again) != 2 {
				t.Fatalf("scan imported %d snapshots", len(again))
			}

			// Deleting a snapshot prunes blobs only it referenced.
			id, _ = f.eng.StartDeleteSnapshot(f.job.ID, snaps[1].ID)
			f.wait(id)
		})
	}
}
