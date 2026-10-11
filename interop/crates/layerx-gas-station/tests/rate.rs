use std::cell::RefCell;
use std::path::PathBuf;
use std::rc::Rc;
use std::time::Duration;

use base64::engine::general_purpose::STANDARD;
use base64::Engine as _;
use ed25519_dalek::Signer as _;
use k256::ecdsa::SigningKey;
use k256::elliptic_curve::rand_core::OsRng;
use layerx_gas_station::config::ConfigError;
use layerx_gas_station::journal::{Entry, Journal, Publication};
use layerx_gas_station::quote::{address_word, keccak, word, Address, Word};
use layerx_gas_station::rate::{
    PublisherConfig, RateFile, RatePublisher, RateRefusal, RATE_GAS_LIMIT,
};
use layerx_gas_station::rpc::{bytes, hex, ConfiguredRpc, Exchange, RpcFault};
use layerx_gas_station::signer::{LocalSigner, QuoteSigner};
use layerx_gas_station::tx::recover;
use layerx_wire::encode::Encoder;
use layerx_wire::hash::receipt_digest;
use layerx_wire::receipt::{decode, encode};
use layerx_wire::WireError;
use serde_json::{json, Value};

type TestResult = Result<(), Box<dyn std::error::Error>>;

const PAYMASTER: Address = [0x44; 20];
const NOW: u64 = 1_900_000_000;

const BLOCK: u64 = 0x10;
const BLOCK_HASH: Word = [0x77; 32];
const FEE_TOKEN: Address = [
    0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x10, 0x18,
];
/// The pinned sequencer receipt key: its keyId, ed25519 seed and the
/// inclusive height range it signs for.
const RECEIPT_KEY_ID: Word = [0x5a; 32];
const RECEIPT_KEY_SEED: [u8; 32] = [0x42; 32];
const RECEIPT_FIRST_HEIGHT: u64 = 1;
const RECEIPT_LAST_HEIGHT: u64 = 0x100;

/// How the sequencer corrupts the signed receipt it serves.
#[derive(Clone, Copy, Debug)]
enum Tamper {
    None,
    Signature,
    UnpinnedKeyId,
    HeightOutsideRange,
    OtherActivity,
    Missing,
}

fn receipt_key() -> ed25519_dalek::SigningKey {
    ed25519_dalek::SigningKey::from_bytes(&RECEIPT_KEY_SEED)
}

fn wire<T>(result: Result<T, WireError>) -> Result<T, Box<dyn std::error::Error>> {
    result.map_err(|error| format!("{error:?}").into())
}

fn account(address: &Address) -> Word {
    let mut word = [0; 32];
    word[12..].copy_from_slice(address);
    word
}

/// The `ProtocolReceipt` v2 the sequencer signs for an EVM transaction
/// (contracts/sequencer.md a1), encoded by the canonical wire encoder and
/// signed by the pinned receipt key unless `tamper` corrupts it.
fn signed_receipt(
    tamper: Tamper,
    activity_id: Word,
    nonce: u64,
    from: Address,
    to: Address,
    status: u64,
) -> Result<Vec<u8>, Box<dyn std::error::Error>> {
    let (key_id, height, activity_id) = match tamper {
        Tamper::UnpinnedKeyId => ([0x5b; 32], BLOCK, activity_id),
        Tamper::HeightOutsideRange => (RECEIPT_KEY_ID, RECEIPT_LAST_HEIGHT + 1, activity_id),
        Tamper::OtherActivity => (RECEIPT_KEY_ID, BLOCK, word(0xbad)),
        Tamper::None | Tamper::Signature | Tamper::Missing => (RECEIPT_KEY_ID, BLOCK, activity_id),
    };
    let fields = |signature: Option<[u8; 64]>| -> Result<Vec<u8>, WireError> {
        let mut encoder = Encoder::new(4096);
        encoder.structure_header_version(0x5201, layerx_wire::limits::PROTOCOL_VERSION)?;
        encoder.u16(layerx_wire::limits::PROTOCOL_VERSION)?;
        encoder.bytes(&activity_id, 32)?;
        encoder.u64(nonce + 1)?;
        encoder.bytes(&[0x21; 32], 32)?;
        encoder.bytes(&[0x22; 32], 32)?;
        encoder.bytes(&[0x23; 32], 32)?;
        encoder.i32(i32::from(status != 1))?;
        encoder.sequence_length(0, 512)?;
        encoder.u128(1_500_000_000 * 35_000)?;
        let mut batch_id = [0; 32];
        batch_id[24..].copy_from_slice(&height.to_be_bytes());
        encoder.bytes(&batch_id, 32)?;
        encoder.u16(12)?;
        encoder.u32(1)?;
        encoder.u32(0)?;
        encoder.u8(2)?;
        encoder.bytes(&[0x24; 32], 32)?;
        encoder.u128(0)?;
        encoder.bytes(&account(&from), 32)?;
        encoder.u128(10_u128.pow(18))?;
        encoder.u128(10_u128.pow(18) - 1_500_000_000 * 35_000)?;
        encoder.u64(nonce)?;
        encoder.bytes(&account(&to), 32)?;
        encoder.u128(0)?;
        encoder.u128(0)?;
        encoder.bytes(&[0; 32], 32)?;
        encoder.bytes(&[0; 32], 32)?;
        encoder.bytes(&key_id, 32)?;
        encoder.u64(NOW * 1_000)?;
        encoder.u8(u8::from(signature.is_some()))?;
        if let Some(signature) = signature {
            encoder.bytes(&signature, 64)?;
        }
        Ok(encoder.finish())
    };
    let digest = wire(receipt_digest(&wire(fields(None))?))?;
    let mut signature = receipt_key().sign(&digest).to_bytes();
    if matches!(tamper, Tamper::Signature) {
        signature[0] ^= 1;
    }
    let signed = wire(fields(Some(signature)))?;
    let canonical = wire(encode(&wire(decode(&signed))?))?;
    assert_eq!(canonical, signed);
    Ok(canonical)
}

