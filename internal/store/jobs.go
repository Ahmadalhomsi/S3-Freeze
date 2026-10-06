package store

import "time"

type Job struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	Enabled         bool      `json:"enabled"`
	Schedule        string    `json:"schedule"`
	SourceStorageID int64     `json:"source_storage_id"`
	SourceBucket    string    `json:"source_bucket"`
	SourcePrefix    string    `json:"source_prefix"`
	DestStorageID   int64     `json:"dest_storage_id"`
	DestBucket      string    `json:"dest_bucket"`
	DestPrefix      string    `json:"dest_prefix"`
	Compression     bool      `json:"compression"`
	Encryption      bool      `json:"encryption"`
	Passphrase      string    `json:"-"`
	Concurrency     int       `json:"concurrency"`
	KeepLast        int       `json:"keep_last"`
	KeepDays        int       `json:"keep_days"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

const jobCols = `id, name, enabled, schedule, source_storage_id, source_bucket, source_prefix, dest_storage_id, dest_bucket, dest_prefix,
	compression, encryption, passphrase, concurrency, keep_last, keep_days, created_at, updated_at`

func (s *Store) scanJob(sc interface{ Scan(...any) error }) (*Job, error) {
	var j Job
	var pass string
	var created, updated int64
	if err := sc.Scan(&j.ID, &j.Name, &j.Enabled, &j.Schedule, &j.SourceStorageID, &j.SourceBucket, &j.SourcePrefix,
		&j.DestStorageID, &j.DestBucket, &j.DestPrefix, &j.Compression, &j.Encryption, &pass,
		&j.Concurrency, &j.KeepLast, &j.KeepDays, &created, &updated); err != nil {
		return nil, err
	}
	var err error
	if j.Passphrase, err = s.box.Open(pass); err != nil {
		return nil, err
	}
	j.CreatedAt, j.UpdatedAt = toTime(created), toTime(updated)
	return &j, nil
}

func (s *Store) ListJobs() ([]*Job, error) {
	rows, err := s.db.Query(`SELECT ` + jobCols + ` FROM jobs ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []*Job{}
	for rows.Next() {
		j, err := s.scanJob(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, j)
	}
	return list, rows.Err()
}

func (s *Store) GetJob(id int64) (*Job, error) {
	j, err := s.scanJob(s.db.QueryRow(`SELECT `+jobCols+` FROM jobs WHERE id = ?`, id))
	return j, notFound(err)
}

func (s *Store) CreateJob(j *Job) error {
	pass, err := s.box.Seal(j.Passphrase)
	if err != nil {
		return err
	}
	t := now()
	res, err := s.db.Exec(`INSERT INTO jobs(name, enabled, schedule, source_storage_id, source_bucket, source_prefix, dest_storage_id, dest_bucket, dest_prefix,
		compression, encryption, passphrase, concurrency, keep_last, keep_days, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		j.Name, b2i(j.Enabled), j.Schedule, j.SourceStorageID, j.SourceBucket, j.SourcePrefix, j.DestStorageID, j.DestBucket, j.DestPrefix,
		b2i(j.Compression), b2i(j.Encryption), pass, j.Concurrency, j.KeepLast, j.KeepDays, t, t)
	if err != nil {
		return err
	}
	j.ID, _ = res.LastInsertId()
	j.CreatedAt, j.UpdatedAt = toTime(t), toTime(t)
	return nil
}

func (s *Store) UpdateJob(j *Job) error {
	pass, err := s.box.Seal(j.Passphrase)
	if err != nil {
		return err
	}
	t := now()
	_, err = s.db.Exec(`UPDATE jobs SET name=?, enabled=?, schedule=?, source_storage_id=?, source_bucket=?, source_prefix=?, dest_storage_id=?, dest_bucket=?, dest_prefix=?,
		compression=?, encryption=?, passphrase=?, concurrency=?, keep_last=?, keep_days=?, updated_at=? WHERE id=?`,
		j.Name, b2i(j.Enabled), j.Schedule, j.SourceStorageID, j.SourceBucket, j.SourcePrefix, j.DestStorageID, j.DestBucket, j.DestPrefix,
		b2i(j.Compression), b2i(j.Encryption), pass, j.Concurrency, j.KeepLast, j.KeepDays, t, j.ID)
	j.UpdatedAt = toTime(t)
	return err
}

func (s *Store) DeleteJob(id int64) error {
	_, err := s.db.Exec(`DELETE FROM jobs WHERE id = ?`, id)
	return err
}
