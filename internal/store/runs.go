package store

import (
	"database/sql"
	"time"
)

const (
	KindBackup  = "backup"
	KindRestore = "restore"
	KindDelete  = "delete"
	KindScan    = "scan"

	StatusRunning   = "running"
	StatusSuccess   = "success"
	StatusWarning   = "warning" // finished, but some objects failed
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

type Progress struct {
	ObjectsTotal  int64 `json:"objects_total"`
	ObjectsDone   int64 `json:"objects_done"`
	ObjectsFailed int64 `json:"objects_failed"`
	BytesTotal    int64 `json:"bytes_total"`
	BytesDone     int64 `json:"bytes_done"`
	BytesUploaded int64 `json:"bytes_uploaded"`
}

type Run struct {
	ID         int64      `json:"id"`
	JobID      int64      `json:"job_id"`
	JobName    string     `json:"job_name"`
	Kind       string     `json:"kind"`
	Status     string     `json:"status"`
	Detail     string     `json:"detail"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Progress
	SnapshotID string `json:"snapshot_id"`
	Error      string `json:"error"`
	Log        string `json:"log,omitempty"`
}

type RunFilter struct {
	JobID  int64
	Kind   string
	Status string
	Limit  int
}

const runCols = `r.id, COALESCE(r.job_id, 0), COALESCE(j.name, ''), r.kind, r.status, r.detail, r.started_at, r.finished_at,
	r.objects_total, r.objects_done, r.objects_failed, r.bytes_total, r.bytes_done, r.bytes_uploaded, r.snapshot_id, r.error`

func scanRun(sc interface{ Scan(...any) error }, extra ...any) (*Run, error) {
	var r Run
	var started int64
	var finished sql.NullInt64
	dest := []any{&r.ID, &r.JobID, &r.JobName, &r.Kind, &r.Status, &r.Detail, &started, &finished,
		&r.ObjectsTotal, &r.ObjectsDone, &r.ObjectsFailed, &r.BytesTotal, &r.BytesDone, &r.BytesUploaded, &r.SnapshotID, &r.Error}
	if err := sc.Scan(append(dest, extra...)...); err != nil {
		return nil, err
	}
	r.StartedAt = toTime(started)
	r.FinishedAt = toTimePtr(finished)
	return &r, nil
}

func (s *Store) CreateRun(jobID int64, kind, detail string) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO runs(job_id, kind, status, detail, started_at) VALUES(?,?,?,?,?)`,
		jobID, kind, StatusRunning, detail, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateRunProgress(id int64, p Progress, log string) error {
	_, err := s.db.Exec(`UPDATE runs SET objects_total=?, objects_done=?, objects_failed=?, bytes_total=?, bytes_done=?, bytes_uploaded=?, log=? WHERE id=?`,
		p.ObjectsTotal, p.ObjectsDone, p.ObjectsFailed, p.BytesTotal, p.BytesDone, p.BytesUploaded, log, id)
	return err
}

func (s *Store) FinishRun(id int64, status, errMsg, snapshotID string, p Progress, log string) error {
	_, err := s.db.Exec(`UPDATE runs SET status=?, error=?, snapshot_id=?, finished_at=?, objects_total=?, objects_done=?, objects_failed=?,
		bytes_total=?, bytes_done=?, bytes_uploaded=?, log=? WHERE id=?`,
		status, errMsg, snapshotID, now(), p.ObjectsTotal, p.ObjectsDone, p.ObjectsFailed, p.BytesTotal, p.BytesDone, p.BytesUploaded, log, id)
	return err
}

// FailInterruptedRuns marks runs left "running" by a previous process as failed.
func (s *Store) FailInterruptedRuns() error {
	_, err := s.db.Exec(`UPDATE runs SET status=?, error='interrupted by application restart', finished_at=? WHERE status=?`,
		StatusFailed, now(), StatusRunning)
	return err
}

func (s *Store) ListRuns(f RunFilter) ([]*Run, error) {
	q := `SELECT ` + runCols + ` FROM runs r LEFT JOIN jobs j ON j.id = r.job_id WHERE 1=1`
	var args []any
	if f.JobID != 0 {
		q += ` AND r.job_id = ?`
		args = append(args, f.JobID)
	}
	if f.Kind != "" {
		q += ` AND r.kind = ?`
		args = append(args, f.Kind)
	}
	if f.Status != "" {
		q += ` AND r.status = ?`
		args = append(args, f.Status)
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 50
	}
	q += ` ORDER BY r.id DESC LIMIT ?`
	args = append(args, f.Limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []*Run{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, rows.Err()
}

func (s *Store) GetRun(id int64) (*Run, error) {
	var log string
	r, err := scanRun(s.db.QueryRow(`SELECT `+runCols+`, r.log FROM runs r LEFT JOIN jobs j ON j.id = r.job_id WHERE r.id = ?`, id), &log)
	if err != nil {
		return nil, notFound(err)
	}
	r.Log = log
	return r, nil
}

// LatestBackupRuns returns the most recent backup run for each job.
func (s *Store) LatestBackupRuns() (map[int64]*Run, error) {
	rows, err := s.db.Query(`SELECT ` + runCols + ` FROM runs r LEFT JOIN jobs j ON j.id = r.job_id
		WHERE r.id IN (SELECT MAX(id) FROM runs WHERE kind = 'backup' GROUP BY job_id)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]*Run{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out[r.JobID] = r
	}
	return out, rows.Err()
}

// LastSuccessfulBackups returns, per job, when the last backup finished successfully.
func (s *Store) LastSuccessfulBackups() (map[int64]time.Time, error) {
	rows, err := s.db.Query(`SELECT job_id, MAX(finished_at) FROM runs
		WHERE kind = 'backup' AND status IN ('success', 'warning') AND job_id IS NOT NULL GROUP BY job_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]time.Time{}
	for rows.Next() {
		var id, t int64
		if err := rows.Scan(&id, &t); err != nil {
			return nil, err
		}
		out[id] = toTime(t)
	}
	return out, rows.Err()
}

type RunCounts struct {
	Success int `json:"success"`
	Warning int `json:"warning"`
	Failed  int `json:"failed"`
}

func (s *Store) RunCountsSince(t time.Time) (RunCounts, error) {
	var c RunCounts
	err := s.db.QueryRow(`SELECT
		COALESCE(SUM(status = 'success'), 0), COALESCE(SUM(status = 'warning'), 0), COALESCE(SUM(status = 'failed'), 0)
		FROM runs WHERE started_at >= ?`, t.Unix()).Scan(&c.Success, &c.Warning, &c.Failed)
	return c, err
}
