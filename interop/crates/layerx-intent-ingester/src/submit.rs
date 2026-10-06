//! Kernel submission of journaled exchange intents: each order, cancel or
//! settlement becomes the matching kernel perps or spot activity, signed by
//! the ingester key and sent with the gateway's `lx_sendActivity`. Every
//! outcome is appended once per intent id to `settlement.jsonl`, so a
//! restart never submits an intent that already settled, was refused or was
//! skipped.

use std::collections::{HashMap, HashSet};
use std::fs::{self, File, OpenOptions};
use std::io::{BufRead as _, BufReader, Read as _, Write as _};
use std::net::{TcpStream, ToSocketAddrs as _};
use std::path::{Path, PathBuf};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use ed25519_dalek::{Signer as _, SigningKey};
use layerx_crypto::SignatureMessage;
use layerx_intent_ingester::{hex, unhex, IngestError, IngestedIntent, JsonRpc};
use layerx_intents::precompile::{route_event, OrderPlaced, PrecompileEvent, RouteBinding};
use layerx_intents::{compile, IntentKind};
use layerx_types::activity::{Authority, EnvelopeBuilder, Signature, TimestampBound};
use layerx_types::amount::Amount;
use layerx_types::ids::{Did, IdempotencyKey};
use layerx_types::payload::{
    ActivityType, ModuleId, ModuleRegistration, ModuleRegistry, Payload, PerpsPayload,
    SpotOrderKind, SpotPayload, SpotTimeInForce, TradeSide,
};
use layerx_wire::activity::{decode_signed, encode_signed_envelope, encode_unsigned_envelope};
use layerx_wire::hash::{activity_id, payload_hash_for, Domain};
use serde_json::{json, Value};

/// Commitment requested from `lx_sendActivity`.
pub const COMMITMENT: &str = "executed";

/// Validity window of one signed activity.
pub const ACTIVITY_VALIDITY_MS: u64 = 60_000;

/// JSON-RPC codes the gateway answers while the outcome is still open
/// (requested commitment pending, rate limited, receipt not yet verified);
/// the intent stays queued and is retried under the same idempotency key.
const TRANSIENT_CODES: [i64; 3] = [-32001, -32005, -32603];

/// Kernel module a chain market settles on.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum MarketRoute {
    Perps {
        kernel_market_id: [u8; 32],
        owner_account_id: [u8; 32],
        perps_abi_version: u32,
    },
    Spot {
        kernel_market_id: [u8; 32],
        base_account_id: [u8; 32],
        quote_account_id: [u8; 32],
    },
}

/// Chain market id to kernel market, from `LAYERX_INGESTER_MARKET_MAP`.
#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct MarketMap(HashMap<[u8; 32], MarketRoute>);

impl MarketMap {
    /// Parses `{"0x<chain market>": {"module": "perps"|"spot",
    /// "kernel_market_id": "0x..", ...}}`. Perps entries name the
    /// `owner_account_id` (and optionally `perps_abi_version`, default 1);
    /// spot entries name `base_account_id` and `quote_account_id`.
    ///
    /// # Errors
    ///
    /// Refuses malformed JSON, ids or modules.
    pub fn parse(text: &str) -> Result<Self, IngestError> {
        let bad = |detail: String| IngestError::Configuration(format!("market map: {detail}"));
        let value: Value = serde_json::from_str(text).map_err(|error| bad(error.to_string()))?;
        let entries = value
            .as_object()
            .ok_or_else(|| bad("not an object".to_owned()))?;
        let mut map = HashMap::new();
        for (market, entry) in entries {
            let id = |name: &str| -> Result<[u8; 32], IngestError> {
                entry
                    .get(name)
                    .and_then(Value::as_str)
                    .and_then(|text| unhex(text).ok())
                    .and_then(|bytes| bytes.try_into().ok())
                    .ok_or_else(|| bad(format!("{market}.{name}")))
            };
            let route = match entry.get("module").and_then(Value::as_str) {
                Some("perps") => MarketRoute::Perps {
                    kernel_market_id: id("kernel_market_id")?,
                    owner_account_id: id("owner_account_id")?,
                    perps_abi_version: match entry.get("perps_abi_version") {
                        None => 1,
                        Some(version) => version
                            .as_u64()
                            .and_then(|version| u32::try_from(version).ok())
                            .ok_or_else(|| bad(format!("{market}.perps_abi_version")))?,
                    },
                },
                Some("spot") => MarketRoute::Spot {
                    kernel_market_id: id("kernel_market_id")?,
                    base_account_id: id("base_account_id")?,
                    quote_account_id: id("quote_account_id")?,
                },
                _ => return Err(bad(format!("{market}.module"))),
            };
            let chain: [u8; 32] = unhex(market)
                .ok()
                .and_then(|bytes| bytes.try_into().ok())
                .ok_or_else(|| bad(format!("market id {market}")))?;
            map.insert(chain, route);
        }
        Ok(Self(map))
    }

    #[must_use]
    pub fn get(&self, chain_market: &[u8; 32]) -> Option<&MarketRoute> {
        self.0.get(chain_market)
    }
}

