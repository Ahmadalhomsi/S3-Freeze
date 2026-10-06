package store

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

func (s *Store) UserCount() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(username, hash string) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO users(username, password_hash, created_at) VALUES(?,?,?)`, username, hash, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) userBy(where string, arg any) (*User, error) {
	var u User
	var created int64
	err := s.db.QueryRow(`SELECT id, username, password_hash, created_at FROM users WHERE `+where, arg).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &created)
	if err != nil {
		return nil, notFound(err)
	}
	u.CreatedAt = toTime(created)
	return &u, nil
}

func (s *Store) UserByName(name string) (*User, error) { return s.userBy("username = ?", name) }

func (s *Store) UserByID(id int64) (*User, error) { return s.userBy("id = ?", id) }

func (s *Store) UpdatePassword(id int64, hash string) error {
	_, err := s.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, hash, id)
	return err
}

func (s *Store) CreateSession(userID int64, ttl time.Duration) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	s.db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, now())
	_, err := s.db.Exec(`INSERT INTO sessions(token, user_id, expires_at) VALUES(?,?,?)`, token, userID, time.Now().Add(ttl).Unix())
	return token, err
}

// SessionUser returns the user owning a valid, unexpired session token.
func (s *Store) SessionUser(token string) (*User, error) {
	var uid int64
	err := s.db.QueryRow(`SELECT user_id FROM sessions WHERE token = ? AND expires_at > ?`, token, now()).Scan(&uid)
	if err != nil {
		return nil, notFound(err)
	}
	return s.UserByID(uid)
}

func (s *Store) DeleteSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	return err
}

func (s *Store) DeleteUserSessions(userID int64, except string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE user_id = ? AND token != ?`, userID, except)
	return err
}
