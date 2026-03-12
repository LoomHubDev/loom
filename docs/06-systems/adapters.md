# 06 — Systems: Space Adapters

## Overview

A space adapter is the bridge between Loom's universal versioning model and a specific content type. Each adapter knows how to:

1. **Detect** — find trackable content in a directory
2. **Watch** — observe changes (file modifications, saves, etc.)
3. **Normalize** — convert changes into Loom operations
4. **Diff** — produce meaningful diffs for the content type
5. **Restore** — write content back from the object store
6. **Report** — provide state summaries and refs

## Adapter Interface

```go
type SpaceAdapter interface {
    // Identity
    ID() string           // "code", "docs", "design", etc.
    Name() string         // "Code", "Documentation", "Design"

    // Setup
    Init(config SpaceConfig) error
    Detect(projectPath string) (bool, error) // Auto-detect if this space exists

    // Change tracking
    ScanEntities() ([]EntityState, error)          // Full scan of current state
    NormalizeChange(event FileEvent) ([]Operation, error)  // Convert fs event to ops
    GetRefs() map[string]string                    // Adapter-specific refs

    // Content
    ReadEntity(path string) ([]byte, error)        // Read current content
    WriteEntity(path string, content []byte) error // Write content (for restore)
    GetEntityStates(ops []Operation) ([]EntityState, error)  // States from ops

    // Diffing
    Diff(oldContent, newContent []byte, path string) (*DiffOutput, error)
    DiffSummary(ops []Operation) (SpaceSummary, error)

    // Merge (optional — default falls back to core merge engine)
    Merge(base, ours, theirs []byte, path string) (*MergeOutput, error)
}

type SpaceConfig struct {
    ID          string
    Path        string            // Root path within project
    ProjectPath string            // Absolute project path
    IgnoreRules []string
    Options     map[string]string // Adapter-specific options
}

type FileEvent struct {
    Path      string
    EventType string // "create", "modify", "delete", "rename"
    OldPath   string // For rename events
    Timestamp time.Time
}

type DiffOutput struct {
    Type     string     // "text", "structured", "binary"
    Hunks    []DiffHunk // For text diffs
    Patches  []byte     // For structured diffs (JSON patch)
    Summary  string     // Human-readable summary
}

type MergeOutput struct {
    OK       bool
    Content  []byte
    Strategy string // "auto", "ours", "theirs", "combined"
}
```

## Adapter Registry

```go
type AdapterRegistry struct {
    adapters map[string]SpaceAdapter
}

func NewAdapterRegistry() *AdapterRegistry {
    r := &AdapterRegistry{adapters: make(map[string]SpaceAdapter)}
    // Register built-in adapters
    r.Register(NewCodeAdapter())
    r.Register(NewDocsAdapter())
    r.Register(NewDesignAdapter())
    r.Register(NewDataAdapter())
    r.Register(NewConfigAdapter())
    r.Register(NewNotesAdapter())
    return r
}

func (r *AdapterRegistry) Register(adapter SpaceAdapter) {
    r.adapters[adapter.ID()] = adapter
}

func (r *AdapterRegistry) Get(id string) SpaceAdapter {
    return r.adapters[id]
}

func (r *AdapterRegistry) DetectSpaces(projectPath string) []Space {
    var detected []Space
    for _, adapter := range r.adapters {
        if found, _ := adapter.Detect(projectPath); found {
            detected = append(detected, Space{
                ID:      adapter.ID(),
                Name:    adapter.Name(),
                Adapter: adapter.ID(),
            })
        }
    }
    return detected
}
```

## Built-in Adapters

### Code Adapter

The code adapter handles source code files. It detects projects by looking for common project indicators (build files, manifests) and tracks changes via filesystem events.

