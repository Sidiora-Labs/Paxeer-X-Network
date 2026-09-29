use std::env;
use std::fs;
use std::net::SocketAddr;
use std::path::PathBuf;
use std::process::ExitCode;
use std::sync::Arc;
use std::time::Duration;

use layerx_human_service::server::{
    default_component_limits, BoundPlainServer, HttpConfig, HttpsConfig, HttpsServer, Listener,
    PlainConfig, PlainServer, PrincipalLimits, Router, UnixComponents,
};

fn main() -> ExitCode {
    match run() {
        Ok(()) => ExitCode::SUCCESS,
        Err(message) => {
            eprintln!("layerx-human-service refused startup: {message}");
            ExitCode::FAILURE
        }
    }
}

fn run() -> Result<(), String> {
    let bind = required("LAYERX_HUMAN_BIND")?
        .parse::<SocketAddr>()
        .map_err(|_| "LAYERX_HUMAN_BIND is not a socket address".to_owned())?;
    let listener = Listener::parse(
        optional("LAYERX_HUMAN_LISTENER")?.as_deref(),
        optional("LAYERX_HUMAN_TLS_CERT_DER")?.as_deref(),
        optional("LAYERX_HUMAN_TLS_KEY_DER")?.as_deref(),
    )?;
    let tls_material = match listener {
        Listener::Tls => {
            let certificate_path = PathBuf::from(required("LAYERX_HUMAN_TLS_CERT_DER")?);
            let private_key_path = PathBuf::from(required("LAYERX_HUMAN_TLS_KEY_DER")?);
            let certificate_der = fs::read(certificate_path)
                .map_err(|_| "the configured TLS certificate cannot be read".to_owned())?;
            let private_key_der = fs::read(private_key_path)
                .map_err(|_| "the configured TLS private key cannot be read".to_owned())?;
            Some((certificate_der, private_key_der))
        }
        Listener::Plain => None,
    };
    let component_endpoint = PathBuf::from(required("LAYERX_HUMAN_COMPONENT_SOCKET")?);
    let allowed_origin = required("LAYERX_HUMAN_WEB_ORIGIN")?;
    let component_limits = default_component_limits();
    let backend = Arc::new(
        UnixComponents::new(&component_endpoint, component_limits)
            .map_err(|_| "the component boundary configuration is invalid".to_owned())?,
    );
    let principal_limits = PrincipalLimits::new(
        number("LAYERX_HUMAN_REQUESTS_PER_MINUTE", 240_u32)?,
        60,
        number("LAYERX_HUMAN_MAX_PRINCIPALS", 100_000_usize)?,
    )
    .map_err(|_| "the principal limit configuration is invalid".to_owned())?;
    let router = Arc::new(
        Router::new(
            backend,
            principal_limits,
            HttpConfig {
                maximum_header_bytes: number("LAYERX_HUMAN_MAX_HEADER_BYTES", 32_768_usize)?,
                maximum_body_bytes: number("LAYERX_HUMAN_MAX_BODY_BYTES", 1_048_576_usize)?,
                allowed_origin,
                service_version: env!("CARGO_PKG_VERSION").to_owned(),
            },
            layerx_client::runtime_clock::RuntimeClock::from_environment()
                .map_err(|error| error.to_string())?,
        )
        .map_err(|_| "the human-api router configuration is invalid".to_owned())?,
    );
    let maximum_connections = number("LAYERX_HUMAN_MAX_CONNECTIONS", 1_024_usize)?;
    let io_deadline = Duration::from_secs(number("LAYERX_HUMAN_IO_DEADLINE_SECONDS", 15_u64)?);
    match tls_material {
        Some((certificate_der, private_key_der)) => HttpsServer::new(
            router,
            HttpsConfig {
                bind,
                certificate_der,
                private_key_der,
                maximum_connections,
                io_deadline,
            },
        )
        .run()
        .map_err(|error| error.to_string()),
        None => PlainServer::new(
            router,
            PlainConfig {
                bind,
                maximum_connections,
                io_deadline,
            },
        )
        .bind()
        .and_then(BoundPlainServer::serve)
        .map_err(|error| error.to_string()),
    }
}

fn optional(name: &str) -> Result<Option<String>, String> {
    match env::var(name) {
        Ok(value) => Ok(Some(value).filter(|value| !value.is_empty())),
        Err(env::VarError::NotPresent) => Ok(None),
        Err(env::VarError::NotUnicode(_)) => Err(format!("{name} is not valid Unicode")),
    }
}

fn required(name: &str) -> Result<String, String> {
    env::var(name)
        .ok()
        .filter(|value| !value.is_empty())
        .ok_or_else(|| format!("{name} is required"))
}

fn number<T>(name: &str, default: T) -> Result<T, String>
where
    T: std::str::FromStr,
{
    match env::var(name) {
        Ok(value) => value
            .parse::<T>()
            .map_err(|_| format!("{name} is not a valid positive integer")),
        Err(env::VarError::NotPresent) => Ok(default),
        Err(env::VarError::NotUnicode(_)) => Err(format!("{name} is not valid Unicode")),
    }
}
