use std::collections::BTreeMap;
use std::io::{Read as _, Write as _};
use std::net::{SocketAddr, TcpListener, TcpStream};
use std::path::PathBuf;
use std::sync::mpsc;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use k256::ecdsa::SigningKey;
use k256::elliptic_curve::rand_core::OsRng;
use layerx_gas_station::config::{
    ConfigError, PaymasterRequired, RegisteredSender, ServiceConfig, StationConfig,
};
use layerx_gas_station::journal::Journal;
use layerx_gas_station::price::PaymasterRateSource;
use layerx_gas_station::quote::{
    address_word, keccak, quote_digest, word, Address, Quote, SIDIORA,
};
use layerx_gas_station::rpc::{
    bytes, hex, ConfiguredRpc, Exchange, HttpsExchange, JsonRpc, RpcFault, Rung, SequencerEndpoint,
    SequencerReceipt,
};
use layerx_gas_station::service::{serve, Limits, Service};
use layerx_gas_station::signer::{LocalSigner, QuoteSigner};
use layerx_gas_station::station::GasStation;
use layerx_gas_station::tx::{
    authorization_digest, batch_digest, encode_sponsored, recover, Authorization, Call,
};
use layerx_gas_station::SignedQuote;
use serde_json::{json, Value};

type TestResult = Result<(), Box<dyn std::error::Error>>;

const CHAIN_ID: u64 = 1325;
const PAYMASTER: Address = [0x44; 20];
const AMOUNT: u128 = 3_145_140;
const GAS_COST: &str = "1000000000000000000";
const DID_PRECOMPILE: Address = [
    0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x10, 0x04,
];

/// The ABI answer of `getLayerXDid(address)` for an account bound to the
/// identity `key`: the key, then the string `did:layerx:<key>`.
fn did_binding(key: &[u8; 32]) -> Result<Vec<u8>, std::num::TryFromIntError> {
    let did = format!("did:layerx:{}", &hex(key)[2..]);
    let mut padded = did.into_bytes();
    let length = padded.len();
    padded.resize(length.div_ceil(32) * 32, 0);
    Ok([
        key.as_slice(),
        word(64).as_slice(),
        word(u128::try_from(length)?).as_slice(),
        &padded,
    ]
    .concat())
}

