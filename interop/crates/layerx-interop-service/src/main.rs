mod config;
mod server;

use layerx_platform_gateway::http;
use rustls::{ServerConnection, StreamOwned};
use std::io::{Read, Write};
use std::net::{TcpListener, TcpStream};
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;
use std::thread;
use std::time::Duration;

const MAX_REQUEST: usize = 512 * 1024;
const MAX_CONNECTIONS: usize = 256;
static ACTIVE_CONNECTIONS: AtomicUsize = AtomicUsize::new(0);

struct ConnectionGuard;

impl Drop for ConnectionGuard {
    fn drop(&mut self) {
        ACTIVE_CONNECTIONS.fetch_sub(1, Ordering::AcqRel);
    }
}

fn serve(config: &Arc<config::Config>, tcp: TcpStream) -> Result<(), String> {
    tcp.set_read_timeout(Some(Duration::from_secs(10)))
        .map_err(|error| error.to_string())?;
    tcp.set_write_timeout(Some(Duration::from_secs(10)))
        .map_err(|error| error.to_string())?;
    match &config.listener {
        config::Listener::Tls(tls) => {
            let connection =
                ServerConnection::new(Arc::clone(tls)).map_err(|error| error.to_string())?;
            exchange(config, &mut StreamOwned::new(connection, tcp))
        }
        config::Listener::Plain => {
            let mut stream = tcp;
            exchange(config, &mut stream)
        }
    }
}

fn exchange<S: Read + Write>(config: &Arc<config::Config>, stream: &mut S) -> Result<(), String> {
    let Ok(request) = http::read_request(stream, MAX_REQUEST) else {
        return http::write_response(
            stream,
            &http::OutgoingResponse {
                status: 400,
                body: b"{\"ok\":false,\"error\":{\"code\":\"invalid_http_request\"}}".to_vec(),
                retry_after: None,
            },
        );
    };
    http::write_response(stream, &server::route(config, &request))
}

fn run() -> Result<(), String> {
    let config = Arc::new(config::load()?);
    let listener = TcpListener::bind(config.listen).map_err(|error| error.to_string())?;
    for incoming in listener.incoming() {
        let tcp = incoming.map_err(|error| error.to_string())?;
        if ACTIVE_CONNECTIONS.fetch_add(1, Ordering::AcqRel) >= MAX_CONNECTIONS {
            ACTIVE_CONNECTIONS.fetch_sub(1, Ordering::AcqRel);
            let _ = tcp.shutdown(std::net::Shutdown::Both);
            continue;
        }
        let config = Arc::clone(&config);
        thread::spawn(move || {
            let _guard = ConnectionGuard;
            let _ = serve(&config, tcp);
        });
    }
    Ok(())
}

fn main() {
    if let Err(error) = run() {
        let _ = writeln!(
            std::io::stderr(),
            "layerx interoperability gateway refused startup: {error}"
        );
        std::process::exit(1);
    }
}