/// The kernel registry of every perps and spot activity the ingester sends.
///
/// # Errors
///
/// Returns the registry construction refusal.
pub fn trading_registry() -> Result<ModuleRegistry, IngestError> {
    let bad = |error: layerx_types::payload::PayloadError| {
        IngestError::Configuration(format!("trading registry: {error:?}"))
    };
    let types = |module: ModuleId, last: u16| -> Result<ModuleRegistration, IngestError> {
        let kinds = (1..=last)
            .map(|ordinal| ActivityType::new(module, ordinal))
            .collect::<Result<Vec<_>, _>>()
            .map_err(bad)?;
        ModuleRegistration::new(module, &kinds).map_err(bad)
    };
    ModuleRegistry::new(&[types(ModuleId::Perps, 11)?, types(ModuleId::Spot, 5)?]).map_err(bad)
}

/// The kernel activity an intent maps to, or the reason it maps to none.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum Plan {
    Submit {
        activity_type: ActivityType,
        payload: Vec<u8>,
    },
    Refuse(String),
    Skip(String),
}

/// Outcome recorded for one intent.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum Outcome {
    Settled {
        activity_id: [u8; 32],
        receipt: String,
    },
    Refused(String),
    Skipped(String),
}

/// One signed kernel activity.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SignedActivity {
    pub canonical: Vec<u8>,
    pub activity_id: [u8; 32],
}

/// Envelope scope the ingester signs under.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Scope {
    pub protocol_version: u16,
    pub network_id: u32,
    pub fee_limit: u128,
}

/// The ingester's kernel identity: an Ed25519 owner key and its DID.
pub struct Signer {
    key: SigningKey,
    did: String,
}

impl Signer {
    #[must_use]
    pub fn new(seed: [u8; 32]) -> Self {
        let key = SigningKey::from_bytes(&seed);
        let did = format!(
            "did:layerx:{}",
            hex(&key.verifying_key().to_bytes())
                .trim_start_matches("0x")
                .to_owned()
        );
        Self { key, did }
    }

    /// Reads a hex seed (with or without `0x`) from the key file.
    ///
    /// # Errors
    ///
    /// Refuses an unreadable file or a seed that is not 32 bytes.
    pub fn from_file(path: &Path) -> Result<Self, IngestError> {
        let text = fs::read_to_string(path)?;
        let text = text.trim();
        let prefixed = if text.starts_with("0x") {
            text.to_owned()
        } else {
            format!("0x{text}")
        };
        let seed: [u8; 32] = unhex(&prefixed)
            .ok()
            .and_then(|bytes| bytes.try_into().ok())
            .ok_or_else(|| IngestError::Configuration("signer key is not 32 bytes".to_owned()))?;
        Ok(Self::new(seed))
    }

    #[must_use]
    pub fn did(&self) -> &str {
        &self.did
    }

    #[must_use]
    pub fn public_key(&self) -> [u8; 32] {
        self.key.verifying_key().to_bytes()
    }

    /// Builds and signs the activity at `account_sequence`, keyed by the
    /// intent id so a resubmission is the same kernel request.
    ///
    /// # Errors
    ///
    /// Returns the envelope or wire refusal.
    pub fn sign(
        &self,
        registry: &ModuleRegistry,
        scope: Scope,
        activity_type: ActivityType,
        payload: &[u8],
        idempotency_key: [u8; 32],
        account_sequence: u64,
        now_ms: u64,
    ) -> Result<SignedActivity, IngestError> {
        let bad = |detail: String| IngestError::Malformed(format!("activity: {detail}"));
        let payload = Payload::new(registry, activity_type, payload)
            .map_err(|error| bad(format!("{error:?}")))?;
        let payload_hash = payload_hash_for(&payload).map_err(|error| bad(format!("{error:?}")))?;
        let mut builder = EnvelopeBuilder::new();
        let build = |error: layerx_types::activity::ActivityBuildError| bad(format!("{error:?}"));
        builder
            .protocol_version(scope.protocol_version)
            .map_err(build)?;
        builder.network_id(scope.network_id).map_err(build)?;
        builder.activity_type(activity_type).map_err(build)?;
        builder
            .actor_did(Did::new(self.did.as_bytes()).map_err(|error| bad(format!("{error:?}")))?)
            .map_err(build)?;
        builder
            .authority(Authority::owner(&self.public_key()).map_err(build)?)
            .map_err(build)?;
        builder.account_sequence(account_sequence).map_err(build)?;
        builder
            .timestamp_bound(
                TimestampBound::new(
                    now_ms.saturating_sub(1_000),
                    now_ms.saturating_add(ACTIVITY_VALIDITY_MS),
                )
                .map_err(build)?,
            )
            .map_err(build)?;
        builder
            .idempotency_key(IdempotencyKey::new(idempotency_key))
            .map_err(build)?;
        builder
            .fee_limit(Amount::from_u128(scope.fee_limit))
            .map_err(build)?;
        builder.payload_hash(payload_hash).map_err(build)?;
        builder.payload(payload).map_err(build)?;
        let envelope = builder.build().map_err(build)?;
        let unsigned =
            encode_unsigned_envelope(&envelope).map_err(|error| bad(format!("{error:?}")))?;
        let digest = SignatureMessage::new(
            Domain::SignaturePreimage,
            scope.protocol_version,
            scope.network_id,
            &unsigned,
        )
        .map_err(|error| bad(format!("{error:?}")))?
        .digest();
        let signature = Signature::new(&self.key.sign(&digest).to_bytes())
            .map_err(|error| bad(format!("{error:?}")))?;
        let canonical = encode_signed_envelope(&envelope.attach_signature(signature))
            .map_err(|error| bad(format!("{error:?}")))?;
        let decoded =
            decode_signed(&canonical, registry).map_err(|error| bad(format!("{error:?}")))?;
        let activity_id = activity_id(&decoded).map_err(|error| bad(format!("{error:?}")))?;
        Ok(SignedActivity {
            canonical,
            activity_id,
        })
    }
}

