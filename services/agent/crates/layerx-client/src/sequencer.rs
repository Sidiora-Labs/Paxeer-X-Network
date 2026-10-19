//! Sequencer JSON-RPC 2.0 endpoint.
//!
//! One HTTPS connection to the unified router (or a sequencer node) serving
//! the pending-tag reads, committed blocks by height, `eth_sendRawTransaction`,
//! `pax_sequencerReceipt` and the router's `px_getReceiptKeys`. Refusals keep the JSON-RPC code, message
//! and data exactly as the server sent them; transport failures are reported
//! as [`SequencerError::Unavailable`] so a caller can map an interrupted send
//! to an unknown outcome.

use std::collections::HashSet;
use std::fmt::Write as _;
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::Duration;

use serde_json::{json, Map, Value};

/// The `addr` precompile serving the `LayerX` DID binding reads.
pub const ADDR_PRECOMPILE: [u8; 20] = [
    0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x10, 0x04,
];

/// Selector of `getEvmAddrByLayerX(bytes32)`.
pub const GET_EVM_ADDR_BY_LAYERX: [u8; 4] = [0xc4, 0x13, 0xba, 0xd3];

/// JSON-RPC error code of an EVM revert carrying its reason in `data`.
pub const REVERT_CODE: i64 = 3;

const DID_NOT_BOUND: &str = "LXP_ADDR_DID_NOT_BOUND:";
const ERROR_SELECTOR: [u8; 4] = [0x08, 0xc3, 0x79, 0xa0];
const REQUEST_TIMEOUT: Duration = Duration::from_secs(30);
const MAX_RESPONSE_BYTES: u64 = 16 * 1_048_576;
const MAX_LOG_TOPICS: usize = 4;
const MAX_FEE_DENOM_BYTES: usize = 64;

/// Why a sequencer call produced no answer.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum SequencerError {
    /// The endpoint URL or trust anchors are unusable.
    Endpoint,
    /// The TLS session could not be established or authenticated; nothing was
    /// sent.
    Tls,
    /// The request timed out, the connection failed or closed, or a proxy
    /// reported the upstream unreachable (HTTP 502, 503 or 504). A send may or
    /// may not have reached the sequencer.
    Unavailable,
    /// The server answered with an HTTP status other than 200.
    HttpStatus(u16),
    /// The server refused the call with a JSON-RPC error.
    Rpc(RpcRefusal),
    /// The answer is not the JSON-RPC response or result shape the method
    /// defines.
    Malformed,
}

/// A JSON-RPC error object exactly as the server sent it.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct RpcRefusal {
    /// The error code.
    pub code: i64,
    /// The error message.
    pub message: String,
    /// The error data; `None` when the member is absent, `Some(Value::Null)`
    /// when it is an explicit null.
    pub data: Option<Value>,
}

/// A call evaluated against the sequencer's pending state.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct CallRequest {
    /// Sender; omitted when `None`.
    pub from: Option<[u8; 20]>,
    /// Target; `None` estimates a contract creation.
    pub to: Option<[u8; 20]>,
    /// Value in wei; omitted when zero.
    pub value: u128,
    /// Call data.
    pub data: Vec<u8>,
}

/// One log of a sequencer receipt.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct SequencerLog {
    /// Emitting contract.
    pub address: [u8; 20],
    /// Indexed topics, at most four.
    pub topics: Vec<[u8; 32]>,
    /// Unindexed data.
    pub data: Vec<u8>,
    /// Log index.
    pub log_index: u64,
}

/// The execution fields of a receipted or committed transaction.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct ExecutedReceipt {
    /// Transaction hash, the receipt's `activity_id`.
    pub tx_hash: [u8; 32],
    /// Transaction nonce.
    pub nonce: u64,
    /// Gap-free sequence position.
    pub seq: u64,
    /// Height of the block the receipt is issued for.
    pub height: u64,
    /// Position of the transaction in that block.
    pub index: u64,
    /// Receipt timestamp.
    pub timestamp: u64,
    /// `true` for status 1 (success), `false` for status 0 (reverted).
    pub success: bool,
    /// EVM return data.
    pub return_data: Vec<u8>,
    /// Emitted logs.
    pub logs: Vec<SequencerLog>,
    /// Gas used by the transaction.
    pub gas_used: u64,
    /// Gas used up to and including the transaction in its block.
    pub cumulative_gas_used: u64,
    /// Effective gas price.
    pub effective_gas_price: u64,
    /// Denomination the fee was charged in.
    pub fee_denom: String,
    /// Fee charged, a 256-bit big-endian amount.
    pub fee_amount: [u8; 32],
    /// Fee sponsor; the zero address when unsponsored.
    pub sponsor: [u8; 20],
    /// Sender.
    pub from: [u8; 20],
    /// Recipient; the zero address for a contract creation.
    pub to: [u8; 20],
    /// Created contract; the zero address otherwise.
    pub contract_address: [u8; 20],
    /// EVM error text; empty on success.
    pub vm_error: String,
    /// Receipt key that signed the receipt.
    pub key_id: [u8; 32],
    /// Canonical signed receipt bytes.
    pub receipt: Vec<u8>,
}

