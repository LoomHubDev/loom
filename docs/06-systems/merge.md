# 06 — Systems: Merge Engine

## Overview

Loom's merge engine is designed around one principle: **merge conflicts should not exist for the user.** When two streams have concurrent changes, Loom resolves them automatically whenever possible and escalates to an LLM agent for semantic conflicts.

## Merge Tiers

```
Tier 1: No Conflict
  Both sides changed different entities → apply both
  │
  ▼ (same entity changed on both sides)

Tier 2: Structural Auto-Merge
  Non-overlapping changes to same entity → merge automatically
  │
  ▼ (overlapping changes)

Tier 3: LLM Resolution
  Semantic conflict → LLM proposes resolution → user approves
  │
  ▼ (LLM can't resolve or user rejects)

Tier 4: Manual (last resort)
  Present both versions side by side → user picks
```

### Success Rate Targets

| Tier | Expected Resolution Rate |
|------|-------------------------|
| Tier 1 (different entities) | ~70% of all merges |
| Tier 2 (auto-merge) | ~20% of all merges |
| Tier 3 (LLM) | ~9% of all merges |
| Tier 4 (manual) | ~1% of all merges |

The goal: 99% of merges require zero user intervention.

## Merge Engine

```go
type MergeEngine struct {
    reader   *OpReader
    store    *ObjectStore
    registry *AdapterRegistry
    llm      *LLMMerger
}

type MergeInput struct {
    SourceStream string
    TargetStream string
    ForkSeq      int64
    SourceOps    []Operation
    TargetOps    []Operation
}

func (m *MergeEngine) Merge(input MergeInput) (*MergeResult, error) {
    // Group ops by entity
    sourceByEntity := groupByEntity(input.SourceOps)
    targetByEntity := groupByEntity(input.TargetOps)

    var merged []Operation
    var suggestions []MergeSuggestion

    // Tier 1: Different entities — no conflict
    for entityID, sops := range sourceByEntity {
        if _, exists := targetByEntity[entityID]; !exists {
            merged = append(merged, sops...)
        }
    }
    for entityID, tops := range targetByEntity {
        if _, exists := sourceByEntity[entityID]; !exists {
            merged = append(merged, tops...)
        }
    }

    // Tiers 2-4: Same entity on both sides
    for entityID, sops := range sourceByEntity {
        tops, exists := targetByEntity[entityID]
        if !exists {
            continue // Already handled in Tier 1
        }

        result, err := m.mergeEntity(entityID, sops, tops, input.ForkSeq)
        if err != nil {
            return nil, err
        }

        merged = append(merged, result.Operations...)
        suggestions = append(suggestions, result.Suggestions...)
    }

    return &MergeResult{
        OK:          true,
        Strategy:    m.determineStrategy(suggestions),
        Operations:  merged,
        Suggestions: suggestions,
    }, nil
}
```

## Tier 2: Structural Auto-Merge

When the same entity is modified on both sides, Loom attempts structural merging:

### Text Files (Code, Docs)

Three-way merge using the base version (at fork point):

```go
func (m *MergeEngine) mergeText(entityID string, sourceOps, targetOps []Operation, forkSeq int64) (*EntityMergeResult, error) {
    // Get base content (at fork point)
    baseContent, _ := m.getEntityContentAt(entityID, forkSeq)

    // Get source content (after source ops)
    sourceContent := applyTextOps(baseContent, sourceOps)

    // Get target content (after target ops)
    targetContent := applyTextOps(baseContent, targetOps)

    // Three-way merge
    result, conflicts := threeWayMerge(baseContent, sourceContent, targetContent)

    if len(conflicts) == 0 {
        // Clean merge
        return &EntityMergeResult{
            OK:      true,
            Content: result,
        }, nil
    }

    // Has conflicts — escalate to Tier 3
    return m.llmMerge(entityID, baseContent, sourceContent, targetContent, conflicts)
}
```

### Three-Way Merge Algorithm