/// The paymaster, the owner account and the sequencer as the node reports
/// them: `rung` is how far the sequencer has taken every sent transaction
/// ("unknown", "submitted", "receipted" or "committed"); a receipted
/// transaction advances the pending nonce, a committed one the finalized nonce.
struct Chain {
    rate: u128,
    updated_at: u64,
    owner: Address,
    balance: u128,
    nonce: u64,
    finalized: u64,
    status: u64,
    gas_used: u64,
    effective_gas_price: u128,
    rung: &'static str,
    sent: Vec<Vec<u8>>,
    nonces: Vec<(Word, u64)>,
    tamper: Tamper,
}
impl Chain {
    fn sequencer_receipt(&self, hash: &Word) -> Result<Value, RpcFault> {
        let Some(nonce) = self.nonce_of(hash) else {
            return Ok(Value::Null);
        };
        Ok(match self.rung {
            "submitted" => json!({"rung": "submitted", "txHash": hex(hash),
                "nonce": quantity(nonce.into())}),
            rung => {
                let raw = self
                    .sent
                    .iter()
                    .find(|raw| keccak(raw) == *hash)
                    .ok_or(RpcFault::Malformed)?;
                let (to, rate, from) = published_rate(raw).map_err(|_| RpcFault::Malformed)?;
                let logs = if self.status == 1 {
                    json!([{"address": hex(&to),
                        "topics": [hex(&keccak(b"RateUpdated(uint256,uint256)"))],
                        "data": hex(&[rate, word(NOW.into())].concat()),
                        "logIndex": "0x0"}])
                } else {
                    json!([])
                };
                let mut receipt = json!({"rung": rung, "txHash": hex(hash),
                    "seq": quantity(nonce.into()), "height": quantity(BLOCK.into()),
                    "index": "0x1", "timestamp": quantity(NOW.into()),
                    "status": quantity(self.status.into()), "returnData": "0x",
                    "logs": logs, "gasUsed": quantity(self.gas_used.into()),
                    "cumulativeGasUsed": quantity(self.gas_used.into()),
                    "effectiveGasPrice": quantity(self.effective_gas_price),
                    "from": hex(&from), "to": hex(&to), "contractAddress": Value::Null,
                    "nonce": quantity(nonce.into()), "vmError": ""});
                if rung == "committed" {
                    receipt["blockHash"] = Value::String(hex(&BLOCK_HASH));
                }
                if !matches!(self.tamper, Tamper::Missing) {
                    let signed = signed_receipt(self.tamper, *hash, nonce, from, to, self.status)
                        .map_err(|_| RpcFault::Malformed)?;
                    receipt["keyId"] = Value::String(hex(&RECEIPT_KEY_ID));
                    receipt["receipt"] = Value::String(hex(&signed));
                }
                receipt
            }
        })
    }

    fn executed(&self) -> Option<u64> {
        matches!(self.rung, "receipted" | "committed")
            .then(|| self.nonces.iter().map(|(_, n)| n + 1).max())
            .flatten()
    }
    fn nonce_of(&self, hash: &Word) -> Option<u64> {
        self.nonces
            .iter()
            .find_map(|(h, n)| (h == hash).then_some(*n))
    }
}
fn quantity(v: u128) -> Value {
    Value::String(format!("{v:#x}"))
}

struct ChainExchange(Rc<RefCell<Chain>>);
impl Exchange for ChainExchange {
    fn request_until(
        &self,
        endpoint: &str,
        method: &str,
        params: &Value,
        _: std::time::Instant,
    ) -> Result<Value, RpcFault> {
        self.request(endpoint, method, params)
    }

    fn request(&self, _: &str, method: &str, params: &Value) -> Result<Value, RpcFault> {
        let mut chain = self.0.borrow_mut();
        let hash_param = || -> Result<Word, RpcFault> {
            bytes(params[0].as_str().ok_or(RpcFault::Malformed)?)?
                .try_into()
                .map_err(|_| RpcFault::Malformed)
        };
        Ok(match method {
            "eth_chainId" => quantity(1325),
            "eth_getBlockByNumber" => match params[0].as_str() {
                Some("latest") => {
                    json!({"timestamp": quantity(NOW.into()), "baseFeePerGas": quantity(1_000_000_000)})
                }
                Some("finalized" | "0x10") => {
                    json!({"number": quantity(BLOCK.into()), "hash": hex(&BLOCK_HASH)})
                }
                _ => return Err(RpcFault::Rejected { code: -32602 }),
            },
            "eth_call" => {
                assert_eq!(params[0]["to"], hex(&PAYMASTER));
                let selector = |name: &[u8]| hex(&keccak(name)[..4]);
                let data = &params[0]["data"];
                let value = if *data == selector(b"rate()") {
                    word(chain.rate)
                } else if *data == selector(b"rateUpdatedAt()") {
                    word(chain.updated_at.into())
                } else if *data == selector(b"owner()") {
                    address_word(chain.owner)
                } else {
                    return Err(RpcFault::Rejected { code: 3 });
                };
                Value::String(hex(&value))
            }
            "eth_getBalance" => quantity(chain.balance),
            "eth_getTransactionCount" => {
                let executed = chain.executed().unwrap_or(0);
                match params[1].as_str() {
                    Some("pending") => quantity(chain.nonce.max(executed).into()),
                    Some("finalized") if chain.rung == "committed" => {
                        quantity(chain.finalized.max(executed).into())
                    }
                    Some("finalized") => quantity(chain.finalized.into()),
                    _ => return Err(RpcFault::Rejected { code: -32602 }),
                }
            }
            "eth_sendRawTransaction" => {
                let raw = bytes(params[0].as_str().ok_or(RpcFault::Malformed)?)?;
                let hash = keccak(&raw);
                chain.sent.push(raw);
                if chain.rung == "unknown" {
                    return Err(RpcFault::Rejected { code: -32000 });
                }
                if chain.nonce_of(&hash).is_none() {
                    let nonce = chain.nonce.max(chain.executed().unwrap_or(0));
                    chain.nonces.push((hash, nonce));
                }
                Value::String(hex(&hash))
            }
            "pax_sequencerReceipt" => chain.sequencer_receipt(&hash_param()?)?,
            "eth_getTransactionReceipt" => {
                let hash = hash_param()?;
                if chain.rung != "committed" || chain.nonce_of(&hash).is_none() {
                    return Ok(Value::Null);
                }
                json!({"transactionHash": hex(&hash), "from": hex(&chain.owner), "to": hex(&PAYMASTER),
                    "blockNumber": quantity(BLOCK.into()), "blockHash": hex(&BLOCK_HASH),
                    "gasUsed": quantity(chain.gas_used.into()),
                    "effectiveGasPrice": quantity(chain.effective_gas_price),
                    "status": quantity(chain.status.into())})
            }
            _ => return Err(RpcFault::Rejected { code: -32601 }),
        })
    }
}

