use std::cell::{Cell, RefCell};
use std::collections::BTreeMap;
use std::path::PathBuf;
use std::rc::Rc;

use k256::ecdsa::SigningKey;
use k256::elliptic_curve::rand_core::OsRng;
use layerx_gas_station::config::StationConfig;
use layerx_gas_station::journal::{Completion, Entry, Journal, Key};
use layerx_gas_station::price::{PaymasterRateSource, PriceError};
use layerx_gas_station::quote::{address_word, keccak, word, Address, Word, SIDIORA};
use layerx_gas_station::rpc::{
    bytes, hex, quantity, ConfiguredRpc, Exchange, HttpsExchange, JsonRpc, RpcFault,
};
use layerx_gas_station::signer::{LocalSigner, QuoteSigner, SignerError};
use layerx_gas_station::station::{
    GasStation, Progress, QuoteOutcome, StationError, SubmitRequest,
};
use layerx_gas_station::tx::{
    authorization_digest, batch_digest, Authorization, Call, Fees, CANCELLATION_GAS,
};
use layerx_gas_station::{QuoteError, QuoteRequest};
use serde_json::{json, Value};

type TestResult = Result<(), Box<dyn std::error::Error>>;

const AMOUNT: u128 = 3_145_140;

struct CountedSigner {
    inner: LocalSigner,
    count: Rc<Cell<usize>>,
}
impl QuoteSigner for CountedSigner {
    fn address(&self) -> Address {
        self.inner.address()
    }
    fn sign_digest(&self, digest: Word) -> Result<[u8; 65], SignerError> {
        self.count.set(self.count.get() + 1);
        self.inner.sign_digest(digest)
    }
}
struct Recording {
    fixture: Value,
    phase: RefCell<String>,
    bindings: RefCell<BTreeMap<String, Value>>,
    sent: RefCell<Vec<Vec<u8>>>,
    journal: PathBuf,
}
fn substitute(value: &Value, bindings: &BTreeMap<String, Value>) -> Value {
    match value {
        Value::String(s) if s.starts_with('$') => bindings
            .get(s)
            .cloned()
            .unwrap_or_else(|| panic!("unbound fixture field")),
        Value::Array(values) => {
            Value::Array(values.iter().map(|v| substitute(v, bindings)).collect())
        }
        Value::Object(values) => Value::Object(
            values
                .iter()
                .map(|(k, v)| (k.clone(), substitute(v, bindings)))
                .collect(),
        ),
        _ => value.clone(),
    }
}
struct ReplayExchange(Rc<Recording>);
impl Exchange for ReplayExchange {
    fn request(&self, _endpoint: &str, method: &str, params: &Value) -> Result<Value, RpcFault> {
        if method == "eth_sendRawTransaction" {
            let raw = bytes(params[0].as_str().ok_or(RpcFault::Malformed)?)?;
            let hash = keccak(&raw);
            let journal =
                std::fs::read_to_string(&self.0.journal).map_err(|_| RpcFault::Unavailable)?;
            let entries = journal
                .lines()
                .map(serde_json::from_str::<Entry>)
                .collect::<Result<Vec<_>, _>>()
                .map_err(|_| RpcFault::Malformed)?;
            assert!(entries.iter().any(|entry| match entry {
                Entry::Prepared { submission, .. } =>
                    submission.raw == raw && submission.hash == hash,
                Entry::Replaced { replacement, .. } =>
                    replacement.raw == raw && replacement.hash == hash,
                _ => false,
            }));
            assert!(journal.ends_with('\n'));
            let (raw_name, hash_name) = if raw.first() == Some(&2) {
                ("$replacement_raw", "$replacement_hash")
            } else {
                ("$raw", "$hash")
            };
            self.0
                .bindings
                .borrow_mut()
                .insert(raw_name.into(), json!(hex(&raw)));
            self.0
                .bindings
                .borrow_mut()
                .insert(hash_name.into(), json!(hex(&hash)));
            self.0.sent.borrow_mut().push(raw);
        }
        for phase in [self.0.phase.borrow().as_str(), "*"] {
            let rules = self.0.fixture["phases"][phase]
                .as_array()
                .ok_or(RpcFault::Malformed)?;
            for rule in rules {
                if rule["method"] == method
                    && substitute(&rule["params"], &self.0.bindings.borrow()) == *params
                {
                    if rule["fault"] == "unavailable" {
                        return Err(RpcFault::Unavailable);
                    }
                    if let Some(code) = rule["error"]["code"].as_i64() {
                        return Err(RpcFault::Rejected { code });
                    }
                    return Ok(substitute(&rule["result"], &self.0.bindings.borrow()));
                }
            }
        }
        panic!("unmatched recorded exchange: {method}")
    }
}
fn config() -> StationConfig {
    StationConfig {
        chain_id: 1325,
        endpoints: vec![format!("https://{}", std::process::id())],
        paymaster: [0x44; 20],
        token: SIDIORA,
        decimals: 6,
        max_rate_age: 300,
        spread_bps: 500,
        margin_bps: 100,
        per_account_limit: 8_000_000,
        per_interval_limit: 16_000_000,
        per_quote_limit: 4_000_000,
        interval_seconds: 60,
        balance_floor: 100,
        relayer_key_env: "PAXEER_STATION_TEST_KEY".into(),
    }
}
fn install_ephemeral_signer(config: &StationConfig) -> Result<LocalSigner, SignerError> {
    let key = SigningKey::random(&mut OsRng);
    std::env::set_var(&config.relayer_key_env, hex(&key.to_bytes()));
    let signer = LocalSigner::from_config(config);
    std::env::remove_var(&config.relayer_key_env);
    signer
}
fn recording(
    path: PathBuf,
    account: Address,
    sponsor: Address,
) -> Result<Rc<Recording>, Box<dyn std::error::Error>> {
    let fixture: Value = serde_json::from_str(include_str!("fixtures/cycle.json"))?;
    let consumed = [
        keccak(b"usedQuoteNonces(address,uint256)")[..4].to_vec(),
        address_word(sponsor).to_vec(),
        word(7).to_vec(),
    ]
    .concat();
    let bindings = BTreeMap::from([
        ("$sponsor", hex(&sponsor)),
        ("$paymaster", hex(&[0x44; 20])),
        ("$current_rate_call", hex(&keccak(b"currentRate()")[..4])),
        ("$rate_updated_call", hex(&keccak(b"rateUpdatedAt()")[..4])),
        ("$account", hex(&account)),
        ("$token", hex(&SIDIORA)),
        ("$zero", hex(&word(0))),
        ("$one", hex(&word(1))),
        ("$batch_nonce_call", hex(&keccak(b"nonce()")[..4])),
        ("$consumed_call", hex(&consumed)),
        ("$block", hex(&[0x10; 32])),
        ("$cancel_block", hex(&[0x11; 32])),
        ("$finalized", hex(&[0x12; 32])),
        ("$account_word", hex(&address_word(account))),
        ("$sponsor_word", hex(&address_word(sponsor))),
        ("$token_word", hex(&address_word(SIDIORA))),
        ("$amount", hex(&word(AMOUNT))),
        (
            "$transfer_topic",
            hex(&keccak(b"Transfer(address,address,uint256)")),
        ),
        (
            "$sponsored_topic",
            hex(&keccak(b"Sponsored(address,address,uint256,uint256)")),
        ),
        ("$sponsored_data", hex(&[word(AMOUNT), word(7)].concat())),
        (
            "$balance_call",
            hex(&[
                keccak(b"balanceOf(address)")[..4].to_vec(),
                address_word(sponsor).to_vec(),
            ]
            .concat()),
        ),
    ])
    .into_iter()
    .map(|(k, v)| (k.into(), json!(v)))
    .collect();
    Ok(Rc::new(Recording {
        fixture,
        phase: RefCell::new("submit".into()),
        bindings: RefCell::new(bindings),
        sent: RefCell::new(vec![]),
        journal: path,
    }))
}
type TestStation = GasStation<
    SharedSigner,
    ConfiguredRpc<ReplayExchange>,
    PaymasterRateSource<ConfiguredRpc<ReplayExchange>>,
