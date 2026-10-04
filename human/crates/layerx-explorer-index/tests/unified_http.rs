use std::collections::BTreeSet;
use std::io::{Read, Write};
use std::net::{SocketAddr, TcpListener, TcpStream};
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::time::{Duration, Instant};

use layerx_explorer_index::unified::AccountIdentifier;
use layerx_types::verify::VerificationLevel;
use serde_json::Value;

struct Explorer {
    child: Child,
    address: SocketAddr,
    bearer: String,
}

impl Explorer {
    fn start(state: &std::path::Path) -> Result<Self, Box<dyn std::error::Error>> {
        let bearer = std::env::var("LAYERX_EXPLORER_PROGRAM_BEARER_TOKEN")?;
        let reserved = TcpListener::bind("127.0.0.1:0")?;
        let address = reserved.local_addr()?;
        drop(reserved);
        let child = Command::new(env!("CARGO_BIN_EXE_layerx-explorer-index"))
            .env("LAYERX_EXPLORER_PROGRAM_LISTEN", address.to_string())
            .env("LAYERX_EXPLORER_INGEST_CURSOR", state.join("ingest.cursor"))
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()?;
        Ok(Self {
            child,
            address,
            bearer,
        })
    }

    fn get(&self, path: &str) -> Result<(u16, Value), Box<dyn std::error::Error>> {
        let mut socket = TcpStream::connect_timeout(&self.address, Duration::from_secs(10))?;
        socket.set_read_timeout(Some(Duration::from_secs(20)))?;
        socket.set_write_timeout(Some(Duration::from_secs(10)))?;
        write!(socket, "GET {path} HTTP/1.1\r\nHost: {}\r\nAuthorization: Bearer {}\r\nConnection: close\r\n\r\n", self.address, self.bearer)?;
        let mut bytes = Vec::new();
        socket.take(2_097_153).read_to_end(&mut bytes)?;
        assert!(
            bytes.len() <= 2_097_152,
            "unified response exceeds bounded body"
        );
        let split = bytes
            .windows(4)
            .position(|window| window == b"\r\n\r\n")
            .ok_or("HTTP response lacks headers")?;
        let headers = std::str::from_utf8(&bytes[..split])?;
        let status = headers
            .split_ascii_whitespace()
            .nth(1)
            .ok_or("HTTP response lacks status")?
            .parse()?;
        Ok((status, serde_json::from_slice(&bytes[split + 4..])?))
    }

    fn ready(&mut self) -> Result<(), Box<dyn std::error::Error>> {
        let deadline = Instant::now() + Duration::from_secs(180);
        while Instant::now() < deadline {
            assert!(
                self.child.try_wait()?.is_none(),
                "real explorer exited before ingestion completed"
            );
            if let Ok((200, value)) = self.get("/v1/readiness") {
                if value["complete"] == true && value["source_available"] == true {
                    return Ok(());
                }
            }
            std::thread::sleep(Duration::from_millis(250));
        }
        Err("genuine node/authority ingestion did not converge within the test budget".into())
    }