struct Lane {
    dir: PathBuf,
    chain: Rc<RefCell<Chain>>,
    owner: Address,
    key: Vec<u8>,
}
impl Lane {
    fn new(name: &str) -> Result<Self, Box<dyn std::error::Error>> {
        let dir = std::env::temp_dir().join(format!("paxeer-rate-{name}-{}", std::process::id()));
        if dir.exists() {
            std::fs::remove_dir_all(&dir)?;
        }
        std::fs::create_dir_all(&dir)?;
        let key = SigningKey::random(&mut OsRng).to_bytes().to_vec();
        let owner = signer(&key)?.address();
        Ok(Self {
            dir,
            chain: Rc::new(RefCell::new(Chain {
                rate: 3_114_000,
                updated_at: NOW - 120,
                owner,
                balance: 10_u128.pow(18),
                nonce: 7,
                finalized: 7,
                status: 1,
                gas_used: 35_000,
                effective_gas_price: 1_500_000_000,
                rung: "committed",
                sent: Vec::new(),
                nonces: Vec::new(),
                tamper: Tamper::None,
            })),
            owner,
            key,
        })
    }
    fn rate_file(&self, text: &str) -> Result<PathBuf, std::io::Error> {
        use std::os::unix::fs::PermissionsExt as _;
        let path = self.dir.join("rate.toml");
        std::fs::write(&path, text)?;
        std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o600))?;
        Ok(path)
    }
    fn publisher(
        &self,
        budget: u128,
    ) -> Result<RatePublisher<LocalSigner, ConfiguredRpc<ChainExchange>>, Box<dyn std::error::Error>>
    {
        self.publisher_with(budget, 5_000_000_000)
    }
    fn publisher_with(
        &self,
        budget: u128,
        ceiling: u128,
    ) -> Result<RatePublisher<LocalSigner, ConfiguredRpc<ChainExchange>>, Box<dyn std::error::Error>>
    {
        let mut value = config_json();
        value["rate_gas_budget_per_day"] = json!(budget);
        value["rate_max_fee_per_gas"] = json!(ceiling);
        self.publisher_from(&value, Duration::ZERO)
    }
    /// A publisher that waits `retry` seconds for a sequencer receipt,
    /// polling every 10 ms.
    fn publisher_confirming_within(
        &self,
        retry: u64,
    ) -> Result<RatePublisher<LocalSigner, ConfiguredRpc<ChainExchange>>, Box<dyn std::error::Error>>
    {
        let mut value = config_json();
        value["rate_confirmation_retry_seconds"] = json!(retry);
        self.publisher_from(&value, Duration::from_millis(10))
    }
    fn publisher_from(
        &self,
        value: &Value,
        poll: Duration,
    ) -> Result<RatePublisher<LocalSigner, ConfiguredRpc<ChainExchange>>, Box<dyn std::error::Error>>
    {
        let config = PublisherConfig::parse(&value.to_string())?;
        let rpc = ConfiguredRpc::new(&config.station, ChainExchange(self.chain.clone()))?;
        let journal = Journal::open(&self.dir.join("rate.jsonl"))?;
        Ok(RatePublisher::new(
            config,
            signer(&self.key)?,
            rpc,
            journal,
            poll,
        )?)
    }
    fn sent(&self) -> usize {
        self.chain.borrow().sent.len()
    }
}
impl Drop for Lane {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.dir);
    }
}

fn signer(key: &[u8]) -> Result<LocalSigner, Box<dyn std::error::Error>> {
    let name = format!(
        "PAXEER_RATE_TEST_KEY_{}",
        hex(&keccak(key)[..8])[2..].to_uppercase()
    );
    std::env::set_var(&name, hex(key));
    let signer = LocalSigner::from_env(&name);
    std::env::remove_var(&name);
    Ok(signer?)
}