```go
type CodeAdapter struct {
    config SpaceConfig
}

func (a *CodeAdapter) ID() string   { return "code" }
func (a *CodeAdapter) Name() string { return "Code" }

func (a *CodeAdapter) Detect(projectPath string) (bool, error) {
    // Check for common project files
    indicators := []string{"go.mod", "package.json", "Cargo.toml", "pyproject.toml", "Makefile", "CMakeLists.txt", "pom.xml"}
    for _, f := range indicators {
        if _, err := os.Stat(filepath.Join(projectPath, f)); err == nil {
            return true, nil
        }
    }
    // Check for common source directories
    srcDirs := []string{"src", "lib", "cmd", "pkg"}
    for _, d := range srcDirs {
        if info, err := os.Stat(filepath.Join(projectPath, d)); err == nil && info.IsDir() {
            return true, nil
        }
    }
    return false, nil
}

func (a *CodeAdapter) GetRefs() map[string]string {
    return make(map[string]string)
}

func (a *CodeAdapter) NormalizeChange(event FileEvent) ([]Operation, error) {
    // Read file content
    content, err := os.ReadFile(filepath.Join(a.config.ProjectPath, a.config.Path, event.Path))
    if err != nil && event.EventType != "delete" {
        return nil, err
    }

    op := Operation{
        SpaceID:  "code",
        EntityID: event.Path,
        Path:     event.Path,
        Meta: OpMeta{
            ContentType: detectMIME(event.Path),
            Source:      "watch",
        },
    }

    switch event.EventType {
    case "create":
        op.Type = OpCreate
        op.Meta.Size = int64(len(content))
    case "modify":
        op.Type = OpModify
        // Generate delta (text diff against previous version)
        op.Delta = a.generateDelta(event.Path, content)
        op.Meta.Size = int64(len(content))
    case "delete":
        op.Type = OpDelete
    case "rename":
        op.Type = OpMove
        op.Meta.OldPath = event.OldPath
    }

    return []Operation{op}, nil
}

func (a *CodeAdapter) Diff(oldContent, newContent []byte, path string) (*DiffOutput, error) {
    if isBinary(oldContent) || isBinary(newContent) {
        return &DiffOutput{
            Type:    "binary",
            Summary: fmt.Sprintf("Binary file changed (%d → %d bytes)", len(oldContent), len(newContent)),
        }, nil
    }

    hunks := computeTextDiff(string(oldContent), string(newContent))
    return &DiffOutput{
        Type:    "text",
        Hunks:   hunks,
        Summary: fmt.Sprintf("+%d -%d lines", countInsertions(hunks), countDeletions(hunks)),
    }, nil
}
```

### Docs Adapter

Handles documentation files — markdown, text, RST, etc.

```go
type DocsAdapter struct {
    config SpaceConfig
}

func (a *DocsAdapter) ID() string   { return "docs" }
func (a *DocsAdapter) Name() string { return "Documentation" }

func (a *DocsAdapter) Detect(projectPath string) (bool, error) {
    docDirs := []string{"docs", "doc", "documentation", "wiki"}
    for _, d := range docDirs {
        if info, err := os.Stat(filepath.Join(projectPath, d)); err == nil && info.IsDir() {
            return true, nil
        }
    }
    return false, nil
}

func (a *DocsAdapter) ScanEntities() ([]EntityState, error) {
    var entities []EntityState
    root := filepath.Join(a.config.ProjectPath, a.config.Path)

    filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
        if err != nil || d.IsDir() {
            return nil
        }
        if !isDocFile(path) {
            return nil
        }

        rel, _ := filepath.Rel(root, path)
        info, _ := d.Info()
        content, _ := os.ReadFile(path)
        hash := HashContent(content)

        entities = append(entities, EntityState{
            ID:        rel,
            SpaceID:   "docs",
            Kind:      "document",
            Path:      rel,
            ObjectRef: hash,
            Size:      info.Size(),
            ModTime:   info.ModTime().Format(time.RFC3339),
        })
        return nil
    })

    return entities, nil
}

func isDocFile(path string) bool {
    exts := []string{".md", ".mdx", ".txt", ".rst", ".adoc", ".tex", ".html"}
    ext := strings.ToLower(filepath.Ext(path))
    for _, e := range exts {
        if ext == e {
            return true
        }
    }
    return false
}
```

### Design Adapter (Structured)

Handles design files — JSON-based structured data with nodes, components, pages.

```go
type DesignAdapter struct {
    config SpaceConfig
}

func (a *DesignAdapter) ID() string   { return "design" }
func (a *DesignAdapter) Name() string { return "Design" }

func (a *DesignAdapter) Detect(projectPath string) (bool, error) {
    designDirs := []string{"design", "ui", ".design"}
    for _, d := range designDirs {
        if info, err := os.Stat(filepath.Join(projectPath, d)); err == nil && info.IsDir() {
            return true, nil
        }
    }
    // Check for design file extensions
    matches, _ := filepath.Glob(filepath.Join(projectPath, "**/*.design.json"))
    return len(matches) > 0, nil
}

func (a *DesignAdapter) Diff(oldContent, newContent []byte, path string) (*DiffOutput, error) {
    // Structural diff for design files
    // Parse both as JSON, compute JSON patch
    patch, err := computeJSONPatch(oldContent, newContent)
    if err != nil {
        // Fall back to text diff
        return textDiff(oldContent, newContent, path)
    }

    return &DiffOutput{
        Type:    "structured",
        Patches: patch,
        Summary: summarizeJSONPatch(patch),
    }, nil
}

func (a *DesignAdapter) Merge(base, ours, theirs []byte, path string) (*MergeOutput, error) {
    // Structural merge for design files
    // Use JSON merge patch (RFC 7396) for non-conflicting changes
    merged, err := structuralMerge(base, ours, theirs)
    if err != nil {
        return &MergeOutput{OK: false}, nil
    }
    return &MergeOutput{OK: true, Content: merged, Strategy: "auto"}, nil
}
```