struct Recording {
    fixture: Value,
    phase: Mutex<String>,
    bindings: Mutex<BTreeMap<String, Value>>,
    sent: Mutex<Vec<Vec<u8>>>,
}
impl Recording {
    fn phase(&self, phase: &str) {
        if let Ok(mut current) = self.phase.lock() {
            *current = phase.into();
        }
    }
    fn bind(&self, name: &str, value: Value) {
        if let Ok(mut bindings) = self.bindings.lock() {
            bindings.insert(name.into(), value);
        }
    }
    fn expect_quote_nonce(&self, sponsor: Address, nonce: u128) {
        let consumed = [
            keccak(b"usedQuoteNonces(address,uint256)")[..4].to_vec(),
            address_word(sponsor).to_vec(),
            word(nonce).to_vec(),
        ]
        .concat();
        self.bind("$consumed_call", json!(hex(&consumed)));
    }
    fn sent(&self) -> Vec<Vec<u8>> {
        self.sent
            .lock()
            .map(|sent| sent.clone())
            .unwrap_or_default()
    }
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

/// The recorded node: every exchange is answered from tests/fixtures/cycle.json,
/// except in the `unreachable` phase, where the request goes to a real HTTPS
/// exchange aimed at a closed loopback port.
struct ReplayExchange(Arc<Recording>);
impl Exchange for ReplayExchange {
    fn request(&self, _endpoint: &str, method: &str, params: &Value) -> Result<Value, RpcFault> {
        let phase = self
            .0
            .phase
            .lock()
            .map_err(|_| RpcFault::Unavailable)?
            .clone();
        if phase == "unreachable" {
            return HttpsExchange.request("https://127.0.0.1:1", method, params);
        }
        if method == "eth_sendRawTransaction" {
            let raw = bytes(params[0].as_str().ok_or(RpcFault::Malformed)?)?;
            self.0.bind("$raw", json!(hex(&raw)));
            self.0.bind("$hash", json!(hex(&keccak(&raw))));
            self.0
                .sent
                .lock()
                .map_err(|_| RpcFault::Unavailable)?
                .push(raw);
        }
        let bindings = self
            .0
            .bindings
            .lock()
            .map_err(|_| RpcFault::Unavailable)?
            .clone();
        for name in [phase.as_str(), "*"] {
            let rules = self.0.fixture["phases"][name]
                .as_array()
                .ok_or(RpcFault::Malformed)?;
            for rule in rules {
                if rule["method"] == method && substitute(&rule["params"], &bindings) == *params {
                    if rule["fault"] == "unavailable" {
                        return Err(RpcFault::Unavailable);
                    }
                    if let Some(code) = rule["error"]["code"].as_i64() {
                        return Err(RpcFault::Rejected { code });
                    }
                    return Ok(substitute(&rule["result"], &bindings));
                }
            }
        }
        panic!("unmatched recorded exchange: {method}")
    }
}

/// Recorded exchanges the sequencer endpoint adds to tests/fixtures/cycle.json:
/// the anchored block reads the station makes, the delegated account code, and
/// per phase the send and the `pax_sequencerReceipt` answer. Each phase's rules
/// are matched before the fixture's own.
fn sequencer_endpoint_rules() -> Value {
    let latest = json!({"blockHash": "$latest_block", "requireCanonical": true});
    let finalized = json!({"blockHash": "$finalized_block", "requireCanonical": true});
    let executed = |rung: &str, status: &str, sponsor: Value| {
        json!({"rung": rung, "txHash": "$hash", "seq": "0x2a", "height": "0x11", "index": "0x0",
            "timestamp": "0x3e8", "status": status, "returnData": "0x", "logs": [],
            "gasUsed": "0x186a0", "cumulativeGasUsed": "0x186a0",
            "effectiveGasPrice": "0x48c27395000", "feeDenom": "ahpx", "feeAmount": "0x0",
            "sponsor": sponsor, "from": "$sponsor", "to": "$account", "contractAddress": null,
            "nonce": "0x5", "vmError": "", "keyId": "0x1", "receipt": "0x"})
    };
    let sent = json!({"method": "eth_sendRawTransaction", "params": ["$raw"], "result": "$hash"});
    let receipt = |result: Value| json!({"method": "pax_sequencerReceipt", "params": ["$hash"], "result": result});
    let rate = |updated: &str| {
        json!([{"method": "eth_call",
            "params": [{"to": "$paymaster", "data": "$rate_updated_call"}, "latest"],
            "result": updated}])
    };
    json!({
        "*": [
            {"method": "eth_getBlockByNumber", "params": ["latest", false],
                "result": {"number": "0x10", "hash": "$latest_block", "timestamp": "0x3e8"}},
            {"method": "eth_getBlockByNumber", "params": ["finalized", false],
                "result": {"number": "0xf", "hash": "$finalized_block", "timestamp": "0x3e0"}},
            {"method": "eth_getBlockByNumber", "params": ["0xf", false],
                "result": {"number": "0xf", "hash": "$finalized_block", "timestamp": "0x3e0"}},
            {"method": "eth_getCode", "params": ["$account", latest], "result": "$delegated_code"},
            {"method": "eth_getCode", "params": ["$account", finalized], "result": "$delegated_code"},
            {"method": "eth_getCode", "params": ["$account", "pending"], "result": "$delegated_code"},
            {"method": "eth_call", "params": [{"to": "$account", "data": "$batch_nonce_call"}, latest],
                "result": "$zero"},
            {"method": "eth_call", "params": [{"to": "$did_precompile", "data": "$did_call"}, "pending"],
                "result": "$did_binding"},
            {"method": "eth_call", "params": [{"to": "$account", "data": "$consumed_call"}, finalized],
                "result": "$zero"},
        ],
        "receipted": [sent.clone(), receipt(executed("receipted", "0x1", json!("$sponsor")))],
        "reverted": [sent.clone(), receipt(executed("receipted", "0x0", Value::Null))],
        "foreign_sponsor": [sent.clone(), receipt(executed("receipted", "0x1", json!("$paymaster")))],
        "held": [sent, receipt(json!({"rung": "submitted", "txHash": "$hash", "nonce": "0x5"}))],
        "refused": [receipt(Value::Null)],
        "rate_age_150": rate("0x0000000000000000000000000000000000000000000000000000000000000352"),
        "rate_age_120": rate("0x0000000000000000000000000000000000000000000000000000000000000370"),
    })
}

fn with_sequencer_endpoint_rules(mut fixture: Value) -> Result<Value, Box<dyn std::error::Error>> {
    let phases = fixture["phases"]
        .as_object_mut()
        .ok_or("fixture without phases")?;
    if let Value::Object(added) = sequencer_endpoint_rules() {
        for (phase, rules) in added {
            let mut merged = rules.as_array().cloned().ok_or("phase rules not a list")?;
            if let Some(existing) = phases.get(&phase).and_then(Value::as_array) {
                merged.extend(existing.iter().cloned());
            }
            phases.insert(phase, Value::Array(merged));
        }
    }
    Ok(fixture)
}

#[derive(Clone)]
struct SharedLog(Arc<Mutex<Vec<u8>>>);
impl std::io::Write for SharedLog {
    fn write(&mut self, data: &[u8]) -> std::io::Result<usize> {
        self.0
            .lock()
            .map_err(|_| std::io::Error::other("log poisoned"))?
            .extend_from_slice(data);
        Ok(data.len())
    }
    fn flush(&mut self) -> std::io::Result<()> {
        Ok(())
    }
}

fn wire_calls() -> Value {
    json!([{"to": hex(&[0x55; 20]), "data": "0x010203", "value": "0"}])
}

struct Harness {
    address: SocketAddr,
    recorded: Arc<Recording>,
    account: LocalSigner,
    sponsor: Address,
    secrets: Vec<String>,
    log: SharedLog,
    journal: PathBuf,
}

fn station_config(name: &str) -> StationConfig {
    StationConfig {
        chain_id: CHAIN_ID,
        endpoints: vec![format!("https://{}", std::process::id())],
        paymaster: PAYMASTER,
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
        relayer_key_env: format!("PAXEER_SERVICE_{name}_KEY"),
    }
}

fn ephemeral_signer(
    config: &StationConfig,
) -> Result<(LocalSigner, String), Box<dyn std::error::Error>> {
    let secret = hex(&SigningKey::random(&mut OsRng).to_bytes());
    std::env::set_var(&config.relayer_key_env, &secret);
    let signer = LocalSigner::from_config(config);
    std::env::remove_var(&config.relayer_key_env);
    Ok((signer?, secret))
}

/// Builds the real service over the recorded node inside its own thread and
/// serves it on `listener`, returning once it is ready or refused to start.
fn spawn_service(
    config: ServiceConfig,
    signer: LocalSigner,
    recorded: Arc<Recording>,
    journal: PathBuf,
    listener: TcpListener,
    mut log: SharedLog,
) -> TestResult {
    std::env::set_var("GAS_STATION_DID_GAS_LIMIT", "1000000");
    let (ready, started) = mpsc::channel();
    std::thread::spawn(move || {
        let built = (|| -> Result<_, Box<dyn std::error::Error>> {
            let rpc = ConfiguredRpc::new(&config.station, ReplayExchange(Arc::clone(&recorded)))?;
            let rates = PaymasterRateSource::new(
                ConfiguredRpc::new(&config.station, ReplayExchange(Arc::clone(&recorded)))?,
                config.station.paymaster,
            );
            let sequencer = SequencerEndpoint::new(ConfiguredRpc::new(
                &config.station,
                ReplayExchange(recorded),
            )?);
            let station = GasStation::new(
                config.station.clone(),
                signer,
                rpc,
                rates,
                Journal::open(&journal)?,
            )?;
            Ok(Service::new(
                &config,
                station,
                || Some(1000),
                Limits {
                    max_body: 65_536,
                    read_time: Duration::from_millis(500),
                },
            )
            .with_sequencer(sequencer))
        })();
        match built {
            Ok(mut service) => {
                if ready.send(Ok(())).is_ok() {
                    let _ = serve(&listener, &mut service, &mut log);
                }
            }
            Err(error) => {
                let _ = ready.send(Err(error.to_string()));
            }
        }
    });
    started.recv_timeout(Duration::from_secs(10))??;
    Ok(())
}

fn start(name: &str) -> Result<Harness, Box<dyn std::error::Error>> {
    start_with_rate_age(name, 300)
}

fn start_with_rate_age(
    name: &str,
    max_rate_age: u64,
) -> Result<Harness, Box<dyn std::error::Error>> {
    start_configured(name, |config, _| config.station.max_rate_age = max_rate_age)
}

/// Starts the service with its configuration changed by `adjust`, which also
/// receives the address of the account the harness quotes for.
fn start_configured(
    name: &str,
    adjust: impl FnOnce(&mut ServiceConfig, Address),
) -> Result<Harness, Box<dyn std::error::Error>> {
    let station = station_config(name);
    let (signer, relayer_secret) = ephemeral_signer(&station)?;
    let (account, account_secret) = ephemeral_signer(&station)?;
    let sponsor = signer.address();
    let journal = std::env::temp_dir().join(format!(
        "paxeer-service-{}-{}.jsonl",
        name.to_ascii_lowercase(),
        std::process::id()
    ));
    if journal.exists() {
        std::fs::remove_file(&journal)?;
    }
    let bindings = [
        ("$sponsor", hex(&sponsor)),
        ("$paymaster", hex(&PAYMASTER)),
        ("$current_rate_call", hex(&keccak(b"currentRate()")[..4])),
        ("$rate_updated_call", hex(&keccak(b"rateUpdatedAt()")[..4])),
        ("$account", hex(&account.address())),
        ("$zero", hex(&word(0))),
        ("$one", hex(&word(1))),
        ("$batch_nonce_call", hex(&keccak(b"nonce()")[..4])),
        ("$latest_block", hex(&[0x10; 32])),
        ("$finalized_block", hex(&[0x0f; 32])),
        ("$did_precompile", hex(&DID_PRECOMPILE)),
        (
            "$did_call",
            hex(&[
                &keccak(b"getLayerXDid(address)")[..4],
                address_word(account.address()).as_slice(),
            ]
            .concat()),
        ),
        ("$did_binding", hex(&did_binding(&[0xd1; 32])?)),
        (
            "$delegated_code",
            hex(&[[0xef, 0x01, 0x00].as_slice(), PAYMASTER.as_slice()].concat()),
        ),
    ]
    .into_iter()
    .map(|(k, v)| (k.to_owned(), json!(v)))
    .collect();
    let recorded = Arc::new(Recording {
        fixture: with_sequencer_endpoint_rules(serde_json::from_str(include_str!(
            "fixtures/cycle.json"
        ))?)?,
        phase: Mutex::new("submit".into()),
        bindings: Mutex::new(bindings),
        sent: Mutex::new(vec![]),
    });
    recorded.expect_quote_nonce(sponsor, 1);
    let listener = TcpListener::bind("127.0.0.1:0")?;
    let address = listener.local_addr()?;
    let mut config = ServiceConfig {
        station,
        listen: address,
        gas_limit: 200_000,
        max_priority_fee_per_gas: 1_000_000_000,
        registered_senders: Vec::new(),
    };
    adjust(&mut config, account.address());
    let log = SharedLog(Arc::new(Mutex::new(Vec::new())));
    spawn_service(
        config,
        signer,
        Arc::clone(&recorded),
        journal.clone(),
        listener,
        log.clone(),
    )?;
    Ok(Harness {
        address,
        recorded,
        account,
        sponsor,
        secrets: vec![relayer_secret, account_secret],
        log,
        journal,
    })
}

impl Harness {
    fn exchange(&self, request: &[u8]) -> Result<(u16, Value), Box<dyn std::error::Error>> {
        let mut stream = TcpStream::connect(self.address)?;
        stream.set_read_timeout(Some(Duration::from_secs(10)))?;
        stream.write_all(request)?;
        let mut response = Vec::new();
        stream.read_to_end(&mut response)?;
        let split = response
            .windows(4)
            .position(|w| w == b"\r\n\r\n")
            .ok_or("response without head")?;
        let head = std::str::from_utf8(&response[..split])?;
        let status = head
            .split(' ')
            .nth(1)
            .ok_or("response without status")?
            .parse()?;
        let body = &response[split + 4..];
        assert!(head.contains(&format!("Content-Length: {}", body.len())));
        Ok((status, serde_json::from_slice(body)?))
    }