fn config_json() -> Value {
    json!({"chain_id":1325,"endpoints":["https://paxeer.app"],
        "paymaster":hex(&PAYMASTER),
        "token":"0x21f7b20a555199fa73A238B1a91FD0f549068fEe","decimals":6,
        "max_rate_age":300,"spread_bps":500,"margin_bps":100,"per_account_limit":4_000_000,
        "per_interval_limit":8_000_000,"per_quote_limit":3_000_000,"interval_seconds":60,
        "balance_floor":100,"relayer_key_env":"GAS_STATION_RELAYER_KEY",
        "max_priority_fee_per_gas":1_000_000_000,"rate_owner_key_env":"GAS_STATION_RATE_OWNER_KEY",
        "rate_cadence_seconds":120,"rate_gas_budget_per_day":1_000_000,
        "rate_balance_floor":10_u64.pow(15),"rate_max_fee_per_gas":5_000_000_000_u64,
        "rate_confirmation_retry_seconds":60,
        "sequencer_receipt_keys":[receipt_key_entry(RECEIPT_KEY_ID, RECEIPT_FIRST_HEIGHT, RECEIPT_LAST_HEIGHT)]})
}

/// One pinned key in the shape `px_getReceiptKeys` returns.
fn receipt_key_entry(key_id: Word, first: u64, last: u64) -> Value {
    json!({"keyId": STANDARD.encode(key_id),
        "publicKey": STANDARD.encode(receipt_key().verifying_key().to_bytes()),
        "firstHeight": first.to_string(), "lastHeight": last.to_string()})
}

/// The paymaster, the rate and the recovered signer of a type-2 setRate
/// envelope: 0x02, the list header, the fields up to the data, an empty
/// access list, then y parity, r and s.
fn published_rate(raw: &[u8]) -> Result<(Address, Word, Address), Box<dyn std::error::Error>> {
    assert_eq!(&raw[..2], &[2, 0xf8]);
    let selector = &keccak(b"setRate(uint256)")[..4];
    let at = raw
        .windows(4)
        .position(|w| w == selector)
        .ok_or("setRate selector absent")?;
    assert_eq!((raw[at - 23], raw[at - 2], raw[at - 1]), (0x94, 0x80, 0xa4));
    let to: Address = raw[at - 22..at - 2].try_into()?;
    let rate: Word = raw[at + 4..at + 36].try_into()?;
    assert_eq!(raw[at + 36], 0xc0);
    let fields = &raw[3..at + 37];
    let mut signature = [0_u8; 65];
    let mut cursor = at + 37;
    signature[64] = match raw[cursor] {
        0x80 => 27,
        1 => 28,
        _ => return Err("invalid parity".into()),
    };
    for end in [32, 64] {
        cursor += 1;
        let length = usize::from(raw[cursor] - 0x80);
        signature[end - length..end].copy_from_slice(&raw[cursor + 1..cursor + 1 + length]);
        cursor += length;
    }
    assert_eq!(cursor + 1, raw.len());
    let mut unsigned = vec![2, 0xf8, u8::try_from(fields.len())?];
    unsigned.extend_from_slice(fields);
    Ok((to, rate, recover(keccak(&unsigned), &signature)?))
}

#[test]
fn publishes_the_owner_rate_with_set_rate_and_journals_it() -> TestResult {
    let lane = Lane::new("publish")?;
    let file = lane.rate_file("# owner rate\nrate = 3_114_000\nset_at = 1_899_000_000\n")?;
    let mut publisher = lane.publisher(1_000_000)?;
    let publication = publisher.publish(&file)?;
    assert_eq!(publication.owner, lane.owner);
    assert_eq!(publication.nonce, 7);
    assert_eq!(publication.rate, word(3_114_000));
    assert_eq!(publication.gas_limit, RATE_GAS_LIMIT);
    assert_eq!(publication.signed_at, NOW);
    let raw = lane.chain.borrow().sent[0].clone();
    assert_eq!(publication.hash, keccak(&raw));
    assert_eq!(
        published_rate(&raw)?,
        (PAYMASTER, word(3_114_000), lane.owner)
    );
    let state = publisher.journal().state().clone();
    let (journalled, settlement) = state.publications[&publication.hash];
    assert_eq!(journalled, publication);
    assert_eq!(
        settlement.map(|s| (s.gas_used, s.succeeded)),
        Some((35_000, true))
    );
    assert_eq!(state.publication_gas(NOW / 86_400), 35_000);
    drop(publisher);
    let reopened = Journal::open(&lane.dir.join("rate.jsonl"))?;
    assert_eq!(reopened.state(), &state);
    let lines = std::fs::read_to_string(lane.dir.join("rate.jsonl"))?;
    let entries: Vec<Entry> = lines
        .lines()
        .map(serde_json::from_str)
        .collect::<Result<_, _>>()?;
    assert_eq!(entries.len(), 3);
    assert!(
        matches!(&entries[0], Entry::RatePrepared { transaction } if transaction.publication == publication && transaction.raw == raw)
    );
    assert_eq!(
        entries[1],
        Entry::RateBroadcast {
            hash: publication.hash
        }
    );
    assert!(
        matches!(&entries[2], Entry::RateFinalized { hash, settlement, .. } if *hash == publication.hash && settlement.succeeded)
    );
    assert!(!lines.contains(&hex(&lane.key)[2..]));
    Ok(())
}