>;
fn prepare_submission(
    station: &mut TestStation,
    config: &StationConfig,
    account_signer: &LocalSigner,
    signer: &CountedSigner,
    recorded: &Recording,
) -> Result<SubmitRequest, Box<dyn std::error::Error>> {
    let account = account_signer.address();
    let sponsor = signer.address();
    let fees = Fees {
        gas_limit: 200_000,
        max_fee_per_gas: 5_000_000_000_000,
        max_priority_fee_per_gas: 1_000_000_000,
    };
    let request = QuoteRequest {
        account,
        max_token_amount: 3_200_000,
        gas_cost: fees.gas_cost()?,
        deadline: 1019,
        quote_nonce: word(7),
    };
    let QuoteOutcome::Signed(quoted) = station.quote(&request, fees, 1000)? else {
        panic!("expected quote")
    };
    assert_eq!(quoted.quote.token_amount, word(AMOUNT));
    assert_eq!(signer.count.get(), 1);
    assert!(matches!(
        station.quote(&request, fees, 1000)?,
        QuoteOutcome::Signed(_)
    ));
    assert_eq!(signer.count.get(), 1);
    let calls = vec![Call {
        to: [0x55; 20],
        value: word(0),
        data: vec![1, 2, 3],
    }];
    let mut auth = Authorization {
        chain_id: config.chain_id,
        delegate: config.paymaster,
        nonce: 0,
        signature: [0; 65],
    };
    auth.signature = account_signer.sign_digest(authorization_digest(&auth)?)?;
    let signature = account_signer.sign_digest(batch_digest(
        config.chain_id,
        account,
        word(0),
        &calls,
        &quoted.quote,
    )?)?;
    let key = Key {
        sponsor,
        quote_nonce: word(7),
    };
    let submit = SubmitRequest {
        key,
        account,
        calls,
        authorizations: vec![auth],
        account_signature: signature,
    };
    assert!(recorded.sent.borrow().is_empty());
    Ok(submit)
}
fn assert_balances(
    config: &StationConfig,
    recorded: &Rc<Recording>,
    sponsor: Address,
) -> TestResult {
    let rpc = ConfiguredRpc::new(config, ReplayExchange(Rc::clone(recorded)))?;
    let before = quantity("0x3782dace9d900000")?;
    let after = rpc.call("eth_getBalance", json!([hex(&sponsor), "latest"]))?;
    let after = quantity(after.as_str().ok_or(RpcFault::Malformed)?)?;
    assert_eq!(before - after, 500_000_000_000_000_000);
    let data = recorded.bindings.borrow()["$balance_call"].clone();
    let balance = rpc.call(
        "eth_call",
        json!([{"to":hex(&SIDIORA),"data":data},"latest"]),
    )?;
    assert_eq!(balance, hex(&word(AMOUNT)));
    Ok(())
}
fn open_station(
    config: &StationConfig,
    signer: &Rc<CountedSigner>,
    recorded: &Rc<Recording>,
    path: &std::path::Path,
) -> Result<TestStation, StationError> {
    GasStation::new(
        config.clone(),
        SharedSigner(Rc::clone(signer)),
        ConfiguredRpc::new(config, ReplayExchange(Rc::clone(recorded)))?,
        PaymasterRateSource::new(
            ConfiguredRpc::new(config, ReplayExchange(Rc::clone(recorded)))?,
            config.paymaster,
        ),
        Journal::open(path)?,
    )
}
fn full_cycle() -> TestResult {
    let config = config();
    let inner = install_ephemeral_signer(&config)?;
    let account_signer = install_ephemeral_signer(&config)?;
    let account = account_signer.address();
    let sponsor = inner.address();
    let count = Rc::new(Cell::new(0));
    let signer = Rc::new(CountedSigner {
        inner,
        count: Rc::clone(&count),
    });
    let path = std::env::temp_dir().join(format!("paxeer-cycle-{}.jsonl", std::process::id()));
    if path.exists() {
        std::fs::remove_file(&path)?;
    }
    let recorded = recording(path.clone(), account, sponsor)?;
    let start = || open_station(&config, &signer, &recorded, &path);
    let mut station = start()?;
    let mut submit =
        prepare_submission(&mut station, &config, &account_signer, &signer, &recorded)?;
    let key = submit.key;
    submit.account_signature[0] ^= 1;
    assert!(station.submit(&submit, 1000).is_err());
    assert_eq!(count.get(), 1);
    assert!(recorded.sent.borrow().is_empty());
    submit.account_signature[0] ^= 1;
    assert_eq!(station.submit(&submit, 1000)?, Progress::Pending);
    assert_eq!(count.get(), 2);
    assert_eq!(recorded.sent.borrow().len(), 1);
    assert_envelope(&recorded.sent.borrow()[0], sponsor, account)?;
    drop(station);
    *recorded.phase.borrow_mut() = "restart".into();
    let mut station = start()?;
    assert_eq!(station.resume(key, 1000)?, Progress::Pending);
    assert_eq!(count.get(), 2);
    assert_eq!(recorded.sent.borrow()[0], recorded.sent.borrow()[1]);
    drop(station);
    for phase in ["already_known", "nonce_too_low"] {
        *recorded.phase.borrow_mut() = phase.into();
        let mut station = start()?;
        assert_eq!(station.resume(key, 1000)?, Progress::Pending);
        assert!(station.journal().state().items[&key].completion.is_none());
        assert_eq!(count.get(), 2);
        assert!(recorded
            .sent
            .borrow()
            .iter()
            .all(|raw| raw == &recorded.sent.borrow()[0]));
    }
    *recorded.phase.borrow_mut() = "included".into();
    let mut station = start()?;
    let expected = Completion::Included {
        hash: keccak(&recorded.sent.borrow()[0]),
        block_number: 16,
        sid_collected: word(AMOUNT),
        pax_spent: word(500_000_000_000_000_000),
    };
    assert_eq!(station.resume(key, 1000)?, Progress::Completed(expected));
    assert_eq!(station.resume(key, 1000)?, Progress::Completed(expected));
    assert_eq!(count.get(), 2);
    assert_eq!(recorded.sent.borrow().len(), 4);
    assert_balances(&config, &recorded, sponsor)?;
    drop(station);
    let recovered = Journal::open(&path)?;
    assert_eq!(recovered.state().items[&key].completion, Some(expected));
    drop(recovered);
    std::fs::remove_file(&path)?;
    *recorded.phase.borrow_mut() = "consumed".into();
    let mut station = start()?;
    assert_eq!(
        station.submit(&submit, 1000)?,
        Progress::Completed(Completion::Consumed)
    );
    assert_eq!(
        station.journal().state().items[&key].completion,
        Some(Completion::Consumed)
    );
    assert_eq!(count.get(), 2);
    assert_eq!(recorded.sent.borrow().len(), 4);
    drop(station);
    std::fs::remove_file(&path)?;
    Ok(())
}
struct SharedSigner(Rc<CountedSigner>);
impl QuoteSigner for SharedSigner {
    fn address(&self) -> Address {
        self.0.address()
    }
    fn sign_digest(&self, digest: Word) -> Result<[u8; 65], SignerError> {
        self.0.sign_digest(digest)
    }
}
#[test]
fn quote_submit_collect_restart_and_consumed_nonce() -> TestResult {
    full_cycle()
}

