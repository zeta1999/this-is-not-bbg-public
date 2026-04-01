use serde::{Deserialize, Serialize};
use std::io::{Read, Write};
use std::os::unix::net::UnixStream;
use std::sync::Mutex;
use tauri::{Emitter, State};

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ConnectionStatus {
    pub connected: bool,
    pub server_addr: String,
    pub latency_ms: u64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SubscribeRequest {
    pub channel: String,
    pub instrument: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct QueryRequest {
    pub query_type: String,
    pub instrument: Option<String>,
    pub timeframe: Option<String>,
    pub limit: Option<u32>,
}

// ---------------------------------------------------------------------------
// App state
// ---------------------------------------------------------------------------

pub struct AppState {
    pub connected: Mutex<bool>,
    pub server_addr: Mutex<String>,
    pub stream: Mutex<Option<UnixStream>>,
}

impl Default for AppState {
    fn default() -> Self {
        Self {
            connected: Mutex::new(false),
            server_addr: Mutex::new(String::from("/tmp/notbbg.sock")),
            stream: Mutex::new(None),
        }
    }
}

// ---------------------------------------------------------------------------
// Frame helpers
// ---------------------------------------------------------------------------

fn write_frame(stream: &mut UnixStream, payload: &[u8]) -> Result<(), String> {
    let len = payload.len() as u32;
    stream
        .write_all(&len.to_be_bytes())
        .map_err(|e| e.to_string())?;
    stream.write_all(payload).map_err(|e| e.to_string())?;
    Ok(())
}

fn read_frame(stream: &mut UnixStream) -> Result<Vec<u8>, String> {
    let mut len_buf = [0u8; 4];
    stream
        .read_exact(&mut len_buf)
        .map_err(|e| e.to_string())?;
    let len = u32::from_be_bytes(len_buf) as usize;
    if len > 16 * 1024 * 1024 {
        return Err("frame too large".into());
    }
    let mut payload = vec![0u8; len];
    stream
        .read_exact(&mut payload)
        .map_err(|e| e.to_string())?;
    Ok(payload)
}

// ---------------------------------------------------------------------------
// IPC Commands
// ---------------------------------------------------------------------------

/// Connect to the notbbg-server via Unix socket and start streaming data.
#[tauri::command]
pub async fn connect_server(
    app: tauri::AppHandle,
    state: State<'_, AppState>,
    addr: Option<String>,
) -> Result<ConnectionStatus, String> {
    let socket_path = addr.unwrap_or_else(|| "/tmp/notbbg.sock".to_string());

    let mut stream =
        UnixStream::connect(&socket_path).map_err(|e| format!("connect {}: {}", socket_path, e))?;

    // Send subscribe request.
    let sub_msg = serde_json::json!({
        "type": "subscribe",
        "patterns": ["ohlc.*.*", "lob.*.*", "trade.*.*", "news", "alert", "feed.status", "indicator.*"]
    });
    let sub_bytes = serde_json::to_vec(&sub_msg).map_err(|e| e.to_string())?;
    write_frame(&mut stream, &sub_bytes)?;

    // Store connection.
    {
        let mut connected = state.connected.lock().map_err(|e| e.to_string())?;
        *connected = true;
        let mut server_addr = state.server_addr.lock().map_err(|e| e.to_string())?;
        *server_addr = socket_path.clone();
    }

    // Clone stream for the reading thread.
    let read_stream = stream.try_clone().map_err(|e| e.to_string())?;
    {
        let mut st = state.stream.lock().map_err(|e| e.to_string())?;
        *st = Some(stream);
    }

    // Spawn a thread to read frames and emit Tauri events.
    std::thread::spawn(move || {
        let mut s = read_stream;
        loop {
            match read_frame(&mut s) {
                Ok(frame) => {
                    if let Ok(msg) = serde_json::from_slice::<serde_json::Value>(&frame) {
                        let msg_type = msg.get("type").and_then(|t| t.as_str()).unwrap_or("");
                        let topic = msg
                            .get("topic")
                            .and_then(|t| t.as_str())
                            .unwrap_or("");

                        if msg_type == "update" {
                            // Emit typed events to the frontend.
                            if topic.starts_with("ohlc.") {
                                let _ = app.emit("ohlc-update", &msg);
                            } else if topic.starts_with("lob.") {
                                let _ = app.emit("lob-update", &msg);
                            } else if topic.starts_with("trade.") {
                                let _ = app.emit("trade-update", &msg);
                            } else if topic == "news" {
                                let _ = app.emit("news-update", &msg);
                            } else if topic == "alert" {
                                let _ = app.emit("alert-update", &msg);
                            } else if topic == "feed.status" {
                                let _ = app.emit("feed-status-update", &msg);
                            } else if topic.starts_with("indicator.") {
                                let _ = app.emit("indicator-update", &msg);
                            }
                            // Also emit generic update for anything else.
                            let _ = app.emit("server-update", &msg);
                        }
                    }
                }
                Err(_) => {
                    let _ = app.emit("server-disconnected", "");
                    break;
                }
            }
        }
    });

    Ok(ConnectionStatus {
        connected: true,
        server_addr: socket_path,
        latency_ms: 1,
    })
}

/// Subscribe to additional data channels.
#[tauri::command]
pub async fn subscribe(
    state: State<'_, AppState>,
    request: SubscribeRequest,
) -> Result<String, String> {
    let mut guard = state.stream.lock().map_err(|e| e.to_string())?;
    let stream = guard.as_mut().ok_or("not connected")?;

    let pattern = match &request.instrument {
        Some(inst) => format!("{}.*.{}", request.channel, inst),
        None => format!("{}.*.*", request.channel),
    };

    let msg = serde_json::json!({
        "type": "subscribe",
        "patterns": [pattern]
    });
    let bytes = serde_json::to_vec(&msg).map_err(|e| e.to_string())?;
    write_frame(stream, &bytes)?;

    Ok(format!("Subscribed to {}", pattern))
}

/// Query data (not yet implemented — returns empty for now).
#[tauri::command]
pub async fn query(
    _state: State<'_, AppState>,
    _request: QueryRequest,
) -> Result<serde_json::Value, String> {
    Ok(serde_json::json!([]))
}

/// Get current feed statuses.
#[tauri::command]
pub async fn get_feed_status(
    state: State<'_, AppState>,
) -> Result<Vec<serde_json::Value>, String> {
    let connected = state.connected.lock().map_err(|e| e.to_string())?;
    if !*connected {
        return Err("not connected".into());
    }
    // Feed statuses come via events; this is a placeholder for direct query.
    Ok(vec![])
}
