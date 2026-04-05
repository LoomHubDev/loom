package core

import (
	"database/sql"
	"errors"
	"fmt"
)

var (
	ErrStreamNotFound = errors.New("stream not found")
	ErrStreamExists   = errors.New("stream already exists")
)

// StreamManager handles stream lifecycle.
type StreamManager struct {
	db *sql.DB
}

// NewStreamManager creates a new stream manager.
func NewStreamManager(db *sql.DB) *StreamManager {
	return &StreamManager{db: db}
}

// Create creates a new stream.
func (sm *StreamManager) Create(name string) (*Stream, error) {
	s := &Stream{
		ID:        NewID(),
		Name:      name,
		HeadSeq:   0,
		CreatedAt: Now(),
		UpdatedAt: Now(),
		Status:    "active",
	}

	_, err := sm.db.Exec(`
		INSERT INTO streams (id, name, head_seq, created_at, updated_at, status)
		VALUES (?, ?, ?, ?, ?, ?)`,
		s.ID, s.Name, s.HeadSeq, s.CreatedAt, s.UpdatedAt, s.Status,
	)
	if err != nil {
		return nil, fmt.Errorf("create stream: %w", err)
	}

	return s, nil
}

// Fork creates a new stream from the current head of a parent stream.
func (sm *StreamManager) Fork(parentName, newName string) (*Stream, error) {
	parent, err := sm.GetByName(parentName)
	if err != nil {
		return nil, fmt.Errorf("parent stream: %w", err)
	}

	s := &Stream{
		ID:        NewID(),
		Name:      newName,
		HeadSeq:   parent.HeadSeq,
		CreatedAt: Now(),
		UpdatedAt: Now(),
		ParentID:  parent.ID,
		ForkSeq:   parent.HeadSeq,
		Status:    "active",
	}

	_, err = sm.db.Exec(`
		INSERT INTO streams (id, name, head_seq, created_at, updated_at, parent_id, fork_seq, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.Name, s.HeadSeq, s.CreatedAt, s.UpdatedAt, s.ParentID, s.ForkSeq, s.Status,
	)
	if err != nil {
		return nil, fmt.Errorf("fork stream: %w", err)
	}

	return s, nil
}

// GetByName returns a stream by name.
func (sm *StreamManager) GetByName(name string) (*Stream, error) {
	s := &Stream{}
	var parentID sql.NullString
	var forkSeq sql.NullInt64

	err := sm.db.QueryRow(
		"SELECT id, name, head_seq, created_at, updated_at, parent_id, fork_seq, status FROM streams WHERE name = ?",
		name,
	).Scan(&s.ID, &s.Name, &s.HeadSeq, &s.CreatedAt, &s.UpdatedAt, &parentID, &forkSeq, &s.Status)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrStreamNotFound
		}
		return nil, fmt.Errorf("get stream: %w", err)
	}

	if parentID.Valid {
		s.ParentID = parentID.String
	}
	if forkSeq.Valid {
		s.ForkSeq = forkSeq.Int64
	}

	return s, nil
}

// GetByID returns a stream by ID.
func (sm *StreamManager) GetByID(id string) (*Stream, error) {
	s := &Stream{}
	var parentID sql.NullString
	var forkSeq sql.NullInt64

	err := sm.db.QueryRow(
		"SELECT id, name, head_seq, created_at, updated_at, parent_id, fork_seq, status FROM streams WHERE id = ?",
		id,
	).Scan(&s.ID, &s.Name, &s.HeadSeq, &s.CreatedAt, &s.UpdatedAt, &parentID, &forkSeq, &s.Status)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrStreamNotFound
		}
		return nil, fmt.Errorf("get stream: %w", err)
	}

	if parentID.Valid {
		s.ParentID = parentID.String
	}
	if forkSeq.Valid {
		s.ForkSeq = forkSeq.Int64
	}

	return s, nil
}

// List returns all streams.
func (sm *StreamManager) List() ([]Stream, error) {
	rows, err := sm.db.Query(
		"SELECT id, name, head_seq, created_at, updated_at, parent_id, fork_seq, status FROM streams ORDER BY updated_at DESC",
	)
	if err != nil {
		return nil, fmt.Errorf("list streams: %w", err)
	}
	defer rows.Close()

	var streams []Stream
	for rows.Next() {
		var s Stream
		var parentID sql.NullString
		var forkSeq sql.NullInt64

		if err := rows.Scan(&s.ID, &s.Name, &s.HeadSeq, &s.CreatedAt, &s.UpdatedAt, &parentID, &forkSeq, &s.Status); err != nil {
			return nil, fmt.Errorf("scan stream: %w", err)
		}

		if parentID.Valid {
			s.ParentID = parentID.String
		}
		if forkSeq.Valid {
			s.ForkSeq = forkSeq.Int64
		}

		streams = append(streams, s)
	}

	return streams, rows.Err()
}

// SetActive sets the active stream name in metadata.
func (sm *StreamManager) SetActive(name string) error {
	// Verify stream exists
	if _, err := sm.GetByName(name); err != nil {
		return err
	}
	_, err := sm.db.Exec("INSERT OR REPLACE INTO metadata (key, value) VALUES ('active_stream', ?)", name)
	return err
}

// ActiveName returns the active stream name.
func (sm *StreamManager) ActiveName() (string, error) {
	var name string
	err := sm.db.QueryRow("SELECT value FROM metadata WHERE key = 'active_stream'").Scan(&name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "main", nil
		}
		return "", err
	}
	return name, nil
}

// SetStatus updates a stream status by id.
func (sm *StreamManager) SetStatus(id, status string) error {
	res, err := sm.db.Exec("UPDATE streams SET status = ?, updated_at = ? WHERE id = ?", status, Now(), id)
	if err != nil {
		return fmt.Errorf("set stream status: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set stream status: rows affected: %w", err)
	}
	if rows == 0 {
		return ErrStreamNotFound
	}
	return nil
}