struct Lane {
    config: StationConfig,
    account_signer: LocalSigner,
    signer: Rc<CountedSigner>,
    count: Rc<Cell<usize>>,
    recorded: Rc<Recording>,
    path: PathBuf,
}
impl Lane {
    fn new(name: &str) -> Result<Self, Box<dyn std::error::Error>> {
        let mut config = config();
        config.relayer_key_env = format!("PAXEER_STATION_{name}_KEY");
        let inner = install_ephemeral_signer(&config)?;
        let account_signer = install_ephemeral_signer(&config)?;
        let count = Rc::new(Cell::new(0));
        let path = std::env::temp_dir().join(format!(
            "paxeer-{}-{}.jsonl",
            name.to_ascii_lowercase(),
            std::process::id()
        ));
        if path.exists() {
            std::fs::remove_file(&path)?;
        }
        let recorded = recording(path.clone(), account_signer.address(), inner.address())?;
        Ok(Self {
            config,
            account_signer,
            signer: Rc::new(CountedSigner {
                inner,
                count: Rc::clone(&count),
            }),
            count,
            recorded,
            path,
        })
    }
    fn phase(&self, phase: &str) {
        *self.recorded.phase.borrow_mut() = phase.into();
    }
    fn start(&self) -> Result<TestStation, StationError> {
        open_station(&self.config, &self.signer, &self.recorded, &self.path)
    }
    fn submitted(&self) -> Result<(TestStation, SubmitRequest), Box<dyn std::error::Error>> {
        let mut station = self.start()?;
        let submit = prepare_submission(
            &mut station,
            &self.config,
            &self.account_signer,
            &self.signer,
            &self.recorded,
        )?;
        Ok((station, submit))
    }
    fn entries(&self) -> Result<Vec<Entry>, Box<dyn std::error::Error>> {
        Ok(std::fs::read_to_string(&self.path)?
            .lines()
            .map(serde_json::from_str::<Entry>)
            .collect::<Result<Vec<_>, _>>()?)
    }
    fn prepared_nonces(&self) -> Result<Vec<u64>, Box<dyn std::error::Error>> {
        Ok(self
            .entries()?
            .iter()
            .filter_map(|entry| match entry {
                Entry::Prepared { submission, .. } => Some(submission.nonce),
                _ => None,
            })
            .collect())
    }
    fn sent_originals(&self) -> usize {
        self.recorded
            .sent
            .borrow()
            .iter()
            .filter(|raw| raw.first() == Some(&4))
            .count()
    }
    fn finish(self) -> TestResult {
        std::fs::remove_file(&self.path)?;
        Ok(())
    }
}

