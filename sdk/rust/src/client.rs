use base64::Engine as _;
use base64::engine::general_purpose::STANDARD as BASE64;
use reqwest::Client;
use std::collections::HashMap;

use crate::error::LoomError;
use crate::types::*;

pub struct LoomClient {
    base_url: String,
    client: Client,
    token: Option<String>,
}

impl LoomClient {
    pub fn new(base_url: &str, token: Option<&str>) -> Self {
        Self {
            base_url: base_url.trim_end_matches('/').to_string(),
            client: Client::new(),
            token: token.map(|t| t.to_string()),
        }
    }

    pub async fn checkpoint(
        &self,
        title: &str,
        summary: Option<&str>,
        tags: Option<HashMap<String, String>>,
    ) -> Result<Checkpoint, LoomError> {
        let body = CheckpointRequest {
            title: title.to_string(),
            summary: summary.unwrap_or_default().to_string(),
            tags: tags.unwrap_or_default(),
        };
        self.post("/api/v1/checkpoint", &body).await
    }

    pub async fn rollback(
        &self,
        checkpoint_id: &str,
        entity_id: Option<&str>,
    ) -> Result<(), LoomError> {
        let body = RollbackRequest {
            checkpoint_id: checkpoint_id.to_string(),
            scope: if entity_id.is_some() { "entity".into() } else { "full".into() },
            entity_id: entity_id.map(|e| e.to_string()),
        };
        self.post_no_response("/api/v1/rollback", &body).await
    }

    pub async fn diff(
        &self,
        from: &str,
        to: Option<&str>,
        space: Option<&str>,
        summary: bool,
    ) -> Result<DiffResult, LoomError> {
        let mut params = vec![
            ("from", from.to_string()),
            ("to", to.unwrap_or("HEAD").to_string()),
        ];
        if let Some(s) = space {
            params.push(("space", s.to_string()));
        }
        if summary {
            params.push(("summary", "true".to_string()));
        }
        self.get("/api/v1/diff", &params).await
    }

    pub async fn log(&self, limit: u32) -> Result<Vec<Checkpoint>, LoomError> {
        self.get("/api/v1/log", &[("limit", limit.to_string())]).await
    }

    pub async fn status(&self) -> Result<StatusResult, LoomError> {
        self.get::<StatusResult>("/api/v1/status", &[]).await
    }

    pub async fn search(&self, query: &str, limit: u32) -> Result<Vec<Checkpoint>, LoomError> {
        self.get(
            "/api/v1/search",
            &[("q", query.to_string()), ("limit", limit.to_string())],
        )
        .await
    }

    pub async fn explain(
        &self,
        from: &str,
        to: Option<&str>,
    ) -> Result<ExplainResult, LoomError> {
        let body = ExplainRequest {
            from: from.to_string(),
            to: to.unwrap_or("HEAD").to_string(),
        };
        self.post("/api/v1/explain", &body).await
    }

    pub async fn record(
        &self,
        space_id: &str,
        path: &str,
        content: &[u8],
    ) -> Result<(), LoomError> {
        let encoded = BASE64.encode(content);
        let body = RecordRequest {
            space_id: space_id.to_string(),
            path: path.to_string(),
            content: encoded,
        };
        self.post_no_response("/api/v1/record", &body).await
    }

    pub async fn tools(&self) -> Result<Vec<ToolDefinition>, LoomError> {
        self.get("/api/v1/tools", &[]).await
    }

    // Private helpers

    async fn get<T: serde::de::DeserializeOwned>(
        &self,
        path: &str,
        params: &[(&str, String)],
    ) -> Result<T, LoomError> {
        let mut req = self.client.get(format!("{}{}", self.base_url, path));
        if !params.is_empty() {
            req = req.query(params);
        }
        if let Some(ref token) = self.token {
            req = req.bearer_auth(token);
        }
        let resp = req.send().await?;
        if !resp.status().is_success() {
            let status = resp.status().as_u16();
            let message = resp.text().await.unwrap_or_default();
            return Err(LoomError::Api { status, message });
        }
        Ok(resp.json().await?)
    }

    async fn post<T: serde::de::DeserializeOwned>(
        &self,
        path: &str,
        body: &impl serde::Serialize,
    ) -> Result<T, LoomError> {
        let mut req = self
            .client
            .post(format!("{}{}", self.base_url, path))
            .json(body);
        if let Some(ref token) = self.token {
            req = req.bearer_auth(token);
        }
        let resp = req.send().await?;
        if !resp.status().is_success() {
            let status = resp.status().as_u16();
            let message = resp.text().await.unwrap_or_default();
            return Err(LoomError::Api { status, message });
        }
        Ok(resp.json().await?)
    }

    async fn post_no_response(
        &self,
        path: &str,
        body: &impl serde::Serialize,
    ) -> Result<(), LoomError> {
        let mut req = self
            .client
            .post(format!("{}{}", self.base_url, path))
            .json(body);
        if let Some(ref token) = self.token {
            req = req.bearer_auth(token);
        }
        let resp = req.send().await?;
        if !resp.status().is_success() {
            let status = resp.status().as_u16();
            let message = resp.text().await.unwrap_or_default();
            return Err(LoomError::Api { status, message });
        }
        Ok(())
    }
}