fn spot_order(event: &OrderPlaced, route: &MarketRoute) -> Plan {
    let MarketRoute::Spot {
        kernel_market_id,
        base_account_id,
        quote_account_id,
    } = route
    else {
        return Plan::Refuse("spot_route_mismatch".to_owned());
    };
    let side = match event.side {
        1 => TradeSide::Buy,
        2 => TradeSide::Sell,
        _ => return Plan::Refuse("side_out_of_range".to_owned()),
    };
    let (Some(price), Some(quantity)) = (
        layerx_intents::precompile::uint256_to_u128(&event.price),
        layerx_intents::precompile::uint256_to_u128(&event.quantity),
    ) else {
        return Plan::Refuse("amount_too_wide".to_owned());
    };
    let (kind, time_in_force) = match (event.time_in_force, price) {
        (0, _) => (SpotOrderKind::Limit, SpotTimeInForce::GoodTilCancelled),
        (1, 0) => (SpotOrderKind::Market, SpotTimeInForce::ImmediateOrCancel),
        (1, _) => (SpotOrderKind::Limit, SpotTimeInForce::ImmediateOrCancel),
        _ => return Plan::Refuse("time_in_force_unsupported_on_spot".to_owned()),
    };
    let payload = SpotPayload::OrderPlace {
        market_id: *kernel_market_id,
        order_id: event.intent_id,
        base_account_id: *base_account_id,
        quote_account_id: *quote_account_id,
        side,
        kind,
        time_in_force,
        price,
        quantity,
    };
    spot(&payload)
}

fn spot(payload: &SpotPayload) -> Plan {
    match (
        payload.encode(),
        ActivityType::new(ModuleId::Spot, payload.ordinal()),
    ) {
        (Ok(bytes), Ok(activity_type)) => Plan::Submit {
            activity_type,
            payload: bytes,
        },
        (Err(error), _) => Plan::Refuse(format!("spot_payload:{error:?}")),
        (_, Err(error)) => Plan::Refuse(format!("spot_activity:{error:?}")),
    }
}

fn perps(registry: &ModuleRegistry, event: PrecompileEvent, binding: RouteBinding) -> Plan {
    let intent = match route_event(event, binding) {
        Ok(intent) => intent,
        Err(error) => return Plan::Refuse(format!("route:{error:?}")),
    };
    if !matches!(
        intent.kind(),
        IntentKind::ExchangeOrder(_)
            | IntentKind::ExchangeCancel(_)
            | IntentKind::ExchangeSettle(_)
    ) {
        return Plan::Refuse("route:not_a_perps_intent".to_owned());
    }
    match compile(&intent, registry) {
        Ok(compiled) => Plan::Submit {
            activity_type: compiled.activity_type(),
            payload: compiled.payload().as_bytes().to_vec(),
        },
        Err(error) => Plan::Refuse(format!("compile:{:?}:{:?}", error.field, error.reason)),
    }
}

/// Plans the kernel activity for one journaled intent. `order_markets`
/// names the chain market of every order placed so far (by order id), which
/// cancels and settlements need because their events carry none.
#[must_use]
pub fn plan(
    registry: &ModuleRegistry,
    markets: &MarketMap,
    order_markets: &HashMap<[u8; 32], [u8; 32]>,
    intent: &IngestedIntent,
) -> Plan {
    let routed = |chain_market: &[u8; 32]| {
        markets
            .get(chain_market)
            .ok_or_else(|| Plan::Skip(format!("market {} not in market map", hex(chain_market))))
    };
    match &intent.event {
        PrecompileEvent::OrderPlaced(event) => match routed(&event.market_id) {
            Err(skip) => skip,
            Ok(MarketRoute::Perps {
                kernel_market_id,
                owner_account_id,
                perps_abi_version,
            }) => {
                let mut event = event.clone();
                event.market_id = *kernel_market_id;
                perps(
                    registry,
                    PrecompileEvent::OrderPlaced(event),
                    RouteBinding::MarketVersioned {
                        market_id: *kernel_market_id,
                        owner_account_id: *owner_account_id,
                        perps_abi_version: *perps_abi_version,
                    },
                )
            }
            Ok(route) => spot_order(event, route),
        },
        PrecompileEvent::OrderCancelRequested(event) => {
            let Some(chain_market) = order_markets.get(&event.order_id) else {
                return Plan::Skip(format!(
                    "order {} has no known market",
                    hex(&event.order_id)
                ));
            };
            match routed(chain_market) {
                Err(skip) => skip,
                Ok(MarketRoute::Perps {
                    kernel_market_id,
                    owner_account_id,
                    ..
                }) => perps(
                    registry,
                    intent.event.clone(),
                    RouteBinding::Market {
                        market_id: *kernel_market_id,
                        owner_account_id: *owner_account_id,
                    },
                ),
                Ok(MarketRoute::Spot {
                    kernel_market_id, ..
                }) => spot(&SpotPayload::OrderCancel {
                    market_id: *kernel_market_id,
                    order_id: event.order_id,
                }),
            }
        }
        PrecompileEvent::SettlementRequested(event) => {
            let Some(chain_market) = order_markets.get(&event.position_id) else {
                return Plan::Skip(format!(
                    "position {} has no known market",
                    hex(&event.position_id)
                ));
            };
            match routed(chain_market) {
                Err(skip) => skip,
                Ok(MarketRoute::Perps {
                    kernel_market_id,
                    owner_account_id,
                    ..
                }) => perps(
                    registry,
                    intent.event.clone(),
                    RouteBinding::Market {
                        market_id: *kernel_market_id,
                        owner_account_id: *owner_account_id,
                    },
                ),
                Ok(MarketRoute::Spot { .. }) => {
                    Plan::Refuse("settlement_has_no_spot_activity".to_owned())
                }
            }
        }
        other => Plan::Skip(format!("{:?} is not a kernel trading intent", other.kind())),
    }
}