/// Replacement lifecycle shared by the dropped and expired submissions: the
/// replacement is journalled with the bumped fee before broadcast, a restart
/// rebroadcasts its durable bytes without signing, and its finalized receipt
/// cancels the quote.
fn replacement_resumes_and_cancels(lane: &Lane, key: Key, now: u64) -> TestResult {
    let replacements: Vec<_> = lane
        .entries()?
        .into_iter()
        .filter_map(|entry| match entry {
            Entry::Replaced {
                key: k,
                replacement,
            } if k == key => Some(replacement),
            _ => None,
        })
        .collect();
    assert_eq!(replacements.len(), 1);
    let replacement = &replacements[0];
    assert_eq!(replacement.nonce, 5);
    assert_eq!(
        replacement.fees,
        Fees {
            gas_limit: CANCELLATION_GAS,
            max_fee_per_gas: 5_500_000_000_000,
            max_priority_fee_per_gas: 1_100_000_000,
        }
    );
    assert_eq!(lane.count.get(), 3);
    assert_eq!(lane.recorded.sent.borrow().last(), Some(&replacement.raw));
    let originals = lane.sent_originals();
    lane.phase("replacement_pending");
    let mut station = lane.start()?;
    assert_eq!(station.resume(key, now)?, Progress::Pending);
    assert_eq!(lane.count.get(), 3);
    assert_eq!(lane.sent_originals(), originals);
    assert_eq!(lane.recorded.sent.borrow().last(), Some(&replacement.raw));
    drop(station);
    lane.phase("cancelled");
    let mut station = lane.start()?;
    let cancelled = Completion::Cancelled {
        hash: replacement.hash,
        block_number: 17,
    };
    assert_eq!(station.resume(key, now)?, Progress::Completed(cancelled));
    drop(station);
    let mut station = lane.start()?;
    assert_eq!(station.resume(key, now)?, Progress::Completed(cancelled));
    assert_eq!(
        station.journal().state().items[&key].completion,
        Some(cancelled)
    );
    assert_eq!(lane.count.get(), 3);
    assert_eq!(lane.sent_originals(), originals);
    Ok(())
}

