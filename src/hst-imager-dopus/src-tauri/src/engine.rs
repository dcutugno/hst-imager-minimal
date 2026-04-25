use crate::{config, models::EngineError};
use serde_json::Value;
use std::{path::PathBuf, process::Command};
use tauri::{AppHandle, Manager};

#[derive(Debug, Clone)]
pub struct EngineClient {
    executable: PathBuf,
}

impl EngineClient {
    pub fn resolve(app: &AppHandle) -> Result<Self, EngineError> {
        Ok(Self {
            executable: resolve_engine_path(app)?,
        })
    }

    pub fn command_line(&self, args: &[String]) -> Vec<String> {
        let mut command = vec![
            self.executable.to_string_lossy().to_string(),
            "--format".into(),
            "json".into(),
        ];
        command.extend(args.iter().cloned());
        command
    }

    pub fn run_json(&self, args: &[String]) -> Result<Value, EngineError> {
        let command_line = self.command_line(args);
        let output = Command::new(&self.executable)
            .arg("--format")
            .arg("json")
            .args(args)
            .env("HST_IMAGER_LEGACY_MODE", "off")
            .output()
            .map_err(|err| EngineError {
                message: format!("failed to start hst-imager-go: {err}"),
                stderr: String::new(),
                stdout: String::new(),
                code: None,
                command: command_line.clone(),
            })?;

        let stdout = String::from_utf8_lossy(&output.stdout).to_string();
        let stderr = String::from_utf8_lossy(&output.stderr).to_string();
        if !output.status.success() {
            return Err(EngineError {
                message: first_non_empty_line(&stderr)
                    .unwrap_or_else(|| "hst-imager-go command failed".into()),
                stderr,
                stdout,
                code: output.status.code(),
                command: command_line,
            });
        }

        serde_json::from_str(&stdout).map_err(|err| EngineError {
            message: format!("hst-imager-go returned invalid JSON: {err}"),
            stderr,
            stdout,
            code: output.status.code(),
            command: command_line,
        })
    }

    pub fn run_text(&self, args: &[String]) -> Result<String, EngineError> {
        let command_line = self.command_line(args);
        let output = Command::new(&self.executable)
            .arg("--format")
            .arg("json")
            .args(args)
            .env("HST_IMAGER_LEGACY_MODE", "off")
            .output()
            .map_err(|err| EngineError {
                message: format!("failed to start hst-imager-go: {err}"),
                stderr: String::new(),
                stdout: String::new(),
                code: None,
                command: command_line.clone(),
            })?;

        let stdout = String::from_utf8_lossy(&output.stdout).to_string();
        let stderr = String::from_utf8_lossy(&output.stderr).to_string();
        if !output.status.success() {
            return Err(EngineError {
                message: first_non_empty_line(&stderr)
                    .unwrap_or_else(|| "hst-imager-go command failed".into()),
                stderr,
                stdout,
                code: output.status.code(),
                command: command_line,
            });
        }
        Ok(stdout)
    }
}

pub fn build_fs_dir_args(path: &str) -> Vec<String> {
    vec!["fs".into(), "dir".into(), path.into()]
}

pub fn image_container_probe_paths(path: &str) -> Vec<String> {
    if has_virtual_container_suffix(path) {
        return vec![];
    }
    ["rdb", "mbr", "gpt"]
        .iter()
        .map(|container| format!("{}\\{}", path.trim_end_matches(['\\', '/']), container))
        .collect()
}

pub fn build_fs_copy_args(sources: &[String], destination: &str) -> Vec<String> {
    let mut args = vec!["fs".into(), "copy".into()];
    args.extend(sources.iter().cloned());
    args.push(destination.into());
    args
}

fn has_virtual_container_suffix(path: &str) -> bool {
    let lower = path.to_lowercase();
    ["\\rdb", "\\mbr", "\\gpt", "/rdb", "/mbr", "/gpt"]
        .iter()
        .any(|suffix| lower.ends_with(suffix))
}

pub fn build_fs_extract_args(source: &str, destination: &str) -> Vec<String> {
    vec![
        "fs".into(),
        "extract".into(),
        source.into(),
        destination.into(),
    ]
}

pub fn build_fs_mkdir_args(path: &str) -> Vec<String> {
    vec!["fs".into(), "mkdir".into(), path.into()]
}

pub fn build_fs_delete_args(paths: &[String]) -> Vec<String> {
    let mut args = vec!["fs".into(), "delete".into()];
    args.extend(paths.iter().cloned());
    args
}

pub fn build_fs_rename_args(source: &str, destination: &str) -> Vec<String> {
    vec![
        "fs".into(),
        "rename".into(),
        source.into(),
        destination.into(),
    ]
}

pub fn build_info_args(path: &str) -> Vec<String> {
    vec!["info".into(), path.into()]
}

