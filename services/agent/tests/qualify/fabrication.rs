use std::collections::BTreeSet;
use std::path::Path;
use std::process::Command;

use layerx_agent_api::read::{
    AccountRef, BalanceValue, BatchRef, CheckpointRef, Freshness, RelativeTo, VerifiedRead,
};
use layerx_agent_api::verify::Level;
use layerx_agent_api::{Amount, Sequence};
use layerx_sdk::{Client, SdkError};

use crate::hostile_node::{agent_hostile_node_harness, HostileAttack};

fn run(repository: &Path, program: &str, arguments: &[&str]) -> Result<(), String> {
    let output = Command::new(program)
        .args(arguments)
        .current_dir(repository)
        .output()
        .map_err(|error| format!("could not run {program}: {error}"))?;
    if output.status.success() {
        Ok(())
    } else {
        Err(format!(
            "{} {:?} failed: status={} stderr={}",
            program,
            arguments,
            output.status,
            String::from_utf8_lossy(&output.stderr).trim()
        ))
    }
}

fn run_rust_suites(repository: &Path) -> Result<(), String> {
    for (package, test) in [
        ("layerx-proof", "negative"),
        ("layerx-proof", "availability"),
        ("layerx-agentd", "balance"),
        ("layerx-agentd", "history"),
        ("layerx-agentd", "checkpoint"),
        ("layerx-agentd", "finality"),
        ("layerx-agentd", "export"),
        ("layerx-agentd", "gaps"),
        ("layerx-mcp", "read"),
    ] {
        run(
            repository,
            "cargo",
            &[
                "test",
                "--manifest-path",
                "services/agent/Cargo.toml",
                "--locked",
                "-p",
                package,
                "--test",
                test,
            ],
        )?;
    }
    Ok(())
}

fn freshness() -> Result<Freshness, String> {
    let batch = BatchRef::new("batch-1").map_err(|error| format!("batch: {error:?}"))?;
    Ok(Freshness {
        chain_head: Sequence(1),
        latest_sealed_batch: batch.clone(),
        latest_finalised_checkpoint: CheckpointRef::new("checkpoint-1")
            .map_err(|error| format!("checkpoint: {error:?}"))?,
        value_sequence: Sequence(1),
        relative_to: RelativeTo::Batch(batch),
    })
}

fn rust_sdk_refuses_unverified() -> Result<(), String> {
    let value = BalanceValue {
        account: AccountRef::new("account-a").map_err(|error| format!("account: {error:?}"))?,
        asset: layerx_agent_api::identity::Asset::new("LXP")
            .map_err(|error| format!("asset: {error:?}"))?,
        amount: Amount(999),
        canonical_state: layerx_agent_api::prepare::CanonicalBytes::new(b"hostile".to_vec())
            .map_err(|error| format!("state: {error:?}"))?,
    };
    if Client::accept_verified_read(
        Level::StateProven,
        VerifiedRead::new(value, Level::Unverified, freshness()?),
    ) == Err(SdkError::UnverifiedRead)
    {
        Ok(())
    } else {
        Err("Rust SDK surfaced a hostile unverified value".to_owned())
    }
}

fn dynamic_sdks_refuse_unverified(repository: &Path) -> Result<(), String> {
    let typescript = r#"import {VerificationLevel,requireVerified} from './agent/sdk/typescript/dist/src/index.js';
const read={value:999n,achievedVerificationLevel:VerificationLevel.Unverified,freshness:{chainHead:1n,latestBatch:'b',latestCheckpoint:'c',valueSequence:1n}};
let refused=false; try { requireVerified(VerificationLevel.StateProven,read); } catch { refused=true; }
if (!refused) process.exit(1);"#;
    run(
        repository,
        "node",
        &["--input-type=module", "-e", typescript],
    )?;
    let python = r#"from layerx_sdk import VerificationLevel, VerifiedRead, require_verified
read=VerifiedRead(999,VerificationLevel.UNVERIFIED,1,'b','c',1)
try:
    require_verified(VerificationLevel.STATE_PROVEN,read)
except ValueError:
    raise SystemExit(0)
raise SystemExit(1)"#;
    let output = Command::new("python3")
        .arg("-c")
        .arg(python)
        .current_dir(repository)
        .env("PYTHONPATH", repository.join("services/agent/sdk/python"))
        .output()
        .map_err(|error| format!("could not run Python hostile SDK check: {error}"))?;
    if output.status.success() {
        Ok(())
    } else {
        Err(format!(
            "Python SDK surfaced a hostile unverified value: {}",
            String::from_utf8_lossy(&output.stderr).trim()
        ))
    }
}

/// Runs the hostile-node, independent-evidence and every-surface no-fabrication gate.
///
/// # Errors
///
/// Fails if an attack becomes a result or a required read/stream/MCP/SDK suite fails.
pub fn agent_qualify_fabrication_gate(repository: &Path) -> Result<String, String> {
    let observed = agent_hostile_node_harness()?;
    let expected = BTreeSet::from([
        HostileAttack::AlteredBalance,
        HostileAttack::AlteredReceipt,
        HostileAttack::ResignedReceipt,
        HostileAttack::SubthresholdCertificate,
        HostileAttack::TruncatedProof,
        HostileAttack::ReorderedEvents,
        HostileAttack::WithheldAvailability,
    ]);
    if observed != expected {
        return Err(format!(
            "hostile-node attack coverage drift: observed={observed:?} expected={expected:?}"
        ));
    }
    run_rust_suites(repository)?;
    rust_sdk_refuses_unverified()?;
    dynamic_sdks_refuse_unverified(repository)?;
    Ok(format!(
        "agent_qualify_fabrication_gate passed attacks={} read_surfaces=balance,history,checkpoint,receipt,availability,export streaming=gaps mcp=read sdks=rust,typescript,python",
        observed.len()
    ))
}