/// The answer of `pax_sequencerReceipt` for a known transaction.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum SequencerReceipt {
    /// Held by the sequencer without a receipt (future nonce).
    Submitted {
        /// Transaction hash.
        tx_hash: [u8; 32],
        /// Transaction nonce.
        nonce: u64,
    },
    /// Signed receipt issued; its batch is not committed yet.
    Receipted(ExecutedReceipt),
    /// Signed receipt whose batch is covered by a committed block.
    Committed {
        /// The receipt, byte-identical to the receipted rung.
        receipt: ExecutedReceipt,
        /// Committed block covering the receipt's sequence position.
        block_number: u64,
        /// Hash of that block.
        block_hash: [u8; 32],
    },
}

/// One pinned receipt key of the sequencer `ReceiptKeys` list.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct ReceiptKeyEntry {
    /// Non-zero key id receipts carry as `context_hash`.
    pub key_id: [u8; 32],
    /// Ed25519 public key.
    pub public_key: [u8; 32],
    /// First block height the key signs, inclusive.
    pub first_height: u64,
    /// Last block height the key signs, inclusive.
    pub last_height: u64,
}

/// A committed block with every transaction as its raw signed bytes.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct SequencerBlock {
    /// Block height.
    pub number: u64,
    /// Block hash.
    pub hash: [u8; 32],
    /// Hash of the parent block.
    pub parent_hash: [u8; 32],
    /// Block timestamp in seconds.
    pub timestamp: u64,
    /// Root the header commits over the block's transactions.
    pub transactions_root: [u8; 32],
    /// The signed transactions in block index order, each in the encoding
    /// `eth_sendRawTransaction` takes.
    pub transactions: Vec<Vec<u8>>,
}

/// A JSON-RPC 2.0 client for the sequencer methods.
#[derive(Debug)]
pub struct SequencerEndpoint {
    agent: ureq::Agent,
    url: String,
    next_id: AtomicU64,
}

impl SequencerEndpoint {
    /// Opens an endpoint at `url`.
    ///
    /// An `https` URL authenticates the server against `trust_anchors`, the
    /// DER certificates of the roots the operator pins. Plain `http` is
    /// accepted only for a loopback host.
    ///
    /// # Errors
    /// [`SequencerError::Endpoint`] for another scheme, a non-loopback plain
    /// URL, an `https` URL without trust anchors or an empty anchor.
    pub fn new(url: &str, trust_anchors: &[&[u8]]) -> Result<Self, SequencerError> {
        let uri: ureq::http::Uri = url.parse().map_err(|_| SequencerError::Endpoint)?;
        let host = uri.host().ok_or(SequencerError::Endpoint)?;
        match uri.scheme_str() {
            Some("https") if !trust_anchors.is_empty() => {}
            Some("http") if matches!(host, "127.0.0.1" | "[::1]" | "localhost") => {}
            _ => return Err(SequencerError::Endpoint),
        }
        if trust_anchors.iter().any(|anchor| anchor.is_empty()) {
            return Err(SequencerError::Endpoint);
        }
        let roots: Vec<_> = trust_anchors
            .iter()
            .map(|anchor| ureq::tls::Certificate::from_der(anchor).to_owned())
            .collect();
        let agent = ureq::Agent::config_builder()
            .timeout_global(Some(REQUEST_TIMEOUT))
            .http_status_as_error(false)
            .max_redirects(0)
            .tls_config(
                ureq::tls::TlsConfig::builder()
                    .provider(ureq::tls::TlsProvider::Rustls)
                    .root_certs(ureq::tls::RootCerts::new_with_certs(&roots))
                    .build(),
            )
            .build()
            .into();
        Ok(Self {
            agent,
            url: url.to_owned(),
            next_id: AtomicU64::new(1),
        })
    }

    /// Next nonce of `address` from the sequencer's pending nonce map.
    ///
    /// # Errors
    /// Transport, refusal and decode failures as [`SequencerError`].
    pub fn pending_nonce(&self, address: &[u8; 20]) -> Result<u64, SequencerError> {
        let result = self.call("eth_getTransactionCount", &json!([hex(address), "pending"]))?;
        quantity_u64(&result).ok_or(SequencerError::Malformed)
    }

