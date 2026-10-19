use std::cell::{Cell, RefCell};
use std::collections::BTreeMap;
use std::path::{Path, PathBuf};
use std::rc::Rc;
use std::sync::atomic::{AtomicUsize, Ordering};

use k256::ecdsa::SigningKey;
use k256::elliptic_curve::rand_core::OsRng;
use layerx_gas_station::config::StationConfig;
use layerx_gas_station::journal::{Entry, Journal, Key};
use layerx_gas_station::policy::{DidBudget, PolicyRefusal, QuotePolicy};
use layerx_gas_station::price::PaymasterRateSource;
use layerx_gas_station::quote::{address_word, keccak, word, Address, Word, SIDIORA};
use layerx_gas_station::rpc::{hex, ConfiguredRpc, Exchange, RpcFault};
use layerx_gas_station::signer::{LocalSigner, QuoteSigner, SignerError};
use layerx_gas_station::station::{GasStation, QuoteOutcome, StationError};
use layerx_gas_station::tx::Fees;
use layerx_gas_station::{QuoteError, QuoteRequest};
use serde_json::{json, Value};

type TestResult = Result<(), Box<dyn std::error::Error>>;

static NEXT_ID: AtomicUsize = AtomicUsize::new(0);

const PAYMASTER: Address = [0x44; 20];
const PAYMASTER_CODE: &str = "0x6080604052";
const AMOUNT: u128 = 3_145_140;
const DID_GAS_LIMIT: &str = "1000000";

fn config() -> StationConfig {
    StationConfig {
        chain_id: 1325,
        endpoints: vec![format!("https://{}", std::process::id())],
        paymaster: PAYMASTER,
        token: SIDIORA,
        decimals: 6,
        max_rate_age: 300,
        spread_bps: 500,
        margin_bps: 100,
        per_account_limit: 8_000_000,
        per_interval_limit: 16_000_000,
        per_quote_limit: 8_000_000,
        interval_seconds: 60,
        balance_floor: 100,
        relayer_key_env: "PAXEER_DID_BUDGET_UNUSED_KEY".into(),
    }
}

fn signer() -> Result<LocalSigner, Box<dyn std::error::Error>> {
    signer_from(&hex(&SigningKey::random(&mut OsRng).to_bytes()))
}

fn signer_from(key: &str) -> Result<LocalSigner, Box<dyn std::error::Error>> {
    let name = format!(
        "PAXEER_DID_BUDGET_KEY_{}_{}",
        std::process::id(),
        NEXT_ID.fetch_add(1, Ordering::Relaxed)
    );
    std::env::set_var(&name, key);
    let result = LocalSigner::from_env(&name);
    std::env::remove_var(&name);
    Ok(result?)
}

fn journal_path() -> Result<PathBuf, Box<dyn std::error::Error>> {
    use std::os::unix::fs::DirBuilderExt as _;
    let directory = std::env::temp_dir().join(format!(
        "paxeer-did-budget-{}-{}",
        std::process::id(),
        NEXT_ID.fetch_add(1, Ordering::Relaxed)
    ));
    std::fs::DirBuilder::new().mode(0o700).create(&directory)?;
    Ok(directory.join("journal.jsonl"))
}

fn remove_journal(path: &Path) -> TestResult {
    std::fs::remove_file(path)?;
    std::fs::remove_dir(path.parent().ok_or("journal directory missing")?)?;
    Ok(())
}

fn precompile() -> Address {
    let mut address = [0; 20];
    address[18..].copy_from_slice(&[0x10, 0x04]);
    address
}

fn did_of(key: Word) -> String {
    format!("did:layerx:{}", &hex(&key)[2..])
}

/// What the addr precompile answers for an account.
#[derive(Clone, Copy)]
enum Binding {
    Bound(Word),
    Unbound,
    Unavailable,
}

