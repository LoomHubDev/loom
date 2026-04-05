package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"

	lsync "github.com/constructspace/loom/internal/sync"
)

func (s *HubServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	token, err := s.users.Authenticate(req.Username, req.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

func (s *HubServer) handleNegotiate(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	name := r.PathValue("name")

	var req lsync.NegotiateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	pdb, err := s.projects.GetOrCreate(owner, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "project store error: "+err.Error())
		return
	}

	commonSeqs := make(map[string]int64)
	serverSeqs := make(map[string]int64)
	needsPush := false
	needsPull := false

	for _, cs := range req.Streams {
		var serverHead int64
		err := pdb.db.QueryRow(
			"SELECT head_seq FROM streams WHERE id = ?", cs.StreamID,
		).Scan(&serverHead)
		if err == sql.ErrNoRows {
			// Server doesn't have this stream yet.
			serverHead = 0
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "query stream: "+err.Error())
			return
		}

		serverSeqs[cs.StreamID] = serverHead

		// Common seq is the minimum of both sides.
		common := cs.HeadSeq
		if serverHead < common {
			common = serverHead
		}
		commonSeqs[cs.StreamID] = common

		if cs.HeadSeq > serverHead {
			needsPush = true
		}
		if serverHead > cs.HeadSeq {
			needsPull = true
		}
	}

	// Also check for server streams the client doesn't know about.
	clientStreamSet := make(map[string]bool, len(req.Streams))
	for _, cs := range req.Streams {
		clientStreamSet[cs.StreamID] = true
	}
	rows, err := pdb.db.Query("SELECT id, head_seq FROM streams")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var sid string
			var head int64
			if err := rows.Scan(&sid, &head); err != nil {
				continue
			}
			if !clientStreamSet[sid] {
				serverSeqs[sid] = head
				commonSeqs[sid] = 0
				if head > 0 {
					needsPull = true
				}
			}
		}
	}

	resp := lsync.NegotiateResponse{
		CommonSeqs: commonSeqs,
		ServerSeqs: serverSeqs,
		NeedsPush:  needsPush,
		NeedsPull:  needsPull,
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *HubServer) handlePush(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	name := r.PathValue("name")

	var req lsync.PushRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	pdb, err := s.projects.GetOrCreate(owner, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "project store error: "+err.Error())
		return
	}

	// Store objects first.
	for _, obj := range req.Objects {
		if _, err := pdb.store.Write(obj.Content, ""); err != nil {
			writeError(w, http.StatusInternalServerError, "store object: "+err.Error())
			return
		}
	}

	// Ensure stream exists.
	if _, err := pdb.db.Exec(
		"INSERT OR IGNORE INTO streams (id, name, head_seq) VALUES (?, ?, 0)",
		req.StreamID, req.StreamID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "ensure stream: "+err.Error())
		return
	}

	// Insert operations.
	applied := 0
	var maxSeq int64
	for _, op := range req.Operations {
		deltaBytes := []byte(nil)
		if op.Delta != nil {
			deltaBytes = []byte(op.Delta)
		}
		metaBytes := []byte(nil)
		if op.Meta != nil {
			metaBytes = []byte(op.Meta)
		}

		res, err := pdb.db.Exec(`
			INSERT OR IGNORE INTO operations
				(id, seq, stream_id, space_id, entity_id, type, path, delta, object_ref, parent_seq, author, timestamp, meta)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			op.ID, op.Seq, op.StreamID, op.SpaceID, op.EntityID,
			op.Type, op.Path, deltaBytes, op.ObjectRef,
			op.ParentSeq, op.Author, op.Timestamp, metaBytes,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "insert op: "+err.Error())
			return
		}
		n, _ := res.RowsAffected()
		applied += int(n)
		if op.Seq > maxSeq {
			maxSeq = op.Seq
		}
	}

	// Update stream head_seq and the global seq_counter.
	if maxSeq > 0 {
		pdb.db.Exec(
			"UPDATE streams SET head_seq = MAX(head_seq, ?), updated_at = datetime('now') WHERE id = ?",
			maxSeq, req.StreamID,
		)
		pdb.db.Exec(
			"UPDATE metadata SET value = CAST(MAX(CAST(value AS INTEGER), ?) AS TEXT) WHERE key = 'seq_counter'",
			maxSeq,
		)
	}

	// Read back the current head.
	var serverHead int64
	pdb.db.QueryRow("SELECT head_seq FROM streams WHERE id = ?", req.StreamID).Scan(&serverHead)

	writeJSON(w, http.StatusOK, lsync.PushResponse{
		OK:         true,
		Applied:    applied,
		ServerHead: serverHead,
	})
}

func (s *HubServer) handlePull(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	name := r.PathValue("name")

	var req lsync.PullRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	pdb, err := s.projects.GetOrCreate(owner, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "project store error: "+err.Error())
		return
	}

	rows, err := pdb.db.Query(`
		SELECT id, seq, stream_id, space_id, entity_id, type, path,
		       delta, object_ref, parent_seq, author, timestamp, meta
		FROM operations
		WHERE stream_id = ? AND seq > ?
		ORDER BY seq ASC`,
		req.StreamID, req.FromSeq,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query ops: "+err.Error())
		return
	}
	defer rows.Close()

	var ops []lsync.OperationWire
	objectRefs := make(map[string]bool)

	for rows.Next() {
		var op lsync.OperationWire
		var delta, meta []byte
		var objectRef sql.NullString
		err := rows.Scan(
			&op.ID, &op.Seq, &op.StreamID, &op.SpaceID, &op.EntityID,
			&op.Type, &op.Path, &delta, &objectRef,
			&op.ParentSeq, &op.Author, &op.Timestamp, &meta,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "scan op: "+err.Error())
			return
		}
		if len(delta) > 0 {
			op.Delta = json.RawMessage(delta)
		}
		if len(meta) > 0 {
			op.Meta = json.RawMessage(meta)
		}
		if objectRef.Valid {
			op.ObjectRef = objectRef.String
			if op.ObjectRef != "" {
				objectRefs[op.ObjectRef] = true
			}
		}
		ops = append(ops, op)
	}

	// Collect referenced objects.
	var objects []lsync.ObjectData
	for ref := range objectRefs {
		content, err := pdb.store.Read(ref)
		if err != nil {
			// Object might not exist; skip it rather than fail the whole pull.
			continue
		}
		objects = append(objects, lsync.ObjectData{
			Hash:    ref,
			Content: content,
		})
	}

	// Read server head.
	var serverHead int64
	pdb.db.QueryRow("SELECT head_seq FROM streams WHERE id = ?", req.StreamID).Scan(&serverHead)

	resp := lsync.PullResponse{
		Operations: ops,
		Objects:    objects,
		ServerHead: serverHead,
	}
	if resp.Operations == nil {
		resp.Operations = []lsync.OperationWire{}
	}
	if resp.Objects == nil {
		resp.Objects = []lsync.ObjectData{}
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"error":   fmt.Sprintf("%d", status),
		"message": message,
	})
}