    /// Gas estimate of `call` against the pending state.
    ///
    /// # Errors
    /// Transport, refusal and decode failures as [`SequencerError`].
    pub fn estimate_gas(&self, call: &CallRequest) -> Result<u64, SequencerError> {
        let mut object = Map::new();
        if let Some(from) = &call.from {
            object.insert("from".to_owned(), Value::String(hex(from)));
        }
        if let Some(to) = &call.to {
            object.insert("to".to_owned(), Value::String(hex(to)));
        }
        if call.value != 0 {
            object.insert(
                "value".to_owned(),
                Value::String(format!("{:#x}", call.value)),
            );
        }
        object.insert("data".to_owned(), Value::String(hex(&call.data)));
        let result = self.call("eth_estimateGas", &json!([object, "pending"]))?;
        quantity_u64(&result).ok_or(SequencerError::Malformed)
    }

    /// Return data of a read-only call of `to` with `data` at the pending tag.
    ///
    /// # Errors
    /// Transport, refusal (a revert keeps code, message and data) and decode
    /// failures as [`SequencerError`].
    pub fn call_pending(&self, to: &[u8; 20], data: &[u8]) -> Result<Vec<u8>, SequencerError> {
        self.eth_call(to, data, "pending")
    }

    /// Return data of a read-only call of `to` with `data` against the
    /// committed state at block `height`.
    ///
    /// # Errors
    /// Transport, refusal (a revert keeps code, message and data) and decode
    /// failures as [`SequencerError`].
    pub fn call_at(
        &self,
        height: u64,
        to: &[u8; 20],
        data: &[u8],
    ) -> Result<Vec<u8>, SequencerError> {
        self.eth_call(to, data, &format!("{height:#x}"))
    }

    fn eth_call(&self, to: &[u8; 20], data: &[u8], block: &str) -> Result<Vec<u8>, SequencerError> {
        let result = self.call(
            "eth_call",
            &json!([{"to": hex(to), "data": hex(data)}, block]),
        )?;
        result
            .as_str()
            .and_then(bytes)
            .ok_or(SequencerError::Malformed)
    }

    /// Sends a signed raw transaction and returns its hash.
    ///
    /// # Errors
    /// [`SequencerError::Unavailable`] when the outcome of the send is
    /// unknown, including an answer that is not a usable JSON-RPC response
    /// after the request was sent; [`SequencerError::Rpc`] when the sequencer
    /// refused it.
    pub fn send_raw_transaction(&self, raw: &[u8]) -> Result<[u8; 32], SequencerError> {
        let result = self
            .call("eth_sendRawTransaction", &json!([hex(raw)]))
            .map_err(|error| match error {
                SequencerError::Malformed => SequencerError::Unavailable,
                error => error,
            })?;
        fixed(&result).ok_or(SequencerError::Unavailable)
    }

    /// Rung and signed receipt of `tx_hash`; `None` when the hash is unknown.
    ///
    /// # Errors
    /// Transport and refusal failures as [`SequencerError`];
    /// [`SequencerError::Malformed`] for any answer that is not null, the
    /// submitted shape or the receipted or committed shape for `tx_hash`.
    pub fn sequencer_receipt(
        &self,
        tx_hash: &[u8; 32],
    ) -> Result<Option<SequencerReceipt>, SequencerError> {
        let result = self.call("pax_sequencerReceipt", &json!([hex(tx_hash)]))?;
        if result.is_null() {
            return Ok(None);
        }
        let receipt = decode_receipt(&result).ok_or(SequencerError::Malformed)?;
        let answered = match &receipt {
            SequencerReceipt::Submitted { tx_hash, .. }
            | SequencerReceipt::Receipted(ExecutedReceipt { tx_hash, .. })
            | SequencerReceipt::Committed {
                receipt: ExecutedReceipt { tx_hash, .. },
                ..
            } => tx_hash,
        };
        if answered != tx_hash {
            return Err(SequencerError::Malformed);
        }
        Ok(Some(receipt))
    }

    /// The pinned `ReceiptKeys` list served by the router.
    ///
    /// # Errors
    /// Transport and refusal failures as [`SequencerError`];
    /// [`SequencerError::Malformed`] for an entry with a zero or repeated key
    /// id, a public key that is not an ed25519 point, or a first height above
    /// its last height.
    pub fn receipt_keys(&self) -> Result<Vec<ReceiptKeyEntry>, SequencerError> {
        let result = self.call("px_getReceiptKeys", &json!([]))?;
        let entries = result
            .as_array()
            .ok_or(SequencerError::Malformed)?
            .iter()
            .map(decode_receipt_key)
            .collect::<Option<Vec<_>>>()
            .ok_or(SequencerError::Malformed)?;
        let mut seen = HashSet::with_capacity(entries.len());
        if entries.iter().all(|entry| seen.insert(entry.key_id)) {
            Ok(entries)
        } else {
            Err(SequencerError::Malformed)
        }
    }