fn resolve_engine_path(app: &AppHandle) -> Result<PathBuf, EngineError> {
    if let Ok(path) = std::env::var("HST_DOPUS_ENGINE_PATH") {
        return require_executable(PathBuf::from(path), "HST_DOPUS_ENGINE_PATH");
    }

    if let Some(path) = config::read_settings().engine_path_override {
        if !path.trim().is_empty() {
            return require_executable(PathBuf::from(path), "settings.enginePathOverride");
        }
    }

    if let Ok(path) = app
        .path()
        .resolve(sidecar_file_name(), tauri::path::BaseDirectory::Resource)
    {
        if path.exists() {
            return Ok(path);
        }
    }

    for path in dev_engine_candidates() {
        if path.exists() {
            return Ok(path);
        }
    }

    Err(EngineError {
        message: "hst-imager-go binary was not found. Set HST_DOPUS_ENGINE_PATH or run npm run prepare-engine.".into(),
        stderr: String::new(),
        stdout: String::new(),
        code: None,
        command: vec![],
    })
}

fn dev_engine_candidates() -> Vec<PathBuf> {
    let manifest_dir = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    let current_dir = std::env::current_dir().unwrap_or_else(|_| manifest_dir.clone());
    vec![
        // npm run prepare-engine writes here for Tauri dev and packaging.
        manifest_dir.join(sidecar_file_name().trim_start_matches("binaries/")),
        manifest_dir.join("binaries").join(sidecar_binary_name()),
        current_dir.join(sidecar_file_name()),
        current_dir.join("src-tauri").join(sidecar_file_name()),
        // Useful when the Go CLI was built directly in its own project.
        manifest_dir
            .join("../../hst-imager-go")
            .join(executable_name()),
        current_dir.join("../hst-imager-go").join(executable_name()),
    ]
}

fn require_executable(path: PathBuf, source: &str) -> Result<PathBuf, EngineError> {
    if path.exists() {
        Ok(path)
    } else {
        Err(EngineError {
            message: format!(
                "{source} points to a missing hst-imager-go binary: {}",
                path.display()
            ),
            stderr: String::new(),
            stdout: String::new(),
            code: None,
            command: vec![],
        })
    }
}

fn executable_name() -> &'static str {
    if cfg!(windows) {
        "hst-imager-go.exe"
    } else {
        "hst-imager-go"
    }
}

fn sidecar_file_name() -> &'static str {
    if cfg!(all(target_os = "macos", target_arch = "aarch64")) {
        "binaries/hst-imager-go-aarch64-apple-darwin"
    } else if cfg!(target_os = "macos") {
        "binaries/hst-imager-go-x86_64-apple-darwin"
    } else if cfg!(target_os = "windows") {
        "binaries/hst-imager-go-x86_64-pc-windows-msvc.exe"
    } else if cfg!(target_arch = "aarch64") {
        "binaries/hst-imager-go-aarch64-unknown-linux-gnu"
    } else {
        "binaries/hst-imager-go-x86_64-unknown-linux-gnu"
    }
}

fn sidecar_binary_name() -> &'static str {
    sidecar_file_name()
        .rsplit('/')
        .next()
        .unwrap_or(sidecar_file_name())
}

fn first_non_empty_line(text: &str) -> Option<String> {
    text.lines()
        .map(str::trim)
        .find(|line| !line.is_empty())
        .map(str::to_string)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn constructs_json_fs_dir_command() {
        let client = EngineClient {
            executable: PathBuf::from("/tmp/hst-imager-go"),
        };

        assert_eq!(
            client.command_line(&build_fs_dir_args("/tmp/disk.img")),
            strings(&[
                "/tmp/hst-imager-go",
                "--format",
                "json",
                "fs",
                "dir",
                "/tmp/disk.img"
            ])
        );
    }

    #[test]
    fn constructs_multi_source_copy_command_without_native_fallback() {
        assert_eq!(
            build_fs_copy_args(&["a".into(), "b".into()], "dst"),
            strings(&["fs", "copy", "a", "b", "dst"])
        );
    }

    #[test]
    fn constructs_delete_and_rename_commands_without_native_fallback() {
        assert_eq!(
            build_fs_delete_args(&["a".into()]),
            strings(&["fs", "delete", "a"])
        );
        assert_eq!(
            build_fs_rename_args("a", "b"),
            strings(&["fs", "rename", "a", "b"])
        );
    }

    #[test]
    fn validates_missing_override_path() {
        let err =
            require_executable(PathBuf::from("/definitely/not/hst-imager-go"), "test").unwrap_err();
        assert!(err.message.contains("missing hst-imager-go binary"));
    }

    #[test]
    fn probes_known_image_container_paths() {
        assert_eq!(
            image_container_probe_paths("/tmp/disk.img"),
            strings(&[
                "/tmp/disk.img\\rdb",
                "/tmp/disk.img\\mbr",
                "/tmp/disk.img\\gpt"
            ])
        );
        assert!(image_container_probe_paths("/tmp/disk.img\\rdb").is_empty());
    }

    fn strings(values: &[&str]) -> Vec<String> {
        values.iter().map(|value| value.to_string()).collect()
    }
}