fn chain_owner(event: &PrecompileEvent) -> Option<[u8; 20]> {
    match event {
        PrecompileEvent::OrderPlaced(inner) => Some(inner.owner),
        PrecompileEvent::OrderCancelRequested(inner) => Some(inner.owner),
        PrecompileEvent::SettlementRequested(inner) => Some(inner.owner),
        PrecompileEvent::MarginDeposited(inner) => Some(inner.owner),
        PrecompileEvent::MarginWithdrawalRequested(inner) => Some(inner.owner),
        _ => None,
    }
}

/// JSON-RPC to the gateway with its API key as a bearer credential.
pub struct GatewayRpc {
    host: String,
    port: u16,
    path: String,
    api_key: String,
    timeout: Duration,
}

impl GatewayRpc {
    /// # Errors
    ///
    /// Refuses a URL that is not `http://host[:port][/path]`.
    pub fn new(url: &str, api_key: String, timeout: Duration) -> Result<Self, IngestError> {
        let bad = || IngestError::Configuration(format!("gateway url: {url}"));
        let rest = url.strip_prefix("http://").ok_or_else(bad)?;
        let (authority, path) = rest
            .find('/')
            .map_or((rest, "/"), |index| (&rest[..index], &rest[index..]));
        let (host, port) = match authority.rsplit_once(':') {
            Some((host, port)) => (host, port.parse::<u16>().map_err(|_| bad())?),
            None => (authority, 80),
        };
        if host.is_empty() {
            return Err(bad());
        }
        Ok(Self {
            host: host.to_owned(),
            port,
            path: path.to_owned(),
            api_key,
            timeout,
        })
    }
}

impl JsonRpc for GatewayRpc {
    fn call(&self, method: &str, params: Value) -> Result<Value, IngestError> {
        let transport = |error: std::io::Error| IngestError::Transport(error.to_string());
        let address = (self.host.as_str(), self.port)
            .to_socket_addrs()
            .map_err(transport)?
            .next()
            .ok_or_else(|| IngestError::Transport(format!("no address for {}", self.host)))?;
        let mut stream = TcpStream::connect_timeout(&address, self.timeout).map_err(transport)?;
        stream
            .set_read_timeout(Some(self.timeout))
            .map_err(transport)?;
        stream
            .set_write_timeout(Some(self.timeout))
            .map_err(transport)?;
        let body =
            json!({"jsonrpc": "2.0", "id": 1, "method": method, "params": params}).to_string();
        let request = format!(
            "POST {} HTTP/1.0\r\nHost: {}:{}\r\nAuthorization: Bearer {}\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{body}",
            self.path,
            self.host,
            self.port,
            self.api_key,
            body.len()
        );
        stream.write_all(request.as_bytes()).map_err(transport)?;
        let mut reply = Vec::new();
        stream
            .take(16 * 1024 * 1024)
            .read_to_end(&mut reply)
            .map_err(transport)?;
        let split = reply
            .windows(4)
            .position(|window| window == b"\r\n\r\n")
            .ok_or_else(|| IngestError::Malformed("no http header terminator".to_owned()))?;
        let value: Value = serde_json::from_slice(&reply[split + 4..])
            .map_err(|error| IngestError::Malformed(error.to_string()))?;
        if let Some(error) = value.get("error") {
            return Err(IngestError::Rpc {
                code: error
                    .get("code")
                    .and_then(Value::as_i64)
                    .unwrap_or_default(),
                message: error
                    .get("message")
                    .and_then(Value::as_str)
                    .unwrap_or_default()
                    .to_owned(),
            });
        }
        let head = String::from_utf8_lossy(&reply[..split]);
        let status = head.split_whitespace().nth(1).unwrap_or_default();
        if status != "200" {
            return Err(IngestError::Transport(format!("http status {status}")));
        }
        value
            .get("result")
            .cloned()
            .ok_or_else(|| IngestError::Malformed("reply without result".to_owned()))
    }
}

/// Submits journaled intents to the kernel and records each outcome once.
pub struct Submitter<G> {
    gateway: G,
    signer: Signer,
    scope: Scope,
    registry: ModuleRegistry,
    markets: MarketMap,
    path: PathBuf,
    recorded: HashSet<[u8; 32]>,
    order_markets: HashMap<[u8; 32], [u8; 32]>,
}

