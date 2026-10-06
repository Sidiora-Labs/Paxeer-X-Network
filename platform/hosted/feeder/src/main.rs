use std::io::{Read, Write};
use std::net::TcpListener;
use std::path::PathBuf;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::thread;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use layerx_oracle_feeder::{http_agent, load_key, oracle_registry, run_round, Config, Journal};

fn env(name: &str) -> Result<String, String> {
    std::env::var(name).map_err(|_| format!("{name} is required"))
}

fn now_ms() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_or(0, |d| u64::try_from(d.as_millis()).unwrap_or(u64::MAX))
}

fn serve_readyz(address: &str, last_ok: Arc<AtomicU64>, window_ms: u64) -> Result<(), String> {
    let listener = TcpListener::bind(address).map_err(|e| format!("readyz bind {address}: {e}"))?;
    thread::spawn(move || {
        for mut stream in listener.incoming().flatten() {
            let mut request = [0u8; 1024];
            let _ = stream.set_read_timeout(Some(Duration::from_secs(2)));
            let read = stream.read(&mut request).unwrap_or(0);
            let ready_path = request[..read].starts_with(b"GET /readyz ");
            let last = last_ok.load(Ordering::Relaxed);
            let ready = last != 0 && now_ms().saturating_sub(last) <= window_ms;
            let status = match (ready_path, ready) {
                (false, _) => "404 Not Found",
                (true, true) => "200 OK",
                (true, false) => "503 Service Unavailable",
            };
            let _ = write!(
                stream,
                "HTTP/1.1 {status}\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"
            );
        }
    });
    Ok(())
}

fn run() -> Result<(), String> {
    let config = Config::load(&PathBuf::from(env("LAYERX_FEEDER_CONFIG")?))?;
    let key = load_key(&PathBuf::from(env("LAYERX_FEEDER_ORACLE_KEY_FILE")?))?;
    let state_dir = PathBuf::from(env("LAYERX_FEEDER_STATE_DIR")?);
    let readyz =
        std::env::var("LAYERX_FEEDER_READYZ_ADDR").unwrap_or_else(|_| "0.0.0.0:8080".to_owned());
    let registry = oracle_registry()?;
    let agent = http_agent()?;
    let mut journal = Journal::load(&state_dir, config.initial_account_sequence)?;
    let last_ok = Arc::new(AtomicU64::new(0));
    serve_readyz(
        &readyz,
        Arc::clone(&last_ok),
        config.cadence_ms.saturating_mul(3).max(10_000),
    )?;
    let cadence = Duration::from_millis(config.cadence_ms);
    loop {
        let started = std::time::Instant::now();
        if run_round(
            &config,
            &agent,
            &key,
            &registry,
            &mut journal,
            &state_dir,
            now_ms(),
        )? == config.markets.len()
        {
            last_ok.store(now_ms(), Ordering::Relaxed);
        }
        thread::sleep(cadence.saturating_sub(started.elapsed()));
    }
}

fn main() {
    if let Err(error) = run() {
        eprintln!("feeder: {error}");
        std::process::exit(1);
    }
}
