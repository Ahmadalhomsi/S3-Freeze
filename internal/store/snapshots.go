package store

import "time"

type Snapshot struct {
	ID         string    `json:"id"`
	JobID      int64     `json:"job_id"`
	RunID      int64     `json:"run_id"`
	CreatedAt  time.Time `json:"created_at"`
	Objects    int64     `json:"objects"`
	Size       int64     `json:"size"`
	AddedBytes int64     `json:"added_bytes"`
}

const snapshotCols = `id, job_id, run_id, created_at, objects, size, added_bytes`

func scanSnapshot(sc interface{ Scan(...any) error }) (*Snapshot, error) {
	var sn Snapshot
	var created int64
	if err := sc.Scan(&sn.ID, &sn.JobID, &sn.RunID, &created, &sn.Objects, &sn.Size, &sn.AddedBytes); err != nil {
		return nil, err
	}
	sn.CreatedAt = time.UnixMilli(created).UTC()
	return &sn, nil
}

// InsertSnapshot records a snapshot; it is a no-op if the snapshot is already known.
func (s *Store) InsertSnapshot(sn *Snapshot) (bool, error) {
	res, err := s.db.Exec(`INSERT OR IGNORE INTO snapshots(`+snapshotCols+`) VALUES(?,?,?,?,?,?,?)`,
		sn.ID, sn.JobID, sn.RunID, sn.CreatedAt.UnixMilli(), sn.Objects, sn.Size, sn.AddedBytes)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ListSnapshots returns the snapshots of a job, newest first.
func (s *Store) ListSnapshots(jobID int64) ([]*Snapshot, error) {
	rows, err := s.db.Query(`SELECT `+snapshotCols+` FROM snapshots WHERE job_id = ? ORDER BY created_at DESC, id DESC`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []*Snapshot{}
	for rows.Next() {
		sn, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, sn)
	}
	return list, rows.Err()
}

func (s *Store) GetSnapshot(jobID int64, id string) (*Snapshot, error) {
	sn, err := scanSnapshot(s.db.QueryRow(`SELECT `+snapshotCols+` FROM snapshots WHERE job_id = ? AND id = ?`, jobID, id))
	return sn, notFound(err)
}

func (s *Store) LatestSnapshot(jobID int64) (*Snapshot, error) {
	sn, err := scanSnapshot(s.db.QueryRow(`SELECT `+snapshotCols+` FROM snapshots WHERE job_id = ? ORDER BY created_at DESC, id DESC LIMIT 1`, jobID))
	return sn, notFound(err)
}

func (s *Store) DeleteSnapshot(jobID int64, id string) error {
	_, err := s.db.Exec(`DELETE FROM snapshots WHERE job_id = ? AND id = ?`, jobID, id)
	return err
}

type SnapshotSummary struct {
	Count      int        `json:"count"`
	LatestID   string     `json:"latest_id"`
	LatestAt   *time.Time `json:"latest_at"`
	LatestSize int64      `json:"latest_size"`
	LatestObjs int64      `json:"latest_objects"`
}

// SnapshotSummaries returns per-job snapshot counts and the latest snapshot's size.
func (s *Store) SnapshotSummaries() (map[int64]*SnapshotSummary, error) {
	rows, err := s.db.Query(`SELECT s.job_id, c.n, s.id, s.created_at, s.size, s.objects FROM snapshots s
		JOIN (SELECT job_id, COUNT(*) AS n, MAX(created_at) AS latest FROM snapshots GROUP BY job_id) c
		ON c.job_id = s.job_id AND c.latest = s.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]*SnapshotSummary{}
	for rows.Next() {
		var jobID, created int64
		var sum SnapshotSummary
		if err := rows.Scan(&jobID, &sum.Count, &sum.LatestID, &created, &sum.LatestSize, &sum.LatestObjs); err != nil {
			return nil, err
		}
		t := time.UnixMilli(created).UTC()
		sum.LatestAt = &t
		out[jobID] = &sum
	}
	return out, rows.Err()
}