    /// The committed block at `height` with full transactions; `None` when the
    /// node has no block at that height.
    ///
    /// # Errors
    /// Transport and refusal failures as [`SequencerError`];
    /// [`SequencerError::Malformed`] for a block of another height, a
    /// transaction out of index order or located in another block, a
    /// transaction type other than 0 to 4, or a transaction object that is not
    /// exactly the shape of its type.
    pub fn block_by_number(&self, height: u64) -> Result<Option<SequencerBlock>, SequencerError> {
        let result = self.call(
            "eth_getBlockByNumber",
            &json!([format!("{height:#x}"), true]),
        )?;
        if result.is_null() {
            return Ok(None);
        }
        decode_block(&result, height)
            .map(Some)
            .ok_or(SequencerError::Malformed)
    }

    /// EVM address bound to the `LayerX` DID public key, read at the pending
    /// tag from the `addr` precompile; `None` when the DID is not bound.
    ///
    /// # Errors
    /// Any revert other than `LXP_ADDR_DID_NOT_BOUND` as
    /// [`SequencerError::Rpc`]; an answer that is not one ABI address word as
    /// [`SequencerError::Malformed`].
    pub fn bound_address(
        &self,
        did_public_key: &[u8; 32],
    ) -> Result<Option<[u8; 20]>, SequencerError> {
        let mut data = GET_EVM_ADDR_BY_LAYERX.to_vec();
        data.extend_from_slice(did_public_key);
        let answer = match self.call_pending(&ADDR_PRECOMPILE, &data) {
            Err(SequencerError::Rpc(refusal)) if did_not_bound(&refusal) => return Ok(None),
            other => other?,
        };
        let (padding, address) = answer
            .split_at_checked(12)
            .ok_or(SequencerError::Malformed)?;
        let address: [u8; 20] = address.try_into().map_err(|_| SequencerError::Malformed)?;
        if padding.iter().any(|byte| *byte != 0) || address == [0; 20] {
            return Err(SequencerError::Malformed);
        }
        Ok(Some(address))
    }

    fn call(&self, method: &str, params: &Value) -> Result<Value, SequencerError> {
        let id = self
            .next_id
            .fetch_update(Ordering::Relaxed, Ordering::Relaxed, |id| id.checked_add(1))
            .map_err(|_| SequencerError::Endpoint)?;
        let body = serde_json::to_vec(
            &json!({"jsonrpc": "2.0", "id": id, "method": method, "params": params}),
        )
        .map_err(|_| SequencerError::Endpoint)?;
        let mut response = self
            .agent
            .post(&self.url)
            .header("Content-Type", "application/json")
            .header("Accept", "application/json")
            .send(body.as_slice())
            .map_err(|error| transport(&error))?;
        match response.status().as_u16() {
            200 => {}
            502..=504 => return Err(SequencerError::Unavailable),
            status => return Err(SequencerError::HttpStatus(status)),
        }
        let answer = response
            .body_mut()
            .with_config()
            .limit(MAX_RESPONSE_BYTES)
            .read_to_vec()
            .map_err(|error| transport(&error))?;
        let document: Value =
            serde_json::from_slice(&answer).map_err(|_| SequencerError::Malformed)?;
        decode_response(document, id)
    }
}

fn transport(error: &ureq::Error) -> SequencerError {
    match error {
        ureq::Error::Timeout(_)
        | ureq::Error::Io(_)
        | ureq::Error::HostNotFound
        | ureq::Error::ConnectionFailed
        | ureq::Error::Protocol(_)
        | ureq::Error::BodyStalled => SequencerError::Unavailable,
        ureq::Error::Tls(_)
        | ureq::Error::Pem(_)
        | ureq::Error::Rustls(_)
        | ureq::Error::TlsRequired => SequencerError::Tls,
        ureq::Error::BodyExceedsLimit(_) | ureq::Error::LargeResponseHeader(..) => {
            SequencerError::Malformed
        }
        _ => SequencerError::Endpoint,
    }
}

fn decode_response(document: Value, id: u64) -> Result<Value, SequencerError> {
    let Value::Object(mut object) = document else {
        return Err(SequencerError::Malformed);
    };
    if object.len() != 3
        || object.get("jsonrpc").and_then(Value::as_str) != Some("2.0")
        || object.get("id").and_then(Value::as_u64) != Some(id)
    {
        return Err(SequencerError::Malformed);
    }
    if let Some(result) = object.remove("result") {
        return Ok(result);
    }
    let Some(Value::Object(mut error)) = object.remove("error") else {
        return Err(SequencerError::Malformed);
    };
    let code = error.remove("code").as_ref().and_then(Value::as_i64);
    let message = error.remove("message");
    let data = error.remove("data");
    match (code, message) {
        (Some(code), Some(Value::String(message))) if error.is_empty() => {
            Err(SequencerError::Rpc(RpcRefusal {
                code,
                message,
                data,
            }))
        }
        _ => Err(SequencerError::Malformed),
    }
}