#[test]
fn refused_submission_retains_exact_bytes_until_finalized_receipt() -> TestResult {
    let lane = Lane::new("REFUSED")?;
    let (mut station, submit) = lane.submitted()?;
    let key = submit.key;
    lane.phase("refused");
    assert_eq!(station.submit(&submit, 1000)?, Progress::Pending);
    let prepared = station.journal().state().items[&key].submission.clone()
        .ok_or("refused broadcast lost durable transaction")?;
    assert_eq!(lane.count.get(), 2);
    assert!(station.journal().state().items[&key].completion.is_none());
    assert!(station.journal().state().holds(key.sponsor, prepared.nonce));
    assert!(!lane.entries()?.iter().any(|entry| matches!(entry, Entry::Released { .. })));
    drop(station);
    lane.phase("restart");
    let mut station = lane.start()?;
    assert_eq!(station.journal().state().items[&key].submission.as_ref(), Some(&prepared));
    let signature: [u8; 65] = station.journal().state().items[&key].quote.as_ref()
        .ok_or("quote missing")?.signature.as_slice().try_into()?;
    let started = std::time::Instant::now();
    assert!(station.status(key, submit.account, &signature)?.completion.is_none());
    assert_eq!(station.resume(key, 1000)?, Progress::Pending);
    assert!(started.elapsed() < std::time::Duration::from_secs(21));
    assert_eq!(lane.recorded.sent.borrow().last(), Some(&prepared.raw));
    assert_eq!(lane.prepared_nonces()?, vec![prepared.nonce]);
    assert_eq!(lane.count.get(), 2);
    drop(station);
    lane.phase("included");
    let mut station = lane.start()?;
    assert!(station.status(key, submit.account, &signature)?.completion.is_none());
    let done = station.resume(key, 1020)?;
    assert!(matches!(done, Progress::Completed(Completion::Included { hash, .. }) if hash == prepared.hash));
    assert_eq!(station.resume(key, 1020)?, done);
    assert!(station.status(key, submit.account, &signature)?.completion.is_some());
    assert!(station.journal().state().receipts.contains_key(&prepared.hash));
    assert_eq!(lane.count.get(), 2);
    drop(station);
    lane.finish()
}

#[test]
fn dropped_submission_is_replaced_at_its_nonce() -> TestResult {
    let lane = Lane::new("DROPPED")?;
    let (mut station, submit) = lane.submitted()?;
    let key = submit.key;
    assert_eq!(station.submit(&submit, 1000)?, Progress::Pending);
    drop(station);
    lane.phase("dropped");
    let mut station = lane.start()?;
    assert_eq!(station.resume(key, 1000)?, Progress::Pending);
    assert_eq!(lane.sent_originals(), 2);
    assert_eq!(
        station.journal().state().items[&key]
            .replacement
            .as_ref()
            .map(|r| r.nonce),
        Some(5)
    );
    drop(station);
    replacement_resumes_and_cancels(&lane, key, 1000)?;
    lane.finish()
}

#[test]
fn expired_quote_is_never_rebroadcast_and_its_nonce_is_cancelled() -> TestResult {
    let lane = Lane::new("EXPIRED")?;
    let (mut station, submit) = lane.submitted()?;
    let key = submit.key;
    assert_eq!(station.submit(&submit, 1000)?, Progress::Pending);
    assert_eq!(lane.sent_originals(), 1);
    drop(station);
    lane.phase("expired");
    let mut station = lane.start()?;
    assert_eq!(station.submit(&submit, 1020)?, Progress::Pending);
    assert_eq!(lane.sent_originals(), 1);
    assert!(station.journal().state().items[&key].replacement.is_some());
    drop(station);
    replacement_resumes_and_cancels(&lane, key, 1020)?;
    lane.finish()
}