/// The node the station talks to: one finalized block, an undelegated
/// account set, the paymaster's governed rate and the addr precompile's
/// DID bindings. Every `eth_call` to the precompile is recorded.
struct Chain {
    balance: Cell<u128>,
    bindings: RefCell<BTreeMap<Address, Binding>>,
    did_calls: RefCell<Vec<Value>>,
    sent: Cell<usize>,
}
impl Chain {
    fn new(balance: u128) -> Rc<Self> {
        Rc::new(Self {
            balance: Cell::new(balance),
            bindings: RefCell::new(BTreeMap::new()),
            did_calls: RefCell::new(vec![]),
            sent: Cell::new(0),
        })
    }
    fn bind(&self, account: Address, binding: Binding) {
        self.bindings.borrow_mut().insert(account, binding);
    }
    fn precompile_call(&self, data: &[u8]) -> Result<Value, RpcFault> {
        let account: Address = data
            .get(16..36)
            .ok_or(RpcFault::Malformed)?
            .try_into()
            .map_err(|_| RpcFault::Malformed)?;
        let binding = self
            .bindings
            .borrow()
            .get(&account)
            .copied()
            .ok_or(RpcFault::Malformed)?;
        let selector = &data[..4];
        if selector == &keccak(b"getLayerXDid(address)")[..4] {
            return match binding {
                Binding::Bound(key) => {
                    let mut text = did_of(key).into_bytes();
                    let length = text.len();
                    text.resize(length.div_ceil(32) * 32, 0);
                    let length = u128::try_from(length).map_err(|_| RpcFault::Malformed)?;
                    Ok(json!(hex(&[
                        key.to_vec(),
                        word(64).to_vec(),
                        word(length).to_vec(),
                        text
                    ]
                    .concat())))
                }
                Binding::Unbound => Err(RpcFault::Rejected { code: -32000 }),
                Binding::Unavailable => Err(RpcFault::Unavailable),
            };
        }
        if selector == &keccak(b"getUnifiedAccount(address)")[..4] {
            return Ok(json!(hex(&[
                address_word(account).to_vec(),
                word(128).to_vec(),
                word(0).to_vec(),
                word(0).to_vec(),
                word(0).to_vec(),
            ]
            .concat())));
        }
        Err(RpcFault::Malformed)
    }
    fn call(&self, params: &Value) -> Result<Value, RpcFault> {
        let to = params[0]["to"].as_str().ok_or(RpcFault::Malformed)?;
        let data =
            layerx_gas_station::rpc::bytes(params[0]["data"].as_str().ok_or(RpcFault::Malformed)?)?;
        if data.len() < 4 {
            return Err(RpcFault::Malformed);
        }
        if to == hex(&precompile()) {
            assert_eq!(params[1], "pending");
            self.did_calls.borrow_mut().push(params.clone());
            return self.precompile_call(&data);
        }
        if to == hex(&PAYMASTER) {
            assert_eq!(params[1], "latest");
            if data == keccak(b"rateUpdatedAt()")[..4] {
                return Ok(json!(hex(&word(1000))));
            }
            if data == keccak(b"currentRate()")[..4] {
                return Ok(json!(hex(&word(3_114_000))));
            }
            return Err(RpcFault::Malformed);
        }
        let overrides = params[2].as_object().ok_or(RpcFault::Malformed)?;
        if overrides.get(to) != Some(&json!({ "code": PAYMASTER_CODE })) {
            return Err(RpcFault::Malformed);
        }
        Ok(json!(hex(&word(0))))
    }
}
struct Node(Rc<Chain>);
impl Exchange for Node {
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
        let chain = &self.0;
        match method {
            "eth_chainId" => Ok(json!("0x52d")),
            "eth_getBlockByNumber" => Ok(json!({
                "number": "0x12",
                "hash": hex(&[0x12; 32]),
                "timestamp": "0x3e8",
            })),
            "eth_getCode" if params[0] == hex(&PAYMASTER) => Ok(json!(PAYMASTER_CODE)),
            "eth_getCode" => Ok(json!("0x")),
            "eth_getBalance" => Ok(json!(format!("0x{:x}", chain.balance.get()))),
            "eth_call" => chain.call(params),
            "eth_sendRawTransaction" => {
                chain.sent.set(chain.sent.get() + 1);
                Err(RpcFault::Unavailable)
            }
            _ => Err(RpcFault::Malformed),
        }
    }
}

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

type TestStation =
    GasStation<CountedSigner, ConfiguredRpc<Node>, PaymasterRateSource<ConfiguredRpc<Node>>>;

struct Harness {
    config: StationConfig,
    chain: Rc<Chain>,
    sponsor_key: String,
    sponsor: Address,
    signatures: Rc<Cell<usize>>,
    path: PathBuf,
}
impl Harness {
    fn new(balance: u128) -> Result<Self, Box<dyn std::error::Error>> {
        let sponsor_key = hex(&SigningKey::random(&mut OsRng).to_bytes());
        Ok(Self {
            config: config(),
            chain: Chain::new(balance),
            sponsor: signer_from(&sponsor_key)?.address(),
            sponsor_key,
            signatures: Rc::new(Cell::new(0)),
            path: journal_path()?,
        })
    }
    fn open(&self) -> Result<TestStation, Box<dyn std::error::Error>> {
        std::env::set_var("GAS_STATION_DID_GAS_LIMIT", DID_GAS_LIMIT);
        let sponsor = CountedSigner {
            inner: signer_from(&self.sponsor_key)?,
            count: Rc::clone(&self.signatures),
        };
        Ok(GasStation::new(
            self.config.clone(),
            sponsor,
            ConfiguredRpc::new(&self.config, Node(Rc::clone(&self.chain)))?,
            PaymasterRateSource::new(
                ConfiguredRpc::new(&self.config, Node(Rc::clone(&self.chain)))?,
                PAYMASTER,
            ),
            Journal::open(&self.path)?,
        )?)
    }
    fn key(&self, quote_nonce: u128) -> Key {
        Key {
            sponsor: self.sponsor,
            quote_nonce: word(quote_nonce),
        }
    }
}