```go
func threeWayMerge(base, ours, theirs []byte) ([]byte, []Conflict) {
    baseLines := splitLines(base)
    ourLines := splitLines(ours)
    theirLines := splitLines(theirs)

    // Compute diffs from base
    ourDiff := computeDiff(baseLines, ourLines)
    theirDiff := computeDiff(baseLines, theirLines)

    var result []string
    var conflicts []Conflict

    i := 0
    for i < len(baseLines) {
        ourChange := findChange(ourDiff, i)
        theirChange := findChange(theirDiff, i)

        if ourChange == nil && theirChange == nil {
            // No changes — keep base
            result = append(result, baseLines[i])
            i++
        } else if ourChange != nil && theirChange == nil {
            // Only we changed — take ours
            result = append(result, ourChange.NewLines...)
            i = ourChange.EndLine
        } else if ourChange == nil && theirChange != nil {
            // Only they changed — take theirs
            result = append(result, theirChange.NewLines...)
            i = theirChange.EndLine
        } else {
            // Both changed same region
            if sameChange(ourChange, theirChange) {
                // Identical change — take either
                result = append(result, ourChange.NewLines...)
            } else {
                // Conflict — record it
                conflicts = append(conflicts, Conflict{
                    BaseLine:   i,
                    OurLines:   ourChange.NewLines,
                    TheirLines: theirChange.NewLines,
                })
            }
            i = max(ourChange.EndLine, theirChange.EndLine)
        }
    }

    return []byte(strings.Join(result, "\n")), conflicts
}
```

### Structured Files (JSON, YAML, Design)

For structured data, merge at the field level:

```go
func (m *MergeEngine) mergeStructured(entityID string, sourceOps, targetOps []Operation, forkSeq int64) (*EntityMergeResult, error) {
    baseContent, _ := m.getEntityContentAt(entityID, forkSeq)

    var base, source, target map[string]interface{}
    json.Unmarshal(baseContent, &base)
    json.Unmarshal(applyStructuredOps(baseContent, sourceOps), &source)
    json.Unmarshal(applyStructuredOps(baseContent, targetOps), &target)

    // Compute JSON patches from base
    sourcePatch := computeJSONPatch(base, source)
    targetPatch := computeJSONPatch(base, target)

    // Check for path conflicts (same JSON path modified on both sides)
    conflicting := findConflictingPaths(sourcePatch, targetPatch)

    if len(conflicting) == 0 {
        // No conflicts — apply both patches
        result := applyJSONPatch(base, sourcePatch)
        result = applyJSONPatch(result, targetPatch)
        merged, _ := json.Marshal(result)
        return &EntityMergeResult{OK: true, Content: merged}, nil
    }

    // Escalate conflicting paths to LLM
    return m.llmMerge(entityID, baseContent,
        mustMarshal(source), mustMarshal(target), toConflicts(conflicting))
}
```

## Tier 3: LLM Resolution

When structural merge finds conflicts, the LLM agent resolves them:

```go
type LLMMerger struct {
    endpoint string // LLM API endpoint
    model    string // Model to use
}

func (l *LLMMerger) Resolve(ctx MergeContext) (*LLMMergeResult, error) {
    prompt := buildMergePrompt(ctx)

    // Call LLM API
    response, err := l.callLLM(prompt)
    if err != nil {
        return nil, err
    }

    return &LLMMergeResult{
        ResolvedContent: response.Content,
        Explanation:     response.Explanation,
        Confidence:      response.Confidence,
    }, nil
}

func buildMergePrompt(ctx MergeContext) string {
    return fmt.Sprintf(`You are resolving a merge conflict in a versioning system.

File: %s
Content type: %s

## Base version (common ancestor)
%s

## Version A (source stream changes)
%s

## Version B (target stream changes)
%s

## Conflicts
%s

Produce the merged result that incorporates both sets of changes correctly.
Explain your reasoning. Rate your confidence (0.0 to 1.0).

Return as JSON:
{
  "content": "the merged file content",
  "explanation": "what you did and why",
  "confidence": 0.95
}`,
        ctx.EntityID,
        ctx.ContentType,
        string(ctx.Base),
        string(ctx.Ours),
        string(ctx.Theirs),
        formatConflicts(ctx.Conflicts),
    )
}
```