impl<G: JsonRpc> Submitter<G> {
    /// Opens `settlement.jsonl` under `state_dir`.
    ///
    /// # Errors
    ///
    /// Refuses an unreadable settlement journal or registry.
    pub fn open(
        gateway: G,
        signer: Signer,
        scope: Scope,
        markets: MarketMap,
        state_dir: &Path,
    ) -> Result<Self, IngestError> {
        let path = state_dir.join("settlement.jsonl");
        let mut recorded = HashSet::new();
        if path.exists() {
            for line in BufReader::new(File::open(&path)?).lines() {
                let line = line?;
                if line.trim().is_empty() {
                    continue;
                }
                let value: Value = serde_json::from_str(&line)
                    .map_err(|error| IngestError::Malformed(format!("settlement line: {error}")))?;
                let id: [u8; 32] = value
                    .get("id")
                    .and_then(Value::as_str)
                    .and_then(|text| unhex(text).ok())
                    .and_then(|bytes| bytes.try_into().ok())
                    .ok_or_else(|| IngestError::Malformed("settlement id".to_owned()))?;
                recorded.insert(id);
            }
        }
        Ok(Self {
            gateway,
            signer,
            scope,
            registry: trading_registry()?,
            markets,
            path,
            recorded,
            order_markets: HashMap::new(),
        })
    }

    #[must_use]
    pub fn is_recorded(&self, id: &[u8; 32]) -> bool {
        self.recorded.contains(id)
    }

    /// Learns the market of an order so later cancels and settlements
    /// against it can be routed; called for every journaled intent in order.
    pub fn observe(&mut self, intent: &IngestedIntent) {
        if let PrecompileEvent::OrderPlaced(event) = &intent.event {
            self.order_markets.insert(event.intent_id, event.market_id);
        }
    }

    fn next_sequence(&self) -> Result<u64, IngestError> {
        let answer = self
            .gateway
            .call("lx_getSequence", json!([self.signer.did(), "identity"]))?;
        answer
            .get("next_sequence")
            .and_then(Value::as_str)
            .filter(|text| !text.is_empty() && text.bytes().all(|byte| byte.is_ascii_digit()))
            .and_then(|text| text.parse().ok())
            .ok_or_else(|| IngestError::Malformed(format!("next_sequence in {answer}")))
    }

    fn record(&mut self, intent: &IngestedIntent, outcome: &Outcome) -> Result<(), IngestError> {
        let owner = chain_owner(&intent.event).map(|owner| hex(&owner));
        let line = match outcome {
            Outcome::Settled {
                activity_id,
                receipt,
            } => json!({"id": hex(&intent.id), "state": "settled",
                "activity_id": hex(activity_id), "receipt": receipt, "chain_owner": owner}),
            Outcome::Refused(code) => json!({"id": hex(&intent.id), "state": "refused",
                "code": code, "chain_owner": owner}),
            Outcome::Skipped(reason) => json!({"id": hex(&intent.id), "state": "skipped",
                "reason": reason, "chain_owner": owner}),
        };
        let mut file = OpenOptions::new()
            .create(true)
            .append(true)
            .open(&self.path)?;
        file.write_all(format!("{line}\n").as_bytes())?;
        file.sync_data()?;
        self.recorded.insert(intent.id);
        Ok(())
    }

    /// Submits one intent unless it is already recorded. `Ok(None)` means it
    /// was recorded before; an `Err` leaves it unrecorded for a retry.
    ///
    /// # Errors
    ///
    /// Returns a transport failure or a transient gateway answer, and a state
    /// write failure.
    pub fn submit(
        &mut self,
        intent: &IngestedIntent,
        now_ms: u64,
    ) -> Result<Option<Outcome>, IngestError> {
        self.observe(intent);
        if self.recorded.contains(&intent.id) {
            return Ok(None);
        }
        let outcome = match plan(&self.registry, &self.markets, &self.order_markets, intent) {
            Plan::Skip(reason) => Outcome::Skipped(reason),
            Plan::Refuse(code) => Outcome::Refused(code),
            Plan::Submit {
                activity_type,
                payload,
            } => {
                let sequence = self.next_sequence()?;
                match self.signer.sign(
                    &self.registry,
                    self.scope,
                    activity_type,
                    &payload,
                    intent.id,
                    sequence,
                    now_ms,
                ) {
                    Err(error) => Outcome::Refused(format!("sign:{error}")),
                    Ok(signed) => match self.gateway.call(
                        "lx_sendActivity",
                        json!([hex(&signed.canonical), COMMITMENT]),
                    ) {
                        Ok(result) => match result.get("receipt").and_then(Value::as_str) {
                            Some(receipt) if !receipt.is_empty() => Outcome::Settled {
                                activity_id: signed.activity_id,
                                receipt: receipt.to_owned(),
                            },
                            _ => {
                                return Err(IngestError::Malformed(
                                    "lx_sendActivity result without receipt".to_owned(),
                                ))
                            }
                        },
                        Err(IngestError::Rpc { code, .. }) if !TRANSIENT_CODES.contains(&code) => {
                            Outcome::Refused(format!("rpc:{code}"))
                        }
                        Err(error) => return Err(error),
                    },
                }
            }
        };
        self.record(intent, &outcome)?;
        Ok(Some(outcome))
    }
}

/// Milliseconds since the Unix epoch.
#[must_use]
pub fn now_ms() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_or(0, |elapsed| {
            u64::try_from(elapsed.as_millis()).unwrap_or(u64::MAX)
        })
}

#[cfg(test)]
mod tests {
    use super::*;
    use layerx_intent_ingester::{Config, Ingester, Journal};
    use layerx_intents::precompile::{PrecompileEventKind, EXCHANGE_PRECOMPILE};
    use std::net::TcpListener;
    use std::sync::{Arc, Mutex};