#[test]
fn refusals_never_publish() -> TestResult {
    let lane = Lane::new("refusals")?;
    let valid = "rate = 3200000\nset_at = 1899000000\n";
    let mut publisher = lane.publisher(1_000_000)?;
    for (text, refusal) in [
        (None, RateRefusal::RateFileMissing),
        (Some("rate = 3200000\n"), RateRefusal::RateFileMalformed),
        (
            Some("rate = 1\nrate = 2\nset_at = 1\n"),
            RateRefusal::RateFileMalformed,
        ),
        (
            Some("rate = 1\nset_at = 1\nsid_usd = 1\n"),
            RateRefusal::RateFileMalformed,
        ),
        (
            Some("rate = \"3200000\"\nset_at = 1\n"),
            RateRefusal::RateFileMalformed,
        ),
        (
            Some("rate = 1\nset_at = 0\n"),
            RateRefusal::RateFileMalformed,
        ),
        (
            Some("rate = 1\nset_at = 5\nnot_after = 5\n"),
            RateRefusal::RateFileMalformed,
        ),
        (Some("rate = 0\nset_at = 1\n"), RateRefusal::ZeroRate),
        (
            Some("rate = 1\nset_at = 1900000001\n"),
            RateRefusal::NotYetSet,
        ),
        (
            Some("rate = 1\nset_at = 1\nnot_after = 1900000000\n"),
            RateRefusal::Expired,
        ),
        (
            Some("rate = 3114000\nset_at = 1\n"),
            RateRefusal::Unchanged { age: 119 },
        ),
    ] {
        let path = if let Some(text) = text {
            lane.rate_file(text)?
        } else {
            let path = lane.dir.join("rate.toml");
            let _ = std::fs::remove_file(&path);
            path
        };
        if refusal == (RateRefusal::Unchanged { age: 119 }) {
            lane.chain.borrow_mut().updated_at = NOW - 119;
        }
        assert_eq!(publisher.publish(&path), Err(refusal), "{text:?}");
    }
    let file = lane.rate_file(valid)?;
    lane.chain.borrow_mut().owner = [0x99; 20];
    assert_eq!(publisher.publish(&file), Err(RateRefusal::NotOwner));
    lane.chain.borrow_mut().owner = lane.owner;
    lane.chain.borrow_mut().balance = 10_u128.pow(15) - 1;
    assert_eq!(publisher.publish(&file), Err(RateRefusal::BelowFloor));
    assert_eq!(lane.sent(), 0);
    assert!(publisher.journal().state().publications.is_empty());
    lane.chain.borrow_mut().balance = 10_u128.pow(18);
    lane.chain.borrow_mut().status = 0;
    assert_eq!(publisher.publish(&file), Err(RateRefusal::Reverted));
    assert_eq!(lane.sent(), 1);
    Ok(())
}

#[test]
fn daily_gas_budget_stops_publishing() -> TestResult {
    let lane = Lane::new("budget")?;
    let file = lane.rate_file("rate = 3200000\nset_at = 1899000000\n")?;
    let mut publisher = lane.publisher(u128::from(RATE_GAS_LIMIT) + 35_000 - 1)?;
    publisher.publish(&file)?;
    lane.chain.borrow_mut().nonce = 8;
    assert_eq!(publisher.publish(&file), Err(RateRefusal::BudgetExhausted));
    assert_eq!(lane.sent(), 1);
    drop(publisher);
    let mut publisher = lane.publisher(u128::from(RATE_GAS_LIMIT) + 35_000)?;
    publisher.publish(&file)?;
    assert_eq!(lane.sent(), 2);
    Ok(())
}

#[test]
fn cadence_must_stay_below_max_rate_age() -> TestResult {
    for (name, value) in [
        ("rate_cadence_seconds", json!(300)),
        ("rate_cadence_seconds", json!(301)),
        ("rate_cadence_seconds", json!(0)),
        ("rate_owner_key_env", json!("GAS_STATION_RELAYER_KEY")),
        ("rate_owner_key_env", json!("sensitive-value")),
        ("rate_gas_budget_per_day", json!(RATE_GAS_LIMIT - 1)),
        ("rate_balance_floor", json!(0)),
        ("rate_max_fee_per_gas", json!(1_000_000_000)),
        ("paymaster", json!(hex(&FEE_TOKEN))),
    ] {
        let mut value_map = config_json();
        value_map[name] = value;
        assert_eq!(
            PublisherConfig::parse(&value_map.to_string()).err(),
            Some(ConfigError { field: name })
        );
    }
    let mut bounded = config_json();
    bounded["rate_cadence_seconds"] = json!(239);
    assert_eq!(
        PublisherConfig::parse(&bounded.to_string())?.cadence_seconds,
        239
    );
    bounded["rate_cadence_seconds"] = json!(240);
    assert_eq!(
        PublisherConfig::parse(&bounded.to_string()).err(),
        Some(ConfigError {
            field: "rate_confirmation_retry_seconds"
        })
    );
    bounded["rate_cadence_seconds"] = json!(299);
    assert_eq!(
        PublisherConfig::parse(&bounded.to_string()).err(),
        Some(ConfigError {
            field: "rate_confirmation_retry_seconds"
        })
    );
    bounded["max_rate_age"] = json!(120);
    bounded["rate_cadence_seconds"] = json!(120);
    assert_eq!(
        PublisherConfig::parse(&bounded.to_string()).err(),
        Some(ConfigError {
            field: "rate_cadence_seconds"
        })
    );
    let Value::Object(fields) = config_json() else {
        return Err("object required".into());
    };
    for name in fields.keys() {
        let mut incomplete = fields.clone();
        incomplete.remove(name);
        let result = PublisherConfig::parse(&Value::Object(incomplete).to_string());
        assert_eq!(result.err().map(|e| e.field.to_owned()), Some(name.clone()));
    }
    Ok(())
}

#[test]
fn rate_file_reads_the_owner_rate() {
    assert_eq!(
        RateFile::parse("# SID base units per whole PAX\nrate = 3_114_000\nset_at = 1899000000\nnot_after = 1999000000\n"),
        Ok(RateFile {
            rate: 3_114_000,
            set_at: 1_899_000_000,
            not_after: Some(1_999_000_000),
        })
    );
}

/// The most the owner pays for one publication at a 1 gwei base fee and a
/// 1 gwei tip: (2 * 1 gwei + 1 gwei) * 60000.
const RESERVE: u128 = 3_000_000_000 * RATE_GAS_LIMIT as u128;

