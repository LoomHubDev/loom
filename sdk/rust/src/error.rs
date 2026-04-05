use thiserror::Error;

#[derive(Error, Debug)]
pub enum LoomError {
    #[error("HTTP error {status}: {message}")]
    Api { status: u16, message: String },

    #[error("Request failed: {0}")]
    Request(#[from] reqwest::Error),

    #[error("JSON error: {0}")]
    Json(#[from] serde_json::Error),
}