#[test]
fn quote_refused_when_governed_rate_is_stale_or_missing() -> TestResult {
    let mut config = config();
    config.relayer_key_env = "PAXEER_STATION_RATE_TEST_KEY".into();
    let inner = install_ephemeral_signer(&config)?;
    let account = install_ephemeral_signer(&config)?.address();
    let sponsor = inner.address();
    let count = Rc::new(Cell::new(0));
    let signer = Rc::new(CountedSigner {
        inner,
        count: Rc::clone(&count),
    });
    let path = std::env::temp_dir().join(format!("paxeer-rate-{}.jsonl", std::process::id()));
    if path.exists() {
        std::fs::remove_file(&path)?;
    }
    let recorded = recording(path.clone(), account, sponsor)?;
    let mut station = open_station(&config, &signer, &recorded, &path)?;
    let fees = Fees {
        gas_limit: 200_000,
        max_fee_per_gas: 5_000_000_000_000,
        max_priority_fee_per_gas: 1_000_000_000,
    };
    let request = QuoteRequest {
        account,
        max_token_amount: 3_200_000,
        gas_cost: fees.gas_cost()?,
        deadline: 1019,
        quote_nonce: word(7),
    };
    for (phase, source_refusal, pricing_refusal) in [
        ("stale_rate", Some(PriceError::StaleRate), None),
        ("aged_rate", None, Some(PriceError::StaleRate)),
        ("missing_rate", Some(PriceError::MissingRate), None),
    ] {
        *recorded.phase.borrow_mut() = phase.into();
        let refusal = match station.quote(&request, fees, 1000) {
            Err(StationError::Price(refusal)) => (Some(refusal), None),
            Err(StationError::Quote(QuoteError::Price(refusal))) => (None, Some(refusal)),
            _ => (None, None),
        };
        assert_eq!(refusal, (source_refusal, pricing_refusal));
        assert!(station.journal().state().items.is_empty());
        assert_eq!(count.get(), 0);
    }
    drop(station);
    std::fs::remove_file(&path)?;
    Ok(())
}

#[test]
fn response_reads_bound_total_time_and_size() -> TestResult {
    use std::io::{Cursor, Write as _};
    use std::os::unix::net::UnixStream;
    use std::time::{Duration, Instant};

    let budget = Duration::from_millis(300);
    let (mut reader, mut writer) = UnixStream::pair()?;
    reader.set_nonblocking(true)?;
    let sending = std::thread::spawn(move || {
        let mut sent = 0;
        while writer.write_all(b"x").is_ok() {
            sent += 1;
            std::thread::sleep(Duration::from_millis(20));
        }
        sent
    });
    let started = Instant::now();
    assert_eq!(
        HttpsExchange::read_response(&mut reader, budget),
        Err(RpcFault::Unavailable)
    );
    assert!(started.elapsed() >= budget);
    assert!(started.elapsed() < Duration::from_secs(2));
    drop(reader);
    assert!(sending.join().map_err(|_| "response writer panicked")? > 1);

    let body = json!({"jsonrpc":"2.0","id":1,"result":"0x5"}).to_string();
    let response = format!(
        "HTTP/1.1 200 OK\r\nContent-Length: {}\r\n\r\n{body}",
        body.len()
    );
    assert_eq!(
        HttpsExchange::read_response(&mut Cursor::new(response.as_bytes()), budget)?,
        json!("0x5")
    );
    assert_eq!(
        HttpsExchange::read_response(&mut Cursor::new(response.as_bytes()), Duration::ZERO),
        Err(RpcFault::Unavailable)
    );
    assert_eq!(
        HttpsExchange::read_response(&mut Cursor::new(vec![b'x'; 1_048_577]), budget),
        Err(RpcFault::Malformed)
    );
    let (mut reader, _writer) = UnixStream::pair()?;
    reader.set_nonblocking(true)?;
    assert_eq!(
        HttpsExchange::read_response(&mut reader, budget),
        Err(RpcFault::Unavailable)
    );
    Ok(())
}