fn fees(gas_limit: u64) -> Fees {
    Fees {
        gas_limit,
        max_fee_per_gas: 5_000_000_000_000,
        max_priority_fee_per_gas: 1_000_000_000,
    }
}

fn request(
    account: Address,
    quote_nonce: u128,
    fees: Fees,
) -> Result<QuoteRequest, Box<dyn std::error::Error>> {
    Ok(QuoteRequest {
        account,
        max_token_amount: 8_000_000,
        gas_cost: fees.gas_cost()?,
        deadline: 1019,
        quote_nonce: word(quote_nonce),
    })
}

fn quote(
    station: &mut TestStation,
    account: Address,
    quote_nonce: u128,
) -> Result<QuoteOutcome, StationError> {
    let fees = fees(200_000);
    let request = request(account, quote_nonce, fees).map_err(|_| StationError::Invalid)?;
    station.quote(&request, fees, 1000)
}

fn signed_amount(outcome: &QuoteOutcome) -> Option<Word> {
    match outcome {
        QuoteOutcome::Signed(signed) => Some(signed.quote.token_amount),
        QuoteOutcome::Completed(_) => None,
    }
}

fn recorded_did(station: &TestStation, key: &Key) -> Option<String> {
    station.journal().state().items[key]
        .quote
        .as_ref()
        .map(|quote| quote.did.clone())
}

fn policy(per_account_limit: u128) -> Result<QuotePolicy, Box<dyn std::error::Error>> {
    let mut config = config();
    config.per_quote_limit = per_account_limit;
    config.per_account_limit = per_account_limit;
    config.per_interval_limit = per_account_limit * 10;
    Ok(QuotePolicy::new(&config)?.with_did_budget(DidBudget::new(1_000_000)))
}

#[test]
fn accounts_sharing_did_share_budget() -> TestResult {
    let did = did_of([0x21; 32]);
    let other = did_of([0x22; 32]);
    let mut policy = policy(100_000_000)?;
    assert_eq!(
        policy.reserve_for_did(&did, [1; 20], 60_000_000, fees(1), u128::MAX, 1000),
        Ok(())
    );
    let outstanding = policy.outstanding_pax();
    assert_eq!(
        policy.reserve_for_did(&did, [2; 20], 50_000_000, fees(1), u128::MAX, 1000),
        Err(PolicyRefusal::PerAccount)
    );
    assert_eq!(policy.used(&did, 1000), 60_000_000);
    assert_eq!(policy.did_budget().used(&did, 16), 1);
    assert_eq!(policy.outstanding_pax(), outstanding);
    assert_eq!(policy.used(&hex(&[2; 20]), 1000), 0);
    assert_eq!(
        policy.reserve_for_did(&did, [2; 20], 40_000_000, fees(1), u128::MAX, 1000),
        Ok(())
    );
    assert_eq!(policy.used(&did, 1000), 100_000_000);
    assert_eq!(
        policy.reserve_for_did(&other, [2; 20], 50_000_000, fees(1), u128::MAX, 1000),
        Ok(())
    );
    assert_eq!(policy.used(&other, 1000), 50_000_000);
    assert_eq!(policy.used(&did, 1060), 0);
    Ok(())
}