    fn post(&self, path: &str, body: &Value) -> Result<(u16, Value), Box<dyn std::error::Error>> {
        let body = body.to_string();
        self.exchange(
            format!(
                "POST {path} HTTP/1.1\r\nHost: station\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{body}",
                body.len()
            )
            .as_bytes(),
        )
    }

    /// The body `requestGasQuote` in the TypeScript SDK sends.
    fn quote_body(&self) -> Value {
        json!({
            "account": hex(&self.account.address()),
            "nonce": "0",
            "calls": wire_calls(),
            "maxTokenAmount": "3200000",
            "gasCost": GAS_COST,
            "chainId": "1325",
            "token": "0x21f7b20a555199fa73A238B1a91FD0f549068fEe",
            "decimals": 6,
        })
    }

    fn quoted(&self, response: &Value) -> Result<SignedQuote, Box<dyn std::error::Error>> {
        let quote = &response["quote"];
        let number = |name: &str| -> Result<u128, Box<dyn std::error::Error>> {
            Ok(quote[name]
                .as_str()
                .ok_or("numeric field not a string")?
                .parse()?)
        };
        let address = |name: &str| -> Result<Address, Box<dyn std::error::Error>> {
            Ok(bytes(quote[name].as_str().ok_or("address not a string")?)?
                .try_into()
                .map_err(|_| "address length")?)
        };
        let quote = Quote {
            sponsor: address("sponsor")?,
            token: address("token")?,
            max_token_amount: word(number("maxTokenAmount")?),
            token_amount: word(number("tokenAmount")?),
            deadline: word(number("deadline")?),
            nonce: word(number("quoteNonce")?),
            gas_cost: word(number("gasCost")?),
        };
        let signature: [u8; 65] = bytes(
            response["relayerSignature"]
                .as_str()
                .ok_or("signature not a string")?,
        )?
        .try_into()
        .map_err(|_| "signature length")?;
        Ok(SignedQuote {
            digest: quote_digest(word(u128::from(CHAIN_ID)), self.account.address(), &quote),
            quote,
            signature,
        })
    }

