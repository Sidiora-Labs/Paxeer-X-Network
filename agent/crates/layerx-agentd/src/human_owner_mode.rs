use super::{
    config, connect_human_authority, connect_human_node, human_lni_limits, human_peers, optional,
    parse_u64, required, response, serve, start_human_owner, DeadlineStream, HttpPool, Parsed,
    parse_request, SUPERVISION_INTERVAL,
};
use std::env;
use std::net::TcpListener;
use std::path::PathBuf;
use std::sync::{mpsc, Arc};
use std::sync::atomic::{AtomicBool, Ordering};
use std::thread;
use std::time::Duration;

#[derive(Debug, PartialEq, Eq)]
enum Mode {
    Full,
    HumanOwner,
}

impl Mode {
    fn parse(value: Option<&str>) -> Result<Self, String> {
        match value {
            None | Some("full") => Ok(Self::Full),
            Some("human-owner") => Ok(Self::HumanOwner),
            Some(_) => Err("LAYERX_AGENT_MODE is invalid".to_owned()),
        }
    }
}

pub(super) fn run() -> Result<(), String> {
    let mode = match env::var("LAYERX_AGENT_MODE") {
        Ok(value) => Mode::parse(Some(&value))?,
        Err(env::VarError::NotPresent) => Mode::parse(None)?,
        Err(env::VarError::NotUnicode(_)) => return Err("LAYERX_AGENT_MODE is invalid".to_owned()),
    };
    match mode {
        Mode::Full => config().and_then(serve),
        Mode::HumanOwner => serve_human_owner(),
    }
}

fn dependencies_ready() -> Result<(), String> {
    let deadline = Duration::from_millis(parse_u64("LAYERX_AGENT_HUMAN_DEADLINE_MS")?);
    let human_limits = human_lni_limits(deadline)?;
    let node_limits = layerx_client::lni::transport::Limits {
        maximum_frame_bytes: human_limits
            .maximum_frame_bytes
            .max(layerx_client::evidence::MINIMUM_FINALITY_FRAME_BYTES),
        ..human_limits
    };
    connect_human_node(
        PathBuf::from(required("LAYERX_AGENT_HUMAN_NODE_LNI")?),
        node_limits,
    )?;
    connect_human_authority(deadline, &human_peers()?)?;
    Ok(())
}

fn owner_running(human: &mpsc::Receiver<Result<(), String>>) -> Result<(), String> {
    match human.try_recv() {
        Err(mpsc::TryRecvError::Empty) => Ok(()),
        Ok(Err(error)) => Err(error),
        Ok(Ok(())) | Err(mpsc::TryRecvError::Disconnected) => {
            Err("human listener terminated".to_owned())
        }
    }
}

fn serve_human_owner() -> Result<(), String> {
    let listen = required("LAYERX_AGENT_PROGRAM_LISTEN")?;
    let bearer = required("LAYERX_AGENT_PROGRAM_BEARER_TOKEN")?;
    if !listen.starts_with("127.0.0.1:") || bearer.len() < 32 {
        return Err("human health requires loopback and a bounded credential".to_owned());
    }
    if bearer == required("LAYERX_AGENT_HUMAN_AUTHORITY_BEARER")? {
        return Err("human health and authority credentials must be distinct".to_owned());
    }
    if optional("LAYERX_AGENT_MCP_BINDING_ROOT").is_some() {
        return Err(
            "LAYERX_AGENT_MCP_BINDING_ROOT requires the full agent mode, which serves the program endpoint and probe program the binding names"
                .to_owned(),
        );
    }
    let human = start_human_owner(None)?;
    let listener = TcpListener::bind(listen)
        .map_err(|error| format!("human health listener failed: {error}"))?;
    let alive = Arc::new(AtomicBool::new(true));
    let ready = Arc::clone(&alive);
    let pool = HttpPool::start(listener, move |mut stream| {
        let _ = serve_health(&mut stream, &bearer, || ready.load(Ordering::Acquire));
    })?;
    loop {
        if let Err(error) = owner_running(&human).and_then(|()| pool.check()) {
            alive.store(false, Ordering::Release);
            return Err(error);
        }
        thread::sleep(SUPERVISION_INTERVAL);
    }
}

fn serve_health<F: Fn() -> bool>(
    stream: &mut DeadlineStream,
    bearer: &str,
    owner_ready: F,
) -> Result<(), String> {
    match parse_request(stream, bearer) {
        Parsed::Closed => Ok(()),
        Parsed::Refused(status, body) => response(stream, status, body),
        Parsed::Admitted(path) if path != "/healthz" => response(stream, 404, "{\"error\":\"not_found\"}"),
        Parsed::Admitted(_) => {
            let ready = owner_ready() && dependencies_ready().is_ok() && owner_ready();
            if ready {
                response(stream, 200, "{\"ready\":true}")
            } else {
                response(stream, 503, "{\"ready\":false}")
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::Mode;

    #[test]
    fn mode_selection_is_explicit_and_closed() {
        assert_eq!(Mode::parse(None), Ok(Mode::Full));
        assert_eq!(Mode::parse(Some("full")), Ok(Mode::Full));
        assert_eq!(Mode::parse(Some("human-owner")), Ok(Mode::HumanOwner));
        for value in ["", "human", "FULL", "human-owner "] {
            assert!(Mode::parse(Some(value)).is_err());
        }
    }

    #[test]
    fn health_refuses_terminated_owner_and_preserves_http_boundaries(
    ) -> Result<(), Box<dyn std::error::Error>> {
        use std::io::{Read, Write};
        use std::net::{TcpListener, TcpStream};
        use std::sync::mpsc;
        use std::thread;
        use std::time::Duration;

        let bearer = "h".repeat(32);
        let cases = [
            (format!("GET /healthz HTTP/1.1\r\nAuthorization: Bearer {bearer}\r\n\r\n"), 503, "{\"ready\":false}"),
            ("GET /healthz HTTP/1.1\r\n\r\n".to_owned(), 401, "unauthorized"),
            (format!("GET /v1/programs/anything/balances HTTP/1.1\r\nAuthorization: Bearer {bearer}\r\n\r\n"), 404, "not_found"),
            ("POST /healthz HTTP/1.1\r\n\r\n".to_owned(), 400, "invalid_request"),
            ("x".repeat(crate::HEADER_LIMIT), 431, "headers_too_large"),
        ];
        for (request, status, body) in cases {
            let listener = TcpListener::bind("127.0.0.1:0")?;
            let address = listener.local_addr()?;
            let bearer = bearer.clone();
            let server = thread::spawn(move || -> Result<(), String> {
                let (stream, _) = listener.accept().map_err(|e| e.to_string())?;
                stream
                    .set_read_timeout(Some(Duration::from_secs(5)))
                    .map_err(|e| e.to_string())?;
                let (sender, receiver) = mpsc::channel();
                drop(sender);
                super::serve_health(&mut super::DeadlineStream::new(stream), &bearer, || super::owner_running(&receiver).is_ok())
            });
            let mut client = TcpStream::connect(address)?;
            client.set_read_timeout(Some(Duration::from_secs(5)))?;
            client.write_all(request.as_bytes())?;
            let mut response = String::new();
            client.read_to_string(&mut response)?;
            server.join().map_err(|_| "health server panicked")??;
            assert!(response.starts_with(&format!("HTTP/1.1 {status} ")));
            assert!(response.contains(body));
        }
        Ok(())
    }
}
