//! Delivers this instance's cosign shares to its peers and admits theirs over
//! mutually authenticated, pinned TLS 1.3. Enabled only through
//! `LAYERX_BRIDGE_COSIGN_TRANSPORT_CONFIG`.

use std::path::PathBuf;
use std::process::ExitCode;
use std::sync::Arc;

use layerx_bridge_relayer::cosign::transport::{Transport, ENABLE_VARIABLE};

fn main() -> ExitCode {
    let Some(path) = std::env::var_os(ENABLE_VARIABLE) else {
        eprintln!(
            "layerx-bridge-cosign: {ENABLE_VARIABLE} is not set; the transport is not enabled"
        );
        return ExitCode::from(2);
    };
    if path.is_empty() {
        eprintln!("layerx-bridge-cosign: {ENABLE_VARIABLE} is set but empty");
        return ExitCode::FAILURE;
    }
    let transport = match Transport::load(&PathBuf::from(path)) {
        Ok(transport) => Arc::new(transport),
        Err(error) => {
            eprintln!("layerx-bridge-cosign: {error}");
            return ExitCode::FAILURE;
        }
    };
    let server = Arc::clone(&transport);
    let listener = std::thread::spawn(move || server.serve());
    loop {
        if listener.is_finished() {
            let reason = listener.join().map_or_else(
                |_| "listener panicked".to_owned(),
                |result| format!("{result:?}"),
            );
            eprintln!("layerx-bridge-cosign: listener stopped: {reason}");
            return ExitCode::FAILURE;
        }
        transport.sweep();
        std::thread::sleep(transport.interval);
    }
}