#[test]
fn fee_above_the_ceiling_is_refused_unsigned() -> TestResult {
    let lane = Lane::new("ceiling")?;
    let file = lane.rate_file("rate = 3200000\nset_at = 1899000000\n")?;
    let mut publisher = lane.publisher_with(1_000_000, 2_999_999_999)?;
    assert_eq!(
        publisher.publish(&file),
        Err(RateRefusal::FeeAboveCeiling {
            required: 3_000_000_000,
            ceiling: 2_999_999_999,
        })
    );
    lane.chain.borrow_mut().balance = u128::MAX;
    assert!(matches!(
        publisher.publish(&file),
        Err(RateRefusal::FeeAboveCeiling { .. })
    ));
    assert_eq!(lane.sent(), 0);
    assert!(publisher.journal().state().publications.is_empty());
    drop(publisher);
    let mut publisher = lane.publisher_with(1_000_000, 3_000_000_000)?;
    assert_eq!(publisher.publish(&file)?.max_fee_per_gas, 3_000_000_000);
    assert_eq!(lane.sent(), 1);
    Ok(())
}

#[test]
fn unsettled_publications_reserve_their_most_against_the_floor() -> TestResult {
    let lane = Lane::new("reserve")?;
    let file = lane.rate_file("rate = 3200000\nset_at = 1899000000\n")?;
    let legacy = Publication {
        owner: lane.owner,
        nonce: 7,
        hash: word(0xabc),
        rate: word(3_114_000),
        gas_limit: RATE_GAS_LIMIT,
        max_fee_per_gas: 4_000_000_000,
        signed_at: NOW - 60,
    };
    let mut journal = Journal::open(&lane.dir.join("rate.jsonl"))?;
    journal.append(&Entry::RatePublished {
        publication: legacy,
    })?;
    assert_eq!(journal.state().reserved_wei(), 4_000_000_000 * 60_000);
    drop(journal);
    let refused = lane
        .publisher(1_000_000)
        .err()
        .ok_or("legacy journal accepted")?;
    assert_eq!(
        refused.downcast_ref::<RateRefusal>(),
        Some(&RateRefusal::LegacyUnresolved)
    );
    std::fs::remove_file(lane.dir.join("rate.jsonl"))?;
    let floor = 10_u128.pow(15);
    lane.chain.borrow_mut().rung = "receipted";
    lane.chain.borrow_mut().balance = floor + RESERVE - 1;
    let mut publisher = lane.publisher(1_000_000)?;
    assert_eq!(publisher.publish(&file), Err(RateRefusal::BelowFloor));
    assert_eq!(lane.sent(), 0);
    lane.chain.borrow_mut().balance = floor + RESERVE;
    let publication = publisher.publish(&file)?;
    assert_eq!(publication.nonce, 7);
    let state = publisher.journal().state();
    assert_eq!(state.unsettled(), vec![publication]);
    assert_eq!(state.reserved_wei(), RESERVE);
    assert_eq!(
        state.publication_gas(NOW / 86_400),
        u128::from(RATE_GAS_LIMIT)
    );
    assert_eq!(state.publications[&publication.hash].1, None);
    lane.chain.borrow_mut().rung = "committed";
    assert_eq!(publisher.publish(&file)?, publication);
    assert_eq!(lane.sent(), 1);
    let state = publisher.journal().state();
    assert!(state.unsettled().is_empty());
    assert_eq!(state.reserved_wei(), 0);
    assert_eq!(
        state.publications[&publication.hash].1.map(|s| s.cost_wei),
        Some(1_500_000_000 * 35_000)
    );
    Ok(())
}

#[test]
fn settlement_records_the_actual_cost() -> TestResult {
    let lane = Lane::new("cost")?;
    let file = lane.rate_file("rate = 3200000\nset_at = 1899000000\n")?;
    let mut publisher = lane.publisher(1_000_000)?;
    let publication = publisher.publish(&file)?;
    let state = publisher.journal().state().clone();
    let settlement = state.publications[&publication.hash]
        .1
        .ok_or("publication unsettled")?;
    assert_eq!(settlement.cost_wei, 1_500_000_000 * 35_000);
    assert_eq!(state.publication_wei(NOW / 86_400), 1_500_000_000 * 35_000);
    assert_eq!(state.publication_gas(NOW / 86_400), 35_000);
    assert_eq!(state.reserved_wei(), 0);
    drop(publisher);
    assert_eq!(Journal::open(&lane.dir.join("rate.jsonl"))?.state(), &state);
    lane.chain.borrow_mut().effective_gas_price = 3_000_000_001;
    lane.chain.borrow_mut().nonce = 8;
    lane.chain.borrow_mut().updated_at = NOW - 300;
    let mut publisher = lane.publisher(1_000_000)?;
    assert_eq!(
        publisher.publish(&file),
        Err(RateRefusal::Rpc(RpcFault::Malformed))
    );
    assert_eq!(publisher.journal().state().unsettled().len(), 1);
    Ok(())
}

#[test]
fn cadence_equal_to_max_rate_age_is_refused_at_load() -> TestResult {
    use std::os::unix::fs::PermissionsExt as _;
    let lane = Lane::new("load")?;
    let path = lane.dir.join("publisher.json");
    let mut value = config_json();
    value["rate_cadence_seconds"] = json!(300);
    std::fs::write(&path, value.to_string())?;
    std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o600))?;
    assert_eq!(
        PublisherConfig::load(&path),
        Err(ConfigError {
            field: "rate_cadence_seconds"
        })
    );
    value["rate_cadence_seconds"] = json!(120);
    value["max_rate_age"] = json!(301);
    std::fs::write(&path, value.to_string())?;
    assert_eq!(
        PublisherConfig::load(&path),
        Err(ConfigError {
            field: "max_rate_age"
        })
    );
    value["max_rate_age"] = json!(300);
    value["rate_cadence_seconds"] = json!(240);
    std::fs::write(&path, value.to_string())?;
    assert_eq!(
        PublisherConfig::load(&path),
        Err(ConfigError {
            field: "rate_confirmation_retry_seconds"
        })
    );
    value["rate_cadence_seconds"] = json!(298);
    value["rate_confirmation_retry_seconds"] = json!(1);
    std::fs::write(&path, value.to_string())?;
    assert_eq!(PublisherConfig::load(&path)?.cadence_seconds, 298);
    Ok(())
}