### Data Adapter (Schemas)

Handles structured data files — JSON, YAML, TOML, SQL migrations.

```go
type DataAdapter struct {
    config SpaceConfig
}

func (a *DataAdapter) ID() string   { return "data" }
func (a *DataAdapter) Name() string { return "Data" }

func (a *DataAdapter) Detect(projectPath string) (bool, error) {
    dataDirs := []string{"data", "schemas", "migrations"}
    for _, d := range dataDirs {
        if info, err := os.Stat(filepath.Join(projectPath, d)); err == nil && info.IsDir() {
            return true, nil
        }
    }
    return false, nil
}

func (a *DataAdapter) Diff(oldContent, newContent []byte, path string) (*DiffOutput, error) {
    ext := filepath.Ext(path)
    switch ext {
    case ".json":
        patch, _ := computeJSONPatch(oldContent, newContent)
        return &DiffOutput{Type: "structured", Patches: patch}, nil
    case ".yaml", ".yml":
        // Parse YAML → JSON, then JSON patch
        oldJSON := yamlToJSON(oldContent)
        newJSON := yamlToJSON(newContent)
        patch, _ := computeJSONPatch(oldJSON, newJSON)
        return &DiffOutput{Type: "structured", Patches: patch}, nil
    default:
        return textDiff(oldContent, newContent, path)
    }
}
```

### Config Adapter

```go
type ConfigAdapter struct {
    config SpaceConfig
}

func (a *ConfigAdapter) ID() string   { return "config" }
func (a *ConfigAdapter) Name() string { return "Configuration" }

func (a *ConfigAdapter) Detect(projectPath string) (bool, error) {
    configFiles := []string{
        ".env.example", "config.toml", "config.yaml",
        ".editorconfig", "tsconfig.json", "webpack.config.js",
    }
    for _, f := range configFiles {
        if _, err := os.Stat(filepath.Join(projectPath, f)); err == nil {
            return true, nil
        }
    }
    return false, nil
}
```

### Notes Adapter

```go
type NotesAdapter struct {
    config SpaceConfig
}

func (a *NotesAdapter) ID() string   { return "notes" }
func (a *NotesAdapter) Name() string { return "Notes" }

func (a *NotesAdapter) Detect(projectPath string) (bool, error) {
    noteDirs := []string{"notes", "journal", ".notes"}
    for _, d := range noteDirs {
        if info, err := os.Stat(filepath.Join(projectPath, d)); err == nil && info.IsDir() {
            return true, nil
        }
    }
    return false, nil
}
```

## Custom Adapters

Users can register custom adapters by implementing the `SpaceAdapter` interface and registering them:

```go
// In a plugin or custom build
type KanbanAdapter struct { /* ... */ }

func (a *KanbanAdapter) ID() string   { return "kanban" }
func (a *KanbanAdapter) Name() string { return "Kanban Board" }
// ... implement all methods

// Register
registry.Register(NewKanbanAdapter())
```

Future adapter ideas:
- `kanban` — task boards
- `calendar` — schedules and events
- `prompts` — AI prompt libraries
- `conversations` — AI chat history
- `assets` — binary media (images, videos)
- `api` — API definitions (OpenAPI, GraphQL schemas)

## File Watching Integration

Adapters receive events from the file watcher:

```go
func (w *Watcher) Start(adapters *AdapterRegistry) {
    for event := range w.events {
        // Determine which space this file belongs to
        space := w.routeToSpace(event.Path)
        if space == nil {
            continue // Not tracked
        }

        adapter := adapters.Get(space.ID)
        ops, err := adapter.NormalizeChange(FileEvent{
            Path:      event.RelPath,
            EventType: event.Type,
            Timestamp: time.Now(),
        })
        if err != nil {
            slog.Error("normalize failed", "space", space.ID, "path", event.Path, "err", err)
            continue
        }

        for _, op := range ops {
            w.opWriter.Write(op)
        }
    }
}
```

## Ignore Rules

Each adapter respects ignore rules from:
1. `.loomignore` (project-level)
2. Space config `ignore_rules` (per-space)
3. Adapter-specific defaults (e.g., code adapter ignores `node_modules` by default)

Rules use glob/ignore pattern syntax (same as `.gitignore`).
