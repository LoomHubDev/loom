package core

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewID_Unique(t *testing.T) {
	ids := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		id := NewID()
		assert.NotEmpty(t, id)
		assert.Len(t, id, 26, "ULID should be 26 chars")
		assert.False(t, ids[id], "duplicate ID generated: %s", id)
		ids[id] = true
	}
}

func TestNewID_Monotonic(t *testing.T) {
	id1 := NewID()
	id2 := NewID()
	// ULIDs are lexicographically sortable
	assert.True(t, id1 <= id2, "IDs should be monotonically increasing: %s > %s", id1, id2)
}

func TestNow_Format(t *testing.T) {
	ts := Now()
	_, err := time.Parse(time.RFC3339, ts)
	require.NoError(t, err, "Now() should return valid RFC3339: %s", ts)
}

func TestNow_UTC(t *testing.T) {
	ts := Now()
	assert.True(t, strings.HasSuffix(ts, "Z"), "Now() should be UTC (end with Z): %s", ts)
}

func TestNow_RecentTimestamp(t *testing.T) {
	before := time.Now().UTC()
	ts := Now()
	after := time.Now().UTC()

	parsed, err := time.Parse(time.RFC3339, ts)
	require.NoError(t, err)
	assert.False(t, parsed.Before(before.Add(-time.Second)))
	assert.False(t, parsed.After(after.Add(time.Second)))
}

// --- OpType constants ---

func TestOpType_Values(t *testing.T) {
	assert.Equal(t, OpType("create"), OpCreate)
	assert.Equal(t, OpType("modify"), OpModify)
	assert.Equal(t, OpType("delete"), OpDelete)
	assert.Equal(t, OpType("move"), OpMove)
	assert.Equal(t, OpType("rename"), OpRename)
}

// --- CheckpointSource constants ---

func TestCheckpointSource_Values(t *testing.T) {
	assert.Equal(t, CheckpointSource("manual"), SourceManual)
	assert.Equal(t, CheckpointSource("auto"), SourceAuto)
	assert.Equal(t, CheckpointSource("agent"), SourceAgent)
	assert.Equal(t, CheckpointSource("workflow"), SourceWorkflow)
	assert.Equal(t, CheckpointSource("guard"), SourceGuard)
	assert.Equal(t, CheckpointSource("restore"), SourceRestore)
}

// --- ChangeType constants ---

func TestChangeType_Values(t *testing.T) {
	assert.Equal(t, ChangeType("created"), ChangeCreated)
	assert.Equal(t, ChangeType("modified"), ChangeModified)
	assert.Equal(t, ChangeType("deleted"), ChangeDeleted)
	assert.Equal(t, ChangeType("moved"), ChangeMoved)
	assert.Equal(t, ChangeType("none"), ChangeNone)
}

// --- SpaceStatus constants ---

func TestSpaceStatus_Values(t *testing.T) {
	assert.Equal(t, SpaceStatus("changed"), SpaceChanged)
	assert.Equal(t, SpaceStatus("unchanged"), SpaceUnchanged)
}

// --- MarshalJSON ---

func TestMarshalJSON_ValidStruct(t *testing.T) {
	op := OpMeta{Size: 100, ContentType: "text/plain", Source: "test"}
	result := MarshalJSON(op)
	assert.Contains(t, result, `"size":100`)
	assert.Contains(t, result, `"content_type":"text/plain"`)
}

func TestMarshalJSON_EmptyStruct(t *testing.T) {
	result := MarshalJSON(OpMeta{})
	assert.NotEmpty(t, result)
	var parsed map[string]interface{}
	err := json.Unmarshal([]byte(result), &parsed)
	assert.NoError(t, err)
}

func TestMarshalJSON_NilMap(t *testing.T) {
	result := MarshalJSON(map[string]string(nil))
	assert.Equal(t, "null", result)
}

func TestMarshalJSON_WithLabels(t *testing.T) {
	meta := OpMeta{
		Labels: map[string]string{"env": "prod", "team": "core"},
	}
	result := MarshalJSON(meta)
	assert.Contains(t, result, `"env":"prod"`)
	assert.Contains(t, result, `"team":"core"`)
}

func TestMarshalJSON_InvalidValue(t *testing.T) {
	// channels cannot be marshaled
	result := MarshalJSON(make(chan int))
	assert.Equal(t, "{}", result)
}

// --- Operation JSON roundtrip ---

