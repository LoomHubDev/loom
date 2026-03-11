# 06 — Systems: Diff Engine

## Overview

Loom's diff engine is multi-layered and content-aware. Unlike Git's line-based text diff, Loom delegates to space adapters for semantic diffing while providing a unified diff interface.

## Diff Layers

```
┌────────────────────────────────┐
│        Project Diff            │  What spaces changed?
│  "3 spaces changed"           │
├────────────────────────────────┤
│        Space Diff              │  What entities changed?
│  "code: 5 files modified"     │
│  "docs: 2 files modified"     │
├────────────────────────────────┤
│        Entity Diff             │  What exactly changed?
│  "src/auth.go: +12 -3 lines"  │
│  "docs/auth.md: section moved"│
└────────────────────────────────┘
```

## Diff Interface

```go
type DiffEngine struct {
    reader   *OpReader
    registry *AdapterRegistry
    store    *ObjectStore
}

// Diff between two points (checkpoints, sequences, or special refs)
func (d *DiffEngine) Diff(from, to DiffRef, opts DiffOptions) (*DiffResult, error) {
    fromSeq, err := d.resolveRef(from)
    if err != nil {
        return nil, err
    }
    toSeq, err := d.resolveRef(to)
    if err != nil {
        return nil, err
    }

    ops, _ := d.reader.ReadRange(fromSeq, toSeq)

    // Group by space
    spaceOps := groupBySpace(ops)

    var spaceDiffs []SpaceDiff
    for spaceID, sops := range spaceOps {
        if opts.SpaceID != "" && spaceID != opts.SpaceID {
            continue // Filter by space
        }
        sd, _ := d.diffSpace(spaceID, sops, opts)
        spaceDiffs = append(spaceDiffs, sd)
    }

    return &DiffResult{
        FromSeq: fromSeq,
        ToSeq:   toSeq,
        FromRef: from.String(),
        ToRef:   to.String(),
        Spaces:  spaceDiffs,
        Summary: buildDiffSummary(spaceDiffs),
    }, nil
}
```

### DiffRef — Flexible Reference

```go
type DiffRef struct {
    Type  string // "checkpoint", "seq", "head", "relative"
    Value string // Checkpoint ID, sequence number, or relative ref
}

func (d *DiffEngine) resolveRef(ref DiffRef) (int64, error) {
    switch ref.Type {
    case "checkpoint":
        var seq int64
        d.db.QueryRow("SELECT seq FROM checkpoints WHERE id = ?", ref.Value).Scan(&seq)
        return seq, nil
    case "seq":
        return strconv.ParseInt(ref.Value, 10, 64)
    case "head":
        return d.reader.Head()
    case "relative":
        // HEAD~3 means 3 checkpoints back
        n, _ := strconv.Atoi(strings.TrimPrefix(ref.Value, "HEAD~"))
        var seq int64
        d.db.QueryRow(
            "SELECT seq FROM checkpoints ORDER BY seq DESC LIMIT 1 OFFSET ?", n,
        ).Scan(&seq)
        return seq, nil
    }
    return 0, fmt.Errorf("unknown ref type: %s", ref.Type)
}
```

### DiffOptions

```go
type DiffOptions struct {
    SpaceID   string   // Filter to one space
    EntityID  string   // Filter to one entity
    Format    string   // "text", "json", "patch"
    Context   int      // Lines of context (for text diffs)
    Summary   bool     // Only show summary, not full diff
    Color     bool     // ANSI color output
}
```

## Per-Space Diffing

### Text Diff (Code, Docs, Notes, Config)

Uses the Myers diff algorithm for line-level diffing:

```go
func (d *DiffEngine) diffText(oldContent, newContent []byte, path string, opts DiffOptions) *DiffOutput {
    oldLines := strings.Split(string(oldContent), "\n")
    newLines := strings.Split(string(newContent), "\n")

    // Myers diff
    edits := myers.ComputeEdits(oldLines, newLines)
    hunks := groupIntoHunks(edits, opts.Context)

    return &DiffOutput{
        Type:  "text",
        Hunks: hunks,
        Summary: fmt.Sprintf("+%d -%d lines", countAdd(hunks), countDel(hunks)),
    }
}
```

Output (terminal):
```
--- a/src/auth/login.go
+++ b/src/auth/login.go
@@ -40,7 +40,7 @@
 func Login(req *http.Request) (*User, error) {
     username := req.FormValue("username")
-    password := req.FormValue("password")
+    passphrase := req.FormValue("passphrase")

-    user, err := db.FindByPassword(username, password)
+    user, err := db.FindByPassphrase(username, passphrase)
     if err != nil {
```

### Structured Diff (Design, Data)

Uses JSON Patch (RFC 6902) for structured content:

```go
func (d *DiffEngine) diffStructured(oldContent, newContent []byte, path string) *DiffOutput {
    var oldDoc, newDoc interface{}
    json.Unmarshal(oldContent, &oldDoc)
    json.Unmarshal(newContent, &newDoc)

    patches := computeJSONPatch(oldDoc, newDoc)

    return &DiffOutput{
        Type:    "structured",
        Patches: patches,
        Summary: summarizePatches(patches),
    }
}
```

Output (terminal):
```
--- a/design/onboarding.json
  [replace] /pages/0/title: "Welcome" → "Get Started"
  [add]     /pages/0/components/-: Button { label: "Skip", action: "skip" }
  [remove]  /pages/2/components/3
  [replace] /theme/primary: "#3B82F6" → "#2563EB"
```

### Binary Diff (Images, Assets)

No content diff — just metadata comparison:

```go
func (d *DiffEngine) diffBinary(oldHash, newHash string, path string) *DiffOutput {
    oldObj, _ := d.store.GetMeta(oldHash)
    newObj, _ := d.store.GetMeta(newHash)

    return &DiffOutput{
        Type: "binary",
        Summary: fmt.Sprintf(
            "Binary file changed: %s → %s (%s → %s)",
            humanizeBytes(oldObj.Size), humanizeBytes(newObj.Size),
            oldHash[:12], newHash[:12],
        ),
    }
}
```

Output:
```
--- a/assets/logo.png
Binary file changed: 45.2 KB → 52.1 KB (ab3def012345 → cd5678901234)
```

## Diff Formatting

### Terminal Output

```go
type TerminalFormatter struct {
    color   bool
    context int
}

func (f *TerminalFormatter) Format(result *DiffResult) string {
    var buf strings.Builder

    // Project-level summary
    buf.WriteString(fmt.Sprintf("%d spaces changed\n\n", result.Summary.SpacesChanged))

    for _, space := range result.Spaces {
        // Space header
        buf.WriteString(fmt.Sprintf("─── %s ───\n", space.SpaceID))
        buf.WriteString(fmt.Sprintf("  %d created, %d modified, %d deleted\n\n",
            space.Summary.EntitiesCreated,
            space.Summary.EntitiesModified,
            space.Summary.EntitiesDeleted))

        for _, entity := range space.Entities {
            // Entity header
            buf.WriteString(fmt.Sprintf("  %s %s\n", changeIcon(entity.Change), entity.Path))

            if entity.Change == ChangeModified && len(entity.Hunks) > 0 {
                for _, hunk := range entity.Hunks {
                    buf.WriteString(formatHunk(hunk, f.color))
                }
            }
        }
    }

    return buf.String()
}
```

### JSON Output (for Agent API)

```go
func (f *JSONFormatter) Format(result *DiffResult) string {
    data, _ := json.MarshalIndent(result, "", "  ")
    return string(data)
}
```

### Patch Output (for apply/restore)

```go
func (f *PatchFormatter) Format(result *DiffResult) string {
    // Generate unified patch format compatible with `patch` command
    var buf strings.Builder
    for _, space := range result.Spaces {
        for _, entity := range space.Entities {
            if entity.Change == ChangeModified && len(entity.Hunks) > 0 {
                buf.WriteString(fmt.Sprintf("--- a/%s/%s\n", space.SpaceID, entity.Path))
                buf.WriteString(fmt.Sprintf("+++ b/%s/%s\n", space.SpaceID, entity.Path))
                for _, hunk := range entity.Hunks {
                    buf.WriteString(formatUnifiedHunk(hunk))
                }
            }
        }
    }
    return buf.String()
}
```

## CLI Commands

```bash
# Diff between last checkpoint and now
loom diff

# Diff between two checkpoints
loom diff <checkpoint-a> <checkpoint-b>

# Diff for a specific space
loom diff --space code

# Diff for a specific entity
loom diff --entity src/auth/login.go

# Diff with more context lines
loom diff -C 10

# Summary only (no content)
loom diff --summary

# JSON output (for piping/agents)
loom diff --format json

# Patch output (for applying elsewhere)
loom diff --format patch > changes.patch

# Relative refs
loom diff HEAD~1 HEAD        # Last checkpoint to now
loom diff HEAD~3 HEAD~1      # Three checkpoints ago to one ago
```

## Future: Semantic Diff

Beyond line-level and JSON patch diffing, future versions will support semantic diffing:

- **Code**: AST-aware diffs (function renamed, parameter added, logic changed)
- **Design**: Component-level diffs (button moved, color changed, layout restructured)
- **Docs**: Section-level diffs (heading reordered, paragraph rewritten)

This requires per-language/format parsers and is planned for v2.