    const VECTORS: &str = include_str!("../../../../tests/fixtures/trading-payloads/vectors.json");
    const SCOPE: Scope = Scope {
        protocol_version: layerx_wire::limits::STATE_COMMITMENT_PROTOCOL_VERSION,
        network_id: 7,
        fee_limit: 1_000,
    };
    const CHAIN_PERPS: [u8; 32] = [0xa1; 32];
    const CHAIN_SPOT: [u8; 32] = [0xa2; 32];

    fn checked<T, E: std::fmt::Debug>(value: Result<T, E>) -> T {
        match value {
            Ok(value) => value,
            Err(error) => panic!("{error:?}"),
        }
    }

    fn vector(name: &str) -> (u32, Vec<u8>) {
        let marker = format!("\"name\": \"{name}\"");
        let line = VECTORS
            .lines()
            .find(|line| line.contains(&marker))
            .unwrap_or_else(|| panic!("missing vector {name}"));
        let field = |key: &str| -> String {
            let tag = format!("\"{key}\": ");
            let start = line.find(&tag).unwrap_or_else(|| panic!("{name} {key}")) + tag.len();
            let rest = &line[start..];
            let end = rest.find([',', '}']).unwrap_or(rest.len());
            rest[..end].trim_matches('"').to_owned()
        };
        (
            checked(field("activity_type").parse()),
            checked(unhex(&format!("0x{}", field("bytes")))),
        )
    }

    fn word(value: u128) -> [u8; 32] {
        let mut out = [0_u8; 32];
        out[16..].copy_from_slice(&value.to_be_bytes());
        out
    }

    fn markets() -> MarketMap {
        checked(MarketMap::parse(
            &json!({
                hex(&CHAIN_PERPS): {"module": "perps", "kernel_market_id": hex(&[0x11; 32]),
                    "owner_account_id": hex(&[0x32; 32])},
                hex(&CHAIN_SPOT): {"module": "spot", "kernel_market_id": hex(&[0x71; 32]),
                    "base_account_id": hex(&[0x82; 32]), "quote_account_id": hex(&[0x83; 32])},
            })
            .to_string(),
        ))
    }

    fn data(words: &[[u8; 32]]) -> Vec<u8> {
        words.concat()
    }

    fn address_word(address: [u8; 20]) -> [u8; 32] {
        let mut out = [0_u8; 32];
        out[12..].copy_from_slice(&address);
        out
    }

    /// Ingests real exchange logs through `Ingester::poll` against a local
    /// node, so the intents under test are the journaled ones.
    fn ingest(
        dir: &Path,
        logs: Vec<(PrecompileEventKind, Vec<[u8; 32]>, Vec<u8>)>,
    ) -> Vec<IngestedIntent> {
        let listener = checked(TcpListener::bind("127.0.0.1:0"));
        let address = checked(listener.local_addr());
        let logs: Vec<Value> = logs
            .into_iter()
            .enumerate()
            .map(|(index, (kind, mut topics, data))| {
                topics.insert(0, kind.topic0());
                json!({"address": hex(&EXCHANGE_PRECOMPILE), "blockNumber": "0x5",
                    "transactionHash": hex(&[0x77; 32]), "logIndex": format!("0x{index:x}"),
                    "topics": topics.iter().map(|topic| hex(topic)).collect::<Vec<_>>(),
                    "data": hex(&data), "removed": false})
            })
            .collect();
        std::thread::spawn(move || {
            for stream in listener.incoming() {
                let Ok(mut stream) = stream else { continue };
                let request = read_request(&mut stream);
                let result = match request["method"].as_str() {
                    Some("eth_blockNumber") => json!("0x5"),
                    _ => Value::Array(logs.clone()),
                };
                reply(
                    &mut stream,
                    &json!({"jsonrpc": "2.0", "id": 1, "result": result}),
                );
            }
        });
        let rpc = checked(layerx_intent_ingester::HttpRpc::new(
            &format!("http://{address}/"),
            Duration::from_secs(5),
        ));
        let mut config = Config::new(dir.to_owned());
        config.start_block = Some(5);
        let mut ingester = checked(Ingester::open(rpc, &config));
        checked(ingester.poll());
        checked(Journal::read_all(&dir.join("journal.jsonl")))
    }

    fn read_request(stream: &mut TcpStream) -> Value {
        let mut buffer = Vec::new();
        let mut chunk = [0_u8; 4096];
        loop {
            let read = checked(stream.read(&mut chunk));
            buffer.extend_from_slice(&chunk[..read]);
            if let Some(split) = buffer.windows(4).position(|window| window == b"\r\n\r\n") {
                let head = String::from_utf8_lossy(&buffer[..split]).to_lowercase();
                let length: usize = head
                    .lines()
                    .find_map(|line| line.strip_prefix("content-length:"))
                    .map_or(0, |value| checked(value.trim().parse()));
                while buffer.len() < split + 4 + length {
                    let read = checked(stream.read(&mut chunk));
                    if read == 0 {
                        break;
                    }
                    buffer.extend_from_slice(&chunk[..read]);
                }
                let mut value: Value = checked(serde_json::from_slice(&buffer[split + 4..]));
                value["authorization"] = json!(head
                    .lines()
                    .find_map(|line| line.strip_prefix("authorization:"))
                    .map(str::trim));
                return value;
            }
            if read == 0 {
                return Value::Null;
            }
        }
    }

