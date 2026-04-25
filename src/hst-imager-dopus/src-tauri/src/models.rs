use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct EngineSettings {
    pub engine_path_override: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct CommandButton {
    pub id: String,
    pub label: String,
    pub action: String,
    pub enabled: bool,
    pub hint: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct EngineError {
    pub message: String,
    pub stderr: String,
    pub stdout: String,
    pub code: Option<i32>,
    pub command: Vec<String>,
}
