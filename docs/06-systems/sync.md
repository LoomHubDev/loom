# 06 — Systems: Sync Protocol

## Overview

Loom's sync protocol enables pushing and pulling operations between a local project and a remote Loom server. It's operation-based (not snapshot-based), making sync efficient — only new operations and their referenced objects are transferred.

## Architecture

```
┌──────────┐                          ┌──────────────┐
│  Client   │   ── push ops + objs →  │    Server     │
│  (.loom/) │   ← pull ops + objs ──  │  (loom-server)│
│           │                          │               │
│  SQLite   │   HTTP/JSON protocol    │  SQLite/PG    │
│  Objects  │                          │  Objects      │
└──────────┘                          └──────────────┘
```

## Protocol

### Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| POST | `/api/v1/negotiate` | Find common ancestor, plan sync |
| POST | `/api/v1/push` | Send operations and objects to server |
| POST | `/api/v1/pull` | Receive operations and objects from server |
| GET | `/api/v1/project/:id/info` | Get project metadata |
| GET | `/api/v1/project/:id/streams` | List streams |
| GET | `/api/v1/project/:id/log` | Get checkpoint log |
| POST | `/api/v1/auth/token` | Authenticate and get token |

### Negotiate

Before push or pull, client and server negotiate to find the common sync point:

```go
// Client sends
type NegotiateRequest struct {
    ProjectID string            `json:"project_id"`
    Streams   []StreamSyncState `json:"streams"`
}

type StreamSyncState struct {
    StreamID string `json:"stream_id"`
    Name     string `json:"name"`
    HeadSeq  int64  `json:"head_seq"`
}

// Server responds
type NegotiateResponse struct {
    CommonSeqs map[string]int64 `json:"common_seqs"` // stream_id → last common seq
    ServerSeqs map[string]int64 `json:"server_seqs"` // stream_id → server head seq
    NeedsPush  bool             `json:"needs_push"`
    NeedsPull  bool             `json:"needs_pull"`
}
```

### Push

```go
// Client sends operations and referenced objects
type PushRequest struct {
    ProjectID string      `json:"project_id"`
    StreamID  string      `json:"stream_id"`
    FromSeq   int64       `json:"from_seq"`
    Operations []Operation `json:"operations"`
    Objects    []ObjectData `json:"objects"` // New objects not yet on server
}

type ObjectData struct {
    Hash    string `json:"hash"`
    Content []byte `json:"content"` // Raw or compressed bytes
}

// Server responds
type PushResponse struct {
    OK         bool   `json:"ok"`
    Applied    int    `json:"applied"`     // Number of ops applied
    ServerHead int64  `json:"server_head"` // New server head seq
    Error      string `json:"error,omitempty"`
}
```

### Pull

```go
// Client sends
type PullRequest struct {
    ProjectID string `json:"project_id"`
    StreamID  string `json:"stream_id"`
    FromSeq   int64  `json:"from_seq"` // Client's last known seq for this stream
}

// Server responds
type PullResponse struct {
    Operations []Operation  `json:"operations"`
    Objects    []ObjectData `json:"objects"` // Objects referenced by pulled ops
    ServerHead int64        `json:"server_head"`
}
```

## Sync Client

```go
type SyncClient struct {
    remote  Remote
    db      *sql.DB
    reader  *OpReader
    writer  *OpWriter
    store   *ObjectStore
    http    *http.Client
}

func (c *SyncClient) Push(streamName string) error {
    // 1. Negotiate
    stream, _ := c.getStream(streamName)
    lastPushed := c.getLastPushedSeq(stream.ID)

    negReq := NegotiateRequest{
        ProjectID: c.projectID(),
        Streams: []StreamSyncState{{
            StreamID: stream.ID,
            Name:     stream.Name,
            HeadSeq:  stream.HeadSeq,
        }},
    }

    negResp, err := c.post("/api/v1/negotiate", negReq)
    if err != nil {
        return err
    }

    if !negResp.NeedsPush {
        fmt.Println("Already up to date.")
        return nil
    }

    // 2. Get ops to push
    commonSeq := negResp.CommonSeqs[stream.ID]
    ops, _ := c.reader.ReadRange(commonSeq, stream.HeadSeq)

    // 3. Collect referenced objects
    objectHashes := collectObjectRefs(ops)
    var objects []ObjectData
    for _, hash := range objectHashes {
        content, _ := c.store.Read(hash)
        objects = append(objects, ObjectData{Hash: hash, Content: content})
    }

    // 4. Push
    pushReq := PushRequest{
        ProjectID:  c.projectID(),
        StreamID:   stream.ID,
        FromSeq:    commonSeq,
        Operations: ops,
        Objects:    objects,
    }

    pushResp, err := c.post("/api/v1/push", pushReq)
    if err != nil {
        return err
    }

    if !pushResp.OK {
        return fmt.Errorf("push failed: %s", pushResp.Error)
    }

    // 5. Update sync state
    c.updateLastPushedSeq(stream.ID, stream.HeadSeq)
    c.logSync(stream.ID, "push", commonSeq, stream.HeadSeq, pushResp.Applied)

    fmt.Printf("Pushed %d operations to %s\n", pushResp.Applied, c.remote.Name)
    return nil
}

func (c *SyncClient) Pull(streamName string) error {
    stream, _ := c.getStream(streamName)
    lastPulled := c.getLastPulledSeq(stream.ID)

    // 1. Negotiate
    negResp, _ := c.negotiate(stream)

    if !negResp.NeedsPull {
        fmt.Println("Already up to date.")
        return nil
    }

    // 2. Pull
    pullReq := PullRequest{
        ProjectID: c.projectID(),
        StreamID:  stream.ID,
        FromSeq:   lastPulled,
    }

    pullResp, _ := c.post("/api/v1/pull", pullReq)

    // 3. Store objects
    for _, obj := range pullResp.Objects {
        c.store.WriteRaw(obj.Hash, obj.Content)
    }

    // 4. Apply operations
    c.writer.WriteBatch(pullResp.Operations)

    // 5. Update sync state
    c.updateLastPulledSeq(stream.ID, pullResp.ServerHead)
    c.logSync(stream.ID, "pull", lastPulled, pullResp.ServerHead, len(pullResp.Operations))

    fmt.Printf("Pulled %d operations from %s\n", len(pullResp.Operations), c.remote.Name)
    return nil
}
```

