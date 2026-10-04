use std::io::{Read, Write};
use std::net::{SocketAddr, TcpListener, TcpStream};
use std::process::{Child, Command, Stdio};
use std::time::{Duration, Instant};

use layerx_explorer_index::unified::{UnifiedAccountProfile, UnifiedQueryError};
use serde_json::Value;

#[test]
fn availability_profile_is_explicit_and_closed() {
    assert_eq!(
        UnifiedAccountProfile::from_headers("GET / HTTP/1.1\r\n\r\n"),
        Ok(UnifiedAccountProfile::Legacy)
    );
    assert_eq!(
        UnifiedAccountProfile::from_headers("GET / HTTP/1.1\r\nlayerx-unified-profile: 2\r\n\r\n"),
        Ok(UnifiedAccountProfile::AvailabilityV2)
    );
    for headers in [
        "LayerX-Unified-Profile: 0",
        "LayerX-Unified-Profile: 3",
        "LayerX-Unified-Profile: 2,1",
        "LayerX-Unified-Profile: 2\r\nLayerX-Unified-Profile: 2",
    ] {
        assert_eq!(
            UnifiedAccountProfile::from_headers(&format!("GET / HTTP/1.1\r\n{headers}\r\n\r\n")),
            Err(UnifiedQueryError::InvalidQuery)
        );
    }
}

struct Explorer {
    child: Child,
    address: SocketAddr,
}

impl Explorer {
    fn start() -> Result<Self, Box<dyn std::error::Error>> {
        let evidence = std::path::PathBuf::from(std::env::var("PAXEER_X_EVIDENCE_DIR")?);
        let state = evidence.join(format!("unified-availability-{}", std::process::id()));
        std::fs::create_dir(&state)?;
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            std::fs::set_permissions(&state, std::fs::Permissions::from_mode(0o700))?;
        }
        let reserved = TcpListener::bind("127.0.0.1:0")?;
        let address = reserved.local_addr()?;
        drop(reserved);
        let child = Command::new(env!("CARGO_BIN_EXE_layerx-explorer-index"))
            .env("LAYERX_EXPLORER_PROGRAM_LISTEN", address.to_string())
            .env("LAYERX_EXPLORER_INGEST_CURSOR", state.join("ingest.cursor"))
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()?;
        let mut explorer = Self { child, address };
        let deadline = Instant::now() + Duration::from_secs(180);
        while Instant::now() < deadline {
            if explorer.child.try_wait()?.is_some() {
                return Err("genuine explorer exited before authority ingestion completed".into());
            }
            if TcpStream::connect_timeout(&address, Duration::from_millis(100)).is_ok() {
                return Ok(explorer);
            }
            std::thread::sleep(Duration::from_millis(100));
        }
        Err("genuine explorer did not become reachable within startup bound".into())
    }
}

impl Drop for Explorer {
    fn drop(&mut self) {
        if self.child.try_wait().ok().flatten().is_none() {
            let _ = self.child.kill();
            let _ = self.child.wait();
        }
    }
}

fn request(explorer: &Explorer, profile: &str) -> Result<(u16, Value), Box<dyn std::error::Error>> {
    let account = std::env::var("PAXEER_X_STANDALONE_RECEIPTS_ACCOUNT")?;
    let identifier =
        layerx_explorer_index::unified::AccountIdentifier::parse(&account)?.canonical_text();
    let bearer = std::env::var("LAYERX_EXPLORER_PROGRAM_BEARER_TOKEN")?;
    request_path(
        explorer,
        &format!("/v1/accounts/{identifier}/unified?limit=1"),
        profile,
        &bearer,
    )
}

fn request_path(
    explorer: &Explorer,
    path: &str,
    profile: &str,
    bearer: &str,
) -> Result<(u16, Value), Box<dyn std::error::Error>> {
    let address = explorer.address;
    let mut stream = TcpStream::connect_timeout(&address, Duration::from_secs(10))?;
    stream.set_read_timeout(Some(Duration::from_secs(30)))?;
    stream.set_write_timeout(Some(Duration::from_secs(10)))?;
    write!(stream, "GET {path} HTTP/1.1\r\nHost: {address}\r\nAuthorization: Bearer {bearer}\r\nLayerX-Unified-Profile: {profile}\r\nConnection: close\r\n\r\n")?;
    let mut bytes = Vec::new();
    stream.take(2_097_153).read_to_end(&mut bytes)?;
    assert!(bytes.len() <= 2_097_152);
    let split = bytes
        .windows(4)
        .position(|window| window == b"\r\n\r\n")
        .ok_or("missing HTTP header")?;
    let headers = std::str::from_utf8(&bytes[..split])?;
    let status = headers
        .split_ascii_whitespace()
        .nth(1)
        .ok_or("missing HTTP status")?
        .parse()?;
    Ok((status, serde_json::from_slice(&bytes[split + 4..])?))
}