    /// The body `sendSponsoredBatch` in the web application sends.
    fn submission_body(&self, quote_response: &Value) -> Result<Value, Box<dyn std::error::Error>> {
        let signed = self.quoted(quote_response)?;
        let calls = vec![Call {
            to: [0x55; 20],
            value: word(0),
            data: vec![1, 2, 3],
        }];
        let account_signature = self.account.sign_digest(batch_digest(
            CHAIN_ID,
            self.account.address(),
            word(0),
            &calls,
            &signed.quote,
        )?)?;
        let mut authorization = Authorization {
            chain_id: CHAIN_ID,
            delegate: PAYMASTER,
            nonce: 0,
            signature: [0; 65],
        };
        let auth_signature = self
            .account
            .sign_digest(authorization_digest(&authorization)?)?;
        authorization.signature = auth_signature;
        let data = encode_sponsored(&calls, &signed, &account_signature)?;
        Ok(json!({
            "call": {"to": hex(&self.account.address()), "value": "0", "data": hex(&data)},
            "authorization": {
                "chainId": "1325",
                "address": hex(&PAYMASTER),
                "nonce": "0",
                "yParity": auth_signature[64] - 27,
                "r": hex(&auth_signature[..32]),
                "s": hex(&auth_signature[32..64]),
            },
            "batch": {
                "chainId": "1325",
                "account": hex(&self.account.address()),
                "nonce": "0",
                "calls": wire_calls(),
                "quote": quote_response["quote"],
            },
            "accountSignature": hex(&account_signature),
            "relayerSignature": quote_response["relayerSignature"],
        }))
    }

