//! Outbound error isolation: two Ethereum destinations served by recorded
//! JSON-RPC exchanges, a real signer socket and a real journal on disk. One
//! destination's RPC fails while the other is released in the same pass; the
//! failure stays visible with its item and destination, is attempted once per
//! pass, and a restart rebroadcasts the identical pending bytes before both
//! burns complete exactly once.

mod support;

use std::fs::{self, OpenOptions};
use std::io::Write as _;
use std::path::Path;

use layerx_bridge_relayer::abi::{encode_get_chain, encode_nullified, LAYERX_BRIDGE_PRECOMPILE};
use layerx_bridge_relayer::attestation::{uint256_from_u64, OutboundAttestation};
use layerx_bridge_relayer::hex;
use layerx_bridge_relayer::journal::{outbound_key, Completion, Journal, JournalError};
use layerx_bridge_relayer::relayer::{
    ChainLink, ChainSettings, GasPolicy, ItemFailure, PaxeerLink, PaxeerSettings, Relayer,
    RelayerError, RelayerParts, StepReport,
};
use layerx_bridge_relayer::rpc::RpcFault;
use layerx_bridge_relayer::signer::{
    Attestor, Submitter, ATTEST_OUTBOUND_DOMAIN, ETHEREUM_TRANSACTION_DOMAIN,
    PAXEER_TRANSACTION_DOMAIN,
};
use serde_json::{json, Value};
use sha3::{Digest as _, Keccak256};
use support::{key, work_directory, Recording, SignerServer};

const ATTESTOR: u8 = 0xa1;
const PAXEER_FEES: u8 = 0xb1;
const ETHEREUM_FEES: u8 = 0xb2;
const BURN_TX: [u8; 32] = [0x22; 32];
const BURN_OUT_TOPIC: &str = "0x3e990eb54009dcdca53d8fa87307210f07097f37dcf6185dee71a42f8e7d524e";
const BURN_BLOCK_HASH: &str = "0x1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c";
const ATTESTOR_SET: &str = "0x00000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000001000000000000000000000000d2431ca38735c2fd438e2caa23f094191d89675b";
const ONE: &str = "0x0000000000000000000000000000000000000000000000000000000000000001";
const FALSE: &str = "0x0000000000000000000000000000000000000000000000000000000000000000";

const PAXEER: PaxeerSettings = PaxeerSettings {
    chain_id: 229,
    finality_depth: 2,
    start_block: 25,
    max_block_range: 10,
    gas: GasPolicy {
        gas_limit: 1_000_000,
        max_fee_per_gas: 100_000_000_000,
        max_priority_fee_per_gas: 2_000_000_000,
    },
};

const fn chain(chain_id: u64, vault: u8) -> ChainSettings {
    ChainSettings {
        chain_id,
        vault: [vault; 20],
        finality_depth: 12,
        start_block: 95,
        max_block_range: 10,
        gas: GasPolicy {
            gas_limit: 400_000,
            max_fee_per_gas: 200_000_000_000,
            max_priority_fee_per_gas: 3_000_000_000,
        },
    }
}

/// The destination that fails, then recovers.
const FAILING: ChainSettings = chain(1, 0x11);
/// The destination that is ready throughout.
const READY: ChainSettings = chain(2, 0x55);

fn burn(settings: &ChainSettings, nonce: u64) -> OutboundAttestation {
    OutboundAttestation {
        chain_id: settings.chain_id,
        vault: settings.vault,
        paxeer_tx_hash: BURN_TX,
        paxeer_nonce: nonce,
        recipient: [0x33; 20],
        asset: [0x44; 20],
        amount: uint256_from_u64(1_000_000_000_000_000_000),
    }
}

fn word(value: u64) -> String {
    hex::prefixed(&uint256_from_u64(value))
}

fn registration(settings: &ChainSettings) -> Value {
    json!({
        "method": "eth_call",
        "params": [{
            "to": hex::prefixed(&LAYERX_BRIDGE_PRECOMPILE),
            "data": hex::prefixed(&encode_get_chain(settings.chain_id))
        }, "latest"],
        "result": format!(
            "{ONE}000000000000000000000000{}{}{}",
            hex::encode(&settings.vault),
            &word(12)[2..],
            &ONE[2..]
        )
    })
}

