package diff

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/constructspace/loom/internal/core"
)

// DiffRef is a parsed reference to a point in the oplog.
type DiffRef struct {
	Type  string // "checkpoint", "seq", "head", "relative"
	Value string
}

// String returns a human-readable representation of the ref.
func (r DiffRef) String() string {
	switch r.Type {
	case "head":
		return "HEAD"
	case "relative":
		return r.Value
	default:
		return r.Value
	}
}

// ParseRef parses a string reference into a DiffRef.
//
// Rules:
//   - "HEAD" or "head"      → Type="head", Value=""
//   - "HEAD~N" or "head~N"  → Type="relative", Value="HEAD~N"
//   - all-digit string       → Type="seq", Value=s
//   - anything else          → Type="checkpoint", Value=s (checkpoint ID)
func ParseRef(s string) DiffRef {
	upper := strings.ToUpper(s)

	if upper == "HEAD" {
		return DiffRef{Type: "head", Value: ""}
	}

	if strings.HasPrefix(upper, "HEAD~") {
		return DiffRef{Type: "relative", Value: upper}
	}

	// Check if all digits.
	allDigits := len(s) > 0
	for _, c := range s {
		if c < '0' || c > '9' {
			allDigits = false
			break
		}
	}
	if allDigits {
		return DiffRef{Type: "seq", Value: s}
	}

	return DiffRef{Type: "checkpoint", Value: s}
}

// ResolveRef resolves a DiffRef to a sequence number.
func ResolveRef(ref DiffRef, db *sql.DB, reader *core.OpReader) (int64, error) {
	switch ref.Type {
	case "head":
		return reader.Head()

	case "seq":
		seq, err := strconv.ParseInt(ref.Value, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid seq %q: %w", ref.Value, err)
		}
		return seq, nil

	case "checkpoint":
		var seq int64
		err := db.QueryRow("SELECT seq FROM checkpoints WHERE id = ?", ref.Value).Scan(&seq)
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("checkpoint not found: %s", ref.Value)
		}
		if err != nil {
			return 0, fmt.Errorf("query checkpoint %s: %w", ref.Value, err)
		}
		return seq, nil

	case "relative":
		// Parse N from "HEAD~N"
		parts := strings.SplitN(strings.ToUpper(ref.Value), "~", 2)
		if len(parts) != 2 {
			return 0, fmt.Errorf("invalid relative ref: %s", ref.Value)
		}
		n, err := strconv.Atoi(parts[1])
		if err != nil {
			return 0, fmt.Errorf("invalid offset in ref %s: %w", ref.Value, err)
		}

		var seq int64
		err = db.QueryRow(
			"SELECT seq FROM checkpoints ORDER BY seq DESC LIMIT 1 OFFSET ?", n,
		).Scan(&seq)
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("not enough checkpoints for %s", ref.Value)
		}
		if err != nil {
			return 0, fmt.Errorf("query relative ref %s: %w", ref.Value, err)
		}
		return seq, nil

	default:
		return 0, fmt.Errorf("unknown ref type: %s", ref.Type)
	}
}
