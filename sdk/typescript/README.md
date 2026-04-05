# @loom/sdk

TypeScript SDK for the [Loom](https://loomhub.dev) versioning system. Uses native `fetch` — no runtime dependencies. Targets Node 18+ and modern browsers.

## Install

```bash
npm install @loom/sdk
```

## Usage

```typescript
import { LoomClient } from '@loom/sdk';

const loom = new LoomClient('http://localhost:7890');
// With auth token:
// const loom = new LoomClient('http://localhost:7890', 'your-token');
```

### checkpoint

Save a checkpoint of the current state.

```typescript
const cp = await loom.checkpoint('Add auth module', {
  summary: 'Implemented JWT login flow',
  tags: { feature: 'auth', env: 'dev' },
});
console.log(cp.id, cp.seq);
```

### diff

Compare two refs. `from` is required; `to` defaults to `HEAD`.

```typescript
const result = await loom.diff('HEAD~1', { space: 'code', summary: true });
console.log(result.summary.entities_modified);
```

### rollback

Roll back to a prior checkpoint — full project or a single entity.

```typescript
// Full rollback
await loom.rollback('cp_abc123');

// Single entity rollback
await loom.rollback('cp_abc123', 'src/auth/login.go');
```

### search

Full-text search across checkpoints.

```typescript
const results = await loom.search('auth login', 5);
results.forEach(cp => console.log(cp.seq, cp.title));
```

### log

Fetch the N most recent checkpoints.

```typescript
const history = await loom.log(20);
```

### status

Get current project/stream status.

```typescript
const s = await loom.status();
console.log(`HEAD is seq ${s.head_seq}, ${s.pending_ops} pending ops`);
```

### explain

Generate a human-readable explanation of changes between two refs.

```typescript
const { explanation } = await loom.explain('HEAD~3', 'HEAD');
console.log(explanation);
```

### record

Write a file into a space (content is base64-encoded automatically).

```typescript
await loom.record('code', 'src/main.go', '// generated\n');
// or pass Uint8Array for binary content
```

### tools

Fetch available tool definitions (useful for agent integrations).

```typescript
const tools = await loom.tools();
```

## Build

```bash
npm run build   # outputs to dist/
npm test        # runs tests with node:test
```
