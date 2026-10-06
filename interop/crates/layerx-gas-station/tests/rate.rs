use std::cell::RefCell;
use std::path::PathBuf;
use std::rc::Rc;
use std::time::Duration;

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
use serde_json::{json, Value};

type TestResult = Result<(), Box<dyn std::error::Error>>;

const PAYMASTER: Address = [0x44; 20];
const NOW: u64 = 1_900_000_000;

/// The paymaster and the owner account as the node reports them.
struct Chain {
    rate: u128,
    updated_at: u64,
    owner: Address,
    balance: u128,
    nonce: u64,
    status: u64,
    gas_used: u64,
    effective_gas_price: u128,
    sent: Vec<Vec<u8>>,
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
        let quantity = |v: u128| Value::String(format!("{v:#x}"));
        Ok(match method {
            "eth_chainId" => quantity(1325),
            "eth_getBlockByNumber" => {
                assert_eq!(params, &json!(["latest", false]));
                json!({"timestamp": quantity(NOW.into()), "baseFeePerGas": quantity(1_000_000_000)})
            }
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
            "eth_getTransactionCount" => quantity(chain.nonce.into()),
            "eth_sendRawTransaction" => {
                let raw = bytes(params[0].as_str().ok_or(RpcFault::Malformed)?)?;
                chain.sent.push(raw.clone());
                Value::String(hex(&keccak(&raw)))
            }
            "eth_getTransactionReceipt" => {
                let Some(raw) = chain.sent.last() else {
                    return Ok(Value::Null);
                };
                let hash = keccak(raw);
                assert_eq!(params[0], hex(&hash));
                json!({"transactionHash": hex(&hash), "from": hex(&chain.owner), "to": hex(&PAYMASTER),
                    "blockNumber": "0x10", "gasUsed": quantity(chain.gas_used.into()),
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
                status: 1,
                gas_used: 35_000,
                effective_gas_price: 1_500_000_000,
                sent: Vec::new(),
            })),
            owner,
            key,
        })
    }
    fn rate_file(&self, text: &str) -> Result<PathBuf, std::io::Error> {
        let path = self.dir.join("rate.toml");
        std::fs::write(&path, text)?;
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
        let config = PublisherConfig::parse(&value.to_string())?;
        let rpc = ConfiguredRpc::new(&config.station, ChainExchange(self.chain.clone()))?;
        let journal = Journal::open(&self.dir.join("rate.jsonl"))?;
        Ok(RatePublisher::new(
            config,
            signer(&self.key)?,
            rpc,
            journal,
            Duration::ZERO,
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
        "rate_confirmation_retry_seconds":60})
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
    assert!(matches!(entries[0], Entry::RatePublished { .. }));
    assert!(matches!(entries[1], Entry::RateSettled { .. }));
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
        let path = lane.dir.join("rate.toml");
        match text {
            Some(text) => std::fs::write(&path, text)?,
            None => {
                let _ = std::fs::remove_file(&path);
            }
        }
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
fn cadence_must_stay_below_max_rate_age() {
    for (name, value) in [
        ("rate_cadence_seconds", json!(300)),
        ("rate_cadence_seconds", json!(301)),
        ("rate_cadence_seconds", json!(0)),
        ("rate_owner_key_env", json!("GAS_STATION_RELAYER_KEY")),
        ("rate_owner_key_env", json!("sensitive-value")),
        ("rate_gas_budget_per_day", json!(RATE_GAS_LIMIT - 1)),
        ("rate_balance_floor", json!(0)),
        ("rate_max_fee_per_gas", json!(1_000_000_000)),
    ] {
        let mut value_map = config_json();
        value_map[name] = value;
        assert_eq!(
            PublisherConfig::parse(&value_map.to_string()).err(),
            Some(ConfigError { field: name })
        );
    }
    let mut bounded = config_json();
    bounded["rate_cadence_seconds"] = json!(299);
    assert!(PublisherConfig::parse(&bounded.to_string()).is_ok());
    bounded["max_rate_age"] = json!(120);
    bounded["rate_cadence_seconds"] = json!(120);
    assert_eq!(
        PublisherConfig::parse(&bounded.to_string()).err(),
        Some(ConfigError {
            field: "rate_cadence_seconds"
        })
    );
    let Value::Object(fields) = config_json() else {
        panic!("object required")
    };
    for name in fields.keys() {
        let mut incomplete = fields.clone();
        incomplete.remove(name);
        let result = PublisherConfig::parse(&Value::Object(incomplete).to_string());
        assert_eq!(result.err().map(|e| e.field.to_owned()), Some(name.clone()));
    }
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
    let pending = Publication {
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
        publication: pending,
    })?;
    assert_eq!(journal.state().reserved_wei(), 4_000_000_000 * 60_000);
    drop(journal);
    let floor = 10_u128.pow(15);
    let needed = floor + RESERVE + 4_000_000_000 * 60_000;
    lane.chain.borrow_mut().balance = needed - 1;
    let mut publisher = lane.publisher(1_000_000)?;
    assert_eq!(publisher.publish(&file), Err(RateRefusal::BelowFloor));
    assert_eq!(lane.sent(), 0);
    lane.chain.borrow_mut().balance = needed;
    lane.chain.borrow_mut().nonce = 8;
    let publication = publisher.publish(&file)?;
    assert_eq!(publication.nonce, 8);
    let state = publisher.journal().state();
    assert!(state.unsettled().is_empty());
    assert_eq!(state.reserved_wei(), 0);
    assert_eq!(state.publications[&pending.hash].1, None);
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