fn availability(value: &Value, original: &Value) {
    assert_eq!(value["evidence"], "gateway-reported");
    let object = value
        .as_object()
        .unwrap_or_else(|| panic!("availability is not an object"));
    assert_eq!(object.len(), 3);
    if original.is_null() {
        assert_eq!(value["state"], "unavailable");
        assert_eq!(value["reason"], "not_reported");
        assert!(!object.contains_key("value"));
    } else {
        assert_eq!(value["state"], "present");
        assert_eq!(&value["value"], original);
        assert!(!object.contains_key("reason"));
    }
}

#[test]
fn real_unified_handler_preserves_legacy_and_reports_actual_availability(
) -> Result<(), Box<dyn std::error::Error>> {
    std::env::var("PAXEER_X_STANDALONE_RECEIPTS_ACCOUNT")?;
    std::env::var("LAYERX_EXPLORER_PROGRAM_BEARER_TOKEN")?;
    let explorer = Explorer::start()?;
    let bearer = std::env::var("LAYERX_EXPLORER_PROGRAM_BEARER_TOKEN")?;
    let (status, identity) = request_path(&explorer, "/v1/identity", "2", &bearer)?;
    assert_eq!(status, 200);
    assert_eq!(
        identity.as_object().ok_or("missing real identity")?.len(),
        2
    );
    let network = std::env::var("LAYERX_EXPLORER_READ_NETWORK_ID")?
        .parse::<u32>()?
        .to_string();
    assert_eq!(identity["network_id"], network);
    assert_eq!(
        identity["wire_version"],
        layerx_wire::limits::STATE_COMMITMENT_PROTOCOL_VERSION.to_string()
    );
    let (status, _) = request_path(&explorer, "/v1/identity", "2", "unauthorized")?;
    assert_eq!(status, 401);
    let deadline = Instant::now() + Duration::from_secs(180);
    let (status, body) = loop {
        let response = request(&explorer, "2")?;
        if response.0 == 200 || Instant::now() >= deadline {
            break response;
        }
        assert_eq!(
            response.0, 503,
            "genuine authority refused the account request"
        );
        std::thread::sleep(Duration::from_millis(250));
    };
    assert_eq!(
        status, 200,
        "genuine source-bound unified account must be available for this case"
    );
    assert_eq!(body["profile"], 2);
    assert_eq!(body["evidence"], "gateway-reported");
    let reported = &body["availability"];
    assert_eq!(
        reported
            .as_object()
            .ok_or("missing closed availability contract")?
            .len(),
        2
    );
    for key in ["finalized_batch", "anchor_status", "anchor_status_name"] {
        availability(&reported["settlement"][key], &body["settlement"][key]);
    }
    let balances = body["balances"]["items"]
        .as_array()
        .ok_or("missing actual balance rows")?;
    assert!(
        !balances.is_empty(),
        "genuine account must supply actual asset rows for denomination coverage"
    );
    let denominations = reported["denominations"]
        .as_array()
        .ok_or("missing denomination coverage")?;
    assert_eq!(balances.len(), denominations.len());
    for (balance, denomination) in balances.iter().zip(denominations) {
        assert_eq!(balance["asset_id"], denomination["asset_id"]);
        availability(&denomination["denom"], &balance["denom"]);
    }
    let (status, legacy) = request(&explorer, "1")?;
    assert_eq!(status, 200);
    assert!(legacy.get("profile").is_none());
    assert!(legacy.get("availability").is_none());
    for field in ["requested", "canonical", "evidence", "identities"] {
        assert_eq!(
            legacy[field], body[field],
            "profile switched the actual join identity"
        );
    }
    let (status, _) = request(&explorer, "3")?;
    assert_eq!(status, 400);
    let (status, _) = request(&explorer, "2\r\nLayerX-Unified-Profile: 2")?;
    assert_eq!(status, 400);
    Ok(())
}