#[test]
fn unbound_account_uses_address() -> TestResult {
    let did = did_of([0x21; 32]);
    let mut policy = policy(100_000_000)?;
    policy.reserve_for_did(&did, [1; 20], 60_000_000, fees(1), u128::MAX, 1000)?;
    let unbound = hex(&[3; 20]);
    assert_eq!(
        policy.reserve_for_did(&unbound, [3; 20], 60_000_000, fees(1), u128::MAX, 1000),
        Ok(())
    );
    assert_eq!(policy.used(&unbound, 1000), 60_000_000);
    assert_eq!(policy.used(&did, 1000), 60_000_000);
    assert_eq!(policy.did_budget().used(&did, 16), 1);
    assert_eq!(policy.did_budget().used(&unbound, 16), 1);
    assert_eq!(
        policy.reserve([3; 20], 40_000_001, 1, u128::MAX, 1000),
        Err(PolicyRefusal::PerAccount)
    );
    assert_eq!(
        policy.reserve([3; 20], 40_000_000, 1, u128::MAX, 1000),
        Ok(())
    );
    assert_eq!(policy.used(&unbound, 1000), 100_000_000);
    assert_eq!(policy.used(&did, 1000), 60_000_000);

    let harness = Harness::new(4_000_000_000_000_000_000)?;
    let bound = signer()?.address();
    let account = signer()?.address();
    harness.chain.bind(bound, Binding::Bound([0x21; 32]));
    harness.chain.bind(account, Binding::Unbound);
    let mut station = harness.open()?;
    assert_eq!(
        signed_amount(&quote(&mut station, bound, 1)?),
        Some(word(AMOUNT))
    );
    assert_eq!(
        signed_amount(&quote(&mut station, account, 2)?),
        Some(word(AMOUNT))
    );
    assert_eq!(recorded_did(&station, &harness.key(1)), Some(did.clone()));
    assert_eq!(recorded_did(&station, &harness.key(2)), Some(hex(&account)));
    let state = station.journal().state();
    assert_eq!(state.did_spend(&hex(&account), 60, 16)?, (AMOUNT, 200_000));
    assert_eq!(state.did_spend(&did, 60, 16)?, (AMOUNT, 200_000));
    assert_eq!(harness.signatures.get(), 2);
    drop(station);
    remove_journal(&harness.path)
}

#[test]
fn journal_replay_restores_did_totals() -> TestResult {
    let harness = Harness::new(4_000_000_000_000_000_000)?;
    let first = signer()?.address();
    let second = signer()?.address();
    harness.chain.bind(first, Binding::Bound([0x31; 32]));
    harness.chain.bind(second, Binding::Bound([0x31; 32]));
    let did = did_of([0x31; 32]);
    let mut station = harness.open()?;
    quote(&mut station, first, 1)?;
    quote(&mut station, second, 2)?;
    drop(station);

    let journal = Journal::open(&harness.path)?;
    assert_eq!(
        journal.state().did_spend(&did, 60, 16)?,
        (2 * AMOUNT, 400_000)
    );
    assert_eq!(journal.state().did_spend(&did, 60, 17)?, (0, 0));
    assert_eq!(journal.state().did_spend(&hex(&first), 60, 16)?, (0, 0));
    assert_eq!(
        journal.state().did_spend(&did, 0, 16),
        Err(layerx_gas_station::journal::JournalError::Corrupt)
    );
    drop(journal);

    let mut station = harness.open()?;
    assert_eq!(
        quote(&mut station, first, 3).err(),
        Some(StationError::Quote(QuoteError::Policy(
            PolicyRefusal::PerAccount
        )))
    );
    let sharing = signer()?.address();
    harness.chain.bind(sharing, Binding::Bound([0x31; 32]));
    assert_eq!(
        quote(&mut station, sharing, 3).err(),
        Some(StationError::Quote(QuoteError::Policy(
            PolicyRefusal::PerAccount
        )))
    );
    assert_eq!(harness.signatures.get(), 2);
    assert_eq!(station.journal().state().items.len(), 2);
    let third = signer()?.address();
    harness.chain.bind(third, Binding::Bound([0x32; 32]));
    assert_eq!(
        signed_amount(&quote(&mut station, third, 3)?),
        Some(word(AMOUNT))
    );
    assert_eq!(
        recorded_did(&station, &harness.key(3)),
        Some(did_of([0x32; 32]))
    );
    drop(station);
    remove_journal(&harness.path)
}

#[test]
fn did_lookup_failure_refuses() -> TestResult {
    let harness = Harness::new(4_000_000_000_000_000_000)?;
    let account = signer()?.address();
    harness.chain.bind(account, Binding::Unavailable);
    let mut station = harness.open()?;
    assert_eq!(
        quote(&mut station, account, 1).err(),
        Some(StationError::Quote(QuoteError::Policy(
            PolicyRefusal::BalanceFloor
        )))
    );
    assert_eq!(harness.signatures.get(), 0);
    assert!(station.journal().state().items.is_empty());
    assert_eq!(harness.chain.did_calls.borrow().len(), 1);
    assert_eq!(harness.chain.sent.get(), 0);
    harness.chain.bind(account, Binding::Bound([0x41; 32]));
    let did = did_of([0x41; 32]);
    assert_eq!(station.journal().state().did_spend(&did, 60, 16)?, (0, 0));
    assert_eq!(
        signed_amount(&quote(&mut station, account, 1)?),
        Some(word(AMOUNT))
    );
    assert_eq!(
        signed_amount(&quote(&mut station, account, 2)?),
        Some(word(AMOUNT))
    );
    assert_eq!(
        quote(&mut station, account, 3).err(),
        Some(StationError::Quote(QuoteError::Policy(
            PolicyRefusal::PerAccount
        )))
    );
    assert_eq!(
        station.journal().state().did_spend(&did, 60, 16)?,
        (2 * AMOUNT, 400_000)
    );
    assert_eq!(harness.signatures.get(), 2);
    drop(station);
    remove_journal(&harness.path)
}

