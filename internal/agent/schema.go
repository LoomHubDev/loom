package agent

// ToolDefinitions returns JSON tool definitions for LLM function calling.
func ToolDefinitions() []byte {
	return []byte(toolDefsJSON)
}

const toolDefsJSON = `[
  {
    "name": "loom_checkpoint",
    "description": "Create a named version checkpoint. Use before risky operations or after completing a unit of work.",
    "parameters": {
      "type": "object",
      "properties": {
        "title": {"type": "string", "description": "Short descriptive title for the checkpoint"},
        "summary": {"type": "string", "description": "Longer description of what changed"},
        "tags": {"type": "object", "description": "Key-value metadata tags"}
      },
      "required": ["title"]
    }
  },
  {
    "name": "loom_rollback",
    "description": "Rollback to a previous checkpoint. Creates a guard checkpoint first.",
    "parameters": {
      "type": "object",
      "properties": {
        "checkpoint_id": {"type": "string", "description": "The checkpoint ID to rollback to"},
        "entity_id": {"type": "string", "description": "Optional: restore only this entity"}
      },
      "required": ["checkpoint_id"]
    }
  },
  {
    "name": "loom_diff",
    "description": "Get changes between two points in history.",
    "parameters": {
      "type": "object",
      "properties": {
        "from": {"type": "string", "description": "Start ref (checkpoint ID, HEAD, HEAD~N)"},
        "to": {"type": "string", "description": "End ref (default: HEAD)"},
        "space": {"type": "string", "description": "Filter to one space"},
        "summary_only": {"type": "boolean", "description": "Return summary only"}
      },
      "required": ["from"]
    }
  },
  {
    "name": "loom_status",
    "description": "Get current project versioning status.",
    "parameters": {"type": "object", "properties": {}}
  },
  {
    "name": "loom_log",
    "description": "Get recent checkpoints.",
    "parameters": {
      "type": "object",
      "properties": {
        "limit": {"type": "integer", "description": "Max checkpoints to return (default: 10)"}
      }
    }
  },
  {
    "name": "loom_search",
    "description": "Search checkpoints by title or summary.",
    "parameters": {
      "type": "object",
      "properties": {
        "query": {"type": "string", "description": "Search query"}
      },
      "required": ["query"]
    }
  },
  {
    "name": "loom_explain",
    "description": "Get a natural language explanation of what changed between two points.",
    "parameters": {
      "type": "object",
      "properties": {
        "from": {"type": "string", "description": "Start ref"},
        "to": {"type": "string", "description": "End ref (default: HEAD)"}
      },
      "required": ["from"]
    }
  },
  {
    "name": "loom_record",
    "description": "Record a file change made by the agent.",
    "parameters": {
      "type": "object",
      "properties": {
        "space_id": {"type": "string", "description": "Space (e.g. code, docs)"},
        "path": {"type": "string", "description": "File path relative to space"},
        "content": {"type": "string", "description": "Base64-encoded file content"}
      },
      "required": ["space_id", "path", "content"]
    }
  }
]`
