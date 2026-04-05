import assert from 'node:assert/strict';
import { test, mock } from 'node:test';
import { LoomClient } from './client';

// Helper to mock global fetch
function mockFetch(response: unknown, status = 200) {
  const body = JSON.stringify(response);
  return mock.fn(async () => ({
    ok: status >= 200 && status < 300,
    status,
    text: async () => body,
    json: async () => response,
  }));
}

test('LoomClient strips trailing slash from baseUrl', () => {
  const client = new LoomClient('http://localhost:7890/');
  // Access private field via cast for verification
  assert.equal((client as unknown as Record<string, string>)['baseUrl'], 'http://localhost:7890');
});

test('LoomClient sets Authorization header when token provided', () => {
  const client = new LoomClient('http://localhost:7890', 'my-token');
  const headers = (client as unknown as Record<string, Record<string, string>>)['headers'];
  assert.equal(headers['Authorization'], 'Bearer my-token');
});

test('checkpoint calls POST /api/v1/checkpoint with correct body', async () => {
  const fakeCheckpoint = {
    id: 'cp1',
    stream_id: 's1',
    seq: 1,
    title: 'Initial',
    author: 'user',
    timestamp: '2026-01-01T00:00:00Z',
    source: 'sdk',
  };

  const fetcher = mockFetch(fakeCheckpoint);
  globalThis.fetch = fetcher as unknown as typeof fetch;

  const client = new LoomClient('http://localhost:7890');
  const result = await client.checkpoint('Initial', { summary: 'first commit', tags: { env: 'test' } });

  assert.equal(result.id, 'cp1');
  assert.equal(fetcher.mock.calls.length, 1);

  const [url, init] = fetcher.mock.calls[0].arguments as [string, RequestInit];
  assert.equal(url, 'http://localhost:7890/api/v1/checkpoint');
  assert.equal(init.method, 'POST');

  const sentBody = JSON.parse(init.body as string);
  assert.equal(sentBody.title, 'Initial');
  assert.equal(sentBody.summary, 'first commit');
  assert.deepEqual(sentBody.tags, { env: 'test' });
});

test('rollback sends full scope when no entityId', async () => {
  const fetcher = mockFetch('', 200);
  globalThis.fetch = fetcher as unknown as typeof fetch;

  const client = new LoomClient('http://localhost:7890');
  await client.rollback('cp1');

  const [url, init] = fetcher.mock.calls[0].arguments as [string, RequestInit];
  assert.equal(url, 'http://localhost:7890/api/v1/rollback');
  const body = JSON.parse(init.body as string);
  assert.equal(body.scope, 'full');
  assert.equal(body.checkpoint_id, 'cp1');
});

test('rollback sends entity scope when entityId provided', async () => {
  const fetcher = mockFetch('', 200);
  globalThis.fetch = fetcher as unknown as typeof fetch;

  const client = new LoomClient('http://localhost:7890');
  await client.rollback('cp1', 'entity-42');

  const [, init] = fetcher.mock.calls[0].arguments as [string, RequestInit];
  const body = JSON.parse(init.body as string);
  assert.equal(body.scope, 'entity');
  assert.equal(body.entity_id, 'entity-42');
});

test('diff builds correct query params', async () => {
  const fakeDiff = {
    from_seq: 1, to_seq: 2, from_ref: 'HEAD~1', to_ref: 'HEAD',
    spaces: [], summary: { spaces_changed: 0, entities_created: 0, entities_modified: 0, entities_deleted: 0, total_operations: 0 },
  };

  const fetcher = mockFetch(fakeDiff);
  globalThis.fetch = fetcher as unknown as typeof fetch;

  const client = new LoomClient('http://localhost:7890');
  await client.diff('HEAD~1', { space: 'code', summary: true });

  const [url] = fetcher.mock.calls[0].arguments as [string];
  assert.match(url, /from=HEAD~1/);
  assert.match(url, /space=code/);
  assert.match(url, /summary=true/);
});

test('log passes limit param', async () => {
  const fetcher = mockFetch([]);
  globalThis.fetch = fetcher as unknown as typeof fetch;

  const client = new LoomClient('http://localhost:7890');
  await client.log(5);

  const [url] = fetcher.mock.calls[0].arguments as [string];
  assert.match(url, /limit=5/);
});

test('search URL-encodes query', async () => {
  const fetcher = mockFetch([]);
  globalThis.fetch = fetcher as unknown as typeof fetch;

  const client = new LoomClient('http://localhost:7890');
  await client.search('auth & login');

  const [url] = fetcher.mock.calls[0].arguments as [string];
  assert.match(url, /q=auth%20%26%20login/);
});

test('record base64-encodes string content', async () => {
  const fetcher = mockFetch({ ok: true });
  globalThis.fetch = fetcher as unknown as typeof fetch;

  const client = new LoomClient('http://localhost:7890');
  await client.record('code', 'main.go', 'hello');

  const [, init] = fetcher.mock.calls[0].arguments as [string, RequestInit];
  const body = JSON.parse(init.body as string);
  assert.equal(body.content, btoa('hello'));
  assert.equal(body.space_id, 'code');
  assert.equal(body.path, 'main.go');
});

test('get throws on non-ok response', async () => {
  const fetcher = mock.fn(async () => ({
    ok: false,
    status: 404,
    text: async () => 'not found',
  }));
  globalThis.fetch = fetcher as unknown as typeof fetch;

  const client = new LoomClient('http://localhost:7890');
  await assert.rejects(
    () => client.status(),
    /Loom API error 404/,
  );
});
