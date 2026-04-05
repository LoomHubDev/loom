# loom-sdk

Rust SDK for the [Loom](https://loomhub.dev) versioning system.

## Installation

```toml
[dependencies]
loom-sdk = "0.1"
tokio = { version = "1", features = ["full"] }
```

Or via cargo:

```sh
cargo add loom-sdk
```

## Usage

```rust
use loom_sdk::LoomClient;

#[tokio::main]
async fn main() -> Result<(), loom_sdk::LoomError> {
    let client = LoomClient::new("http://localhost:7700", None);

    // Create a checkpoint
    let cp = client.checkpoint("feat: add login", Some("Added OAuth login flow"), None).await?;
    println!("Checkpoint: {} (seq {})", cp.id, cp.seq.unwrap_or(0));

    // View diff between last two checkpoints
    let diff = client.diff("HEAD~1", Some("HEAD"), Some("code"), true).await?;
    if let Some(summary) = diff.summary {
        println!(
            "Changed: {} entities modified, {} created",
            summary.entities_modified.unwrap_or(0),
            summary.entities_created.unwrap_or(0),
        );
    }

    // Rollback to a specific checkpoint (full scope)
    client.rollback(&cp.id, None).await?;

    // Search history
    let results = client.search("auth", 10).await?;
    for r in results {
        println!("- [{}] {}", r.seq.unwrap_or(0), r.title);
    }

    // Get current status
    let status = client.status().await?;
    println!("Head seq: {}", status.head_seq.unwrap_or(0));

    Ok(())
}
```

## API

| Method | Description |
|---|---|
| `checkpoint(title, summary, tags)` | Create a new checkpoint |
| `rollback(checkpoint_id, entity_id)` | Roll back to a checkpoint (full or entity scope) |
| `diff(from, to, space, summary)` | Get diff between two refs |
| `log(limit)` | List recent checkpoints |
| `status()` | Get current project status |
| `search(query, limit)` | Search checkpoints |
| `explain(from, to)` | Get an AI explanation of changes |
| `record(space_id, path, content)` | Record file content to a space |
| `tools()` | List available tool definitions |

## Authentication

Pass a bearer token as the second argument to `LoomClient::new`:

```rust
let client = LoomClient::new("http://localhost:7700", Some("my-token"));
```

## License

MIT