#[test]
fn publication_waits_for_the_sequencer_receipt_and_is_never_resent_at_its_nonce() -> TestResult {
    let lane = Lane::new("unconfirmed")?;
    let file = lane.rate_file("rate = 3200000\nset_at = 1899000000\n")?;
    lane.chain.borrow_mut().rung = "submitted";
    let mut publisher = lane.publisher_confirming_within(1)?;
    assert_eq!(publisher.publish(&file), Err(RateRefusal::ReceiptTimeout));
    assert_eq!(lane.sent(), 1);
    let raw = lane.chain.borrow().sent[0].clone();
    let hash = keccak(&raw);
    let state = publisher.journal().state();
    assert_eq!(state.rate_active.get(&(lane.owner, 7)), Some(&hash));
    assert_eq!(state.rate_transactions.len(), 1);
    assert_eq!(state.rate_broadcast.len(), 1);
    assert_eq!(state.unsettled().len(), 1);
    assert_eq!(publisher.publish(&file), Err(RateRefusal::ReceiptTimeout));
    assert_eq!(lane.sent(), 1);
    assert_eq!(publisher.journal().state().rate_transactions.len(), 1);
    lane.chain.borrow_mut().rung = "committed";
    let publication = publisher.publish(&file)?;
    assert_eq!((publication.nonce, publication.hash), (7, hash));
    assert_eq!(lane.sent(), 1);
    let state = publisher.journal().state();
    assert!(state.rate_active.is_empty());
    assert_eq!(
        state.publications[&hash]
            .1
            .map(|s| (s.gas_used, s.succeeded)),
        Some((35_000, true))
    );
    let next = publisher.publish(&file)?;
    assert_eq!(next.nonce, 8);
    assert_eq!(lane.sent(), 2);
    Ok(())
}

#[test]
fn publication_unknown_to_the_sequencer_is_resent_as_its_exact_bytes() -> TestResult {
    let lane = Lane::new("unknown")?;
    let file = lane.rate_file("rate = 3200000\nset_at = 1899000000\n")?;
    lane.chain.borrow_mut().rung = "unknown";
    let mut publisher = lane.publisher_confirming_within(1)?;
    assert_eq!(
        publisher.publish(&file),
        Err(RateRefusal::Rpc(RpcFault::Rejected { code: -32000 }))
    );
    assert_eq!(lane.sent(), 1);
    assert!(publisher.journal().state().rate_broadcast.is_empty());
    lane.chain.borrow_mut().rung = "receipted";
    let publication = publisher.publish(&file)?;
    let sent = lane.chain.borrow().sent.clone();
    assert_eq!(sent.len(), 2);
    assert_eq!(sent[0], sent[1]);
    assert_eq!(publication.hash, keccak(&sent[0]));
    assert_eq!(publication.nonce, 7);
    let state = publisher.journal().state();
    assert_eq!(state.rate_transactions.len(), 1);
    assert!(state.rate_broadcast.contains(&publication.hash));
    assert_eq!(state.unsettled(), vec![publication]);
    Ok(())
}

#[test]
fn publisher_targets_only_the_paymaster_never_the_fee_token() -> TestResult {
    let lane = Lane::new("targets")?;
    let file = lane.rate_file("rate = 3114000\nset_at = 1899000000\n")?;
    let mut fee_token = config_json();
    fee_token["paymaster"] = json!(hex(&FEE_TOKEN));
    assert_eq!(
        PublisherConfig::parse(&fee_token.to_string()).err(),
        Some(ConfigError { field: "paymaster" })
    );
    lane.chain.borrow_mut().updated_at = NOW - 120;
    let mut publisher = lane.publisher(1_000_000)?;
    let publication = publisher.publish(&file)?;
    assert_eq!(publication.rate, word(3_114_000));
    let sent = lane.chain.borrow().sent.clone();
    assert_eq!(sent.len(), 1);
    for raw in &sent {
        let (to, rate, from) = published_rate(raw)?;
        assert_eq!((to, rate, from), (PAYMASTER, word(3_114_000), lane.owner));
        assert_ne!(to, FEE_TOKEN);
    }
    Ok(())
}

