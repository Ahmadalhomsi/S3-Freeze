package store

import (
	"time"

	"s3sync/internal/storage"
)

type Storage struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Endpoint  string    `json:"endpoint"`
	Region    string    `json:"region"`
	AccessKey string    `json:"access_key"`
	SecretKey string    `json:"-"`
	UseSSL    bool      `json:"use_ssl"`
	PathStyle bool      `json:"path_style"`
	LocalPath string    `json:"local_path"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (s *Storage) Config() storage.Config {
	return storage.Config{
		Type:      s.Type,
		Endpoint:  s.Endpoint,
		Region:    s.Region,
		AccessKey: s.AccessKey,
		SecretKey: s.SecretKey,
		UseSSL:    s.UseSSL,
		PathStyle: s.PathStyle,
		LocalPath: s.LocalPath,
	}
}

const storageCols = `id, name, type, endpoint, region, access_key, secret_key, use_ssl, path_style, local_path, created_at, updated_at`

func (s *Store) scanStorage(sc interface{ Scan(...any) error }) (*Storage, error) {
	var st Storage
	var secretKey string
	var created, updated int64
	if err := sc.Scan(&st.ID, &st.Name, &st.Type, &st.Endpoint, &st.Region, &st.AccessKey, &secretKey,
		&st.UseSSL, &st.PathStyle, &st.LocalPath, &created, &updated); err != nil {
		return nil, err
	}
	var err error
	if st.SecretKey, err = s.box.Open(secretKey); err != nil {
		return nil, err
	}
	st.CreatedAt, st.UpdatedAt = toTime(created), toTime(updated)
	return &st, nil
}

func (s *Store) ListStorages() ([]*Storage, error) {
	rows, err := s.db.Query(`SELECT ` + storageCols + ` FROM storages ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []*Storage{}
	for rows.Next() {
		st, err := s.scanStorage(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, st)
	}
	return list, rows.Err()
}

func (s *Store) GetStorage(id int64) (*Storage, error) {
	st, err := s.scanStorage(s.db.QueryRow(`SELECT `+storageCols+` FROM storages WHERE id = ?`, id))
	return st, notFound(err)
}

func (s *Store) CreateStorage(st *Storage) error {
	secretKey, err := s.box.Seal(st.SecretKey)
	if err != nil {
		return err
	}
	t := now()
	res, err := s.db.Exec(`INSERT INTO storages(name, type, endpoint, region, access_key, secret_key, use_ssl, path_style, local_path, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		st.Name, st.Type, st.Endpoint, st.Region, st.AccessKey, secretKey, b2i(st.UseSSL), b2i(st.PathStyle), st.LocalPath, t, t)
	if err != nil {
		return err
	}
	st.ID, _ = res.LastInsertId()
	st.CreatedAt, st.UpdatedAt = toTime(t), toTime(t)
	return nil
}

func (s *Store) UpdateStorage(st *Storage) error {
	secretKey, err := s.box.Seal(st.SecretKey)
	if err != nil {
		return err
	}
	t := now()
	_, err = s.db.Exec(`UPDATE storages SET name=?, type=?, endpoint=?, region=?, access_key=?, secret_key=?, use_ssl=?, path_style=?, local_path=?, updated_at=? WHERE id=?`,
		st.Name, st.Type, st.Endpoint, st.Region, st.AccessKey, secretKey, b2i(st.UseSSL), b2i(st.PathStyle), st.LocalPath, t, st.ID)
	st.UpdatedAt = toTime(t)
	return err
}

func (s *Store) StorageInUse(id int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE source_storage_id = ? OR dest_storage_id = ?`, id, id).Scan(&n)
	return n > 0, err
}

func (s *Store) DeleteStorage(id int64) error {
	_, err := s.db.Exec(`DELETE FROM storages WHERE id = ?`, id)
	return err
}