fn burn_log(settings: &ChainSettings, nonce: u64, log_index: u64) -> Value {
    json!({
        "address": hex::prefixed(&LAYERX_BRIDGE_PRECOMPILE),
        "topics": [
            BURN_OUT_TOPIC,
            word(settings.chain_id),
            "0x0000000000000000000000004444444444444444444444444444444444444444",
            word(nonce)
        ],
        "data": "0x0000000000000000000000000000000000000000000000000de0b6b3a76400000000000000000000000000003333333333333333333333333333333333333333",
        "blockNumber": "0x1c",
        "blockHash": BURN_BLOCK_HASH,
        "transactionHash": hex::prefixed(&BURN_TX),
        "logIndex": hex::quantity(log_index),
        "removed": false
    })
}

fn vault_call(settings: &ChainSettings, data: &str) -> Value {
    json!([{"to": hex::prefixed(&settings.vault), "data": data}, "latest"])
}

fn nullified(settings: &ChainSettings, nonce: u64) -> Value {
    vault_call(
        settings,
        &hex::prefixed(&encode_nullified(&burn(settings, nonce).nullifier())),
    )
}

/// Every rule a ready destination answers in every phase.
fn destination(settings: &ChainSettings) -> Vec<Value> {
    vec![
        json!({"method": "eth_chainId", "params": [], "result": hex::quantity(settings.chain_id)}),
        json!({"method": "eth_call", "params": vault_call(settings, "0x42cde4e8"), "result": ONE}),
        json!({"method": "eth_call", "params": vault_call(settings, "0xe7eb466f"), "result": ATTESTOR_SET}),
        json!({"method": "eth_estimateGas", "params": "$any", "result": "0x20000"}),
        json!({"method": "eth_getTransactionCount", "params": "$any", "result": "0x9"}),
        json!({"method": "eth_maxPriorityFeePerGas", "params": [], "result": "0x77359400"}),
        json!({"method": "eth_gasPrice", "params": [], "result": "0x2540be400"}),
        json!({"method": "eth_sendRawTransaction", "params": "$any", "result": "$hash"}),
    ]
}

fn receipt(submitted: usize) -> Vec<Value> {
    let hash = format!("$submitted:{submitted}");
    vec![
        json!({
            "method": "eth_getTransactionReceipt",
            "params": [hash],
            "result": {"transactionHash": hash, "blockNumber": "0x100", "status": "0x1"}
        }),
        json!({"method": "eth_blockNumber", "params": [], "result": "0x10c"}),
    ]
}

/// The recorded exchange: Paxeer finalises one burn to each destination;
/// the failing destination's RPC is unavailable in phase `down` and answers
/// in `restart`; the ready destination's first release is in flight in
/// `down`, unknown to the node in `restart` and final in `released`.
fn recording(directory: &Path) -> Recording {
    let fails = json!({
        "method": "eth_call",
        "params": nullified(&FAILING, 7),
        "fault": "unavailable"
    });
    let mut ready = destination(&READY);
    ready.push(json!({"method": "eth_call", "params": nullified(&READY, 8), "result": FALSE}));
    let document = json!({
        "endpoints": {
            "paxeer": {
                "*": [
                    {"method": "eth_chainId", "params": [], "result": "0xe5"},
                    registration(&FAILING),
                    registration(&READY),
                    {"method": "eth_blockNumber", "params": [], "result": "0x20"},
                    {
                        "method": "eth_getLogs",
                        "params": [{
                            "address": hex::prefixed(&LAYERX_BRIDGE_PRECOMPILE),
                            "fromBlock": "0x19",
                            "toBlock": "0x1e",
                            "topics": [BURN_OUT_TOPIC, [word(1), word(2)]]
                        }],
                        "result": [burn_log(&FAILING, 7, 0), burn_log(&READY, 8, 1)]
                    },
                    {
                        "method": "eth_getBlockByNumber",
                        "params": ["0x1c", false],
                        "result": {"number": "0x1c", "hash": BURN_BLOCK_HASH}
                    }
                ]
            },
            "ethereum-1": {
                "*": destination(&FAILING),
                "down": [fails],
                "restart": [{"method": "eth_call", "params": nullified(&FAILING, 7), "result": FALSE}],
                "released": receipt(1)
            },
            "ethereum-2": {
                "*": ready,
                "down": [
                    {"method": "eth_getTransactionReceipt", "params": ["$submitted:0"], "result": null},
                    {"method": "eth_getTransactionByHash", "params": ["$submitted:0"], "result": {"hash": "$submitted:0"}}
                ],
                "restart": [
                    {"method": "eth_getTransactionReceipt", "params": ["$submitted:0"], "result": null},
                    {"method": "eth_getTransactionByHash", "params": ["$submitted:0"], "result": null}
                ],
                "released": receipt(0)
            }
        }
    });
    let path = directory.join("outbound_isolation.json");
    fs::write(&path, document.to_string())
        .unwrap_or_else(|error| panic!("{}: {error}", path.display()));
    Recording::load(
        path.to_str()
            .unwrap_or_else(|| panic!("{} is not text", path.display())),
    )
}

