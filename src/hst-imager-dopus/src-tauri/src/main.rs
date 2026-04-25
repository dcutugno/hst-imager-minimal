mod commands;
mod config;
mod engine;
mod models;

fn main() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![
            commands::browse_path,
            commands::copy_entries,
            commands::extract_entry,
            commands::make_directory,
            commands::delete_entries,
            commands::rename_entry,
            commands::inspect_path,
            commands::get_engine_settings,
            commands::save_engine_settings,
            commands::get_command_bar,
            commands::save_command_bar
        ])
        .run(tauri::generate_context!())
        .expect("error while running Hst Imager DOpus");
}