## Loom Server

### Architecture

```go
type Server struct {
    db     *sql.DB       // Server-side database
    store  *ObjectStore  // Server-side object store
    router chi.Router
}

func NewServer(dbPath, objectsPath string) *Server {
    s := &Server{
        db:    openDB(dbPath),
        store: NewObjectStore(objectsPath),
    }

    r := chi.NewRouter()
    r.Use(middleware.Logger)
    r.Use(middleware.Recoverer)
    r.Use(s.authMiddleware)

    r.Post("/api/v1/negotiate", s.handleNegotiate)
    r.Post("/api/v1/push", s.handlePush)
    r.Post("/api/v1/pull", s.handlePull)
    r.Get("/api/v1/project/{id}/info", s.handleProjectInfo)
    r.Get("/api/v1/project/{id}/streams", s.handleListStreams)
    r.Get("/api/v1/project/{id}/log", s.handleLog)
    r.Post("/api/v1/auth/token", s.handleAuth)

    s.router = r
    return s
}

func (s *Server) Start(addr string) error {
    return http.ListenAndServe(addr, s.router)
}
```

### Server Storage

The server uses the same SQLite schema as the client (operations, checkpoints, streams, objects tables). For larger deployments, Postgres can be used instead.

```go
// Server-side push handler
func (s *Server) handlePush(w http.ResponseWriter, r *http.Request) {
    var req PushRequest
    json.NewDecoder(r.Body).Decode(&req)

    // Validate project access
    if !s.hasAccess(r.Context(), req.ProjectID) {
        http.Error(w, "forbidden", 403)
        return
    }

    // Store objects
    for _, obj := range req.Objects {
        s.store.WriteRaw(obj.Hash, obj.Content)
    }

    // Apply operations
    applied := 0
    for _, op := range req.Operations {
        if err := s.writeOp(req.ProjectID, op); err != nil {
            json.NewEncoder(w).Encode(PushResponse{OK: false, Error: err.Error()})
            return
        }
        applied++
    }

    // Update stream head
    s.updateStreamHead(req.StreamID, req.Operations[len(req.Operations)-1].Seq)

    json.NewEncoder(w).Encode(PushResponse{
        OK:         true,
        Applied:    applied,
        ServerHead: req.Operations[len(req.Operations)-1].Seq,
    })
}
```

### Authentication

```go
func (s *Server) authMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Skip auth for token endpoint
        if r.URL.Path == "/api/v1/auth/token" {
            next.ServeHTTP(w, r)
            return
        }

        token := r.Header.Get("Authorization")
        if token == "" {
            http.Error(w, "unauthorized", 401)
            return
        }

        // Validate JWT token
        claims, err := validateToken(strings.TrimPrefix(token, "Bearer "))
        if err != nil {
            http.Error(w, "unauthorized", 401)
            return
        }

        ctx := context.WithValue(r.Context(), "user", claims.UserID)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```

## CLI Commands

```bash
# Add a remote
loom remote add origin https://loom.example.com/project/my-app

# Set auth token
loom remote auth origin
# Opens browser for OAuth or prompts for token

# Push current stream
loom push
loom push origin        # Explicit remote
loom push --all         # Push all streams

# Pull current stream
loom pull
loom pull origin        # Explicit remote
loom pull --all         # Pull all streams

# List remotes
loom remote list

# Remove remote
loom remote remove origin

# Show sync status
loom remote status
# Output:
#   origin: https://loom.example.com/project/my-app
#     main: 42 ops ahead, 0 behind
#     feature/auth: 15 ops ahead, 3 behind
```

## Server Deployment

### Docker

```dockerfile
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o loom-server ./cmd/loom-server

FROM alpine:3.19
COPY --from=builder /app/loom-server /usr/local/bin/
EXPOSE 8080
CMD ["loom-server", "--addr", ":8080", "--data", "/data"]
```

### Configuration

```toml
# loom-server.toml
[server]
addr = ":8080"
data_dir = "/data/loom"

[storage]
backend = "sqlite"        # or "postgres"
# postgres_url = "postgres://user:pass@host/loomdb"

[auth]
method = "jwt"            # or "api_key"
jwt_secret = "${LOOM_JWT_SECRET}"

[limits]
max_push_size = 104857600  # 100MB
max_ops_per_push = 10000
max_projects = 1000
```

### Self-hosted

```bash
# Run server
loom-server --config loom-server.toml

# Or with docker-compose
docker-compose up -d
```

## Future: Real-Time Sync

v2 will add WebSocket-based real-time sync:

```
Client A ──ws──▶ Server ◀──ws── Client B

1. Client A writes an operation
2. Server receives via WebSocket
3. Server broadcasts to Client B
4. Client B applies the operation in real-time
```

This requires CRDT-based operations (v2) to handle concurrent writes without coordination.
