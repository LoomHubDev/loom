import type {
  Checkpoint,
  CheckpointOptions,
  DiffOptions,
  DiffResult,
  ExplainResult,
  StatusResult,
  ToolDefinition,
} from './types';

export class LoomClient {
  private baseUrl: string;
  private headers: Record<string, string>;

  constructor(baseUrl = 'http://localhost:7890', token?: string) {
    this.baseUrl = baseUrl.replace(/\/+$/, '');
    this.headers = { 'Content-Type': 'application/json' };
    if (token) {
      this.headers['Authorization'] = `Bearer ${token}`;
    }
  }

  async checkpoint(title: string, options?: CheckpointOptions): Promise<Checkpoint> {
    return this.post<Checkpoint>('/api/v1/checkpoint', {
      title,
      summary: options?.summary ?? '',
      tags: options?.tags ?? {},
    });
  }

  async rollback(checkpointId: string, entityId?: string): Promise<void> {
    const body: Record<string, string> = { checkpoint_id: checkpointId };
    if (entityId) {
      body.scope = 'entity';
      body.entity_id = entityId;
    } else {
      body.scope = 'full';
    }
    await this.post('/api/v1/rollback', body);
  }

  async diff(from: string, options?: DiffOptions): Promise<DiffResult> {
    const params = new URLSearchParams({ from, to: options?.to ?? 'HEAD' });
    if (options?.space) params.set('space', options.space);
    if (options?.summary) params.set('summary', 'true');
    return this.get<DiffResult>(`/api/v1/diff?${params}`);
  }

  async log(limit = 10): Promise<Checkpoint[]> {
    return this.get<Checkpoint[]>(`/api/v1/log?limit=${limit}`);
  }

  async status(): Promise<StatusResult> {
    return this.get<StatusResult>('/api/v1/status');
  }

  async search(query: string, limit = 10): Promise<Checkpoint[]> {
    return this.get<Checkpoint[]>(`/api/v1/search?q=${encodeURIComponent(query)}&limit=${limit}`);
  }

  async explain(from: string, to = 'HEAD'): Promise<ExplainResult> {
    return this.post<ExplainResult>('/api/v1/explain', { from, to });
  }

  async record(spaceId: string, path: string, content: Uint8Array | string): Promise<void> {
    const encoded =
      typeof content === 'string'
        ? btoa(content)
        : btoa(String.fromCharCode(...content));
    await this.post('/api/v1/record', { space_id: spaceId, path, content: encoded });
  }

  async tools(): Promise<ToolDefinition[]> {
    return this.get<ToolDefinition[]>('/api/v1/tools');
  }

  private async get<T>(path: string): Promise<T> {
    const resp = await fetch(`${this.baseUrl}${path}`, { headers: this.headers });
    if (!resp.ok) throw new Error(`Loom API error ${resp.status}: ${await resp.text()}`);
    return resp.json() as Promise<T>;
  }

  private async post<T>(path: string, body: unknown): Promise<T> {
    const resp = await fetch(`${this.baseUrl}${path}`, {
      method: 'POST',
      headers: this.headers,
      body: JSON.stringify(body),
    });
    if (!resp.ok) throw new Error(`Loom API error ${resp.status}: ${await resp.text()}`);
    const text = await resp.text();
    return text ? JSON.parse(text) : undefined;
  }
}