func TestOperation_JSONRoundtrip(t *testing.T) {
	op := Operation{
		ID:        "test-id",
		Seq:       42,
		StreamID:  "stream-1",
		SpaceID:   "code",
		EntityID:  "main.go",
		Type:      OpCreate,
		Path:      "src/main.go",
		Delta:     []byte("diff content"),
		ObjectRef: "sha256-abc",
		ParentSeq: 41,
		Author:    "alice",
		Timestamp: "2024-01-01T00:00:00Z",
		Meta: OpMeta{
			OldPath:     "old/main.go",
			Size:        1024,
			ContentType: "text/x-go",
			Checksum:    "abc123",
			Source:      "editor",
			AgentID:     "agent-1",
			Labels:      map[string]string{"priority": "high"},
		},
	}

	data, err := json.Marshal(op)
	require.NoError(t, err)

	var decoded Operation
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, op.ID, decoded.ID)
	assert.Equal(t, op.Seq, decoded.Seq)
	assert.Equal(t, op.Type, decoded.Type)
	assert.Equal(t, op.Meta.OldPath, decoded.Meta.OldPath)
	assert.Equal(t, op.Meta.Labels["priority"], decoded.Meta.Labels["priority"])
}

func TestOperation_JSONOmitEmpty(t *testing.T) {
	op := Operation{
		ID:       "id",
		StreamID: "s1",
		SpaceID:  "code",
		EntityID: "f.go",
		Type:     OpCreate,
		Path:     "f.go",
		Author:   "test",
	}

	data, err := json.Marshal(op)
	require.NoError(t, err)

	// Delta and ObjectRef should be omitted when empty
	assert.NotContains(t, string(data), `"delta"`)
	assert.NotContains(t, string(data), `"object_ref"`)
}

// --- Checkpoint JSON roundtrip ---

func TestCheckpoint_JSONRoundtrip(t *testing.T) {
	cp := Checkpoint{
		ID:        "cp-1",
		StreamID:  "s1",
		Seq:       10,
		Title:     "test checkpoint",
		Summary:   "summary text",
		Author:    "bob",
		Timestamp: "2024-01-01T00:00:00Z",
		Source:    SourceAgent,
		Spaces: []SpaceState{
			{
				SpaceID: "code",
				Adapter: "git",
				Status:  SpaceChanged,
				Summary: SpaceSummary{EntitiesCreated: 3, EntitiesModified: 1},
				Entities: []EntityState{
					{ID: "e1", SpaceID: "code", Kind: "file", Path: "main.go", Change: ChangeCreated},
				},
			},
		},
		Tags:     map[string]string{"release": "v1.0"},
		ParentID: "cp-0",
	}

	data, err := json.Marshal(cp)
	require.NoError(t, err)

	var decoded Checkpoint
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, cp.Title, decoded.Title)
	assert.Equal(t, cp.Source, decoded.Source)
	assert.Equal(t, cp.Tags["release"], decoded.Tags["release"])
	assert.Len(t, decoded.Spaces, 1)
	assert.Equal(t, 3, decoded.Spaces[0].Summary.EntitiesCreated)
}

// --- Stream JSON ---

func TestStream_JSONRoundtrip(t *testing.T) {
	s := Stream{
		ID:        "s1",
		Name:      "feature/auth",
		HeadSeq:   42,
		CreatedAt: "2024-01-01T00:00:00Z",
		UpdatedAt: "2024-01-02T00:00:00Z",
		ParentID:  "s0",
		ForkSeq:   10,
		Status:    "active",
	}

	data, err := json.Marshal(s)
	require.NoError(t, err)

	var decoded Stream
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, s.Name, decoded.Name)
	assert.Equal(t, s.ParentID, decoded.ParentID)
	assert.Equal(t, s.ForkSeq, decoded.ForkSeq)
}

// --- ProjectConfig ---

func TestProjectConfig_JSONRoundtrip(t *testing.T) {
	cfg := ProjectConfig{
		Project: ProjectInfo{Name: "my-project", Version: 1},
		Author:  AuthorInfo{Name: "alice", Email: "alice@example.com"},
		Spaces: map[string]SpaceConfig{
			"code": {Adapter: "git", Path: "."},
			"docs": {Adapter: "filesystem", Path: "docs"},
		},
		Watch: WatchConfig{
			Enabled:    true,
			DebounceMs: 500,
			Ignore:     []string{".git", "node_modules"},
		},
		CPoint: CheckpointConfig{
			Auto:                true,
			IntervalOps:         50,
			IntervalSeconds:     300,
			OnSignificantChange: true,
		},
	}

	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	var decoded ProjectConfig
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "my-project", decoded.Project.Name)
	assert.Equal(t, "alice@example.com", decoded.Author.Email)
	assert.Len(t, decoded.Spaces, 2)
	assert.Equal(t, "git", decoded.Spaces["code"].Adapter)
	assert.True(t, decoded.Watch.Enabled)
	assert.Equal(t, 50, decoded.CPoint.IntervalOps)
}
