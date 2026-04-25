use crate::models::{CommandButton, EngineSettings};
use serde::{de::DeserializeOwned, Serialize};
use std::{fs, io, path::PathBuf};

const SETTINGS_FILE: &str = "settings.json";
const COMMAND_BAR_FILE: &str = "command-bar.json";

pub fn settings_dir() -> PathBuf {
    if let Ok(dir) = std::env::var("HST_DOPUS_CONFIG_DIR") {
        return PathBuf::from(dir);
    }
    dirs::config_dir()
        .unwrap_or_else(std::env::temp_dir)
        .join("hst-imager-dopus")
}

pub fn read_settings() -> EngineSettings {
    read_json(SETTINGS_FILE).unwrap_or(EngineSettings {
        engine_path_override: None,
    })
}

pub fn write_settings(settings: &EngineSettings) -> io::Result<()> {
    write_json(SETTINGS_FILE, settings)
}

pub fn read_command_bar() -> Vec<CommandButton> {
    read_json(COMMAND_BAR_FILE).unwrap_or_else(|_| default_command_bar())
}

pub fn write_command_bar(buttons: &[CommandButton]) -> io::Result<()> {
    write_json(COMMAND_BAR_FILE, buttons)
}

fn read_json<T: DeserializeOwned>(file_name: &str) -> io::Result<T> {
    let data = fs::read_to_string(settings_dir().join(file_name))?;
    serde_json::from_str(&data).map_err(|err| io::Error::new(io::ErrorKind::InvalidData, err))
}

fn write_json<T: Serialize + ?Sized>(file_name: &str, value: &T) -> io::Result<()> {
    let dir = settings_dir();
    fs::create_dir_all(&dir)?;
    let data = serde_json::to_string_pretty(value)
        .map_err(|err| io::Error::new(io::ErrorKind::InvalidData, err))?;
    fs::write(dir.join(file_name), format!("{data}\n"))
}

fn default_command_bar() -> Vec<CommandButton> {
    vec![
        button(
            "copy",
            "Copy",
            "copy",
            true,
            "Copy selected entries to the opposite pane using fs copy",
        ),
        button(
            "extract",
            "Extract",
            "extract",
            true,
            "Extract selected archive/image entry using fs extract",
        ),
        button(
            "mkdir",
            "Mkdir",
            "mkdir",
            true,
            "Create directory using fs mkdir",
        ),
        button(
            "info",
            "Info",
            "info",
            true,
            "Show hst-imager info for the active path",
        ),
        button(
            "delete",
            "Delete",
            "delete",
            false,
            "Disabled by default; hst-imager-go supports local fs delete",
        ),
        button(
            "rename",
            "Rename",
            "rename",
            false,
            "Disabled by default; hst-imager-go supports local fs rename",
        ),
        button("refresh", "Reread", "refresh", true, "Reload active pane"),
        button(
            "swap",
            "Swap",
            "swap",
            true,
            "Swap source and destination panes",
        ),
        button(
            "config",
            "Buttons",
            "config",
            true,
            "Edit command bank JSON",
        ),
    ]
}

fn button(id: &str, label: &str, action: &str, enabled: bool, hint: &str) -> CommandButton {
    CommandButton {
        id: id.into(),
        label: label.into(),
        action: action.into(),
        enabled,
        hint: hint.into(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn default_command_bar_keeps_destructive_future_ops_disabled() {
        let bar = default_command_bar();
        let delete = bar.iter().find(|button| button.action == "delete").unwrap();
        let rename = bar.iter().find(|button| button.action == "rename").unwrap();

        assert!(!delete.enabled);
        assert!(!rename.enabled);
    }
}
