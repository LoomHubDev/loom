# Loom SDK for Python

Python client for the [Loom](https://loomhub.dev) Agent API.

## Install

```
pip install loom-sdk
```

## Usage

```python
from loom_sdk import LoomClient

with LoomClient("http://localhost:7890") as loom:
    # Create checkpoint
    cp = loom.checkpoint("before refactor")
    
    # Check status
    status = loom.status()
    
    # View changes
    diff = loom.diff("HEAD~1", "HEAD")
    
    # Rollback if needed
    loom.rollback(cp["id"])
    
    # Search history
    results = loom.search("auth")
```

## API

All methods map 1:1 to the Loom Agent HTTP API.

| Method | Description |
|--------|-------------|
| checkpoint(title, summary, tags) | Create checkpoint |
| rollback(checkpoint_id, entity_id) | Rollback to checkpoint |
| diff(from_ref, to_ref, space, summary) | Get diff |
| log(limit) | List checkpoints |
| status() | Project status |
| search(query, limit) | Search checkpoints |
| explain(from_ref, to_ref) | Explain changes |
| record(space_id, path, content) | Record file change |
| tools() | Get LLM tool definitions |
