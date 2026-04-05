package core

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// CheckpointEngine manages checkpoint creation and querying.
type CheckpointEngine struct {
	db     *sql.DB
	reader *OpReader
}

// NewCheckpointEngine creates a new checkpoint engine.
func NewCheckpointEngine(db *sql.DB, reader *OpReader) *CheckpointEngine {
	return &CheckpointEngine{db: db, reader: reader}
}

// CheckpointInput is the input for creating a checkpoint.
type CheckpointInput struct {
	StreamID string
	Title    string
	Summary  string
	Author   string
	Source   CheckpointSource
	Tags     map[string]string
}

// Create creates a new checkpoint at the current stream head.
func (ce *CheckpointEngine) Create(input CheckpointInput) (*Checkpoint, error) {
	// Get stream head
	var headSeq int64
	err := ce.db.QueryRow("SELECT head_seq FROM streams WHERE id = ?", input.StreamID).Scan(&headSeq)
	if err != nil {
		return nil, fmt.Errorf("get stream head: %w", err)
	}

	// Find previous checkpoint for parent_id
	var parentID sql.NullString
	var parentSeq int64
	ce.db.QueryRow(
		"SELECT id, seq FROM checkpoints WHERE stream_id = ? ORDER BY seq DESC LIMIT 1",
		input.StreamID,
	).Scan(&parentID, &parentSeq)

	// Count ops per space and type since last checkpoint
	spaceCounts, _ := ce.reader.CountBySpace(input.StreamID, parentSeq)

	// Build space states
	var spaces []SpaceState
	for spaceID, counts := range spaceCounts {
		spaces = append(spaces, SpaceState{
			SpaceID: spaceID,
			Status:  SpaceChanged,
			Summary: SpaceSummary{
				EntitiesCreated:  counts.Created,
				EntitiesModified: counts.Modified,
				EntitiesDeleted:  counts.Deleted,
			},
		})
	}

	cp := &Checkpoint{
		ID:        NewID(),
		StreamID:  input.StreamID,
		Seq:       headSeq,
		Title:     input.Title,
		Summary:   input.Summary,
		Author:    input.Author,
		Timestamp: Now(),
		Source:    input.Source,
		Spaces:    spaces,
		Tags:      input.Tags,
	}
	if parentID.Valid {
		cp.ParentID = parentID.String
	}

	spacesJSON := MarshalJSON(cp.Spaces)
	tagsJSON := MarshalJSON(cp.Tags)

	_, err = ce.db.Exec(`
		INSERT INTO checkpoints (id, stream_id, seq, title, summary, author, timestamp, source, spaces, tags, parent_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cp.ID, cp.StreamID, cp.Seq, cp.Title, cp.Summary, cp.Author,
		cp.Timestamp, string(cp.Source), spacesJSON, tagsJSON, cp.ParentID,
	)
	if err != nil {
		return nil, fmt.Errorf("insert checkpoint: %w", err)
	}

	return cp, nil
}

// Get returns a checkpoint by ID.
func (ce *CheckpointEngine) Get(id string) (*Checkpoint, error) {
	cp := &Checkpoint{}
	var spacesJSON, tagsJSON string
	var summary, parentID sql.NullString

	err := ce.db.QueryRow(
		"SELECT id, stream_id, seq, title, summary, author, timestamp, source, spaces, tags, parent_id FROM checkpoints WHERE id = ?",
		id,
	).Scan(&cp.ID, &cp.StreamID, &cp.Seq, &cp.Title, &summary, &cp.Author,
		&cp.Timestamp, &cp.Source, &spacesJSON, &tagsJSON, &parentID)
	if err != nil {
		return nil, fmt.Errorf("get checkpoint: %w", err)
	}

	if summary.Valid {
		cp.Summary = summary.String
	}
	if parentID.Valid {
		cp.ParentID = parentID.String
	}
	json.Unmarshal([]byte(spacesJSON), &cp.Spaces)
	json.Unmarshal([]byte(tagsJSON), &cp.Tags)

	return cp, nil
}

// List returns checkpoints for a stream, ordered by sequence descending.
func (ce *CheckpointEngine) List(streamID string, limit int) ([]Checkpoint, error) {
	rows, err := ce.db.Query(
		"SELECT id, stream_id, seq, title, summary, author, timestamp, source, spaces, tags, parent_id FROM checkpoints WHERE stream_id = ? ORDER BY seq DESC LIMIT ?",
		streamID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list checkpoints: %w", err)
	}
	defer rows.Close()

	return scanCheckpoints(rows)
}

// ListAll returns all checkpoints across streams, ordered by sequence descending.
func (ce *CheckpointEngine) ListAll(limit int) ([]Checkpoint, error) {
	rows, err := ce.db.Query(
		"SELECT id, stream_id, seq, title, summary, author, timestamp, source, spaces, tags, parent_id FROM checkpoints ORDER BY seq DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list all checkpoints: %w", err)
	}
	defer rows.Close()

	return scanCheckpoints(rows)
}

// Search performs full-text search on checkpoint titles and summaries.
func (ce *CheckpointEngine) Search(query string, limits ...int) ([]Checkpoint, error) {
	limit := -1
	if len(limits) > 0 {
		limit = limits[0]
		if limit <= 0 {
			limit = -1
		}
	}

	rows, err := ce.db.Query(`
		SELECT c.id, c.stream_id, c.seq, c.title, c.summary, c.author, c.timestamp, c.source, c.spaces, c.tags, c.parent_id
		FROM checkpoints c
		JOIN checkpoints_fts f ON c.id = f.id
		WHERE checkpoints_fts MATCH ?
		ORDER BY c.seq DESC`,
		query,
	)
	if err != nil {
		return nil, fmt.Errorf("search checkpoints: %w", err)
	}
	defer rows.Close()

	checkpoints, err := scanCheckpoints(rows)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(checkpoints) > limit {
		checkpoints = checkpoints[:limit]
	}
	return checkpoints, nil
}

// CountSinceCheckpoint returns operation counts per space since the latest checkpoint.
func (ce *CheckpointEngine) CountSinceCheckpoint(streamID string) (map[string]*SpaceOpCounts, error) {
	lastSeq := ce.LatestSeq(streamID)
	return ce.reader.CountBySpace(streamID, lastSeq)
}

// LatestSeq returns the sequence number of the latest checkpoint for a stream.
func (ce *CheckpointEngine) LatestSeq(streamID string) int64 {
	var seq int64
	ce.db.QueryRow(
		"SELECT COALESCE(MAX(seq), 0) FROM checkpoints WHERE stream_id = ?",
		streamID,
	).Scan(&seq)
	return seq
}

func scanCheckpoints(rows *sql.Rows) ([]Checkpoint, error) {
	var checkpoints []Checkpoint
	for rows.Next() {
		var cp Checkpoint
		var spacesJSON, tagsJSON string
		var summary, parentID sql.NullString

		err := rows.Scan(
			&cp.ID, &cp.StreamID, &cp.Seq, &cp.Title, &summary, &cp.Author,
			&cp.Timestamp, &cp.Source, &spacesJSON, &tagsJSON, &parentID,
		)
		if err != nil {
			return nil, fmt.Errorf("scan checkpoint: %w", err)
		}

		if summary.Valid {
			cp.Summary = summary.String
		}
		if parentID.Valid {
			cp.ParentID = parentID.String
		}
		json.Unmarshal([]byte(spacesJSON), &cp.Spaces)
		json.Unmarshal([]byte(tagsJSON), &cp.Tags)

		checkpoints = append(checkpoints, cp)
	}
	return checkpoints, rows.Err()
}
