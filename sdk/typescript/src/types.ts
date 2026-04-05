export interface Checkpoint {
  id: string;
  stream_id: string;
  seq: number;
  title: string;
  summary?: string;
  author: string;
  timestamp: string;
  source: string;
  spaces?: SpaceState[];
  tags?: Record<string, string>;
  parent_id?: string;
}

export interface SpaceState {
  space_id: string;
  status: string;
  summary: {
    entities_created: number;
    entities_modified: number;
    entities_deleted: number;
  };
}

export interface DiffResult {
  from_seq: number;
  to_seq: number;
  from_ref: string;
  to_ref: string;
  spaces: SpaceDiff[];
  summary: DiffSummary;
}

export interface SpaceDiff {
  space_id: string;
  entities: EntityDiff[];
  summary: { entities_created: number; entities_modified: number; entities_deleted: number };
}

export interface EntityDiff {
  entity_id: string;
  path: string;
  change: string;
  hunks?: DiffHunk[];
  summary?: string;
}

export interface DiffHunk {
  old_start: number;
  old_lines: number;
  new_start: number;
  new_lines: number;
  lines: { type: string; content: string }[];
}

export interface DiffSummary {
  spaces_changed: number;
  entities_created: number;
  entities_modified: number;
  entities_deleted: number;
  total_operations: number;
}

export interface StatusResult {
  project: string;
  stream: string;
  head_seq: number;
  last_checkpoint?: Checkpoint;
  pending_ops: number;
  spaces: Record<string, number>;
}

export interface ExplainResult {
  explanation: string;
  spaces_affected: string[];
  entities_affected: string[];
}

export interface CheckpointOptions {
  summary?: string;
  tags?: Record<string, string>;
}

export interface DiffOptions {
  to?: string;
  space?: string;
  summary?: boolean;
}

export interface ToolDefinition {
  name: string;
  description: string;
  parameters: Record<string, unknown>;
}
