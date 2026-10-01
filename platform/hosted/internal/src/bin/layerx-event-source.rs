use std::fs::File;
use std::io::Read;
use std::path::Path;
use std::process::ExitCode;
use std::sync::{Arc, OnceLock};
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
    let upstream = tls::Upstream::from_environment(prefix)?;
    let token = secret::read_token("LAYERX_EVENTS_TOKEN_FILE")?;
    let producers = producers()?;
    let server = tls::server_config(prefix)?;
    let source = Arc::new(OnceLock::new());
    let opened = Arc::clone(&source);
    thread::spawn(move || {
        let result = (|| -> Result<(), String> {
            loop {
                if let Some(principals) = events::await_principals(Path::new(&credentials))? {
                    let service = events::Service::open(
                        kind,
                        upstream,
                        principals,
                        token,
                        Path::new(&state),
                    )?
                    .with_producers(producers)?;
                    let _ = opened.set(service);
                    return Ok(());
                }
                thread::sleep(events::PRINCIPAL_POLL);
            }
        })();
        if let Err(error) = result {
            eprintln!("layerx-event-source: {error}");
            std::process::exit(1);
        }
    });
    http::serve("layerx-event-source", listen, &server, move |request| {
        source.get().map_or_else(
            || events::waiting_principals(request),
            |service: &events::Service| service.route(request),
        )
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