    fn stop(&mut self) -> Result<(), Box<dyn std::error::Error>> {
        assert!(Command::new("kill")
            .args(["-TERM", &self.child.id().to_string()])
            .status()?
            .success());
        let deadline = Instant::now() + Duration::from_secs(10);
        while Instant::now() < deadline {
            if self.child.try_wait()?.is_some() {
                return Ok(());
            }
            std::thread::sleep(Duration::from_millis(20));
        }
        Err("real explorer did not terminate within the shutdown bound".into())
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

fn number(value: &Value) -> Result<u64, Box<dyn std::error::Error>> {
    Ok(value
        .as_str()
        .ok_or("missing canonical decimal field")?
        .parse()?)
}

#[test]
fn unified_http_real_receipts_pages_refusals_and_restart() -> Result<(), Box<dyn std::error::Error>>
{
    let requested = std::env::var("PAXEER_X_STANDALONE_RECEIPTS_ACCOUNT")
        .map_err(|_| "missing genuine populated account: PAXEER_X_STANDALONE_RECEIPTS_ACCOUNT")?;
    let account = AccountIdentifier::parse(&requested)?.canonical_text();
    let evidence = PathBuf::from(std::env::var("PAXEER_X_EVIDENCE_DIR")?);
    let state = evidence.join(format!("unified-http-{}", std::process::id()));
    std::fs::create_dir(&state)?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(&state, std::fs::Permissions::from_mode(0o700))?;
    }
    let route = format!("/v1/accounts/{account}/unified");
    let mut explorer = Explorer::start(&state)?;
    explorer.ready()?;
    for (query, reason) in [
        ("limit=0", "invalid_page_size"),
        ("limit=101", "invalid_page_size"),
        ("limit=18446744073709551616", "invalid_page_size"),
        ("before=malformed", "invalid_cursor"),
        ("before=0", "invalid_cursor"),
        ("before=18446744073709551616", "invalid_cursor"),
        ("before=1&before=2", "invalid_query"),
        ("limit=1&unexpected=1", "invalid_query"),
    ] {
        let (status, body) = explorer.get(&format!("{route}?{query}"))?;
        assert_eq!(status, 400);
        assert_eq!(body["error"], reason);
    }
    let (status, first) = explorer.get(&format!("{route}?limit=1"))?;
    assert_eq!(status, 200);
    for field in [
        "identities",
        "balances",
        "settlement",
        "paxeer_activity",
        "freshness",
    ] {
        assert!(
            first.get(field).is_some(),
            "complete view discarded {field}"
        );
    }
    assert_eq!(first["evidence"], "gateway-reported");
    let first_rows = first["layerx_activity"]["items"]
        .as_array()
        .ok_or("missing LayerX activity page")?;
    assert_eq!(
        first_rows.len(),
        1,
        "genuine fixture must supply a populated account"
    );
    let cursor = first["layerx_activity"]["next_before"]
        .as_str()
        .ok_or("genuine fixture must supply multiple receipts")?;
    assert_eq!(
        number(&first_rows[0]["global_sequence"])?,
        cursor.parse::<u64>()?
    );
    let (status, second) = explorer.get(&format!("{route}?limit=1&before={cursor}"))?;
    assert_eq!(status, 200);
    let second_rows = second["layerx_activity"]["items"]
        .as_array()
        .ok_or("missing continuation page")?;
    assert_eq!(second_rows.len(), 1);
    assert!(
        number(&second_rows[0]["global_sequence"])? < number(&first_rows[0]["global_sequence"])?
    );
    let (status, combined) = explorer.get(&format!("{route}?limit=100"))?;
    assert_eq!(status, 200);
    let rows = combined["layerx_activity"]["items"]
        .as_array()
        .ok_or("missing complete activity page")?;
    assert!(rows.len() >= 2);
    assert_eq!(&rows[0], &first_rows[0]);
    assert_eq!(&rows[1], &second_rows[0]);
    let mut identifiers = BTreeSet::new();
    for row in rows {
        assert!(identifiers.insert(
            row["receipt_id"]
                .as_str()
                .ok_or("missing stable receipt identifier")?
        ));
        let level = row["verification"]
            .as_u64()
            .ok_or("missing achieved verification level")?;
        assert!(
            level > 0 && level <= u64::from(VerificationLevel::SETTLEMENT_ANCHORED.wire_rank()),
            "serializer fabricated a verification rung"
        );
        assert_eq!(
            row["receipt_digest"]
                .as_str()
                .ok_or("missing receipt digest")?
                .len(),
            64
        );
    }
    assert!(rows
        .windows(2)
        .all(|pair| number(&pair[0]["global_sequence"]).ok()
            > number(&pair[1]["global_sequence"]).ok()));
    explorer.stop()?;
    let mut restarted = Explorer::start(&state)?;
    restarted.ready()?;
    let (status, rebuilt) = restarted.get(&format!("{route}?limit=100"))?;
    assert_eq!(status, 200);
    assert_eq!(
        rebuilt["layerx_activity"], combined["layerx_activity"],
        "rebuild discarded or changed verified receipt-backed activity"
    );
    restarted.stop()?;
    Ok(())
}