type RlpParts<'a> = (&'a [u8], &'a [u8]);
fn rlp_item(input: &[u8]) -> Result<RlpParts<'_>, Box<dyn std::error::Error>> {
    let first = *input.first().ok_or("empty RLP")?;
    let (offset, length) = match first {
        0..=127 => (0, 1),
        128..=183 => (1, usize::from(first - 128)),
        184..=191 | 248..=255 => {
            let count = usize::from(if first < 192 {
                first - 183
            } else {
                first - 247
            });
            let mut length = 0_usize;
            for byte in input.get(1..=count).ok_or("short length")? {
                length = length
                    .checked_mul(256)
                    .and_then(|n| n.checked_add(usize::from(*byte)))
                    .ok_or("length overflow")?;
            }
            (1 + count, length)
        }
        192..=247 => (1, usize::from(first - 192)),
    };
    let end = offset + length;
    Ok((
        input.get(offset..end).ok_or("short payload")?,
        input.get(end..).ok_or("short tail")?,
    ))
}
fn assert_envelope(raw: &[u8], sponsor: Address, account: Address) -> TestResult {
    assert_eq!(raw[0], 4);
    let (mut payload, rest) = rlp_item(&raw[1..])?;
    assert!(rest.is_empty());
    let mut fields = Vec::new();
    let mut signing_length = 0;
    while !payload.is_empty() {
        let before = payload.len();
        let (field, tail) = rlp_item(payload)?;
        if fields.len() < 10 {
            signing_length += before - tail.len();
        }
        fields.push(field);
        payload = tail;
    }
    assert_eq!(fields.len(), 13);
    assert_eq!(fields[0], [5, 45]);
    assert_eq!(fields[1], [5]);
    assert_eq!(fields[5], account);
    assert!(fields[6].is_empty());
    assert!(fields[8].is_empty());
    assert_eq!(&fields[7][..4], &keccak(b"executeSponsored((address,uint256,bytes)[],(address,address,uint256,uint256,uint256,uint256,uint256),bytes,bytes)")[..4]);
    let (authorization, rest) = rlp_item(fields[9])?;
    assert!(rest.is_empty());
    let (chain, tail) = rlp_item(authorization)?;
    assert_eq!(chain, [5, 45]);
    let (delegate, _) = rlp_item(tail)?;
    assert_eq!(delegate, [0x44; 20]);
    let (payload, _) = rlp_item(&raw[1..])?;
    let length_bytes = signing_length.to_be_bytes();
    let count = length_bytes.iter().skip_while(|b| **b == 0).count();
    let mut unsigned = vec![4, 247 + u8::try_from(count)?];
    unsigned.extend_from_slice(&length_bytes[length_bytes.len() - count..]);
    unsigned.extend_from_slice(&payload[..signing_length]);
    let mut signature = [0_u8; 65];
    assert!(fields[11].len() <= 32 && fields[12].len() <= 32);
    signature[32 - fields[11].len()..32].copy_from_slice(fields[11]);
    signature[64 - fields[12].len()..64].copy_from_slice(fields[12]);
    signature[64] = 27 + fields[10].first().copied().unwrap_or(0);
    assert_eq!(
        layerx_gas_station::tx::recover(keccak(&unsigned), &signature)?,
        sponsor
    );
    Ok(())
}

#[test]
fn recovery_deadline_bounds_real_stalled_tls() -> TestResult {
    use std::io::Read as _;
    use std::net::TcpListener;
    use std::time::{Duration, Instant};

    let listener = TcpListener::bind("127.0.0.1:0")?;
    let address = listener.local_addr()?;
    let peer = std::thread::spawn(move || -> std::io::Result<()> {
        listener.set_nonblocking(true)?;
        let until = Instant::now() + Duration::from_secs(1);
        let mut stream = loop {
            match listener.accept() {
                Ok((stream, _)) => break stream,
                Err(error) if error.kind() == std::io::ErrorKind::WouldBlock && Instant::now() < until => {
                    std::thread::sleep(Duration::from_millis(5));
                }
                Err(error) => return Err(error),
            }
        };
        stream.set_read_timeout(Some(Duration::from_secs(1)))?;
        let mut bytes = [0_u8; 4096];
        while stream.read(&mut bytes)? != 0 {}
        Ok(())
    });
    let mut configuration = config();
    configuration.endpoints = vec![format!("https://{address}")];
    let rpc = ConfiguredRpc::new(&configuration, HttpsExchange)?;
    let started = Instant::now();
    rpc.set_deadline(Some(started + Duration::from_millis(150)))?;
    assert_eq!(rpc.call("eth_chainId", json!([])), Err(RpcFault::Unavailable));
    assert!(started.elapsed() < Duration::from_secs(2));
    rpc.set_deadline(None)?;
    peer.join().map_err(|_| "TLS peer panicked")??;
    Ok(())
}