    /// The body a client polls `/status` with for the quote it holds.
    fn status_body(&self, quote_response: &Value) -> Value {
        json!({
            "sponsor": quote_response["quote"]["sponsor"],
            "quoteNonce": quote_response["quote"]["quoteNonce"],
            "account": hex(&self.account.address()),
            "relayerSignature": quote_response["relayerSignature"],
        })
    }

    fn log(&self) -> String {
        self.log
            .0
            .lock()
            .map(|log| String::from_utf8_lossy(&log).into_owned())
            .unwrap_or_default()
    }

    /// The service writes a request's log line after it delivered the
    /// response, so the log is read once it holds `lines` lines or after a
    /// bounded wait.
    fn assert_log_clean(&self, lines: &[&str]) {
        let waited = std::time::Instant::now();
        while self.log().lines().count() < lines.len() && waited.elapsed() < Duration::from_secs(5)
        {
            std::thread::sleep(Duration::from_millis(10));
        }
        let log = self.log();
        assert_eq!(log.lines().collect::<Vec<_>>(), lines);
        for secret in &self.secrets {
            assert!(!log.contains(secret.trim_start_matches("0x")));
        }
        for raw in self.recorded.sent() {
            assert!(!log.contains(hex(&raw).trim_start_matches("0x")));
        }
    }

    fn journal_is_empty(&self) -> Result<bool, Box<dyn std::error::Error>> {
        Ok(std::fs::read_to_string(&self.journal)?.is_empty())
    }

