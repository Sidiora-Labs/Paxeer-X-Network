#![forbid(unsafe_code)]

use std::io::{Read as _, Write as _};
use std::net::TcpListener;
use std::path::PathBuf;
use std::process::ExitCode;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use layerx_intent_ingester::{
    hex, readyz_body, Config, HttpRpc, IngestError, Ingester, Status, DEFAULT_MAX_RANGE,
    DEFAULT_RETENTION_WINDOW,
};

type Shared = Arc<Mutex<(Status, Option<String>)>>;

fn env_u64(name: &str, default: u64) -> Result<u64, IngestError> {
    std::env::var(name).map_or(Ok(default), |text| {
        text.parse()
            .map_err(|_| IngestError::Configuration(format!("{name}={text}")))
    })
}

fn required(name: &str) -> Result<String, IngestError> {
    std::env::var(name).map_err(|_| IngestError::Configuration(format!("{name} is required")))
}

fn serve_readyz(listener: &TcpListener, shared: &Shared) {
    for stream in listener.incoming() {
        let Ok(mut stream) = stream else { continue };
        let _ = stream.set_read_timeout(Some(Duration::from_secs(2)));
        let mut request = [0_u8; 1024];
        let read = stream.read(&mut request).unwrap_or_default();
        let line = String::from_utf8_lossy(&request[..read]);
        let target = line.split_whitespace().nth(1).unwrap_or_default();
        let (code, body) = if target == "/readyz" {
            let guard = shared
                .lock()
                .unwrap_or_else(std::sync::PoisonError::into_inner);
            let body = readyz_body(&guard.0, guard.1.as_deref());
            (
                if guard.1.is_none() {
                    "200 OK"
                } else {
                    "503 Service Unavailable"
                },
                body,
            )
        } else {
            ("404 Not Found", String::from("{}"))
        };
        let _ = stream.write_all(
            format!(
                "HTTP/1.1 {code}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
                body.len()
            )
            .as_bytes(),
        );
    }
}

fn run() -> Result<(), IngestError> {
    let rpc = HttpRpc::new(
        &required("INGESTER_RPC_URL")?,
        Duration::from_millis(env_u64("INGESTER_RPC_TIMEOUT_MS", 10_000)?),
    )?;
    let mut config = Config::new(PathBuf::from(required("INGESTER_STATE_DIR")?));
    config.start_block = std::env::var("INGESTER_START_BLOCK")
        .ok()
        .map(|text| {
            text.parse()
                .map_err(|_| IngestError::Configuration(format!("INGESTER_START_BLOCK={text}")))
        })
        .transpose()?;
    config.retention_window = env_u64("INGESTER_RETENTION_WINDOW", DEFAULT_RETENTION_WINDOW)?;
    config.max_range = env_u64("INGESTER_MAX_RANGE", DEFAULT_MAX_RANGE)?;
    let interval = Duration::from_millis(env_u64("INGESTER_INTERVAL_MS", 1_000)?);
    let listen = std::env::var("INGESTER_LISTEN").unwrap_or_else(|_| "127.0.0.1:8490".to_owned());

    let mut ingester = Ingester::open(rpc, &config)?;
    let shared: Shared = Arc::new(Mutex::new((
        ingester.status(),
        Some("no poll yet".to_owned()),
    )));
    let listener = TcpListener::bind(&listen)?;
    let readyz_state = Arc::clone(&shared);
    std::thread::spawn(move || serve_readyz(&listener, &readyz_state));

    let mut alerted = None;
    loop {
        let result = ingester.poll();
        let status = ingester.status();
        if status.alert.is_some() && status.alert != alerted {
            alerted = status.alert;
            if let Some(alert) = alerted {
                eprintln!(
                    "window alert: cursor {} fell outside the retained window (earliest {}, head {}); blocks {}..{} skipped",
                    alert.cursor, alert.earliest, alert.head, alert.cursor, alert.earliest
                );
            }
        }
        let error = match result {
            Ok(fresh) => {
                for intent in &fresh {
                    println!(
                        "intent {} block {} log {} {:?}",
                        hex(&intent.id),
                        intent.block_number,
                        intent.log_index,
                        intent.event.kind()
                    );
                }
                None
            }
            Err(error) => {
                eprintln!("poll failed: {error}");
                Some(error.to_string())
            }
        };
        let behind = error.is_none() && status.lag > 0;
        *shared
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner) = (status, error);
        if !behind {
            std::thread::sleep(interval);
        }
    }
}

fn main() -> ExitCode {
    match run() {
        Ok(()) => ExitCode::SUCCESS,
        Err(error) => {
            eprintln!("layerx-intent-ingester: {error}");
            ExitCode::FAILURE
        }
    }
}