#[test]
fn authenticated_retry_real_https_retains_unknown_liability_within_deadline() -> TestResult {
    use std::io::{Read as _, Write as _};
    use std::os::unix::fs::{OpenOptionsExt as _, PermissionsExt as _};
    use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

    let material_path = PathBuf::from(std::env::var("PAXEER_X_STATION_RECOVERY_MATERIAL")?);
    let read_private = |path: &std::path::Path| -> Result<Vec<u8>, Box<dyn std::error::Error>> {
        if !path.is_absolute() || path.components().any(|part| {
            part.as_os_str().to_str().is_some_and(|name| name == ".env" || name.starts_with(".env."))
        }) {
            return Err("absolute non-environment recovery material required".into());
        }
        let metadata = std::fs::symlink_metadata(path)?;
        if !metadata.file_type().is_file() || metadata.permissions().mode() & 0o077 != 0 {
            return Err("private regular recovery material required".into());
        }
        let mut bytes = Vec::new();
        std::fs::File::open(path)?.take(16_777_217).read_to_end(&mut bytes)?;
        if bytes.len() > 16_777_216 {
            return Err("recovery material exceeds bound".into());
        }
        Ok(bytes)
    };
    let material: Value = serde_json::from_slice(&read_private(&material_path)?)?;
    let scenario = &material["scenarios"]["unreachable"];
    let configuration = PathBuf::from(scenario["config"].as_str().ok_or("real configuration missing")?);
    read_private(&configuration)?;
    let config = layerx_gas_station::config::ServiceConfig::load(&configuration)?;
    for endpoint in &config.station.endpoints {
        let authority = endpoint.strip_prefix("https://").ok_or("HTTPS required")?
            .split('/').next().ok_or("RPC authority missing")?;
        let socket: std::net::SocketAddr = authority.parse()?;
        if !socket.ip().is_loopback() || !socket.is_ipv4() {
            return Err("isolated loopback RPC required".into());
        }
    }
    let snapshot = PathBuf::from(scenario["journal"].as_str().ok_or("genuine journal missing")?);
    let durable = read_private(&snapshot)?;
    let now = SystemTime::now().duration_since(UNIX_EPOCH)?.as_secs();
    let stamp = SystemTime::now().duration_since(UNIX_EPOCH)?.as_nanos();
    let directory = std::env::temp_dir().join(format!("station-authenticated-retry-{}-{stamp}", std::process::id()));
    std::fs::create_dir(&directory)?;
    std::fs::set_permissions(&directory, std::fs::Permissions::from_mode(0o700))?;
    let path = directory.join("journal.jsonl");
    let mut file = std::fs::OpenOptions::new().write(true).create_new(true).mode(0o600).open(&path)?;
    file.write_all(&durable)?;
    file.sync_all()?;
    drop(file);
    let count = Rc::new(Cell::new(0));
    let signer = CountedSigner {
        inner: LocalSigner::from_config(&config.station)?,
        count: Rc::clone(&count),
    };
    let rpc = ConfiguredRpc::new(&config.station, HttpsExchange)?;
    rpc.set_deadline(Some(Instant::now() + Duration::from_secs(20)))?;
    let mut station = GasStation::new(
        config.station.clone(), signer, rpc,
        PaymasterRateSource::new(ConfiguredRpc::new(&config.station, HttpsExchange)?, config.station.paymaster),
        Journal::open(&path)?,
    )?;
    let before = station.journal().state().clone();
    let (key, item) = before.items.iter().find(|(_, item)| {
        item.submission.is_some() && item.completion.is_none()
    }).ok_or("unresolved genuine submission required")?;
    let quote = item.quote.as_ref().ok_or("durable quote missing")?;
    let signature: [u8; 65] = quote.signature.as_slice().try_into()?;
    let submission = item.submission.as_ref().ok_or("durable transaction missing")?;
    let expired_now = now.max(quote.deadline.checked_add(1).ok_or("deadline overflow")?);
    assert!(station.status(*key, item.account, &signature)?.completion.is_none());
    assert!(matches!(station.retry(*key, item.account, &[0; 65], expired_now), Err(StationError::Missing)));
    let started = Instant::now();
    assert!(matches!(
        station.retry(*key, item.account, &signature, expired_now),
        Err(StationError::Rpc(RpcFault::Unavailable | RpcFault::RateLimited))
    ));
    assert!(started.elapsed() < Duration::from_secs(21));
    assert_eq!(station.journal().state(), &before);
    assert_eq!(std::fs::read(&path)?, durable);
    assert!(station.journal().state().holds(key.sponsor, submission.nonce));
    assert_eq!(count.get(), 0);
    let started = Instant::now();
    let recovery = station.recover(expired_now, Duration::from_millis(150))?;
    assert!(started.elapsed() < Duration::from_secs(2));
    assert_eq!(recovery.completed, 0);
    assert_eq!(recovery.pending, 0);
    assert_eq!(recovery.unreachable + recovery.deferred, station.unresolved().len());
    assert_eq!(station.journal().state(), &before);
    assert_eq!(count.get(), 0);
    drop(station);
    let reopened = Journal::open(&path)?;
    assert_eq!(reopened.state(), &before);
    assert_eq!(std::fs::read(&path)?, durable);
    drop(reopened);
    std::fs::remove_file(&path)?;
    std::fs::remove_dir(&directory)?;
    Ok(())
}
