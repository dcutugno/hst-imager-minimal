use crate::{
    config,
    engine::{
        build_fs_copy_args, build_fs_delete_args, build_fs_dir_args, build_fs_extract_args,
        build_fs_mkdir_args, build_fs_rename_args, build_info_args, image_container_probe_paths,
        EngineClient,
    },
    models::{CommandButton, EngineError, EngineSettings},
};
use serde_json::Value;
use tauri::AppHandle;

#[tauri::command]
pub fn browse_path(app: AppHandle, path: String) -> Result<Value, EngineError> {
    let engine = EngineClient::resolve(&app)?;
    match engine.run_json(&build_fs_dir_args(&path)) {
        Ok(value) => Ok(value),
        Err(original_error) => {
            for candidate in image_container_probe_paths(&path) {
                if let Ok(value) = engine.run_json(&build_fs_dir_args(&candidate)) {
                    return Ok(value);
                }
            }
            Err(original_error)
        }
    }
}

#[tauri::command]
pub fn copy_entries(
    app: AppHandle,
    sources: Vec<String>,
    destination: String,
) -> Result<(), EngineError> {
    if sources.is_empty() {
        return Err(simple_error("copy requires at least one selected source"));
    }
    EngineClient::resolve(&app)?.run_json(&build_fs_copy_args(&sources, &destination))?;
    Ok(())
}

#[tauri::command]
pub fn extract_entry(
    app: AppHandle,
    source: String,
    destination: String,
) -> Result<(), EngineError> {
    EngineClient::resolve(&app)?.run_json(&build_fs_extract_args(&source, &destination))?;
    Ok(())
}

#[tauri::command]
pub fn make_directory(app: AppHandle, path: String) -> Result<(), EngineError> {
    EngineClient::resolve(&app)?.run_json(&build_fs_mkdir_args(&path))?;
    Ok(())
}

#[tauri::command]
pub fn delete_entries(app: AppHandle, paths: Vec<String>) -> Result<(), EngineError> {
    if paths.is_empty() {
        return Err(simple_error("delete requires at least one selected path"));
    }
    for path in paths {
        EngineClient::resolve(&app)?.run_json(&build_fs_delete_args(&[path]))?;
    }
    Ok(())
}

#[tauri::command]
pub fn rename_entry(
    app: AppHandle,
    source: String,
    destination: String,
) -> Result<(), EngineError> {
    EngineClient::resolve(&app)?.run_json(&build_fs_rename_args(&source, &destination))?;
    Ok(())
}

#[tauri::command]
pub fn inspect_path(app: AppHandle, path: String) -> Result<String, EngineError> {
    EngineClient::resolve(&app)?.run_text(&build_info_args(&path))
}

#[tauri::command]
pub fn get_engine_settings() -> EngineSettings {
    config::read_settings()
}

#[tauri::command]
pub fn save_engine_settings(settings: EngineSettings) -> Result<(), String> {
    config::write_settings(&settings).map_err(|err| err.to_string())
}

#[tauri::command]
pub fn get_command_bar() -> Vec<CommandButton> {
    config::read_command_bar()
}

#[tauri::command]
pub fn save_command_bar(buttons: Vec<CommandButton>) -> Result<(), String> {
    config::write_command_bar(&buttons).map_err(|err| err.to_string())
}

fn simple_error(message: &str) -> EngineError {
    EngineError {
        message: message.into(),
        stderr: String::new(),
        stdout: String::new(),
        code: None,
        command: vec![],
    }
}