    fn reply(stream: &mut TcpStream, body: &Value) {
        let body = body.to_string();
        let _ = stream.write_all(
            format!(
                "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
                body.len()
            )
            .as_bytes(),
        );
    }

    #[derive(Default)]
    struct Kernel {
        sequence: u64,
        accepted: Vec<(u32, Vec<u8>, [u8; 32])>,
        sends: usize,
    }

    /// A gateway that verifies every signed activity with the real wire
    /// decoder and Ed25519 verifier, refuses spot cancels with -32602, and
    /// otherwise executes the activity at the signer's next sequence.
    fn gateway(public_key: [u8; 32], api_key: &str) -> (GatewayRpc, Arc<Mutex<Kernel>>) {
        let listener = checked(TcpListener::bind("127.0.0.1:0"));
        let address = checked(listener.local_addr());
        let kernel = Arc::new(Mutex::new(Kernel::default()));
        let state = Arc::clone(&kernel);
        let expected = format!("bearer {}", api_key.to_lowercase());
        std::thread::spawn(move || {
            let registry = checked(trading_registry());
            for stream in listener.incoming() {
                let Ok(mut stream) = stream else { continue };
                let request = read_request(&mut stream);
                let mut kernel = state
                    .lock()
                    .unwrap_or_else(std::sync::PoisonError::into_inner);
                let answer = if request["authorization"].as_str() != Some(expected.as_str()) {
                    json!({"error": {"code": -32002, "message": "unauthorized"}})
                } else if request["method"] == "lx_getSequence" {
                    json!({"result": {"next_sequence": kernel.sequence.to_string()}})
                } else {
                    kernel.sends += 1;
                    let canonical = checked(unhex(request["params"][0].as_str().unwrap_or("")));
                    let activity = checked(decode_signed(&canonical, &registry));
                    let unsigned = checked(layerx_wire::activity::encode_unsigned(&activity));
                    let signature: [u8; 64] =
                        checked(activity.signature().unwrap_or_default().try_into());
                    checked(layerx_crypto::ed25519::verify(
                        &public_key,
                        &signature,
                        checked(SignatureMessage::new(
                            Domain::SignaturePreimage,
                            activity.protocol_version(),
                            activity.network_id(),
                            &unsigned,
                        )),
                    ));
                    assert_eq!(activity.account_sequence(), kernel.sequence);
                    assert_eq!(request["params"][1], COMMITMENT);
                    if activity.activity_type().value() == 0x000a_0003 {
                        json!({"error": {"code": -32602, "message": "Invalid canonical activity"}})
                    } else {
                        kernel.sequence += 1;
                        let id = checked(activity_id(&activity));
                        kernel.accepted.push((
                            activity.activity_type().value(),
                            activity.payload().to_vec(),
                            activity.idempotency_key(),
                        ));
                        json!({"result": {"activity_id": hex(&id), "receipt": format!("receipt-{}", hex(&id))}})
                    }
                };
                let mut body = answer;
                body["jsonrpc"] = json!("2.0");
                body["id"] = json!(1);
                reply(&mut stream, &body);
            }
        });
        (
            checked(GatewayRpc::new(
                &format!("http://{address}/rpc"),
                api_key.to_owned(),
                Duration::from_secs(5),
            )),
            kernel,
        )
    }

    fn state_dir(name: &str) -> PathBuf {
        let dir = std::env::temp_dir().join(format!(
            "layerx-intent-submit-{name}-{}-{}",
            std::process::id(),
            now_ms()
        ));
        let _ = fs::remove_dir_all(&dir);
        checked(fs::create_dir_all(&dir));
        dir
    }

    fn order(
        intent: [u8; 32],
        market: [u8; 32],
        side: u8,
        price: u128,
        quantity: u128,
    ) -> (PrecompileEventKind, Vec<[u8; 32]>, Vec<u8>) {
        (
            PrecompileEventKind::OrderPlaced,
            vec![intent, market, address_word([0x55; 20])],
            data(&[
                word(u128::from(side)),
                word(price),
                word(quantity),
                word(0),
                word(9),
            ]),
        )
    }

    fn cancel(
        intent: [u8; 32],
        order_id: [u8; 32],
    ) -> (PrecompileEventKind, Vec<[u8; 32]>, Vec<u8>) {
        (
            PrecompileEventKind::OrderCancelRequested,
            vec![intent, order_id, address_word([0x55; 20])],
            data(&[word(10)]),
        )
    }

    #[test]
    fn submit_encodes_orders_to_the_kernel_trading_vectors() {
        let dir = state_dir("vectors");
        let mut logs = vec![order([0x31; 32], CHAIN_PERPS, 2, (1_u128 << 64) | 2, 5000)];
        logs.push(order(
            [0x81; 32],
            CHAIN_SPOT,
            1,
            250,
            (0x0102_0304_0506_0708_u128 << 64) | 0x090a_0b0c_0d0e_0f10,
        ));
        let intents = ingest(&dir, logs);
        assert_eq!(intents.len(), 2);
        let registry = checked(trading_registry());
        let empty = HashMap::new();
        for (intent, name) in intents
            .iter()
            .zip(["perps_order_place", "spot_order_place_limit"])
        {
            let (activity_type, bytes) = vector(name);
            let Plan::Submit {
                activity_type: planned,
                payload,
            } = plan(&registry, &markets(), &empty, intent)
            else {
                panic!("{name} not planned");
            };
            assert_eq!(planned.value(), activity_type, "{name}");
            assert_eq!(payload, bytes, "{name}");
        }
        let signer = Signer::new([0x09; 32]);
        let (gateway, kernel) = gateway(signer.public_key(), "ingester-key");
        let mut submitter = checked(Submitter::open(gateway, signer, SCOPE, markets(), &dir));
        for intent in &intents {
            let outcome = checked(submitter.submit(intent, now_ms()));
            assert!(
                matches!(outcome, Some(Outcome::Settled { .. })),
                "{outcome:?}"
            );
        }
        let kernel = kernel
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        let expected: Vec<_> = ["perps_order_place", "spot_order_place_limit"]
            .iter()
            .zip(&intents)
            .map(|(name, intent)| {
                let (activity_type, bytes) = vector(name);
                (activity_type, bytes, intent.id)
            })
            .collect();
        assert_eq!(kernel.accepted, expected);
        let _ = fs::remove_dir_all(&dir);
    }