fn signer(name: &str) -> SignerServer {
    SignerServer::start(
        name,
        vec![
            ("attestor-1", key(ATTESTOR), vec![ATTEST_OUTBOUND_DOMAIN]),
            (
                "paxeer-fees",
                key(PAXEER_FEES),
                vec![PAXEER_TRANSACTION_DOMAIN],
            ),
            (
                "ethereum-fees",
                key(ETHEREUM_FEES),
                vec![ETHEREUM_TRANSACTION_DOMAIN],
            ),
        ],
    )
}

fn link(recording: &Recording, signer: &SignerServer, settings: ChainSettings) -> ChainLink {
    ChainLink {
        settings,
        rpc: recording.endpoint(&format!("ethereum-{}", settings.chain_id)),
        submitter: Submitter::ethereum(signer.remote("ethereum-fees", &key(ETHEREUM_FEES)))
            .unwrap_or_else(|error| panic!("ethereum submitter: {error:?}")),
    }
}

fn start(recording: &Recording, signer: &SignerServer, journal: &Path) -> Relayer {
    let parts = RelayerParts {
        attestor: Attestor::new(signer.remote("attestor-1", &key(ATTESTOR)))
            .unwrap_or_else(|error| panic!("attestor: {error:?}")),
        paxeer: PaxeerLink {
            settings: PAXEER,
            rpc: recording.endpoint("paxeer"),
            submitter: Submitter::paxeer(signer.remote("paxeer-fees", &key(PAXEER_FEES)))
                .unwrap_or_else(|error| panic!("paxeer submitter: {error:?}")),
        },
        chains: vec![
            link(recording, signer, FAILING),
            link(recording, signer, READY),
        ],
        journal: Journal::open(journal).unwrap_or_else(|error| panic!("journal: {error}")),
        cosign: None,
        max_submissions: 3,
    };
    Relayer::new(parts).unwrap_or_else(|error| panic!("relayer startup: {error}"))
}

fn requests(signer: &SignerServer, domain: &[u8]) -> usize {
    signer
        .requests()
        .iter()
        .filter(|(_, served, _)| served == domain)
        .count()
}

fn hash(raw: &[u8]) -> [u8; 32] {
    Keccak256::digest(raw).into()
}

fn step(relayer: &mut Relayer, what: &str) -> StepReport {
    relayer
        .outbound_step()
        .unwrap_or_else(|error| panic!("{what}: {error}"))
}

