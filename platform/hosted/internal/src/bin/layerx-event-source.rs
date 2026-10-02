use std::fs::File;
use std::io::Read;
use std::path::Path;
use std::process::ExitCode;
use std::sync::Arc;
use std::thread;

use layerx_platform_internal::{events, http, secret, tls};

#[derive(serde::Deserialize)]
#[serde(deny_unknown_fields)]
struct ProducerFile {
    token_file: String,
    allow_principal_digest: bool,
}

fn producers() -> Result<Vec<events::ProducerCredential>, String> {
    let Ok(path) = std::env::var("LAYERX_EVENTS_PRODUCERS_FILE") else {
        return Ok(Vec::new());
    };
    let mut bytes = Vec::new();
    File::open(path)
        .and_then(|file| file.take(16_385).read_to_end(&mut bytes))
        .map_err(|error| error.to_string())?;
    if bytes.len() > 16_384 {
        return Err("producer configuration exceeds bound".to_owned());
    }
    let entries: Vec<ProducerFile> =
        serde_json::from_slice(&bytes).map_err(|error| error.to_string())?;
    entries
        .into_iter()
        .map(|entry| {
            Ok(events::ProducerCredential {
                token: secret::read_secret_file(Path::new(&entry.token_file))?,
                allow_principal_digest: entry.allow_principal_digest,
            })
        })
        .collect()
}

fn run() -> Result<(), String> {
    let prefix = "LAYERX_EVENTS";
    let listen = secret::required_env("LAYERX_EVENTS_LISTEN")?
        .parse()
        .map_err(|_| "invalid LAYERX_EVENTS_LISTEN".to_owned())?;
    let kind = events::Kind::parse(&secret::required_env("LAYERX_EVENTS_KIND")?)?;
    if !tls::client_ca_configured(prefix) {
        return Err("LAYERX_EVENTS_CLIENT_CA_DER is required".to_owned());
    }
    let credentials = secret::required_env("LAYERX_EVENTS_CREDENTIALS_FILE")?;
    let state = secret::required_env("LAYERX_EVENTS_STATE_DIR")?;
    let key_file = secret::required_env("LAYERX_EVENTS_ENROLLMENT_KEY_FILE")?;
    events::require_protected(Path::new(&key_file))?;
    let key = secret::read_token("LAYERX_EVENTS_ENROLLMENT_KEY_FILE")?;
    let upstream = tls::Upstream::from_environment(prefix)?;
    let token = secret::read_token("LAYERX_EVENTS_TOKEN_FILE")?;
    let producers = producers()?;
    let server = tls::server_config(prefix)?;
    let source = Arc::new(
        events::Service::open(
            kind,
            upstream,
            Path::new(&credentials),
            &key,
            token,
            Path::new(&state),
        )?
        .with_producers(producers)?,
    );
    drop(key);
    let refreshed = Arc::clone(&source);
    thread::spawn(move || loop {
        match refreshed.refresh() {
            Ok(Some(generation)) => eprintln!(
                "layerx-event-source: enrollment generation {generation} adopted with {} principals",
                refreshed.generation().principals()
            ),
            Ok(None) | Err((_, false)) => {}
            Err((code, true)) => eprintln!(
                "layerx-event-source: enrollment generation {} kept, snapshot refused: {code}",
                refreshed.generation().generation()
            ),
        }
        thread::sleep(events::PRINCIPAL_POLL);
    });
    http::serve("layerx-event-source", listen, &server, move |request| {
        source.route(request)
    })
}

fn main() -> ExitCode {
    match run() {
        Ok(()) => ExitCode::SUCCESS,
        Err(error) => {
            eprintln!("layerx-event-source: {error}");
            ExitCode::FAILURE
        }
    }
}
