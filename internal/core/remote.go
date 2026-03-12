package core

import (
	"database/sql"
	"fmt"
)

// Remote represents a hub remote configuration.
type Remote struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	IsDefault bool   `json:"is_default"`
	LastPush  string `json:"last_push,omitempty"`
	LastPull  string `json:"last_pull,omitempty"`
	PushSeq   int64  `json:"push_seq"`
	PullSeq   int64  `json:"pull_seq"`
	CreatedAt string `json:"created_at"`
}

// RemoteStore manages hub remotes in the local database.
type RemoteStore struct {
	db *sql.DB
}

// NewRemoteStore creates a new remote store.
func NewRemoteStore(db *sql.DB) *RemoteStore {
	return &RemoteStore{db: db}
}

// Add creates a new remote.
func (s *RemoteStore) Add(name, url string, isDefault bool) error {
	if isDefault {
		// Clear any existing default
		s.db.Exec("UPDATE remotes SET is_default = 0 WHERE is_default = 1")
	}
	defVal := 0
	if isDefault {
		defVal = 1
	}
	_, err := s.db.Exec(
		"INSERT INTO remotes (name, url, is_default) VALUES (?, ?, ?)",
		name, url, defVal,
	)
	if err != nil {
		return fmt.Errorf("add remote: %w", err)
	}
	return nil
}

// Remove deletes a remote by name.
func (s *RemoteStore) Remove(name string) error {
	res, err := s.db.Exec("DELETE FROM remotes WHERE name = ?", name)
	if err != nil {
		return fmt.Errorf("remove remote: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("remote %q not found", name)
	}
	return nil
}

// Get returns a remote by name.
func (s *RemoteStore) Get(name string) (*Remote, error) {
	r := &Remote{}
	var lastPush, lastPull sql.NullString
	err := s.db.QueryRow(
		"SELECT name, url, is_default, last_push, last_pull, push_seq, pull_seq, created_at FROM remotes WHERE name = ?",
		name,
	).Scan(&r.Name, &r.URL, &r.IsDefault, &lastPush, &lastPull, &r.PushSeq, &r.PullSeq, &r.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get remote %q: %w", name, err)
	}
	if lastPush.Valid {
		r.LastPush = lastPush.String
	}
	if lastPull.Valid {
		r.LastPull = lastPull.String
	}
	return r, nil
}

// Default returns the default remote (or first if none marked default).
func (s *RemoteStore) Default() (*Remote, error) {
	r, err := s.getWhere("is_default = 1")
	if err != nil {
		// Fall back to first remote
		r, err = s.getWhere("1=1")
		if err != nil {
			return nil, fmt.Errorf("no remotes configured (use 'loom hub add')")
		}
	}
	return r, nil
}

func (s *RemoteStore) getWhere(where string) (*Remote, error) {
	r := &Remote{}
	var lastPush, lastPull sql.NullString
	err := s.db.QueryRow(
		"SELECT name, url, is_default, last_push, last_pull, push_seq, pull_seq, created_at FROM remotes WHERE "+where+" LIMIT 1",
	).Scan(&r.Name, &r.URL, &r.IsDefault, &lastPush, &lastPull, &r.PushSeq, &r.PullSeq, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	if lastPush.Valid {
		r.LastPush = lastPush.String
	}
	if lastPull.Valid {
		r.LastPull = lastPull.String
	}
	return r, nil
}

// List returns all configured remotes.
func (s *RemoteStore) List() ([]Remote, error) {
	rows, err := s.db.Query(
		"SELECT name, url, is_default, last_push, last_pull, push_seq, pull_seq, created_at FROM remotes ORDER BY name",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var remotes []Remote
	for rows.Next() {
		var r Remote
		var lastPush, lastPull sql.NullString
		if err := rows.Scan(&r.Name, &r.URL, &r.IsDefault, &lastPush, &lastPull, &r.PushSeq, &r.PullSeq, &r.CreatedAt); err != nil {
			continue
		}
		if lastPush.Valid {
			r.LastPush = lastPush.String
		}
		if lastPull.Valid {
			r.LastPull = lastPull.String
		}
		remotes = append(remotes, r)
	}
	return remotes, nil
}

// UpdatePushSeq records the latest pushed sequence.
func (s *RemoteStore) UpdatePushSeq(name string, seq int64) error {
	_, err := s.db.Exec(
		"UPDATE remotes SET push_seq = ?, last_push = datetime('now') WHERE name = ?",
		seq, name,
	)
	return err
}

// UpdatePullSeq records the latest pulled sequence.
func (s *RemoteStore) UpdatePullSeq(name string, seq int64) error {
	_, err := s.db.Exec(
		"UPDATE remotes SET pull_seq = ?, last_pull = datetime('now') WHERE name = ?",
		seq, name,
	)
	return err
}

// SetAuthToken stores an auth token for a remote in the metadata table.
func (s *RemoteStore) SetAuthToken(remoteName, token string) error {
	key := "auth_token:" + remoteName
	_, err := s.db.Exec(
		"INSERT INTO metadata (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		key, token,
	)
	return err
}

// GetAuthToken retrieves the auth token for a remote.
func (s *RemoteStore) GetAuthToken(remoteName string) (string, error) {
	key := "auth_token:" + remoteName
	var token string
	err := s.db.QueryRow("SELECT value FROM metadata WHERE key = ?", key).Scan(&token)
	if err != nil {
		return "", fmt.Errorf("no auth token for remote %q (use 'loom hub auth %s')", remoteName, remoteName)
	}
	return token, nil
}
