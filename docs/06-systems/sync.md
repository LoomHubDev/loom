# 06 — Systems: Sync Protocol

## Overview

Loom's sync protocol enables sending and receiving operations between a local project and a hub. It's operation-based (not snapshot-based), making sync efficient — only new operations and their referenced objects are transferred.

## Architecture

```
┌──────────┐                          ┌──────────────┐
│  Client   │   ── send ops + objs →  │    Hub        │
│  (.loom/) │   ← recv ops + objs ──  │  (loomhub)    │
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
| POST | `/api/v1/push` | Send operations and objects to hub |
| POST | `/api/v1/pull` | Receive operations and objects from hub |
| GET | `/api/v1/project/:id/info` | Get project metadata |
| GET | `/api/v1/project/:id/streams` | List streams |
| GET | `/api/v1/project/:id/log` | Get checkpoint log |
| POST | `/api/v1/auth/token` | Authenticate and get token |

### Negotiate

Before send or receive, client and hub negotiate to find the common sync point:

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

### Send (Push)

```go
// Client sends operations and referenced objects to hub
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

### Receive (Pull)

```go
// Client requests operations from hub
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

func (c *SyncClient) Send(streamName string) error {
    // 1. Negotiate
    stream, _ := c.getStream(streamName)
    lastSent := c.getLastSentSeq(stream.ID)

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

    // 2. Get ops to send
    commonSeq := negResp.CommonSeqs[stream.ID]
    ops, _ := c.reader.ReadRange(commonSeq, stream.HeadSeq)

    // 3. Collect referenced objects
    objectHashes := collectObjectRefs(ops)
    var objects []ObjectData
    for _, hash := range objectHashes {
        content, _ := c.store.Read(hash)
        objects = append(objects, ObjectData{Hash: hash, Content: content})
    }

    // 4. Send
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
        return fmt.Errorf("send failed: %s", pushResp.Error)
    }

    // 5. Update sync state
    c.updateLastSentSeq(stream.ID, stream.HeadSeq)
    c.logSync(stream.ID, "send", commonSeq, stream.HeadSeq, pushResp.Applied)

    fmt.Printf("Sent %d operations to %s\n", pushResp.Applied, c.hub.Name)
    return nil
}

func (c *SyncClient) Receive(streamName string) error {
    stream, _ := c.getStream(streamName)
    lastReceived := c.getLastReceivedSeq(stream.ID)

    // 1. Negotiate
    negResp, _ := c.negotiate(stream)

    if !negResp.NeedsPull {
        fmt.Println("Already up to date.")
        return nil
    }

    // 2. Receive
    pullReq := PullRequest{
        ProjectID: c.projectID(),
        StreamID:  stream.ID,
        FromSeq:   lastReceived,
    }

    pullResp, _ := c.post("/api/v1/pull", pullReq)

    // 3. Store objects
    for _, obj := range pullResp.Objects {
        c.store.WriteRaw(obj.Hash, obj.Content)
    }

    // 4. Apply operations
    c.writer.WriteBatch(pullResp.Operations)

    // 5. Update sync state
    c.updateLastReceivedSeq(stream.ID, pullResp.ServerHead)
    c.logSync(stream.ID, "receive", lastReceived, pullResp.ServerHead, len(pullResp.Operations))

    fmt.Printf("Received %d operations from %s\n", len(pullResp.Operations), c.hub.Name)
    return nil
}
```

## Hub Server

### Architecture

```go
type HubServer struct {
    db     *sql.DB       // Hub database
    store  *ObjectStore  // Hub object store
    router chi.Router
}

func NewHubServer(dbPath, objectsPath string) *HubServer {
    s := &HubServer{
        db:    openDB(dbPath),
        store: NewObjectStore(objectsPath),
    }

    r := chi.NewRouter()
    r.Use(middleware.Logger)
    r.Use(middleware.Recoverer)
    r.Use(s.authMiddleware)

    r.Post("/api/v1/negotiate", s.handleNegotiate)
    r.Post("/api/v1/push", s.handleSend)
    r.Post("/api/v1/pull", s.handleReceive)
    r.Get("/api/v1/project/{id}/info", s.handleProjectInfo)
    r.Get("/api/v1/project/{id}/streams", s.handleListStreams)
    r.Get("/api/v1/project/{id}/log", s.handleLog)
    r.Post("/api/v1/auth/token", s.handleAuth)

    s.router = r
    return s
}

func (s *HubServer) Start(addr string) error {
    return http.ListenAndServe(addr, s.router)
}
```

### Hub Storage

The hub uses the same SQLite schema as the client (operations, checkpoints, streams, objects tables). For larger deployments, Postgres can be used instead.

```go
// Hub-side send handler (receives ops from client)
func (s *HubServer) handleSend(w http.ResponseWriter, r *http.Request) {
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
# Add a hub
loom hub add origin https://loomhub.dev/flakerimi/my-app

# Set auth token
loom hub auth origin
# Opens browser for OAuth or prompts for token

# Send current stream
loom send
loom send origin        # Explicit hub
loom send --all         # Send all streams

# Receive current stream
loom receive
loom receive origin     # Explicit hub
loom receive --all      # Receive all streams

# List hubs
loom hub list

# Remove hub
loom hub remove origin

# Show sync status
loom hub status
# Output:
#   origin: https://loomhub.dev/flakerimi/my-app
#     main: 42 ops ahead, 0 behind
#     feature/auth: 15 ops ahead, 3 behind
```

## Hub Deployment

For hosting your own hub, see the [LoomHub project](../../loomhub/docs/01-vision.md). LoomHub is the reference hub implementation that provides a full hosting platform with web UI, weave requests, and collaboration features.

For a minimal self-hosted hub:

```dockerfile
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o loomhub ./cmd/loomhub

FROM alpine:3.19
COPY --from=builder /app/loomhub /usr/local/bin/
EXPOSE 3000
CMD ["loomhub", "serve", "--port", "3000", "--data", "/data"]
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