fn did_not_bound(refusal: &RpcRefusal) -> bool {
    refusal.code == REVERT_CODE
        && refusal
            .data
            .as_ref()
            .and_then(Value::as_str)
            .and_then(bytes)
            .and_then(|data| revert_reason(&data))
            .is_some_and(|reason| reason.starts_with(DID_NOT_BOUND))
}

fn revert_reason(data: &[u8]) -> Option<String> {
    let body = data.strip_prefix(&ERROR_SELECTOR)?;
    let (offset, rest) = body.split_at_checked(32)?;
    let (length, text) = rest.split_at_checked(32)?;
    if word_u64(offset)? != 32 {
        return None;
    }
    let length = usize::try_from(word_u64(length)?).ok()?;
    let padded = length.checked_next_multiple_of(32)?;
    if text.len() != padded || text[length..].iter().any(|byte| *byte != 0) {
        return None;
    }
    String::from_utf8(text[..length].to_vec()).ok()
}

fn word_u64(word: &[u8]) -> Option<u64> {
    let (high, low) = word.split_at_checked(24)?;
    if high.iter().any(|byte| *byte != 0) {
        return None;
    }
    Some(u64::from_be_bytes(low.try_into().ok()?))
}

fn decode_receipt_key(value: &Value) -> Option<ReceiptKeyEntry> {
    let fields = Fields::exact(value, &["keyId", "publicKey", "firstHeight", "lastHeight"])?;
    let entry = ReceiptKeyEntry {
        key_id: fields.fixed("keyId")?,
        public_key: fields.fixed("publicKey")?,
        first_height: fields.u64("firstHeight")?,
        last_height: fields.u64("lastHeight")?,
    };
    let valid = entry.key_id != [0; 32]
        && entry.first_height <= entry.last_height
        && ed25519_dalek::VerifyingKey::from_bytes(&entry.public_key).is_ok();
    valid.then_some(entry)
}

const SUBMITTED_FIELDS: [&str; 3] = ["rung", "txHash", "nonce"];
const RECEIPTED_FIELDS: [&str; 22] = [
    "rung",
    "txHash",
    "nonce",
    "seq",
    "height",
    "index",
    "timestamp",
    "status",
    "returnData",
    "logs",
    "gasUsed",
    "cumulativeGasUsed",
    "effectiveGasPrice",
    "feeDenom",
    "feeAmount",
    "sponsor",
    "from",
    "to",
    "contractAddress",
    "vmError",
    "keyId",
    "receipt",
];

fn decode_receipt(value: &Value) -> Option<SequencerReceipt> {
    match value.get("rung")?.as_str()? {
        "submitted" => {
            let fields = Fields::exact(value, &SUBMITTED_FIELDS)?;
            Some(SequencerReceipt::Submitted {
                tx_hash: fields.fixed("txHash")?,
                nonce: fields.u64("nonce")?,
            })
        }
        "receipted" => Some(SequencerReceipt::Receipted(executed(&Fields::exact(
            value,
            &RECEIPTED_FIELDS,
        )?)?)),
        "committed" => {
            let mut names = RECEIPTED_FIELDS.to_vec();
            names.extend(["blockNumber", "blockHash"]);
            let fields = Fields::exact(value, &names)?;
            let receipt = executed(&fields)?;
            let block_number = fields.u64("blockNumber")?;
            (block_number >= receipt.height).then_some(())?;
            Some(SequencerReceipt::Committed {
                receipt,
                block_number,
                block_hash: fields.fixed("blockHash")?,
            })
        }
        _ => None,
    }
}

fn executed(fields: &Fields<'_>) -> Option<ExecutedReceipt> {
    let success = match fields.u64("status")? {
        0 => false,
        1 => true,
        _ => return None,
    };
    let fee_denom = fields.string("feeDenom")?;
    if fee_denom.is_empty() || fee_denom.len() > MAX_FEE_DENOM_BYTES {
        return None;
    }
    let receipt = fields.bytes("receipt")?;
    if receipt.is_empty() {
        return None;
    }
    Some(ExecutedReceipt {
        tx_hash: fields.fixed("txHash")?,
        nonce: fields.u64("nonce")?,
        seq: fields.u64("seq")?,
        height: fields.u64("height")?,
        index: fields.u64("index")?,
        timestamp: fields.u64("timestamp")?,
        success,
        return_data: fields.bytes("returnData")?,
        logs: fields
            .0
            .get("logs")?
            .as_array()?
            .iter()
            .map(decode_log)
            .collect::<Option<_>>()?,
        gas_used: fields.u64("gasUsed")?,
        cumulative_gas_used: fields.u64("cumulativeGasUsed")?,
        effective_gas_price: fields.u64("effectiveGasPrice")?,
        fee_denom,
        fee_amount: quantity_u256(fields.0.get("feeAmount")?)?,
        sponsor: fields.fixed("sponsor")?,
        from: fields.fixed("from")?,
        to: fields.fixed("to")?,
        contract_address: fields.fixed("contractAddress")?,
        vm_error: fields.string("vmError")?,
        key_id: fields.fixed("keyId")?,
        receipt,
    })
}

