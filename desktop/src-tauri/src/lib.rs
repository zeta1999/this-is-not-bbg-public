mod commands;

use commands::AppState;

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .manage(AppState::default())
        .invoke_handler(tauri::generate_handler![
            commands::connect_server,
            commands::subscribe,
            commands::query,
            commands::get_feed_status,
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