### Confidence-Based Auto-Apply

```go
func (m *MergeEngine) llmMerge(entityID string, base, ours, theirs []byte, conflicts []Conflict) (*EntityMergeResult, error) {
    result, err := m.llm.Resolve(MergeContext{
        EntityID:    entityID,
        ContentType: detectContentType(entityID),
        Base:        base,
        Ours:        ours,
        Theirs:      theirs,
        Conflicts:   conflicts,
    })
    if err != nil {
        // LLM unavailable — fall to Tier 4
        return &EntityMergeResult{OK: false}, nil
    }

    suggestion := MergeSuggestion{
        EntityID:    entityID,
        Description: result.Explanation,
        Confidence:  result.Confidence,
    }

    if result.Confidence >= 0.9 {
        // High confidence — auto-apply
        suggestion.Applied = true
        return &EntityMergeResult{
            OK:          true,
            Content:     []byte(result.ResolvedContent),
            Suggestions: []MergeSuggestion{suggestion},
        }, nil
    }

    // Lower confidence — mark as suggestion, don't auto-apply
    suggestion.Applied = false
    return &EntityMergeResult{
        OK:          true, // Still OK, but with unapplied suggestions
        Content:     []byte(result.ResolvedContent),
        Suggestions: []MergeSuggestion{suggestion},
    }, nil
}
```

## Tier 4: Manual Resolution

If LLM is unavailable or confidence is too low:

```bash
# Loom shows the conflict
loom merge feature/auth

# Output:
# Merging feature/auth into main...
# ✓ 15 entities auto-merged
# ✓ 2 entities merged by AI (confidence: 0.95, 0.91)
# ⚠ 1 entity needs review:
#
#   src/auth/login.go
#     Both streams changed the Login() function body.
#     AI suggestion (confidence: 0.72):
#       [shows proposed merge]
#
#   Accept? [y]es / [n]o / [e]dit / [v]iew both

# User reviews and accepts/edits
```

## Merge Policy

Per-space and per-entity merge strategies can be configured:

```toml
# .loom/config.toml

[merge]
default_strategy = "auto+llm"  # Try auto, then LLM

[merge.strategies]
# Per-space overrides
"config" = "ours"              # Config conflicts: always keep our version
"notes" = "auto"               # Notes: auto-merge only, no LLM

[merge.llm]
enabled = true
model = "claude-sonnet-4-5-20250514"
auto_apply_threshold = 0.9     # Auto-apply if confidence >= 0.9
max_file_size = 100000         # Don't send files > 100KB to LLM
```

```go
type MergePolicy struct {
    DefaultStrategy string
    SpaceStrategies map[string]string
    LLMConfig       LLMConfig
}

type LLMConfig struct {
    Enabled              bool
    Model                string
    AutoApplyThreshold   float64
    MaxFileSize          int64
}

func (p *MergePolicy) StrategyFor(spaceID string) string {
    if s, ok := p.SpaceStrategies[spaceID]; ok {
        return s
    }
    return p.DefaultStrategy
}
```

## CLI Commands

```bash
# Merge a stream into the current stream
loom merge feature/auth

# Dry-run (show what would happen)
loom merge feature/auth --dry-run

# Force strategy
loom merge feature/auth --strategy auto   # No LLM, auto-only
loom merge feature/auth --strategy ours   # Keep our version for all conflicts
loom merge feature/auth --strategy theirs # Keep their version for all conflicts

# Accept all LLM suggestions (even low confidence)
loom merge feature/auth --accept-all
```

## Future: CRDTs (v2)

In v2, CRDTs will replace Tier 2 for real-time scenarios:

- Text content: Yjs/Automerge CRDT (convergent by construction)
- Structured data: JSON CRDT (field-level convergence)
- No three-way merge needed — CRDTs converge automatically
- LLM still handles semantic conflicts (two people change the same function's logic differently)

The merge engine is designed with this migration in mind — the `MergeEngine` interface stays the same, only the internal implementation changes.