    fn finish(self) -> TestResult {
        std::fs::remove_file(&self.journal)?;
        Ok(())
    }
}

#[test]
fn quote_and_sponsored_submission_over_a_socket() -> TestResult {
    let harness = start("CYCLE")?;
    harness.recorded.phase("receipted");
    let (status, quote) = harness.post("/quote", &harness.quote_body())?;
    assert_eq!(status, 200);
    let expected = json!({
        "sponsor": hex(&harness.sponsor),
        "token": hex(&SIDIORA),
        "maxTokenAmount": "3200000",
        "tokenAmount": AMOUNT.to_string(),
        "deadline": "1019",
        "quoteNonce": "1",
        "gasCost": GAS_COST,
        "decimals": 6,
    });
    assert_eq!(quote["quote"], expected);
    assert_eq!(quote.as_object().map(serde_json::Map::len), Some(2));
    let signed = harness.quoted(&quote)?;
    assert_eq!(recover(signed.digest, &signed.signature)?, harness.sponsor);

    let (status, submitted) = harness.post("/submit", &harness.submission_body(&quote)?)?;
    assert_eq!(status, 200);
    let sent = harness.recorded.sent();
    assert_eq!(sent.len(), 1);
    assert_eq!(sent[0][0], 4);
    let hash = hex(&keccak(&sent[0]));
    assert_eq!(
        submitted,
        json!({"transactionHash": hash, "rung": "receipted", "seq": "42"})
    );
    let submitted = submitted.to_string();
    assert!(!submitted.contains(hex(&sent[0]).trim_start_matches("0x")));
    for secret in &harness.secrets {
        assert!(!submitted.contains(secret.trim_start_matches("0x")));
        assert!(!quote.to_string().contains(secret.trim_start_matches("0x")));
    }
    assert_eq!(
        harness.post("/status", &harness.status_body(&quote))?,
        (
            200,
            json!({
                "state": "completed",
                "deadline": "1019",
                "submission": {"sponsorNonce": "5", "transactionHash": hash},
                "completion": {"outcome": "receipted", "rung": "receipted",
                    "transactionHash": hash, "seq": "42"},
            })
        )
    );
    assert_eq!(harness.recorded.sent().len(), 1);
    harness.assert_log_clean(&["/quote 200", "/submit 200", "/status 200"]);
    harness.finish()
}

#[test]
fn healthz_answers_a_plain_get_and_refuses_other_methods() -> TestResult {
    let harness = start("HEALTH")?;
    let (status, body) = harness.exchange(b"GET /healthz HTTP/1.1\r\nHost: station\r\n\r\n")?;
    assert_eq!((status, body), (200, json!({"status": "ok"})));
    let (status, body) = harness.post("/healthz", &json!({}))?;
    assert_eq!(
        (status, body),
        (405, json!({"error": "method_not_allowed"}))
    );
    harness.assert_log_clean(&["/healthz 200", "/healthz 405"]);
    harness.finish()
}

#[test]
fn quote_refusals_are_4xx_and_unavailability_is_5xx() -> TestResult {
    let harness = start("SPLIT")?;
    let mut lines = Vec::new();
    for (field, value) in [
        ("chainId", json!("1")),
        ("token", json!(hex(&[0x66; 20]))),
        ("decimals", json!(18)),
        ("gasCost", json!("1000000000000000001")),
        ("maxTokenAmount", json!("1")),
    ] {
        let mut body = harness.quote_body();
        body[field] = value;
        let (status, answer) = harness.post("/quote", &body)?;
        assert_eq!((status, answer), (422, json!({"error": "refused"})));
        lines.push("/quote 422");
    }
    let mut body = harness.quote_body();
    body["extra"] = json!("field");
    assert_eq!(harness.post("/quote", &body)?.0, 400);
    lines.push("/quote 400");
    assert_eq!(
        harness
            .exchange(b"POST /quote HTTP/1.1\r\nContent-Length: 1\r\n\r\n{")?
            .0,
        400
    );
    lines.push("/quote 400");
    assert_eq!(harness.exchange(b"GET /quote HTTP/1.1\r\n\r\n")?.0, 405);
    lines.push("/quote 405");
    assert_eq!(
        harness.exchange(b"POST /elsewhere HTTP/1.1\r\nContent-Length: 0\r\n\r\n")?,
        (404, json!({"error": "not_found"}))
    );
    lines.push("- 404");
    assert_eq!(
        harness.exchange(b"POST /submit HTTP/1.1\r\nHost: station\r\n\r\n")?,
        (411, json!({"error": "length_required"}))
    );
    lines.push("/submit 411");
    assert_eq!(
        harness.exchange(b"POST /quote HTTP/1.1\r\nContent-Length: 65537\r\n\r\n")?,
        (413, json!({"error": "too_large"}))
    );
    lines.push("/quote 413");
    assert_eq!(
        harness.exchange(b"POST /quote HTTP/1.1\r\nContent-Len")?,
        (408, json!({"error": "timeout"}))
    );
    lines.push("- 408");
    assert!(harness.journal_is_empty()?);

    harness.recorded.phase("stale_rate");
    assert_eq!(
        harness.post("/quote", &harness.quote_body())?,
        (503, json!({"error": "stale_rate"}))
    );
    lines.push("/quote 503");
    harness.recorded.phase("unreachable");
    assert_eq!(harness.post("/quote", &harness.quote_body())?.0, 503);
    lines.push("/quote 503");
    assert!(harness.journal_is_empty()?);
    harness.assert_log_clean(&lines);
    harness.finish()
}

#[test]
fn submission_refusals_are_4xx_and_an_unreachable_node_is_5xx() -> TestResult {
    let harness = start("SUBMIT")?;
    let mut lines = Vec::new();
    harness.recorded.phase("refused");
    let (status, quote) = harness.post("/quote", &harness.quote_body())?;
    assert_eq!(status, 200);
    lines.push("/quote 200");
    let submission = harness.submission_body(&quote)?;
    let mut unknown = submission.clone();
    unknown["batch"]["quote"]["quoteNonce"] = json!("99");
    assert_eq!(harness.post("/submit", &unknown)?.0, 422);
    lines.push("/submit 422");
    let mut tampered = submission.clone();
    tampered["call"]["data"] = json!("0x00");
    assert_eq!(harness.post("/submit", &tampered)?.0, 422);
    lines.push("/submit 422");
    let mut raised = submission.clone();
    raised["batch"]["quote"]["tokenAmount"] = json!("3145141");
    assert_eq!(harness.post("/submit", &raised)?.0, 422);
    lines.push("/submit 422");
    assert!(harness.recorded.sent().is_empty());

    assert_eq!(
        harness.post("/submit", &submission)?,
        (422, json!({"error": "refused"}))
    );
    lines.push("/submit 422");
    assert_eq!(harness.recorded.sent().len(), 1);
    harness.recorded.phase("unreachable");
    assert_eq!(
        harness.post("/submit", &submission)?,
        (503, json!({"error": "unavailable"}))
    );
    lines.push("/submit 503");
    assert_eq!(harness.recorded.sent().len(), 1);
    harness.assert_log_clean(&lines);
    harness.finish()
}

#[test]
fn quote_refused_when_the_rate_is_older_than_max_rate_age() -> TestResult {
    let harness = start_with_rate_age("STALE", 120)?;
    harness.recorded.phase("rate_age_150");
    assert_eq!(
        harness.post("/quote", &harness.quote_body())?,
        (503, json!({"error": "stale_rate"}))
    );
    assert!(harness.journal_is_empty()?);
    harness.recorded.phase("rate_age_120");
    let (status, quote) = harness.post("/quote", &harness.quote_body())?;
    assert_eq!(status, 200);
    assert_eq!(quote["quote"]["tokenAmount"], json!(AMOUNT.to_string()));
    assert_eq!(quote["quote"]["quoteNonce"], json!("1"));
    assert!(harness.recorded.sent().is_empty());
    harness.assert_log_clean(&["/quote 503", "/quote 200"]);
    harness.finish()
}

/// The harness, the quote and the submit answer of one sponsored batch.
type SequencerEndpointSubmission = (Harness, Value, (u16, Value));

/// Quotes and submits one sponsored batch with the sequencer answering as
/// `phase` records, returning the harness, the quote and the submit answer.
fn sequencer_endpoint_submission(
    name: &str,
    phase: &str,
) -> Result<SequencerEndpointSubmission, Box<dyn std::error::Error>> {
    let harness = start(name)?;
    harness.recorded.phase(phase);
    let (status, quote) = harness.post("/quote", &harness.quote_body())?;
    assert_eq!(status, 200);
    let answer = harness.post("/submit", &harness.submission_body(&quote)?)?;
    assert_eq!(harness.recorded.sent().len(), 1);
    Ok((harness, quote, answer))
}

#[test]
fn a_held_future_nonce_send_answers_at_rung_submitted_and_stays_pending() -> TestResult {
    let (harness, quote, answer) = sequencer_endpoint_submission("HELD", "held")?;
    let hash = hex(&keccak(&harness.recorded.sent()[0]));
    assert_eq!(
        answer,
        (200, json!({"transactionHash": hash, "rung": "submitted"}))
    );
    assert_eq!(
        harness.post("/status", &harness.status_body(&quote))?,
        (
            200,
            json!({
                "state": "pending",
                "deadline": "1019",
                "submission": {"sponsorNonce": "5", "transactionHash": hash},
                "completion": null,
            })
        )
    );
    harness.assert_log_clean(&["/quote 200", "/submit 200", "/status 200"]);
    harness.finish()
}

#[test]
fn a_reverted_receipt_is_refused_and_reported_reverted() -> TestResult {
    let (harness, quote, answer) = sequencer_endpoint_submission("REVERTED", "reverted")?;
    let hash = hex(&keccak(&harness.recorded.sent()[0]));
    assert_eq!(answer, (422, json!({"error": "refused"})));
    assert_eq!(
        harness.post("/status", &harness.status_body(&quote))?.1["completion"],
        json!({"outcome": "reverted", "rung": "receipted", "transactionHash": hash})
    );
    harness.assert_log_clean(&["/quote 200", "/submit 422", "/status 200"]);
    harness.finish()
}

#[test]
fn a_receipt_naming_another_sponsor_is_refused_as_unavailable() -> TestResult {
    let (harness, _, answer) = sequencer_endpoint_submission("FOREIGN", "foreign_sponsor")?;
    assert_eq!(answer, (503, json!({"error": "unavailable"})));
    harness.assert_log_clean(&["/quote 200", "/submit 503"]);
    harness.finish()
}

/// A served configuration matching `station_config`, with `registered` as its
/// registered senders.
fn service_json(registered: Value) -> Value {
    json!({"listen": "127.0.0.1:8545", "gas_limit": 200_000,
        "max_priority_fee_per_gas": 1_000_000_000, "chain_id": CHAIN_ID,
        "endpoints": ["https://paxeer.app"], "paymaster": hex(&PAYMASTER), "token": hex(&SIDIORA),
        "decimals": 6, "max_rate_age": 300, "spread_bps": 500, "margin_bps": 100,
        "per_account_limit": 8_000_000, "per_interval_limit": 16_000_000,
        "per_quote_limit": 4_000_000, "interval_seconds": 60, "balance_floor": 100,
        "relayer_key_env": "PAXEER_SERVICE_PARSE_KEY", "registered_senders": registered})
}

#[test]
fn registered_senders_parse_resolve_their_own_budget_and_are_quoted() -> TestResult {
    let feeder = [0x0f; 20];
    let config = ServiceConfig::parse(
        &service_json(json!([{"address": hex(&feeder), "budget": 5_000_000}])).to_string(),
    )?;
    assert_eq!(
        config.registered_senders,
        vec![RegisteredSender {
            address: feeder,
            budget: 5_000_000
        }]
    );
    assert_eq!(config.account_budget(&feeder), 5_000_000);
    assert_eq!(config.account_budget(&[0x0e; 20]), 8_000_000);
    let mut unregistered = service_json(json!([]));
    assert!(ServiceConfig::parse(&unregistered.to_string())?
        .registered_senders
        .is_empty());
    if let Some(fields) = unregistered.as_object_mut() {
        fields.remove("registered_senders");
    }
    assert!(ServiceConfig::parse(&unregistered.to_string())?
        .registered_senders
        .is_empty());
    assert_eq!(
        ServiceConfig::parse(
            &service_json(json!([{"address": hex(&feeder), "budget": 15_999_999}])).to_string()
        )?
        .account_budget(&feeder),
        15_999_999
    );
    for refused in [
        json!([{"address": hex(&feeder), "budget": 0}]),
        json!([{"address": hex(&feeder), "budget": 16_000_000}]),
        json!([{"address": hex(&feeder), "budget": -1}]),
        json!([{"address": hex(&[0; 20]), "budget": 1}]),
        json!([{"address": "0x0f", "budget": 1}]),
        json!([{"address": hex(&feeder)}]),
        json!([{"budget": 1}]),
        json!([{"address": hex(&feeder), "budget": 1, "key": "sensitive-value"}]),
        json!([{"address": hex(&feeder), "budget": 1}, {"address": hex(&feeder), "budget": 2}]),
        json!({"address": hex(&feeder), "budget": 1}),
        json!(null),
    ] {
        assert_eq!(
            ServiceConfig::parse(&service_json(refused).to_string()).err(),
            Some(ConfigError {
                field: "registered_senders"
            })
        );
    }

    let harness = start_configured("REGISTERED", |config, account| {
        config.registered_senders = vec![RegisteredSender {
            address: account,
            budget: 12_000_000,
        }];
    })?;
    harness.recorded.phase("receipted");
    let (status, quote) = harness.post("/quote", &harness.quote_body())?;
    assert_eq!(status, 200);
    assert_eq!(quote["quote"]["tokenAmount"], json!(AMOUNT.to_string()));
    assert_eq!(quote["quote"]["quoteNonce"], json!("1"));
    assert!(harness.recorded.sent().is_empty());
    harness.assert_log_clean(&["/quote 200"]);
    harness.finish()
}

#[test]
fn an_empty_or_sidiora_paymaster_is_refused_at_start() -> TestResult {
    for paymaster in [hex(&[0; 20]), hex(&SIDIORA)] {
        let mut value = service_json(json!([]));
        value["paymaster"] = json!(paymaster);
        assert_eq!(
            ServiceConfig::parse(&value.to_string()).err(),
            Some(ConfigError { field: "paymaster" })
        );
    }
    let required = ConfigError {
        field: "GAS_STATION_PAYMASTER",
    };
    for raw in [
        None,
        Some(""),
        Some("0x"),
        Some("0x0000000000000000000000000000000000000000"),
        Some("0x21f7b20a555199fa73A238B1a91FD0f549068fEe"),
    ] {
        assert_eq!(PaymasterRequired::try_from(raw), Err(required.clone()));
    }
    assert_eq!(
        PaymasterRequired::try_from(Some(hex(&PAYMASTER).as_str())),
        Ok(PaymasterRequired(PAYMASTER))
    );
    for (name, paymaster) in [("EMPTY_PAYMASTER", [0; 20]), ("SID_PAYMASTER", SIDIORA)] {
        let refused = start_configured(name, |config, _| config.station.paymaster = paymaster);
        assert!(refused.is_err());
    }
    Ok(())
}

#[test]
fn sequencer_receipt_is_requested_by_transaction_hash() -> TestResult {
    let (harness, _, answer) = sequencer_endpoint_submission("BY_HASH", "held")?;
    let hash = keccak(&harness.recorded.sent()[0]);
    assert_eq!(
        answer,
        (
            200,
            json!({"transactionHash": hex(&hash), "rung": "submitted"})
        )
    );
    let rpc = ConfiguredRpc::new(
        &station_config("BY_HASH"),
        ReplayExchange(Arc::clone(&harness.recorded)),
    )?;
    assert_eq!(
        rpc.sequencer_receipt(&hash)?,
        Some(SequencerReceipt {
            nonce: 5,
            rung: Rung::Submitted
        })
    );
    harness.recorded.phase("refused");
    assert_eq!(rpc.sequencer_receipt(&hash)?, None);
    harness.assert_log_clean(&["/quote 200", "/submit 200"]);
    harness.finish()
}
