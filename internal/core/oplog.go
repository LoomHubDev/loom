package core

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/constructspace/loom/internal/storage"
)

// OpWriter serializes all writes to the operation log.
type OpWriter struct {
	db    *sql.DB
	store *storage.ObjectStore
	mu    sync.Mutex
}

var ErrStreamMissing = errors.New("stream not found")

// NewOpWriter creates a new operation writer.
func NewOpWriter(db *sql.DB, store *storage.ObjectStore) *OpWriter {
	return &OpWriter{db: db, store: store}
}

// Write records a single operation. Assigns ID and sequence number.
func (w *OpWriter) Write(op Operation) (Operation, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	tx, err := w.db.Begin()
	if err != nil {
		return op, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var seq int64
	err = tx.QueryRow(`
		UPDATE metadata SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT)
		WHERE key = 'seq_counter'
		RETURNING CAST(value AS INTEGER)
	`).Scan(&seq)
	if err != nil {
		return op, fmt.Errorf("get next seq: %w", err)
	}

	op.ID = NewID()
	op.Seq = seq
	if op.Timestamp == "" {
		op.Timestamp = Now()
	}

	metaJSON := MarshalJSON(op.Meta)

	_, err = tx.Exec(`
		INSERT INTO operations (id, seq, stream_id, space_id, entity_id, type, path, delta, object_ref, parent_seq, author, timestamp, meta)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		op.ID, op.Seq, op.StreamID, op.SpaceID, op.EntityID,
		string(op.Type), op.Path, op.Delta, op.ObjectRef, op.ParentSeq,
		op.Author, op.Timestamp, metaJSON,
	)
	if err != nil {
		return op, fmt.Errorf("insert operation: %w", err)
	}

	// Update stream head
	result, err := tx.Exec("UPDATE streams SET head_seq = ?, updated_at = ? WHERE id = ?",
		op.Seq, op.Timestamp, op.StreamID)
	if err != nil {
		return op, fmt.Errorf("update stream head: %w", err)
	}
	if err := requireUpdatedStream(result, op.StreamID); err != nil {
		return op, fmt.Errorf("update stream head: %w", err)
	}

	// Upsert entity state
	entityStatus := "active"
	if op.Type == OpDelete {
		entityStatus = "deleted"
	}
	_, err = tx.Exec(`
		INSERT INTO entities (id, space_id, path, kind, object_ref, size, mod_time, status)
		VALUES (?, ?, ?, 'file', ?, ?, ?, ?)
		ON CONFLICT(id, space_id) DO UPDATE SET
			path = excluded.path,
			object_ref = COALESCE(excluded.object_ref, entities.object_ref),
			size = COALESCE(excluded.size, entities.size),
			mod_time = excluded.mod_time,
			status = excluded.status,
			updated_at = datetime('now')`,
		op.EntityID, op.SpaceID, op.Path, op.ObjectRef, op.Meta.Size, op.Timestamp, entityStatus)
	if err != nil {
		return op, fmt.Errorf("upsert entity: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return op, fmt.Errorf("commit: %w", err)
	}

	return op, nil
}

// WriteBatch writes multiple operations atomically.
func (w *OpWriter) WriteBatch(ops []Operation) ([]Operation, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	tx, err := w.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	for i := range ops {
		// Increment seq inside the transaction to avoid lock contention
		var seq int64
		err := tx.QueryRow(`
			UPDATE metadata SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT)
			WHERE key = 'seq_counter'
			RETURNING CAST(value AS INTEGER)
		`).Scan(&seq)
		if err != nil {
			return nil, fmt.Errorf("get next seq: %w", err)
		}

		ops[i].ID = NewID()
		ops[i].Seq = seq
		if ops[i].Timestamp == "" {
			ops[i].Timestamp = Now()
		}

		metaJSON := MarshalJSON(ops[i].Meta)

		_, err = tx.Exec(`
			INSERT INTO operations (id, seq, stream_id, space_id, entity_id, type, path, delta, object_ref, parent_seq, author, timestamp, meta)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			ops[i].ID, ops[i].Seq, ops[i].StreamID, ops[i].SpaceID, ops[i].EntityID,
			string(ops[i].Type), ops[i].Path, ops[i].Delta, ops[i].ObjectRef, ops[i].ParentSeq,
			ops[i].Author, ops[i].Timestamp, metaJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("insert operation %d: %w", i, err)
		}

		// Update stream head
		result, err := tx.Exec("UPDATE streams SET head_seq = ?, updated_at = ? WHERE id = ?",
			ops[i].Seq, ops[i].Timestamp, ops[i].StreamID)
		if err != nil {
			return nil, fmt.Errorf("update stream head %d: %w", i, err)
		}
		if err := requireUpdatedStream(result, ops[i].StreamID); err != nil {
			return nil, fmt.Errorf("update stream head %d: %w", i, err)
		}

		// Upsert entity state
		entityStatus := "active"
		if ops[i].Type == OpDelete {
			entityStatus = "deleted"
		}
		_, err = tx.Exec(`
			INSERT INTO entities (id, space_id, path, kind, object_ref, size, mod_time, status)
			VALUES (?, ?, ?, 'file', ?, ?, ?, ?)
			ON CONFLICT(id, space_id) DO UPDATE SET
				path = excluded.path,
				object_ref = COALESCE(excluded.object_ref, entities.object_ref),
				size = COALESCE(excluded.size, entities.size),
				mod_time = excluded.mod_time,
				status = excluded.status,
				updated_at = datetime('now')`,
			ops[i].EntityID, ops[i].SpaceID, ops[i].Path, ops[i].ObjectRef, ops[i].Meta.Size, ops[i].Timestamp, entityStatus)
		if err != nil {
			return nil, fmt.Errorf("upsert entity %d: %w", i, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit batch: %w", err)
	}

	return ops, nil
}

// ImportBatch writes remote operations using their existing IDs and sequence numbers.
func (w *OpWriter) ImportBatch(ops []Operation) ([]Operation, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	tx, err := w.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var maxSeq int64

	for i := range ops {
		if ops[i].ID == "" {
			return nil, fmt.Errorf("import operation %d: missing id", i)
		}
		if ops[i].Seq <= 0 {
			return nil, fmt.Errorf("import operation %d: invalid seq %d", i, ops[i].Seq)
		}
		if ops[i].Timestamp == "" {
			ops[i].Timestamp = Now()
		}

		metaJSON := MarshalJSON(ops[i].Meta)

		_, err = tx.Exec(`
			INSERT INTO operations (id, seq, stream_id, space_id, entity_id, type, path, delta, object_ref, parent_seq, author, timestamp, meta)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			ops[i].ID, ops[i].Seq, ops[i].StreamID, ops[i].SpaceID, ops[i].EntityID,
			string(ops[i].Type), ops[i].Path, ops[i].Delta, ops[i].ObjectRef, ops[i].ParentSeq,
			ops[i].Author, ops[i].Timestamp, metaJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("import operation %d: %w", i, err)
		}

		result, err := tx.Exec(`
			UPDATE streams
			SET head_seq = CASE WHEN head_seq < ? THEN ? ELSE head_seq END,
			    updated_at = CASE WHEN head_seq < ? THEN ? ELSE updated_at END
			WHERE id = ?`,
			ops[i].Seq, ops[i].Seq, ops[i].Seq, ops[i].Timestamp, ops[i].StreamID,
		)
		if err != nil {
			return nil, fmt.Errorf("update stream head %d: %w", i, err)
		}
		if err := requireUpdatedStream(result, ops[i].StreamID); err != nil {
			return nil, fmt.Errorf("update stream head %d: %w", i, err)
		}

		entityStatus := "active"
		if ops[i].Type == OpDelete {
			entityStatus = "deleted"
		}
		_, err = tx.Exec(`
			INSERT INTO entities (id, space_id, path, kind, object_ref, size, mod_time, status)
			VALUES (?, ?, ?, 'file', ?, ?, ?, ?)
			ON CONFLICT(id, space_id) DO UPDATE SET
				path = excluded.path,
				object_ref = COALESCE(excluded.object_ref, entities.object_ref),
				size = COALESCE(excluded.size, entities.size),
				mod_time = excluded.mod_time,
				status = excluded.status,
				updated_at = datetime('now')`,
			ops[i].EntityID, ops[i].SpaceID, ops[i].Path, ops[i].ObjectRef, ops[i].Meta.Size, ops[i].Timestamp, entityStatus)
		if err != nil {
			return nil, fmt.Errorf("upsert entity %d: %w", i, err)
		}

		if ops[i].Seq > maxSeq {
			maxSeq = ops[i].Seq
		}
	}

	if maxSeq > 0 {
		_, err = tx.Exec(`
			UPDATE metadata
			SET value = CAST(MAX(CAST(value AS INTEGER), ?) AS TEXT)
			WHERE key = 'seq_counter'`,
			maxSeq,
		)
		if err != nil {
			return nil, fmt.Errorf("advance seq counter: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit import: %w", err)
	}

	return ops, nil
}

// OpReader provides read access to the operation log.
type OpReader struct {
	db *sql.DB
}

// NewOpReader creates a new operation reader.
func NewOpReader(db *sql.DB) *OpReader {
	return &OpReader{db: db}
}

// Head returns the current sequence number.
func (r *OpReader) Head() (int64, error) {
	var seq int64
	err := r.db.QueryRow("SELECT CAST(value AS INTEGER) FROM metadata WHERE key = 'seq_counter'").Scan(&seq)
	if err != nil {
		return 0, fmt.Errorf("read head: %w", err)
	}
	return seq, nil
}

// ReadRange returns operations between fromSeq (exclusive) and toSeq (inclusive).
func (r *OpReader) ReadRange(fromSeq, toSeq int64) ([]Operation, error) {
	rows, err := r.db.Query(
		"SELECT id, seq, stream_id, space_id, entity_id, type, path, delta, object_ref, parent_seq, author, timestamp, meta FROM operations WHERE seq > ? AND seq <= ? ORDER BY seq ASC",
		fromSeq, toSeq,
	)
	if err != nil {
		return nil, fmt.Errorf("query range: %w", err)
	}
	defer rows.Close()
	return scanOperations(rows)
}

// ReadByStream returns operations for a specific stream.
func (r *OpReader) ReadByStream(streamID string, fromSeq, toSeq int64) ([]Operation, error) {
	rows, err := r.db.Query(
		"SELECT id, seq, stream_id, space_id, entity_id, type, path, delta, object_ref, parent_seq, author, timestamp, meta FROM operations WHERE stream_id = ? AND seq > ? AND seq <= ? ORDER BY seq ASC",
		streamID, fromSeq, toSeq,
	)
	if err != nil {
		return nil, fmt.Errorf("query by stream: %w", err)
	}
	defer rows.Close()
	return scanOperations(rows)
}

// ReadBySpace returns operations for a specific space.
func (r *OpReader) ReadBySpace(spaceID string, fromSeq, toSeq int64) ([]Operation, error) {
	rows, err := r.db.Query(
		"SELECT id, seq, stream_id, space_id, entity_id, type, path, delta, object_ref, parent_seq, author, timestamp, meta FROM operations WHERE space_id = ? AND seq > ? AND seq <= ? ORDER BY seq ASC",
		spaceID, fromSeq, toSeq,
	)
	if err != nil {
		return nil, fmt.Errorf("query by space: %w", err)
	}
	defer rows.Close()
	return scanOperations(rows)
}

// ReadByEntity returns all operations for a specific entity.
func (r *OpReader) ReadByEntity(entityID string) ([]Operation, error) {
	rows, err := r.db.Query(
		"SELECT id, seq, stream_id, space_id, entity_id, type, path, delta, object_ref, parent_seq, author, timestamp, meta FROM operations WHERE entity_id = ? ORDER BY seq ASC",
		entityID,
	)
	if err != nil {
		return nil, fmt.Errorf("query by entity: %w", err)
	}
	defer rows.Close()
	return scanOperations(rows)
}

// SpaceOpCounts holds per-type operation counts for a space.
type SpaceOpCounts struct {
	Created  int
	Modified int
	Deleted  int
}

// CountBySpace returns operation counts per space and type since a sequence.
func (r *OpReader) CountBySpace(streamID string, sinceSeq int64) (map[string]*SpaceOpCounts, error) {
	rows, err := r.db.Query(
		"SELECT space_id, type, COUNT(*) FROM operations WHERE stream_id = ? AND seq > ? GROUP BY space_id, type",
		streamID, sinceSeq,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]*SpaceOpCounts)
	for rows.Next() {
		var space, opType string
		var count int
		if err := rows.Scan(&space, &opType, &count); err != nil {
			return nil, err
		}
		if result[space] == nil {
			result[space] = &SpaceOpCounts{}
		}
		switch OpType(opType) {
		case OpCreate:
			result[space].Created = count
		case OpDelete:
			result[space].Deleted = count
		default:
			result[space].Modified += count
		}
	}
	return result, nil
}

func requireUpdatedStream(result sql.Result, streamID string) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("%w: %s", ErrStreamMissing, streamID)
	}
	return nil
}

func scanOperations(rows *sql.Rows) ([]Operation, error) {
	var ops []Operation
	for rows.Next() {
		var op Operation
		var opType, metaJSON string
		var delta []byte
		var objectRef sql.NullString

		err := rows.Scan(
			&op.ID, &op.Seq, &op.StreamID, &op.SpaceID, &op.EntityID,
			&opType, &op.Path, &delta, &objectRef, &op.ParentSeq,
			&op.Author, &op.Timestamp, &metaJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("scan operation: %w", err)
		}

		op.Type = OpType(opType)
		op.Delta = delta
		if objectRef.Valid {
			op.ObjectRef = objectRef.String
		}
		json.Unmarshal([]byte(metaJSON), &op.Meta)

		ops = append(ops, op)
	}
	return ops, rows.Err()
}