#[test]
fn balance_floor_refuses_quote() -> TestResult {
    let pax = 1_000_000_000_000_000_000;
    let harness = Harness::new(100 + pax)?;
    let account = signer()?.address();
    harness.chain.bind(account, Binding::Bound([0x51; 32]));
    let mut station = harness.open()?;
    let fees = fees(400_000);
    assert_eq!(fees.gas_cost()?, 2 * pax);
    let request = request(account, 1, fees)?;
    assert_eq!(
        station.quote(&request, fees, 1000).err(),
        Some(StationError::Quote(QuoteError::Policy(
            PolicyRefusal::BalanceFloor
        )))
    );
    assert_eq!(harness.signatures.get(), 0);
    assert_eq!(harness.chain.sent.get(), 0);
    assert!(station.journal().state().items.is_empty());
    harness.chain.balance.set(99 + 2 * pax);
    assert_eq!(
        station.quote(&request, fees, 1000).err(),
        Some(StationError::Quote(QuoteError::Policy(
            PolicyRefusal::BalanceFloor
        )))
    );
    harness.chain.balance.set(100 + 2 * pax);
    assert_eq!(
        signed_amount(&station.quote(&request, fees, 1000)?),
        Some(word(2 * AMOUNT))
    );
    assert_eq!(harness.signatures.get(), 1);
    assert_eq!(harness.chain.sent.get(), 0);
    drop(station);
    remove_journal(&harness.path)
}

#[test]
fn budget_charged_through_precompile() -> TestResult {
    let mut harness = Harness::new(10_000_000_000_000_000_000)?;
    harness.config.per_quote_limit = 40_000_000;
    harness.config.per_account_limit = 40_000_000;
    harness.config.per_interval_limit = 80_000_000;
    let first = signer()?.address();
    let second = signer()?.address();
    harness.chain.bind(first, Binding::Bound([0x61; 32]));
    harness.chain.bind(second, Binding::Bound([0x61; 32]));
    let did = did_of([0x61; 32]);
    let mut station = harness.open()?;
    for (nonce, account) in [(1, first), (2, second), (3, first), (4, second), (5, first)] {
        assert_eq!(
            signed_amount(&quote(&mut station, account, nonce)?),
            Some(word(AMOUNT))
        );
        assert_eq!(
            recorded_did(&station, &harness.key(nonce)),
            Some(did.clone())
        );
    }
    let calls = harness.chain.did_calls.borrow().clone();
    assert_eq!(calls.len(), 5);
    for (call, account) in calls.iter().zip([first, second, first, second, first]) {
        let data = [
            keccak(b"getLayerXDid(address)")[..4].to_vec(),
            address_word(account).to_vec(),
        ]
        .concat();
        assert_eq!(
            call,
            &json!([{"to": hex(&precompile()), "data": hex(&data)}, "pending"])
        );
    }
    assert_eq!(
        station.journal().state().did_spend(&did, 60, 16)?,
        (5 * AMOUNT, 1_000_000)
    );
    assert_eq!(
        quote(&mut station, second, 6).err(),
        Some(StationError::Quote(QuoteError::Policy(
            PolicyRefusal::PerAccount
        )))
    );
    assert_eq!(harness.signatures.get(), 5);
    let fresh = signer()?.address();
    harness.chain.bind(fresh, Binding::Bound([0x62; 32]));
    assert_eq!(
        signed_amount(&quote(&mut station, fresh, 6)?),
        Some(word(AMOUNT))
    );
    let entries = std::fs::read_to_string(&harness.path)?
        .lines()
        .map(serde_json::from_str::<Entry>)
        .collect::<Result<Vec<_>, _>>()?;
    assert_eq!(entries.len(), 6);
    assert!(entries
        .iter()
        .all(|entry| matches!(entry, Entry::QuoteReserved { quote } if quote.did.starts_with("did:layerx:"))));
    drop(station);
    remove_journal(&harness.path)
}