fn decode_log(value: &Value) -> Option<SequencerLog> {
    let fields = Fields::exact(value, &["address", "topics", "data", "logIndex"])?;
    let topics = fields.0.get("topics")?.as_array()?;
    if topics.len() > MAX_LOG_TOPICS {
        return None;
    }
    Some(SequencerLog {
        address: fields.fixed("address")?,
        topics: topics.iter().map(fixed).collect::<Option<_>>()?,
        data: fields.bytes("data")?,
        log_index: fields.u64("logIndex")?,
    })
}

fn decode_block(value: &Value, height: u64) -> Option<SequencerBlock> {
    let fields = Fields(value.as_object()?);
    let number = fields.u64("number")?;
    let hash = fields.fixed("hash")?;
    if number != height {
        return None;
    }
    let transactions = fields
        .0
        .get("transactions")?
        .as_array()?
        .iter()
        .enumerate()
        .map(|(index, transaction)| raw_transaction(transaction, number, &hash, index))
        .collect::<Option<_>>()?;
    Some(SequencerBlock {
        number,
        hash,
        parent_hash: fields.fixed("parentHash")?,
        timestamp: fields.u64("timestamp")?,
        transactions_root: fields.fixed("transactionsRoot")?,
        transactions,
    })
}

const TRANSACTION_FIELDS: [&str; 15] = [
    "blockHash",
    "blockNumber",
    "from",
    "gas",
    "gasPrice",
    "hash",
    "input",
    "nonce",
    "to",
    "transactionIndex",
    "value",
    "type",
    "v",
    "r",
    "s",
];
const LEGACY_TX: u8 = 0;
const ACCESS_LIST_TX: u8 = 1;
const DYNAMIC_FEE_TX: u8 = 2;
const BLOB_TX: u8 = 3;
const SET_CODE_TX: u8 = 4;

/// Re-encodes a full transaction object at `index` of block `number` into its
/// raw signed bytes.
fn raw_transaction(
    value: &Value,
    number: u64,
    block_hash: &[u8; 32],
    index: usize,
) -> Option<Vec<u8>> {
    let object = value.as_object()?;
    let kind = u8::try_from(quantity_u64(object.get("type")?)?).ok()?;
    let mut names = TRANSACTION_FIELDS.to_vec();
    match kind {
        LEGACY_TX if object.contains_key("chainId") => names.push("chainId"),
        LEGACY_TX => {}
        ACCESS_LIST_TX => names.extend(["accessList", "chainId", "yParity"]),
        DYNAMIC_FEE_TX => names.extend([
            "accessList",
            "chainId",
            "yParity",
            "maxFeePerGas",
            "maxPriorityFeePerGas",
        ]),
        BLOB_TX => names.extend([
            "accessList",
            "chainId",
            "yParity",
            "maxFeePerGas",
            "maxPriorityFeePerGas",
            "maxFeePerBlobGas",
            "blobVersionedHashes",
        ]),
        SET_CODE_TX => names.extend([
            "accessList",
            "chainId",
            "yParity",
            "maxFeePerGas",
            "maxPriorityFeePerGas",
            "authorizationList",
        ]),
        _ => return None,
    }
    let fields = Fields::exact(value, &names)?;
    let located = fields.fixed::<32>("blockHash")? == *block_hash
        && fields.u64("blockNumber")? == number
        && fields.u64("transactionIndex")? == u64::try_from(index).ok()?;
    fields.fixed::<20>("from")?;
    fields.fixed::<32>("hash")?;
    let r = fields.integer("r")?;
    let s = fields.integer("s")?;
    if !located || r.is_empty() || s.is_empty() {
        return None;
    }
    let to = match fields.0.get("to")? {
        Value::Null if kind < BLOB_TX => Vec::new(),
        to => fixed::<20>(to)?.to_vec(),
    };
    if kind == LEGACY_TX {
        return legacy_transaction(&fields, &to, &r, &s);
    }
    typed_transaction(&fields, kind, &to, &r, &s)
}