#[test]
fn a_failed_destination_is_isolated_and_replayed_after_restart() {
    let directory = work_directory("outbound-isolation");
    let recording = recording(&directory);
    let signer = signer("outbound-isolation");
    let journal = directory.join("relayer.jsonl");
    let failing = outbound_key(FAILING.chain_id, &BURN_TX, 7);
    let ready = outbound_key(READY.chain_id, &BURN_TX, 8);
    let failure = ItemFailure {
        item: failing.clone(),
        chain_id: FAILING.chain_id,
        error: RelayerError::Rpc(RpcFault::Unavailable),
    };

    // One pass: the failing destination's RPC is down, the ready one is
    // signed, journaled and broadcast in the same pass.
    recording.set_phase("down");
    let mut relayer = start(&recording, &signer, &journal);
    let report = step(&mut relayer, "down");
    assert_eq!(report.observed, 2);
    assert_eq!(report.submitted, 1);
    assert_eq!(relayer.failures(), std::slice::from_ref(&failure));
    let sent = recording.sent();
    assert_eq!(sent.len(), 1);
    assert_eq!(sent[0].0, "ethereum-2");
    let state = relayer.journal().state();
    let isolated = &state.items[&failing];
    assert!(isolated.is_open());
    assert_eq!(isolated.signature, None);
    assert!(isolated.submissions.is_empty());
    let pending = state.items[&ready]
        .pending()
        .unwrap_or_else(|| panic!("the ready release is journaled"))
        .clone();
    assert_eq!(pending.raw, sent[0].1);
    assert_eq!(pending.tx_hash, hash(&sent[0].1));
    assert_eq!(pending.nonce, 9);
    assert_eq!(requests(&signer, ATTEST_OUTBOUND_DOMAIN), 1);
    assert_eq!(requests(&signer, ETHEREUM_TRANSACTION_DOMAIN), 1);

    // The repeated failure stays visible and is attempted exactly once per
    // pass; the in-flight release is neither re-signed nor rebroadcast.
    let report = step(&mut relayer, "down again");
    assert_eq!(report.waiting, 1);
    assert_eq!(report.submitted, 0);
    assert_eq!(relayer.failures(), std::slice::from_ref(&failure));
    assert_eq!(recording.count("ethereum-1", "eth_call"), 2);
    assert_eq!(recording.sent().len(), 1);
    assert_eq!(requests(&signer, ETHEREUM_TRANSACTION_DOMAIN), 1);
    assert_eq!(recording.unmatched(), Vec::<String>::new());
    drop(relayer);

    // Restart: the unknown release is rebroadcast byte for byte at its
    // journaled nonce, and the recovered destination is released.
    recording.set_phase("restart");
    let mut relayer = start(&recording, &signer, &journal);
    let report = step(&mut relayer, "restart");
    assert_eq!(report.observed, 0);
    assert_eq!(report.submitted, 1);
    assert_eq!(report.waiting, 1);
    assert!(relayer.failures().is_empty());
    let sent = recording.sent();
    assert_eq!(sent.len(), 3);
    let rebroadcast = sent
        .iter()
        .filter(|(endpoint, raw)| endpoint == "ethereum-2" && *raw == pending.raw)
        .count();
    assert_eq!(rebroadcast, 2);
    let recovered = sent
        .iter()
        .find(|(endpoint, _)| endpoint == "ethereum-1")
        .unwrap_or_else(|| panic!("the recovered destination is released"))
        .1
        .clone();
    let state = relayer.journal().state();
    assert_eq!(state.items[&ready].pending(), Some(&pending));
    assert_eq!(state.items[&ready].submissions.len(), 1);
    let released = state.items[&failing]
        .pending()
        .unwrap_or_else(|| panic!("the recovered release is journaled"));
    assert_eq!(released.raw, recovered);
    assert_eq!(released.nonce, 9);
    assert_eq!(requests(&signer, ATTEST_OUTBOUND_DOMAIN), 2);
    assert_eq!(requests(&signer, ETHEREUM_TRANSACTION_DOMAIN), 2);

    // Both releases are final; nothing is sent or completed twice.
    recording.set_phase("released");
    let report = step(&mut relayer, "released");
    assert_eq!(report.completed, 2);
    assert!(relayer.failures().is_empty());
    let state = relayer.journal().state();
    assert_eq!(
        state.items[&ready].completion,
        Some(Completion::Included {
            tx_hash: hash(&pending.raw),
            block_number: 0x100,
        })
    );
    assert_eq!(
        state.items[&failing].completion,
        Some(Completion::Included {
            tx_hash: hash(&recovered),
            block_number: 0x100,
        })
    );
    assert_eq!(step(&mut relayer, "idle"), StepReport::default());
    assert_eq!(recording.sent().len(), 3);
    assert_eq!(recording.unmatched(), Vec::<String>::new());
    drop(relayer);

    // Corrupt shared journal state stays fatal: the relayer cannot reopen it.
    OpenOptions::new()
        .append(true)
        .open(&journal)
        .and_then(|mut file| file.write_all(b"not a journal entry\n"))
        .unwrap_or_else(|error| panic!("corrupt journal: {error}"));
    assert!(matches!(
        Journal::open(&journal),
        Err(JournalError::Corrupt { .. })
    ));
}

#[test]
fn shared_journal_and_configuration_failures_are_not_item_scoped() {
    assert!(RelayerError::Rpc(RpcFault::Unavailable).is_item_scoped());
    assert!(RelayerError::NotAttestor.is_item_scoped());
    assert!(!RelayerError::Journal(JournalError::Io("disk full".to_owned())).is_item_scoped());
    assert!(!RelayerError::Journal(JournalError::Corrupt { line: 1 }).is_item_scoped());
    assert!(!RelayerError::Configuration("chain".to_owned()).is_item_scoped());
    assert!(!RelayerError::Reorganised { block_number: 1 }.is_item_scoped());
}
