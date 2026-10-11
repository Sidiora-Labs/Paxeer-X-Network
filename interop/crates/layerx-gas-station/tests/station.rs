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
use layerx_gas_station::tx::{authorization_digest, batch_digest, Authorization, Call, Fees};
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
    fixture: RefCell<Value>,
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
    fn request_until(
        &self,
        endpoint: &str,
        method: &str,
        params: &Value,
        deadline: std::time::Instant,
    ) -> Result<Value, RpcFault> {
        if std::time::Instant::now() >= deadline {
            return Err(RpcFault::Unavailable);
        }
        self.request(endpoint, method, params)
    }
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
            assert!(entries.iter().any(|entry| matches!(
                entry,
                Entry::Prepared { submission, .. }
                    if submission.raw == raw && submission.hash == hash
            )));
            assert!(journal.ends_with('\n'));
            self.0
                .bindings
                .borrow_mut()
                .insert("$raw".into(), json!(hex(&raw)));
            self.0
                .bindings
                .borrow_mut()
                .insert("$hash".into(), json!(hex(&hash)));
            self.0.sent.borrow_mut().push(raw);
        }
        let fixture = self.0.fixture.borrow();
        for phase in [self.0.phase.borrow().as_str(), "*"] {
            let rules = fixture["phases"][phase]
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
const PAYMASTER_CODE: &str = "0x6080604052";
fn finalized_anchor() -> Value {
    json!({"blockHash": hex(&[0x12; 32]), "requireCanonical": true})
}
fn replay_call(account: Address, data: &[u8], block: &Value) -> Value {
    let mut overrides = serde_json::Map::new();
    overrides.insert(hex(&account), json!({ "code": PAYMASTER_CODE }));
    json!([{"to": hex(&account), "data": hex(data)}, block, overrides])
}
fn consumed_call(sponsor: Address, quote_nonce: u128) -> Vec<u8> {
    [
        keccak(b"usedQuoteNonces(address,uint256)")[..4].to_vec(),
        address_word(sponsor).to_vec(),
        word(quote_nonce).to_vec(),
    ]
    .concat()
}
/// The reads the station makes for one undelegated account quoting at
/// `quote_nonce`: its pending nonce and code, the finalized replay of the
/// consumed quote nonce and the pending batch nonce with the paymaster code
/// as override, and its DID from the addr precompile.
fn account_rules(
    account: Address,
    sponsor: Address,
    quote_nonce: u128,
) -> Result<Vec<Value>, Box<dyn std::error::Error>> {
    let key = keccak(&account);
    let mut text = format!("did:layerx:{}", &hex(&key)[2..]).into_bytes();
    let length = text.len();
    text.resize(length.div_ceil(32) * 32, 0);
    let resolved = [
        key.to_vec(),
        word(64).to_vec(),
        word(u128::try_from(length)?).to_vec(),
        text,
    ]
    .concat();
    let mut precompile = [0_u8; 20];
    precompile[18..].copy_from_slice(&[0x10, 0x04]);
    let did_call = [
        keccak(b"getLayerXDid(address)")[..4].to_vec(),
        address_word(account).to_vec(),
    ]
    .concat();
    Ok(vec![
        json!({"method": "eth_getTransactionCount", "params": [hex(&account), "pending"], "result": "0x0"}),
        json!({"method": "eth_getCode", "params": [hex(&account), finalized_anchor()], "result": "0x"}),
        json!({"method": "eth_getCode", "params": [hex(&account), "pending"], "result": "0x"}),
        json!({
            "method": "eth_call",
            "params": replay_call(account, &consumed_call(sponsor, quote_nonce), &finalized_anchor()),
            "result": hex(&word(0)),
        }),
        json!({
            "method": "eth_call",
            "params": replay_call(account, &keccak(b"nonce()")[..4], &json!("pending")),
            "result": hex(&word(0)),
        }),
        json!({
            "method": "eth_call",
            "params": [{"to": hex(&precompile), "data": hex(&did_call)}, "pending"],
            "result": hex(&resolved),
        }),
    ])
}
/// Answers `account` quoting at `quote_nonce` in every phase.
fn admit(
    recorded: &Recording,
    account: Address,
    sponsor: Address,
    quote_nonce: u128,
) -> TestResult {
    let rules = account_rules(account, sponsor, quote_nonce)?;
    recorded.fixture.borrow_mut()["phases"]["*"]
        .as_array_mut()
        .ok_or("shared phase missing")?
        .extend(rules);
    Ok(())
}
fn sequencer_record(nonce: &str) -> Value {
    json!({"txHash": "$hash", "nonce": nonce, "rung": "submitted"})
}
/// Extends the recorded exchange with the sequencer reads: the finalized
/// anchor, the replay of each phase's consumed answer, the paymaster code,
/// the `pax_sequencerReceipt` answer of each resumed phase and the phases of
/// the refusal cases.
fn sequencer_rules(fixture: &mut Value, account: Address, sponsor: Address) -> TestResult {
    let block = json!({"number": "0x12", "hash": "$finalized", "timestamp": "0x3e8"});
    let unknown = json!({"method": "pax_sequencerReceipt", "params": ["$hash"], "result": null});
    let held = |nonce: &str| json!({"method": "pax_sequencerReceipt", "params": ["$hash"], "result": sequencer_record(nonce)});
    let no_receipt =
        json!({"method": "eth_getTransactionReceipt", "params": ["$hash"], "result": null});
    let phases = fixture["phases"]
        .as_object_mut()
        .ok_or("fixture phases missing")?;
    for (name, rules) in phases.iter_mut() {
        let rules = rules.as_array_mut().ok_or("fixture phase is not a list")?;
        let mut front: Vec<Value> = rules
            .iter()
            .filter(|rule| rule["params"][0]["data"] == "$consumed_call")
            .map(|rule| {
                json!({
                    "method": "eth_call",
                    "params": replay_call(account, &consumed_call(sponsor, 7), &finalized_anchor()),
                    "result": rule["result"],
                })
            })
            .collect();
        match name.as_str() {
            "restart" | "dropped" => front.push(unknown.clone()),
            "refused" | "already_known" | "nonce_too_low" => front.push(held("0x5")),
            _ => (),
        }
        front.append(rules);
        *rules = front;
    }
    let shared = phases
        .get_mut("*")
        .and_then(Value::as_array_mut)
        .ok_or("shared phase missing")?;
    shared.extend([
        json!({"method": "eth_getBlockByNumber", "params": ["finalized", false], "result": block}),
        json!({"method": "eth_getBlockByNumber", "params": ["0x12", false], "result": block}),
        json!({"method": "eth_getCode", "params": ["$paymaster", finalized_anchor()], "result": PAYMASTER_CODE}),
        json!({"method": "eth_getCode", "params": ["$paymaster", "pending"], "result": PAYMASTER_CODE}),
    ]);
    shared.extend(account_rules(account, sponsor, 7)?);
    let underpriced = json!({
        "method": "eth_sendRawTransaction",
        "params": ["$raw"],
        "error": {"code": -32000, "message": "transaction underpriced"},
    });
    let accepted =
        json!({"method": "eth_sendRawTransaction", "params": ["$raw"], "result": "$hash"});
    let at_twelve = json!({"method": "eth_getTransactionCount", "params": ["$sponsor", "pending"], "result": "0xc"});
    for (name, rules) in [
        ("expired_unknown", json!([no_receipt, unknown])),
        ("accepted", json!([accepted])),
        (
            "underpriced_at_twelve",
            json!([at_twelve, underpriced, unknown]),
        ),
        ("accepted_at_twelve", json!([at_twelve, accepted])),
        ("held_at_twelve", json!([no_receipt, held("0xc")])),
        (
            "ambiguous",
            json!([
                no_receipt,
                unknown,
                {"method": "eth_sendRawTransaction", "params": ["$raw"], "fault": "unavailable"},
            ]),
        ),
        (
            "unfinalized",
            json!([{
                "method": "eth_getTransactionReceipt",
                "params": ["$hash"],
                "result": {
                    "transactionHash": "$hash",
                    "status": "0x1",
                    "blockNumber": "0x13",
                    "blockHash": "$block",
                },
            }]),
        ),
    ] {
        phases.insert(name.into(), rules);
    }
    Ok(())
}
fn recording(
    path: PathBuf,
    account: Address,
    sponsor: Address,
) -> Result<Rc<Recording>, Box<dyn std::error::Error>> {
    let mut fixture: Value = serde_json::from_str(include_str!("fixtures/cycle.json"))?;
    sequencer_rules(&mut fixture, account, sponsor)?;
    let consumed = consumed_call(sponsor, 7);
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
        fixture: RefCell::new(fixture),
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
fn quote_request(
    account: Address,
    quote_nonce: u128,
) -> Result<(QuoteRequest, Fees), Box<dyn std::error::Error>> {
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
        quote_nonce: word(quote_nonce),
    };
    Ok((request, fees))
}
/// Quotes `quote_nonce` for the account and builds its signed submission.
fn signed_submission(
    station: &mut TestStation,
    config: &StationConfig,
    account_signer: &LocalSigner,
    sponsor: Address,
    quote_nonce: u128,
) -> Result<SubmitRequest, Box<dyn std::error::Error>> {
    let account = account_signer.address();
    let (request, fees) = quote_request(account, quote_nonce)?;
    let QuoteOutcome::Signed(quoted) = station.quote(&request, fees, 1000)? else {
        panic!("expected quote")
    };
    assert_eq!(quoted.quote.token_amount, word(AMOUNT));
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
    Ok(SubmitRequest {
        key: Key {
            sponsor,
            quote_nonce: word(quote_nonce),
        },
        account,
        calls,
        authorizations: vec![auth],
        account_signature: signature,
    })
}
fn prepare_submission(
    station: &mut TestStation,
    config: &StationConfig,
    account_signer: &LocalSigner,
    signer: &CountedSigner,
    recorded: &Recording,
) -> Result<SubmitRequest, Box<dyn std::error::Error>> {
    let submit = signed_submission(station, config, account_signer, signer.address(), 7)?;
    assert_eq!(signer.count.get(), 1);
    let (request, fees) = quote_request(account_signer.address(), 7)?;
    assert!(matches!(
        station.quote(&request, fees, 1000)?,
        QuoteOutcome::Signed(_)
    ));
    assert_eq!(signer.count.get(), 1);
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
    std::env::set_var("GAS_STATION_DID_GAS_LIMIT", "1000000");
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
        assert!(station.journal().state().holds(sponsor, 5));
        assert_eq!(count.get(), 2);
        assert_eq!(recorded.sent.borrow().len(), 2);
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
    assert_eq!(recorded.sent.borrow().len(), 2);
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
    assert_eq!(recorded.sent.borrow().len(), 2);
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

#[test]
fn refused_submission_retains_exact_bytes_until_finalized_receipt() -> TestResult {
    let lane = Lane::new("REFUSED")?;
    let (mut station, submit) = lane.submitted()?;
    let key = submit.key;
    lane.phase("refused");
    assert_eq!(station.submit(&submit, 1000)?, Progress::Pending);
    let prepared = station.journal().state().items[&key]
        .submission
        .clone()
        .ok_or("refused broadcast lost durable transaction")?;
    assert_eq!(lane.count.get(), 2);
    assert!(station.journal().state().items[&key].completion.is_none());
    assert!(station.journal().state().holds(key.sponsor, prepared.nonce));
    assert!(!lane
        .entries()?
        .iter()
        .any(|entry| matches!(entry, Entry::Refused { .. })));
    assert!(!station.journal().state().refused(&key));
    drop(station);
    lane.phase("restart");
    let mut station = lane.start()?;
    assert_eq!(
        station.journal().state().items[&key].submission.as_ref(),
        Some(&prepared)
    );
    let signature: [u8; 65] = station.journal().state().items[&key]
        .quote
        .as_ref()
        .ok_or("quote missing")?
        .signature
        .as_slice()
        .try_into()?;
    let started = std::time::Instant::now();
    assert!(station
        .status(key, submit.account, &signature)?
        .completion
        .is_none());
    assert_eq!(station.resume(key, 1000)?, Progress::Pending);
    assert!(started.elapsed() < std::time::Duration::from_secs(21));
    assert_eq!(lane.recorded.sent.borrow().last(), Some(&prepared.raw));
    assert_eq!(lane.prepared_nonces()?, vec![prepared.nonce]);
    assert_eq!(lane.count.get(), 2);
    drop(station);
    lane.phase("included");
    let mut station = lane.start()?;
    assert!(station
        .status(key, submit.account, &signature)?
        .completion
        .is_none());
    let done = station.resume(key, 1020)?;
    assert!(
        matches!(done, Progress::Completed(Completion::Included { hash, .. }) if hash == prepared.hash)
    );
    assert_eq!(station.resume(key, 1020)?, done);
    assert!(station
        .status(key, submit.account, &signature)?
        .completion
        .is_some());
    assert!(station
        .journal()
        .state()
        .receipts
        .contains_key(&prepared.hash));
    assert_eq!(lane.count.get(), 2);
    drop(station);
    lane.finish()
}

#[test]
fn dropped_submission_frees_its_nonce_and_nothing_is_signed_at_it() -> TestResult {
    let lane = Lane::new("DROPPED")?;
    let (mut station, submit) = lane.submitted()?;
    let key = submit.key;
    assert_eq!(station.submit(&submit, 1000)?, Progress::Pending);
    let dropped = lane.recorded.sent.borrow()[0].clone();
    drop(station);
    lane.phase("dropped");
    let mut station = lane.start()?;
    assert_eq!(
        station.resume(key, 1000),
        Err(StationError::Rpc(RpcFault::Rejected { code: -32_000 }))
    );
    assert_eq!(lane.count.get(), 2);
    assert_eq!(lane.sent_originals(), 2);
    assert_eq!(lane.recorded.sent.borrow().len(), 2);
    assert!(lane
        .recorded
        .sent
        .borrow()
        .iter()
        .all(|raw| *raw == dropped));
    assert!(station.journal().state().refused(&key));
    assert!(station.journal().state().items[&key].completion.is_none());
    assert!(!station.journal().state().holds(key.sponsor, 5));
    assert_eq!(
        station.journal().state().next_nonce(key.sponsor, 5),
        Some(5)
    );
    assert_eq!(station.journal().state().liability(key)?, 0);
    assert_eq!(station.resume(key, 1000), Err(StationError::Invalid));
    assert_eq!(station.submit(&submit, 1000), Err(StationError::Invalid));
    assert_eq!(lane.count.get(), 2);
    assert_eq!(lane.recorded.sent.borrow().len(), 2);
    lane.phase("accepted");
    admit(&lane.recorded, submit.account, key.sponsor, 8)?;
    let next = signed_submission(
        &mut station,
        &lane.config,
        &lane.account_signer,
        key.sponsor,
        8,
    )?;
    assert_eq!(station.submit(&next, 1000)?, Progress::Pending);
    assert_eq!(lane.prepared_nonces()?, vec![5, 5]);
    assert_eq!(lane.count.get(), 4);
    assert_eq!(lane.sent_originals(), 3);
    assert_eq!(lane.recorded.sent.borrow().len(), 3);
    assert_ne!(lane.recorded.sent.borrow()[2], dropped);
    assert!(station.journal().state().holds(key.sponsor, 5));
    drop(station);
    lane.finish()
}

#[test]
fn expired_unknown_submission_frees_its_nonce_without_rebroadcast() -> TestResult {
    let lane = Lane::new("EXPIRED")?;
    let (mut station, submit) = lane.submitted()?;
    let key = submit.key;
    assert_eq!(station.submit(&submit, 1000)?, Progress::Pending);
    assert_eq!(lane.sent_originals(), 1);
    let signature: [u8; 65] = station.journal().state().items[&key]
        .quote
        .as_ref()
        .ok_or("quote missing")?
        .signature
        .as_slice()
        .try_into()?;
    drop(station);
    lane.phase("expired_unknown");
    let mut station = lane.start()?;
    assert!(station.journal().state().holds(key.sponsor, 5));
    let recovery = station.recover(1020, std::time::Duration::from_secs(5))?;
    assert_eq!(recovery.refused, 1);
    assert_eq!(recovery.completed, 0);
    assert_eq!(recovery.pending, 0);
    assert_eq!(recovery.unreachable, 0);
    assert_eq!(recovery.deferred, 0);
    assert!(!recovery.liability_unreachable);
    assert_eq!(lane.sent_originals(), 1);
    assert_eq!(lane.count.get(), 2);
    assert!(station.journal().state().refused(&key));
    assert!(!station.journal().state().holds(key.sponsor, 5));
    assert!(station.unresolved().is_empty());
    let status = station.status(key, submit.account, &signature)?;
    assert!(status.refused);
    assert!(status.completion.is_none());
    assert_eq!(
        station.retry(key, submit.account, &signature, 1020),
        Err(StationError::Invalid)
    );
    assert_eq!(station.submit(&submit, 1020), Err(StationError::Invalid));
    drop(station);
    let mut station = lane.start()?;
    assert!(station.journal().state().refused(&key));
    assert_eq!(
        station.recover(1020, std::time::Duration::from_secs(5))?,
        layerx_gas_station::station::Recovery::default()
    );
    assert_eq!(lane.sent_originals(), 1);
    assert_eq!(lane.count.get(), 2);
    assert_eq!(lane.prepared_nonces()?, vec![5]);
    drop(station);
    lane.finish()
}

/// A definitive sequencer rejection with no sequencer receipt closes the
/// submission and frees its sponsor nonce for the next quote, a replay never
/// sends the refused bytes again, and an ambiguous send or an unfinalized
/// receipt keeps the nonce held.
#[test]
fn refused_send_frees_nonce_without_replacement() -> TestResult {
    let lane = Lane::new("REJECTED")?;
    let (mut station, submit) = lane.submitted()?;
    let key = submit.key;
    let sponsor = key.sponsor;
    let signature: [u8; 65] = station.journal().state().items[&key]
        .quote
        .as_ref()
        .ok_or("quote missing")?
        .signature
        .as_slice()
        .try_into()?;
    lane.phase("underpriced_at_twelve");
    assert_eq!(
        station.submit(&submit, 1000),
        Err(StationError::Rpc(RpcFault::Rejected { code: -32_000 }))
    );
    assert_eq!(lane.prepared_nonces()?, vec![12]);
    assert_eq!(lane.count.get(), 2);
    let refused = lane.recorded.sent.borrow()[0].clone();
    assert_eq!(lane.recorded.sent.borrow().len(), 1);
    assert_eq!(refused[0], 4);
    let state = station.journal().state();
    assert!(state.refused(&key));
    assert!(state.items[&key].completion.is_none());
    assert!(!state.holds(sponsor, 12));
    assert_eq!(state.next_nonce(sponsor, 12), Some(12));
    assert!(station.unresolved().is_empty());
    let status = station.status(key, submit.account, &signature)?;
    assert!(status.refused);
    assert_eq!(status.submission.map(|(nonce, _)| nonce), Some(12));
    assert!(status.completion.is_none());
    assert_eq!(
        lane.entries()?
            .iter()
            .filter(|entry| matches!(entry, Entry::Refused { key: k, .. } if *k == key))
            .count(),
        1
    );

    let other_signer = install_ephemeral_signer(&lane.config)?;
    let other = other_signer.address();
    admit(&lane.recorded, other, sponsor, 8)?;
    lane.phase("accepted_at_twelve");
    let next = signed_submission(&mut station, &lane.config, &other_signer, sponsor, 8)?;
    let next_key = next.key;
    assert_eq!(station.submit(&next, 1000)?, Progress::Pending);
    assert_eq!(lane.prepared_nonces()?, vec![12, 12]);
    assert_eq!(lane.count.get(), 4);
    assert_eq!(lane.recorded.sent.borrow().len(), 2);
    let accepted = lane.recorded.sent.borrow()[1].clone();
    assert_eq!(accepted[0], 4);
    assert_ne!(accepted, refused);
    assert!(station.journal().state().holds(sponsor, 12));
    assert_eq!(station.unresolved(), vec![next_key]);
    drop(station);

    lane.phase("held_at_twelve");
    let mut station = lane.start()?;
    assert!(station.journal().state().refused(&key));
    let recovery = station.recover(1000, std::time::Duration::from_secs(5))?;
    assert_eq!(recovery.pending, 1);
    assert_eq!(recovery.refused, 0);
    assert_eq!(recovery.completed, 0);
    assert_eq!(lane.recorded.sent.borrow().len(), 2);
    assert!(!lane
        .recorded
        .sent
        .borrow()
        .iter()
        .skip(1)
        .any(|raw| *raw == refused));
    assert_eq!(station.resume(key, 1000), Err(StationError::Invalid));
    assert_eq!(lane.recorded.sent.borrow().len(), 2);
    drop(station);

    lane.phase("ambiguous");
    let mut station = lane.start()?;
    assert_eq!(station.resume(next_key, 1000)?, Progress::Pending);
    assert_eq!(lane.recorded.sent.borrow().len(), 3);
    assert_eq!(lane.recorded.sent.borrow()[2], accepted);
    assert!(!station.journal().state().refused(&next_key));
    assert!(station.journal().state().holds(sponsor, 12));
    assert_eq!(station.journal().state().next_nonce(sponsor, 12), Some(13));
    drop(station);

    lane.phase("unfinalized");
    let mut station = lane.start()?;
    assert_eq!(station.resume(next_key, 1020)?, Progress::Pending);
    let recovery = station.recover(1020, std::time::Duration::from_secs(5))?;
    assert_eq!(recovery.pending, 1);
    assert_eq!(recovery.refused, 0);
    assert_eq!(lane.recorded.sent.borrow().len(), 3);
    assert!(!station.journal().state().refused(&next_key));
    assert!(station.journal().state().holds(sponsor, 12));
    assert!(station.journal().state().items[&next_key]
        .completion
        .is_none());
    assert_eq!(lane.count.get(), 4);
    assert_eq!(lane.prepared_nonces()?, vec![12, 12]);
    drop(station);
    lane.finish()
}

/// The delegated account's batch nonce is read at the pending block tag: a
/// latest or finalized answer differs and is never the one the station uses.
#[test]
fn batch_nonce_reads_pending() -> TestResult {
    let lane = Lane::new("BATCH_PENDING")?;
    let account = lane.account_signer.address();
    let nonce_call = &keccak(b"nonce()")[..4];
    lane.recorded.fixture.borrow_mut()["phases"]["batch_pending"] = json!([
        {"method": "eth_call", "params": replay_call(account, nonce_call, &json!("latest")), "result": hex(&word(2))},
        {"method": "eth_call", "params": replay_call(account, nonce_call, &finalized_anchor()), "result": hex(&word(1))},
        {"method": "eth_call", "params": replay_call(account, nonce_call, &json!("pending")), "result": hex(&word(3))},
    ]);
    lane.phase("batch_pending");
    let mut station = lane.start()?;
    assert_eq!(station.batch_nonce(account)?, word(3));
    let (request, fees) = quote_request(account, 7)?;
    assert!(matches!(
        station.quote(&request, fees, 1000)?,
        QuoteOutcome::Signed(_)
    ));
    let key = Key {
        sponsor: lane.signer.address(),
        quote_nonce: word(7),
    };
    assert_eq!(
        station.journal().state().items[&key]
            .quote
            .as_ref()
            .map(|quote| quote.batch_nonce),
        Some(word(3))
    );
    assert_eq!(lane.count.get(), 1);
    assert!(lane.recorded.sent.borrow().is_empty());
    drop(station);
    lane.finish()
}

/// A failed pending read of the batch nonce or of the sponsor nonce refuses
/// the submission with the RPC fault: nothing is signed, journaled or sent,
/// and advancing the key afterwards sends nothing either.
#[test]
fn pending_nonce_read_failure_sends_nothing() -> TestResult {
    let lane = Lane::new("NONCE_READ")?;
    let (mut station, submit) = lane.submitted()?;
    let key = submit.key;
    let account = submit.account;
    let journaled = lane.entries()?;
    {
        let mut fixture = lane.recorded.fixture.borrow_mut();
        fixture["phases"]["batch_nonce_unavailable"] = json!([{
            "method": "eth_call",
            "params": replay_call(account, &keccak(b"nonce()")[..4], &json!("pending")),
            "fault": "unavailable",
        }]);
        fixture["phases"]["sponsor_nonce_unavailable"] = json!([{
            "method": "eth_getTransactionCount",
            "params": [hex(&key.sponsor), "pending"],
            "fault": "unavailable",
        }]);
    }
    lane.phase("batch_nonce_unavailable");
    assert_eq!(
        station.batch_nonce(account),
        Err(StationError::Rpc(RpcFault::Unavailable))
    );
    for phase in ["batch_nonce_unavailable", "sponsor_nonce_unavailable"] {
        lane.phase(phase);
        assert_eq!(
            station.submit(&submit, 1000),
            Err(StationError::Rpc(RpcFault::Unavailable))
        );
        assert_eq!(station.resume(key, 1000)?, Progress::Pending);
        let state = station.journal().state();
        assert!(state.items[&key].submission.is_none());
        assert!(!state.signing_intents.contains_key(&key));
        assert!(!state.refused(&key));
        assert!(!state.holds(key.sponsor, 5));
        assert!(station.unresolved().is_empty());
        assert_eq!(lane.entries()?, journaled);
        assert_eq!(lane.count.get(), 1);
        assert!(lane.recorded.sent.borrow().is_empty());
    }
    lane.phase("accepted");
    assert_eq!(station.submit(&submit, 1000)?, Progress::Pending);
    assert_eq!(lane.prepared_nonces()?, vec![5]);
    assert_eq!(lane.count.get(), 2);
    assert_eq!(lane.recorded.sent.borrow().len(), 1);
    assert!(station.journal().state().holds(key.sponsor, 5));
    drop(station);
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
                Err(error)
                    if error.kind() == std::io::ErrorKind::WouldBlock && Instant::now() < until =>
                {
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
    assert_eq!(
        rpc.call("eth_chainId", json!([])),
        Err(RpcFault::Unavailable)
    );
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
        if !path.is_absolute()
            || path.components().any(|part| {
                part.as_os_str()
                    .to_str()
                    .is_some_and(|name| name == ".env" || name.starts_with(".env."))
            })
        {
            return Err("absolute non-environment recovery material required".into());
        }
        let metadata = std::fs::symlink_metadata(path)?;
        if !metadata.file_type().is_file() || metadata.permissions().mode() & 0o077 != 0 {
            return Err("private regular recovery material required".into());
        }
        let mut bytes = Vec::new();
        std::fs::File::open(path)?
            .take(16_777_217)
            .read_to_end(&mut bytes)?;
        if bytes.len() > 16_777_216 {
            return Err("recovery material exceeds bound".into());
        }
        Ok(bytes)
    };
    let material: Value = serde_json::from_slice(&read_private(&material_path)?)?;
    let scenario = &material["scenarios"]["unreachable"];
    let configuration = PathBuf::from(
        scenario["config"]
            .as_str()
            .ok_or("real configuration missing")?,
    );
    read_private(&configuration)?;
    let config = layerx_gas_station::config::ServiceConfig::load(&configuration)?;
    for endpoint in &config.station.endpoints {
        let authority = endpoint
            .strip_prefix("https://")
            .ok_or("HTTPS required")?
            .split('/')
            .next()
            .ok_or("RPC authority missing")?;
        let socket: std::net::SocketAddr = authority.parse()?;
        if !socket.ip().is_loopback() || !socket.is_ipv4() {
            return Err("isolated loopback RPC required".into());
        }
    }
    let snapshot = PathBuf::from(
        scenario["journal"]
            .as_str()
            .ok_or("genuine journal missing")?,
    );
    let durable = read_private(&snapshot)?;
    let now = SystemTime::now().duration_since(UNIX_EPOCH)?.as_secs();
    let stamp = SystemTime::now().duration_since(UNIX_EPOCH)?.as_nanos();
    let directory = std::env::temp_dir().join(format!(
        "station-authenticated-retry-{}-{stamp}",
        std::process::id()
    ));
    std::fs::create_dir(&directory)?;
    std::fs::set_permissions(&directory, std::fs::Permissions::from_mode(0o700))?;
    let path = directory.join("journal.jsonl");
    let mut file = std::fs::OpenOptions::new()
        .write(true)
        .create_new(true)
        .mode(0o600)
        .open(&path)?;
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
        config.station.clone(),
        signer,
        rpc,
        PaymasterRateSource::new(
            ConfiguredRpc::new(&config.station, HttpsExchange)?,
            config.station.paymaster,
        ),
        Journal::open(&path)?,
    )?;
    let before = station.journal().state().clone();
    let (key, item) = before
        .items
        .iter()
        .find(|(_, item)| item.submission.is_some() && item.completion.is_none())
        .ok_or("unresolved genuine submission required")?;
    let quote = item.quote.as_ref().ok_or("durable quote missing")?;
    let signature: [u8; 65] = quote.signature.as_slice().try_into()?;
    let submission = item
        .submission
        .as_ref()
        .ok_or("durable transaction missing")?;
    let expired_now = now.max(quote.deadline.checked_add(1).ok_or("deadline overflow")?);
    assert!(station
        .status(*key, item.account, &signature)?
        .completion
        .is_none());
    assert!(matches!(
        station.retry(*key, item.account, &[0; 65], expired_now),
        Err(StationError::Missing)
    ));
    let started = Instant::now();
    assert!(matches!(
        station.retry(*key, item.account, &signature, expired_now),
        Err(StationError::Rpc(
            RpcFault::Unavailable | RpcFault::RateLimited
        ))
    ));
    assert!(started.elapsed() < Duration::from_secs(21));
    assert_eq!(station.journal().state(), &before);
    assert_eq!(std::fs::read(&path)?, durable);
    assert!(station
        .journal()
        .state()
        .holds(key.sponsor, submission.nonce));
    assert_eq!(count.get(), 0);
    let started = Instant::now();
    let recovery = station.recover(expired_now, Duration::from_millis(150))?;
    assert!(started.elapsed() < Duration::from_secs(2));
    assert_eq!(recovery.completed, 0);
    assert_eq!(recovery.pending, 0);
    assert_eq!(
        recovery.unreachable + recovery.deferred,
        station.unresolved().len()
    );
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

#[test]
fn first_use_replay_state_real_https_preserves_retained_storage() -> TestResult {
    use std::io::Read as _;
    use std::os::unix::fs::{MetadataExt as _, PermissionsExt as _};
    use std::time::{Duration, Instant};

    let path = PathBuf::from(std::env::var("PAXEER_X_STATION_FIRST_USE_MATERIAL")?);
    let private = |path: &std::path::Path| -> Result<Vec<u8>, Box<dyn std::error::Error>> {
        if !path.is_absolute()
            || path.components().any(|part| {
                part.as_os_str()
                    .to_str()
                    .is_some_and(|name| name == ".env" || name.starts_with(".env."))
            })
        {
            return Err("absolute non-environment first-use material required".into());
        }
        let info = std::fs::symlink_metadata(path)?;
        if !info.file_type().is_file()
            || info.permissions().mode() & 0o077 != 0
            || info.nlink() != 1
        {
            return Err("private single-link first-use material required".into());
        }
        let mut data = Vec::new();
        std::fs::File::open(path)?
            .take(16_777_217)
            .read_to_end(&mut data)?;
        if data.len() > 16_777_216 {
            return Err("first-use material exceeds bound".into());
        }
        Ok(data)
    };
    let material: Value = serde_json::from_slice(&private(&path)?)?;
    let stamp = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)?
        .as_nanos();
    let directory =
        std::env::temp_dir().join(format!("station-first-use-{}-{stamp}", std::process::id()));
    std::fs::create_dir(&directory)?;
    std::fs::set_permissions(&directory, std::fs::Permissions::from_mode(0o700))?;
    for name in ["first-use", "retained-storage", "incompatible-delegation"] {
        let scenario = &material["scenarios"][name];
        let config_path = PathBuf::from(scenario["config"].as_str().ok_or("real config missing")?);
        let config = layerx_gas_station::config::ServiceConfig::parse(std::str::from_utf8(
            &private(&config_path)?,
        )?)?;
        for endpoint in &config.station.endpoints {
            let socket: std::net::SocketAddr = endpoint
                .strip_prefix("https://")
                .ok_or("HTTPS required")?
                .split('/')
                .next()
                .ok_or("authority missing")?
                .parse()?;
            if !socket.ip().is_loopback() {
                return Err("isolated loopback RPC required".into());
            }
        }
        let rpc = ConfiguredRpc::new(&config.station, HttpsExchange)?;
        let account: Address = bytes(scenario["account"].as_str().ok_or("real account missing")?)?
            .as_slice()
            .try_into()?;
        let code = rpc.call("eth_getCode", json!([hex(&account), "finalized"]))?;
        if name != "incompatible-delegation" {
            assert_eq!(code, "0x");
        }
        let count = Rc::new(Cell::new(0));
        let signer = CountedSigner {
            inner: LocalSigner::from_config(&config.station)?,
            count: Rc::clone(&count),
        };
        let prices = PaymasterRateSource::new(
            ConfiguredRpc::new(&config.station, HttpsExchange)?,
            config.station.paymaster,
        );
        let state = directory.join(format!("{name}.jsonl"));
        let station = GasStation::new(config.station, signer, rpc, prices, Journal::open(&state)?)?;
        let started = Instant::now();
        if name == "incompatible-delegation" {
            assert!(matches!(
                station.batch_nonce(account),
                Err(StationError::IncompatibleDelegation)
            ));
        } else {
            let expected = scenario["expected_nonce"]
                .as_str()
                .ok_or("expected nonce missing")?
                .parse::<u128>()?;
            assert_eq!(expected == 0, name == "first-use");
            assert_eq!(station.batch_nonce(account)?, word(expected));
        }
        assert!(started.elapsed() < Duration::from_secs(30));
        assert_eq!(count.get(), 0);
        assert!(station.journal().state().items.is_empty());
        assert!(std::fs::read(&state)?.is_empty());
        drop(station);
        std::fs::remove_file(&state)?;
    }
    std::fs::remove_dir(&directory)?;
    Ok(())
}