/// `rlp([nonce, gasPrice, gas, to, value, input, v, r, s])` with `v` matching
/// the EIP-155 chain id when one is present and 27 or 28 otherwise.
fn legacy_transaction(fields: &Fields<'_>, to: &[u8], r: &[u8], s: &[u8]) -> Option<Vec<u8>> {
    let v = quantity_u128(fields.0.get("v")?)?;
    let protected = match fields.0.get("chainId") {
        Some(chain_id) => {
            let base = u128::from(quantity_u64(chain_id)?) * 2 + 35;
            v == base || v == base + 1
        }
        None => v == 27 || v == 28,
    };
    if !protected {
        return None;
    }
    let mut payload = Vec::new();
    for name in ["nonce", "gasPrice", "gas"] {
        rlp_bytes(&fields.integer(name)?, &mut payload);
    }
    rlp_bytes(to, &mut payload);
    rlp_bytes(&fields.integer("value")?, &mut payload);
    rlp_bytes(&fields.bytes("input")?, &mut payload);
    rlp_bytes(&fields.integer("v")?, &mut payload);
    rlp_bytes(r, &mut payload);
    rlp_bytes(s, &mut payload);
    Some(rlp_list(&payload))
}

/// `kind || rlp([chainId, nonce, fees.., gas, to, value, input, accessList,
/// type fields.., yParity, r, s])` for the EIP-2718 types 1 to 4.
fn typed_transaction(
    fields: &Fields<'_>,
    kind: u8,
    to: &[u8],
    r: &[u8],
    s: &[u8],
) -> Option<Vec<u8>> {
    let y_parity = fields.u64("yParity")?;
    if y_parity > 1 || fields.u64("v")? != y_parity {
        return None;
    }
    let mut payload = Vec::new();
    rlp_bytes(&fields.integer("chainId")?, &mut payload);
    rlp_bytes(&fields.integer("nonce")?, &mut payload);
    if kind == ACCESS_LIST_TX {
        rlp_bytes(&fields.integer("gasPrice")?, &mut payload);
    } else {
        rlp_bytes(&fields.integer("maxPriorityFeePerGas")?, &mut payload);
        rlp_bytes(&fields.integer("maxFeePerGas")?, &mut payload);
    }
    rlp_bytes(&fields.integer("gas")?, &mut payload);
    rlp_bytes(to, &mut payload);
    rlp_bytes(&fields.integer("value")?, &mut payload);
    rlp_bytes(&fields.bytes("input")?, &mut payload);
    payload.extend(access_list(fields.0.get("accessList")?)?);
    if kind == BLOB_TX {
        rlp_bytes(&fields.integer("maxFeePerBlobGas")?, &mut payload);
        let hashes = fields.0.get("blobVersionedHashes")?.as_array()?;
        if hashes.is_empty() {
            return None;
        }
        let mut list = Vec::new();
        for hash in hashes {
            rlp_bytes(&fixed::<32>(hash)?, &mut list);
        }
        payload.extend(rlp_list(&list));
    }
    if kind == SET_CODE_TX {
        let authorizations = fields.0.get("authorizationList")?.as_array()?;
        if authorizations.is_empty() {
            return None;
        }
        let mut list = Vec::new();
        for authorization in authorizations {
            list.extend(authorization_tuple(authorization)?);
        }
        payload.extend(rlp_list(&list));
    }
    rlp_bytes(&fields.integer("yParity")?, &mut payload);
    rlp_bytes(r, &mut payload);
    rlp_bytes(s, &mut payload);
    let mut raw = vec![kind];
    raw.extend(rlp_list(&payload));
    Some(raw)
}

fn access_list(value: &Value) -> Option<Vec<u8>> {
    let mut payload = Vec::new();
    for entry in value.as_array()? {
        let fields = Fields::exact(entry, &["address", "storageKeys"])?;
        let mut keys = Vec::new();
        for key in fields.0.get("storageKeys")?.as_array()? {
            rlp_bytes(&fixed::<32>(key)?, &mut keys);
        }
        let mut item = Vec::new();
        rlp_bytes(&fields.fixed::<20>("address")?, &mut item);
        item.extend(rlp_list(&keys));
        payload.extend(rlp_list(&item));
    }
    Some(rlp_list(&payload))
}

fn authorization_tuple(value: &Value) -> Option<Vec<u8>> {
    let fields = Fields::exact(value, &["chainId", "address", "nonce", "yParity", "r", "s"])?;
    let y_parity = fields.integer("yParity")?;
    if y_parity.len() > 1 || fields.integer("nonce")?.len() > 8 {
        return None;
    }
    let mut item = Vec::new();
    rlp_bytes(&fields.integer("chainId")?, &mut item);
    rlp_bytes(&fields.fixed::<20>("address")?, &mut item);
    rlp_bytes(&fields.integer("nonce")?, &mut item);
    rlp_bytes(&y_parity, &mut item);
    rlp_bytes(&fields.integer("r")?, &mut item);
    rlp_bytes(&fields.integer("s")?, &mut item);
    Some(rlp_list(&item))
}

