#![forbid(unsafe_code)]

mod submit;

use std::collections::VecDeque;
use std::io::{Read as _, Write as _};
use std::net::TcpListener;
use std::path::PathBuf;
use std::process::ExitCode;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use layerx_intent_ingester::{
    hex, readyz_body, Config, HttpRpc, IngestError, Ingester, Journal, Status, DEFAULT_MAX_RANGE,
    DEFAULT_RETENTION_WINDOW,
};
use submit::{GatewayRpc, MarketMap, Outcome, Scope, Signer, Submitter};

/// Intents journaled but not yet submitted; polling pauses above this.
const MAX_BACKLOG: usize = 1_024;

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

    let gateway = GatewayRpc::new(
        &required("LAYERX_INGESTER_GATEWAY_URL")?,
        required("LAYERX_INGESTER_GATEWAY_API_KEY")?,
        Duration::from_millis(env_u64("INGESTER_RPC_TIMEOUT_MS", 10_000)?),
    )?;
    let signer = Signer::from_file(&PathBuf::from(required("LAYERX_INGESTER_SIGNER_KEY_FILE")?))?;
    let markets = MarketMap::parse(&std::fs::read_to_string(required(
        "LAYERX_INGESTER_MARKET_MAP",
    )?)?)?;
    let scope = Scope {
        protocol_version: u16::try_from(env_u64(
            "LAYERX_INGESTER_PROTOCOL_VERSION",
            u64::from(layerx_wire::limits::STATE_COMMITMENT_PROTOCOL_VERSION),
        )?)
        .map_err(|_| IngestError::Configuration("LAYERX_INGESTER_PROTOCOL_VERSION".to_owned()))?,
        network_id: u32::try_from(env_u64("LAYERX_INGESTER_NETWORK_ID", 0)?)
            .ok()
            .filter(|network| *network != 0)
            .ok_or_else(|| IngestError::Configuration("LAYERX_INGESTER_NETWORK_ID".to_owned()))?,
        fee_limit: u128::from(env_u64("LAYERX_INGESTER_FEE_LIMIT", 0)?),
    };
    if scope.fee_limit == 0 {
        return Err(IngestError::Configuration(
            "LAYERX_INGESTER_FEE_LIMIT is required".to_owned(),
        ));
    }

    let mut ingester = Ingester::open(rpc, &config)?;
    let mut submitter = Submitter::open(gateway, signer, scope, markets, &config.state_dir)?;
    let mut backlog = VecDeque::new();
    for intent in Journal::read_all(&config.state_dir.join("journal.jsonl"))? {
        submitter.observe(&intent);
        if !submitter.is_recorded(&intent.id) {
            backlog.push_back(intent);
        }
    }
    let shared: Shared = Arc::new(Mutex::new((
        ingester.status(),
        Some("no poll yet".to_owned()),
    )));
    let listener = TcpListener::bind(&listen)?;
    let readyz_state = Arc::clone(&shared);
    std::thread::spawn(move || serve_readyz(&listener, &readyz_state));

    let mut alerted = None;
    loop {
        // Backpressure: stop tailing while the kernel is behind.
        let result = if backlog.len() < MAX_BACKLOG {
            ingester.poll()
        } else {
            Ok(Vec::new())
        };
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
        let mut error = match result {
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
                backlog.extend(fresh);
                None
            }
            Err(error) => {
                eprintln!("poll failed: {error}");
                Some(error.to_string())
            }
        };
        while let Some(intent) = backlog.front() {
            match submitter.submit(intent, submit::now_ms()) {
                Ok(outcome) => {
                    match outcome {
                        Some(Outcome::Settled { receipt, .. }) => {
                            println!("intent {} settled {receipt}", hex(&intent.id));
                        }
                        Some(Outcome::Refused(code)) => {
                            println!("intent {} refused {code}", hex(&intent.id));
                        }
                        Some(Outcome::Skipped(reason)) => {
                            println!("intent {} skipped: {reason}", hex(&intent.id));
                        }
                        None => {}
                    }
                    backlog.pop_front();
                }
                Err(failure) => {
                    eprintln!("submit {} failed: {failure}", hex(&intent.id));
                    error.get_or_insert_with(|| format!("submit failed: {failure}"));
                    break;
                }
            }
        }
        let behind = error.is_none() && status.lag > 0 && backlog.len() < MAX_BACKLOG;
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