#[test]
fn real_signed_v2_publication_replays_exactly_and_refuses_identity_corruption() -> TestResult {
    use layerx_gas_station::journal::PublicationTransaction;
    use std::os::unix::fs::{MetadataExt as _, PermissionsExt as _};
    let input = PathBuf::from(
        std::env::var_os("PAXEER_X_RATE_AUTHENTICATED_TRANSACTION")
            .ok_or("genuine private signed publisher transaction required")?,
    );
    let metadata = std::fs::symlink_metadata(&input)?;
    if !input.is_absolute()
        || !metadata.is_file()
        || metadata.mode() & 0o077 != 0
        || metadata.nlink() != 1
        || metadata.len() > 16_384
        || metadata.uid() != std::fs::metadata("/proc/self")?.uid()
        || input.components().any(|component| {
            component
                .as_os_str()
                .to_str()
                .is_some_and(|part| part == ".env" || part.starts_with(".env."))
        })
    {
        return Err("protected genuine publisher transaction required".into());
    }
    let transaction: PublicationTransaction = serde_json::from_slice(&std::fs::read(input)?)?;
    let directory =
        std::env::temp_dir().join(format!("paxeer-rate-authenticated-{}", std::process::id()));
    std::fs::create_dir(&directory)?;
    std::fs::set_permissions(&directory, std::fs::Permissions::from_mode(0o700))?;
    let path = directory.join("rate.jsonl");
    let mut journal = Journal::open(&path)?;
    let entry = Entry::RatePrepared {
        transaction: transaction.clone(),
    };
    journal.append(&entry)?;
    let durable = std::fs::read(&path)?;
    journal.append(&entry)?;
    assert_eq!(std::fs::read(&path)?, durable);
    let state = journal.state().clone();
    for field in ["chain", "paymaster", "owner", "nonce", "rate", "fee", "raw"] {
        let mut changed = transaction.clone();
        match field {
            "chain" => changed.chain_id ^= 1,
            "paymaster" => changed.paymaster[0] ^= 1,
            "owner" => changed.publication.owner[0] ^= 1,
            "nonce" => changed.publication.nonce ^= 1,
            "rate" => changed.publication.rate[0] ^= 1,
            "fee" => changed.publication.max_fee_per_gas ^= 1,
            _ => changed.raw[0] ^= 1,
        }
        assert!(
            journal
                .append(&Entry::RatePrepared {
                    transaction: changed
                })
                .is_err(),
            "{field}"
        );
        assert_eq!(journal.state(), &state);
        assert_eq!(std::fs::read(&path)?, durable);
    }
    drop(journal);
    assert_eq!(Journal::open(&path)?.state(), &state);
    std::fs::remove_file(path)?;
    std::fs::remove_dir(directory)?;
    Ok(())
}

#[test]
fn receipt_signature_is_verified_against_pinned_receipt_keys() -> TestResult {
    let refused = Some(ConfigError {
        field: "sequencer_receipt_keys",
    });
    let mut identity = [0_u8; 32];
    identity[0] = 1;
    let mut above_prime = [0xff_u8; 32];
    above_prime[0] = 0xef;
    above_prime[31] = 0x7f;
    let pinned = receipt_key_entry(RECEIPT_KEY_ID, 1, 256);
    let with = |name: &str, value: Value| {
        let mut entry = pinned.clone();
        entry[name] = value;
        entry
    };
    for keys in [
        json!([]),
        json!({}),
        json!([pinned.clone(), receipt_key_entry(RECEIPT_KEY_ID, 300, 400)]),
        json!([receipt_key_entry(RECEIPT_KEY_ID, 257, 256)]),
        json!([receipt_key_entry([0; 32], 1, 256)]),
        json!([with("publicKey", json!(STANDARD.encode(identity)))]),
        json!([with("publicKey", json!(STANDARD.encode(above_prime)))]),
        json!([with("publicKey", json!(STANDARD.encode([0x42_u8; 31])))]),
        json!([with("keyId", json!(hex(&RECEIPT_KEY_ID)))]),
        json!([with("firstHeight", json!("01"))]),
        json!([with("lastHeight", json!(-1))]),
        json!([with("sequencerId", json!(STANDARD.encode(RECEIPT_KEY_ID)))]),
    ] {
        let mut value = config_json();
        value["sequencer_receipt_keys"] = keys.clone();
        assert_eq!(
            PublisherConfig::parse(&value.to_string()).err(),
            refused,
            "{keys}"
        );
    }
    let mut rotated = config_json();
    rotated["sequencer_receipt_keys"] = json!([
        receipt_key_entry([0x5b; 32], 1, 15),
        receipt_key_entry(RECEIPT_KEY_ID, 16, 256)
    ]);
    let keys = PublisherConfig::parse(&rotated.to_string())?.sequencer_receipt_keys;
    assert_eq!(
        keys.iter()
            .map(|key| (
                key.sequencer_id(),
                key.public_key(),
                key.first_batch_number(),
                key.last_batch_number()
            ))
            .collect::<Vec<_>>(),
        vec![
            ([0x5b; 32], receipt_key().verifying_key().to_bytes(), 1, 15),
            (
                RECEIPT_KEY_ID,
                receipt_key().verifying_key().to_bytes(),
                16,
                256
            )
        ]
    );

    let lane = Lane::new("receipt-keys")?;
    let file = lane.rate_file("rate = 3200000\nset_at = 1899000000\n")?;
    lane.chain.borrow_mut().rung = "receipted";
    let mut publisher = lane.publisher(1_000_000)?;
    for tamper in [
        Tamper::Signature,
        Tamper::UnpinnedKeyId,
        Tamper::HeightOutsideRange,
        Tamper::OtherActivity,
        Tamper::Missing,
    ] {
        lane.chain.borrow_mut().tamper = tamper;
        assert_eq!(
            publisher.publish(&file),
            Err(RateRefusal::Rpc(RpcFault::Malformed)),
            "{tamper:?}"
        );
        assert_eq!(lane.sent(), 1, "{tamper:?}");
        let hash = keccak(&lane.chain.borrow().sent[0]);
        let state = publisher.journal().state();
        assert_eq!(state.rate_active.get(&(lane.owner, 7)), Some(&hash));
        assert_eq!(state.unsettled().len(), 1);
        assert_eq!(state.publications[&hash].1, None);
    }
    lane.chain.borrow_mut().tamper = Tamper::None;
    let publication = publisher.publish(&file)?;
    assert_eq!(lane.sent(), 1);
    assert_eq!(
        (publication.nonce, publication.hash),
        (7, keccak(&lane.chain.borrow().sent[0]))
    );
    assert_eq!(publisher.journal().state().unsettled(), vec![publication]);
    Ok(())
}
