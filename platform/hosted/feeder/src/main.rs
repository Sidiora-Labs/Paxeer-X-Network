use std::io::{Read, Write};
use std::net::TcpListener;
use std::path::PathBuf;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::thread;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use layerx_oracle_feeder::{
    health_status, http_agent, load_key, ready, run_round, Config, Journal, Sequencer,
    EVM_SENDER_PREFIX,
};
use layerx_platform_internal::evm_sender::EvmSender;

fn env(name: &str) -> Result<String, String> {
    std::env::var(name).map_err(|_| format!("{name} is required"))
}

fn now_ms() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_or(0, |d| u64::try_from(d.as_millis()).unwrap_or(u64::MAX))
}

fn serve_health(address: &str, last_ok: Arc<AtomicU64>, cadence_ms: u64) -> Result<(), String> {
    let listener = TcpListener::bind(address).map_err(|e| format!("health bind {address}: {e}"))?;
    thread::spawn(move || {
        for mut stream in listener.incoming().flatten() {
            let mut request = [0u8; 1024];
            let _ = stream.set_read_timeout(Some(Duration::from_secs(2)));
            let read = stream.read(&mut request).unwrap_or(0);
            let ready = ready(last_ok.load(Ordering::Relaxed), now_ms(), cadence_ms);
            let status = health_status(&request[..read], ready);
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
    let health = std::env::var("LAYERX_HEALTH_ADDR").unwrap_or_else(|_| "[::]:8080".to_owned());
    let agent = http_agent()?;
    let sequencer = Sequencer::new(agent.clone(), env("LAYERX_FEEDER_SEQUENCER_URL")?)?;
    let sender = EvmSender::from_env(EVM_SENDER_PREFIX).map_err(|e| format!("EVM sender: {e}"))?;
    sequencer.check_sender(&key, sender.address())?;
    let mut journal = Journal::load(&state_dir, config.initial_account_sequence)?;
    let last_ok = Arc::new(AtomicU64::new(0));
    serve_health(&health, Arc::clone(&last_ok), config.cadence_ms)?;
    let cadence = Duration::from_millis(config.cadence_ms);
    loop {
        let started = std::time::Instant::now();
        if run_round(
            &config,
            &agent,
            &key,
            &sender,
            &sequencer,
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