    #[test]
    fn submit_is_idempotent_by_intent_id_across_restarts() {
        let dir = state_dir("idempotent");
        let intents = ingest(&dir, vec![order([0x31; 32], CHAIN_PERPS, 2, 7, 5000)]);
        let seed = [0x0a; 32];
        let (rpc, kernel) = gateway(Signer::new(seed).public_key(), "k");
        let mut submitter = checked(Submitter::open(
            rpc,
            Signer::new(seed),
            SCOPE,
            markets(),
            &dir,
        ));
        let Some(Outcome::Settled {
            activity_id: first,
            receipt,
        }) = checked(submitter.submit(&intents[0], now_ms()))
        else {
            panic!("not settled");
        };
        assert_eq!(receipt, format!("receipt-{}", hex(&first)));
        assert_eq!(checked(submitter.submit(&intents[0], now_ms())), None);
        let (rpc, _) = gateway(Signer::new(seed).public_key(), "k");
        let mut reopened = checked(Submitter::open(
            rpc,
            Signer::new(seed),
            SCOPE,
            markets(),
            &dir,
        ));
        assert!(reopened.is_recorded(&intents[0].id));
        assert_eq!(checked(reopened.submit(&intents[0], now_ms())), None);
        assert_eq!(
            kernel
                .lock()
                .unwrap_or_else(std::sync::PoisonError::into_inner)
                .sends,
            1
        );
        let lines = checked(fs::read_to_string(dir.join("settlement.jsonl")));
        assert_eq!(lines.lines().count(), 1);
        let line: Value = checked(serde_json::from_str(lines.trim()));
        assert_eq!(line["state"], "settled");
        assert_eq!(line["id"], hex(&intents[0].id));
        assert_eq!(line["chain_owner"], hex(&[0x55; 20]));
        let _ = fs::remove_dir_all(&dir);
    }

    #[test]
    fn submit_records_refusals_and_skips_with_their_code() {
        let dir = state_dir("refusal");
        let unmapped = [0xee; 32];
        let intents = ingest(
            &dir,
            vec![
                order([0x81; 32], CHAIN_SPOT, 1, 250, 400),
                cancel([0x91; 32], [0x81; 32]),
                order([0x92; 32], CHAIN_PERPS, 3, 7, 5000),
                order([0x93; 32], unmapped, 1, 7, 5000),
                cancel([0x94; 32], [0x99; 32]),
            ],
        );
        assert_eq!(intents.len(), 5);
        let seed = [0x0b; 32];
        let (rpc, kernel) = gateway(Signer::new(seed).public_key(), "k");
        let mut submitter = checked(Submitter::open(
            rpc,
            Signer::new(seed),
            SCOPE,
            markets(),
            &dir,
        ));
        let outcomes: Vec<_> = intents
            .iter()
            .map(|intent| checked(submitter.submit(intent, now_ms())))
            .collect();
        assert!(matches!(outcomes[0], Some(Outcome::Settled { .. })));
        assert_eq!(outcomes[1], Some(Outcome::Refused("rpc:-32602".to_owned())));
        assert!(
            matches!(&outcomes[2], Some(Outcome::Refused(code)) if code.starts_with("route:")),
            "{:?}",
            outcomes[2]
        );
        assert_eq!(
            outcomes[3],
            Some(Outcome::Skipped(format!(
                "market {} not in market map",
                hex(&unmapped)
            )))
        );
        assert_eq!(
            outcomes[4],
            Some(Outcome::Skipped(format!(
                "order {} has no known market",
                hex(&[0x99; 32])
            )))
        );
        assert_eq!(
            kernel
                .lock()
                .unwrap_or_else(std::sync::PoisonError::into_inner)
                .sends,
            2
        );
        let lines = checked(fs::read_to_string(dir.join("settlement.jsonl")));
        let states: Vec<Value> = lines
            .lines()
            .map(|line| checked(serde_json::from_str::<Value>(line)))
            .collect();
        assert_eq!(
            states
                .iter()
                .map(|line| line["state"].clone())
                .collect::<Vec<_>>(),
            ["settled", "refused", "refused", "skipped", "skipped"]
        );
        assert_eq!(states[1]["code"], "rpc:-32602");
        let (rpc, _) = gateway(Signer::new(seed).public_key(), "k");
        let mut reopened = checked(Submitter::open(
            rpc,
            Signer::new(seed),
            SCOPE,
            markets(),
            &dir,
        ));
        for intent in &intents {
            assert_eq!(checked(reopened.submit(intent, now_ms())), None);
        }
        let _ = fs::remove_dir_all(&dir);
    }
}
