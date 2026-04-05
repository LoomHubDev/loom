use std::collections::HashMap;
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Checkpoint {
    pub id: String,
    pub stream_id: Option<String>,
    pub seq: Option<u64>,
    pub title: String,
    pub summary: Option<String>,
    pub author: Option<String>,
    pub timestamp: Option<String>,
    pub source: Option<String>,
    pub spaces: Option<Vec<SpaceState>>,
    pub tags: Option<HashMap<String, String>>,
    pub parent_id: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SpaceState {
    pub space_id: String,
    pub status: Option<String>,
    pub summary: Option<SpaceSummary>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SpaceSummary {
    pub entities_created: Option<u64>,
    pub entities_modified: Option<u64>,
    pub entities_deleted: Option<u64>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DiffResult {
    pub from_seq: Option<u64>,
    pub to_seq: Option<u64>,
    pub from_ref: Option<String>,
    pub to_ref: Option<String>,
    pub spaces: Option<Vec<SpaceDiff>>,
    pub summary: Option<DiffSummary>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SpaceDiff {
    pub space_id: String,
    pub entities: Option<Vec<EntityDiff>>,
    pub summary: Option<DiffSummary>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EntityDiff {
    pub entity_id: String,
    pub path: Option<String>,
    pub change: Option<String>,
    pub hunks: Option<Vec<DiffHunk>>,
    pub summary: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DiffHunk {
    pub old_start: Option<u64>,
    pub old_lines: Option<u64>,
    pub new_start: Option<u64>,
    pub new_lines: Option<u64>,
    pub lines: Option<Vec<DiffLine>>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DiffLine {
    #[serde(rename = "type")]
    pub kind: Option<String>,
    pub content: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DiffSummary {
    pub spaces_changed: Option<u64>,
    pub entities_created: Option<u64>,
    pub entities_modified: Option<u64>,
    pub entities_deleted: Option<u64>,
    pub total_operations: Option<u64>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StatusResult {
    pub project: Option<String>,
    pub stream: Option<String>,
    pub head_seq: Option<u64>,
    pub last_checkpoint: Option<Checkpoint>,
    pub pending_ops: Option<u64>,
    pub spaces: Option<Vec<SpaceState>>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ExplainResult {
    pub explanation: Option<String>,
    pub spaces_affected: Option<Vec<String>>,
    pub entities_affected: Option<Vec<String>>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CheckpointRequest {
    pub title: String,
    pub summary: String,
    pub tags: HashMap<String, String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RollbackRequest {
    pub checkpoint_id: String,
    pub scope: String,
    pub entity_id: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ExplainRequest {
    pub from: String,
    pub to: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RecordRequest {
    pub space_id: String,
    pub path: String,
    pub content: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ToolDefinition {
    pub name: String,
    pub description: Option<String>,
    pub parameters: Option<serde_json::Value>,
}