fn rlp_header(length: usize, short: u8, out: &mut Vec<u8>) {
    if let Ok(small @ 0..=55) = u8::try_from(length) {
        out.push(short + small);
        return;
    }
    let encoded = length.to_be_bytes();
    let significant = &encoded[encoded.iter().take_while(|byte| **byte == 0).count()..];
    let width = significant.iter().fold(0_u8, |width, _| width + 1);
    out.push(short + 55 + width);
    out.extend_from_slice(significant);
}

fn rlp_bytes(data: &[u8], out: &mut Vec<u8>) {
    if let [byte @ 0..=0x7f] = data {
        out.push(*byte);
    } else {
        rlp_header(data.len(), 0x80, out);
        out.extend_from_slice(data);
    }
}

fn rlp_list(payload: &[u8]) -> Vec<u8> {
    let mut out = Vec::with_capacity(payload.len() + 9);
    rlp_header(payload.len(), 0xc0, &mut out);
    out.extend_from_slice(payload);
    out
}

/// A JSON object holding exactly the expected member names.
struct Fields<'a>(&'a Map<String, Value>);

impl<'a> Fields<'a> {
    fn exact(value: &'a Value, names: &[&str]) -> Option<Self> {
        let object = value.as_object()?;
        (object.len() == names.len() && names.iter().all(|name| object.contains_key(*name)))
            .then_some(Self(object))
    }

    fn u64(&self, name: &str) -> Option<u64> {
        quantity_u64(self.0.get(name)?)
    }

    fn fixed<const N: usize>(&self, name: &str) -> Option<[u8; N]> {
        fixed(self.0.get(name)?)
    }

    fn bytes(&self, name: &str) -> Option<Vec<u8>> {
        bytes(self.0.get(name)?.as_str()?)
    }

    /// A 256-bit quantity as minimal big-endian bytes, empty for zero.
    fn integer(&self, name: &str) -> Option<Vec<u8>> {
        quantity_bytes(self.0.get(name)?)
    }

    fn string(&self, name: &str) -> Option<String> {
        self.0.get(name)?.as_str().map(str::to_owned)
    }
}

fn hex(data: &[u8]) -> String {
    let mut text = String::with_capacity(2 + data.len() * 2);
    text.push_str("0x");
    for byte in data {
        let _ = write!(text, "{byte:02x}");
    }
    text
}

fn nibble(digit: u8) -> Option<u8> {
    match digit {
        b'0'..=b'9' => Some(digit - b'0'),
        b'a'..=b'f' => Some(digit - b'a' + 10),
        b'A'..=b'F' => Some(digit - b'A' + 10),
        _ => None,
    }
}

/// `0x`-prefixed, even-length hex data.
fn bytes(text: &str) -> Option<Vec<u8>> {
    let digits = text.strip_prefix("0x")?.as_bytes();
    if digits.len() % 2 != 0 {
        return None;
    }
    digits
        .chunks_exact(2)
        .map(|pair| Some((nibble(pair[0])? << 4) | nibble(pair[1])?))
        .collect()
}

fn fixed<const N: usize>(value: &Value) -> Option<[u8; N]> {
    bytes(value.as_str()?)?.try_into().ok()
}

/// Digits of a `0x` quantity: non-empty, hex, no leading zero except `0x0`.
fn quantity_digits(value: &Value, max_digits: usize) -> Option<&str> {
    let digits = value.as_str()?.strip_prefix("0x")?;
    let canonical = !digits.is_empty()
        && digits.len() <= max_digits
        && digits.bytes().all(|digit| digit.is_ascii_hexdigit())
        && (digits == "0" || !digits.starts_with('0'));
    canonical.then_some(digits)
}

fn quantity_u64(value: &Value) -> Option<u64> {
    u64::from_str_radix(quantity_digits(value, 16)?, 16).ok()
}

fn quantity_u128(value: &Value) -> Option<u128> {
    u128::from_str_radix(quantity_digits(value, 32)?, 16).ok()
}

fn quantity_bytes(value: &Value) -> Option<Vec<u8>> {
    match quantity_digits(value, 64)? {
        "0" => Some(Vec::new()),
        digits if digits.len().is_multiple_of(2) => bytes(&format!("0x{digits}")),
        digits => bytes(&format!("0x0{digits}")),
    }
}

fn quantity_u256(value: &Value) -> Option<[u8; 32]> {
    let digits = quantity_digits(value, 64)?;
    bytes(&format!("0x{digits:0>64}"))?.try_into().ok()
}
