use std::io;
use std::path::{Path, PathBuf};

use k256::ecdsa::SigningKey;
use serde_json::{json, Value};

use crate::attest::{sign_digest, signer_address, AttestorSet, Ready, SIGNATURE_LENGTH};
use crate::watch::{hex0x, keccak, parse_quantity, unhex0x, EvmError, EvmRpc, XWEB_PRECOMPILE};

/// The precompile method every fulfilment calls.
pub const FULFIL_SIGNATURE: &str = "fulfil(uint64,bytes,bytes32,uint32,bytes[])";

/// The request views the submitter reads.
pub const GET_REQUEST_SIGNATURE: &str = "getRequest(uint64)";
pub const GET_ATTESTORS_SIGNATURE: &str = "getAttestors()";

/// The EIP-1559 transaction type byte.
pub const DYNAMIC_FEE_TYPE: u8 = 2;

/// Request states the precompile records.
pub const STATUS_PENDING: u8 = 0;
pub const STATUS_FULFILLED: u8 = 1;
pub const STATUS_REFUNDED: u8 = 2;

/// The gas every transaction pays before its calldata.
const TRANSACTION_BASE_GAS: u64 = 21_000;
/// The precompile's own base charge for a transaction.
const PRECOMPILE_BASE_GAS: u64 = 30_000;
/// The calldata charge per byte, counted at the non-zero rate.
const CALLDATA_BYTE_GAS: u64 = 16;
/// The precompile's charge per attestor signature.
const SIGNATURE_GAS: u64 = 8_000;
/// What the precompile keeps beyond the callback bound to record the result.
const CALLBACK_RECORD_GAS: u64 = 10_000;

const JOURNAL_SUFFIX: &str = "json";

/// The four-byte selector of a method signature.
#[must_use]
pub fn selector(signature: &str) -> [u8; 4] {
    let hash = keccak(signature.as_bytes());
    [hash[0], hash[1], hash[2], hash[3]]
}

fn word(value: u64) -> [u8; 32] {
    let mut out = [0; 32];
    out[24..].copy_from_slice(&value.to_be_bytes());
    out
}

fn usize_word(value: usize) -> [u8; 32] {
    word(u64::try_from(value).unwrap_or(u64::MAX))
}

fn dynamic(bytes: &[u8]) -> Vec<u8> {
    let mut out = usize_word(bytes.len()).to_vec();
    out.extend_from_slice(bytes);
    out.resize(32 + bytes.len().div_ceil(32) * 32, 0);
    out
}

/// The ABI calldata of `fulfil(requestId, response, contentDigest,
/// fullLength, signatures)`.
#[must_use]
pub fn fulfil_calldata(ready: &Ready) -> Vec<u8> {
    let response = dynamic(&ready.response);
    let mut signatures = usize_word(ready.signatures.len()).to_vec();
    let element = 32 + SIGNATURE_LENGTH.div_ceil(32) * 32;
    for index in 0..ready.signatures.len() {
        signatures.extend(usize_word(ready.signatures.len() * 32 + index * element));
    }
    for signature in &ready.signatures {
        signatures.extend(dynamic(signature));
    }
    let mut out = selector(FULFIL_SIGNATURE).to_vec();
    out.extend(word(ready.request_id));
    out.extend(usize_word(5 * 32));
    out.extend(ready.content_digest);
    out.extend(word(u64::from(ready.full_length)));
    out.extend(usize_word(5 * 32 + response.len()));
    out.extend(response);
    out.extend(signatures);
    out
}

/// The gas limit of a fulfil transaction: the intrinsic cost, the
/// precompile's base, calldata and per-signature charges, the callback bound
/// and what the precompile keeps to record the result.
#[must_use]
pub fn fulfil_gas_limit(calldata: &[u8], signatures: usize, callback_gas: u64) -> u64 {
    let length = u64::try_from(calldata.len()).unwrap_or(u64::MAX);
    let count = u64::try_from(signatures).unwrap_or(u64::MAX);
    TRANSACTION_BASE_GAS
        .saturating_add(CALLDATA_BYTE_GAS.saturating_mul(length))
        .saturating_add(PRECOMPILE_BASE_GAS)
        .saturating_add(CALLDATA_BYTE_GAS.saturating_mul(length.saturating_sub(4)))
        .saturating_add(SIGNATURE_GAS.saturating_mul(count))
        .saturating_add(callback_gas)
        .saturating_add(CALLBACK_RECORD_GAS)
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Fees {
    pub max_fee_per_gas: u128,
    pub max_priority_fee_per_gas: u128,
}

/// One EIP-1559 transaction with no value and an empty access list.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Transaction {
    pub chain_id: u64,
    pub nonce: u64,
    pub fees: Fees,
    pub gas_limit: u64,
    pub to: [u8; 20],
    pub data: Vec<u8>,
}

/// A signed transaction: the raw type-2 envelope and its hash.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SignedTransaction {
    pub raw: Vec<u8>,
    pub hash: [u8; 32],
}

fn rlp_length(length: usize, short: u8, out: &mut Vec<u8>) -> Result<(), SubmitError> {
    if length < 56 {
        out.push(short + u8::try_from(length).map_err(|_| SubmitError::Encode)?);
    } else {
        let bytes = length.to_be_bytes();
        let skip = bytes.iter().take_while(|byte| **byte == 0).count();
        out.push(short + 55 + u8::try_from(bytes.len() - skip).map_err(|_| SubmitError::Encode)?);
        out.extend_from_slice(&bytes[skip..]);
    }
    Ok(())
}

fn rlp_bytes(bytes: &[u8], out: &mut Vec<u8>) -> Result<(), SubmitError> {
    if bytes.len() == 1 && bytes[0] < 128 {
        out.push(bytes[0]);
    } else {
        rlp_length(bytes.len(), 128, out)?;
        out.extend_from_slice(bytes);
    }
    Ok(())
}

fn integer(bytes: &[u8], out: &mut Vec<u8>) -> Result<(), SubmitError> {
    let skip = bytes.iter().take_while(|byte| **byte == 0).count();
    rlp_bytes(&bytes[skip..], out)
}

fn list(payload: &[u8]) -> Result<Vec<u8>, SubmitError> {
    let mut out = Vec::new();
    rlp_length(payload.len(), 192, &mut out)?;
    out.extend_from_slice(payload);
    Ok(out)
}

impl Transaction {
    fn fields(&self) -> Result<Vec<u8>, SubmitError> {
        let mut payload = Vec::new();
        integer(&self.chain_id.to_be_bytes(), &mut payload)?;
        integer(&self.nonce.to_be_bytes(), &mut payload)?;
        integer(
            &self.fees.max_priority_fee_per_gas.to_be_bytes(),
            &mut payload,
        )?;
        integer(&self.fees.max_fee_per_gas.to_be_bytes(), &mut payload)?;
        integer(&self.gas_limit.to_be_bytes(), &mut payload)?;
        rlp_bytes(&self.to, &mut payload)?;
        integer(&[], &mut payload)?;
        rlp_bytes(&self.data, &mut payload)?;
        payload.push(192);
        Ok(payload)
    }

    /// Signs the transaction with `key`: the type byte and the RLP list of
    /// the fields, then the parity, `r` and `s`.
    ///
    /// # Errors
    /// Refuses a zero chain id or gas limit, a priority fee above the fee
    /// cap and a signature that does not recover the key.
    pub fn sign(&self, key: &SigningKey) -> Result<SignedTransaction, SubmitError> {
        if self.chain_id == 0
            || self.gas_limit == 0
            || self.fees.max_fee_per_gas == 0
            || self.fees.max_priority_fee_per_gas > self.fees.max_fee_per_gas
        {
            return Err(SubmitError::Encode);
        }
        let mut payload = self.fields()?;
        let unsigned = [vec![DYNAMIC_FEE_TYPE], list(&payload)?].concat();
        let signature = sign_digest(key, &keccak(&unsigned)).map_err(|_| SubmitError::Sign)?;
        integer(&[signature[64] - 27], &mut payload)?;
        integer(&signature[..32], &mut payload)?;
        integer(&signature[32..64], &mut payload)?;
        let raw = [vec![DYNAMIC_FEE_TYPE], list(&payload)?].concat();
        Ok(SignedTransaction {
            hash: keccak(&raw),
            raw,
        })
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum SubmitError {
    /// The EVM endpoint failed or answered out of shape.
    Evm(EvmError),
    /// The transaction could not be encoded.
    Encode,
    /// The transaction could not be signed.
    Sign,
    /// The journal could not be read or written.
    Journal,
    /// The precompile holds a request state this sidecar does not know.
    UnknownStatus(u8),
    /// The node answered the broadcast with another transaction hash.
    HashMismatch,
    Source,
}

impl std::fmt::Display for SubmitError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Evm(error) => write!(f, "fulfil refused: {error}"),
            Self::Encode => f.write_str("fulfil refused: transaction not encodable"),
            Self::Sign => f.write_str("fulfil refused: transaction not signable"),
            Self::Journal => f.write_str("fulfil refused: journal unreadable or unwritable"),
            Self::UnknownStatus(status) => write!(f, "fulfil refused: request status {status}"),
            Self::HashMismatch => f.write_str("fulfil refused: node answered another hash"),
            Self::Source => f.write_str("fulfil refused: canonical source binding unavailable or changed"),
        }
    }
}

impl std::error::Error for SubmitError {}

impl From<EvmError> for SubmitError {
    fn from(error: EvmError) -> Self {
        Self::Evm(error)
    }
}

fn read_word(bytes: &[u8], index: usize) -> Option<&[u8]> {
    bytes.get(index.checked_mul(32)?..index.checked_add(1)?.checked_mul(32)?)
}

fn small(word: &[u8]) -> Option<u64> {
    let word: &[u8; 32] = word.try_into().ok()?;
    if word[..24].iter().any(|byte| *byte != 0) {
        return None;
    }
    let mut low = [0; 8];
    low.copy_from_slice(&word[24..]);
    Some(u64::from_be_bytes(low))
}

fn offset(bytes: &[u8], at: usize) -> Option<usize> {
    let value = usize::try_from(small(bytes.get(at..at.checked_add(32)?)?)?).ok()?;
    value.is_multiple_of(32).then_some(value)
}

/// The static words `getRequest(uint64)` answers with: the eleven fields
/// `precompiles/xweb/abi.json` declares, `id`, `requester`, `kind`,
/// `payloadHash`, `callbackGas`, `fee`, `height`, `timeoutHeight`, `status`,
/// `level` and `attestor`.
pub const REQUEST_VIEW_WORDS: usize = 11;

const REQUEST_ID_WORD: usize = 0;
const REQUEST_STATUS_WORD: usize = 8;
const REQUEST_LEVEL_WORD: usize = 9;
const REQUEST_ATTESTOR_WORD: usize = 10;

/// What the submitter reads out of a request view: the request's own id, the
/// state the precompile records, the level the request was made at and the
/// attestor a single-level request names.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RequestView {
    pub request_id: u64,
    pub status: u8,
    pub level: u8,
    pub attestor: [u8; 20],
}

fn address(word: &[u8]) -> Option<[u8; 20]> {
    let word: &[u8; 32] = word.try_into().ok()?;
    if word[..12].iter().any(|byte| *byte != 0) {
        return None;
    }
    let mut out = [0; 20];
    out.copy_from_slice(&word[12..]);
    Some(out)
}

fn byte_at(bytes: &[u8], index: usize) -> Option<u8> {
    u8::try_from(small(read_word(bytes, index)?)?).ok()
}

/// A `getRequest` answer for `request_id` decoded against the tuple the
/// precompile publishes: `None` for an answer that is not the eleven-word
/// view, that names another request or that holds a field out of shape.
#[must_use]
pub fn decode_request_view(answer: &[u8], request_id: u64) -> Option<RequestView> {
    if answer.len() != REQUEST_VIEW_WORDS * 32
        || small(read_word(answer, REQUEST_ID_WORD)?)? != request_id
    {
        return None;
    }
    Some(RequestView {
        request_id,
        status: byte_at(answer, REQUEST_STATUS_WORD)?,
        level: byte_at(answer, REQUEST_LEVEL_WORD)?,
        attestor: address(read_word(answer, REQUEST_ATTESTOR_WORD)?)?,
    })
}

/// The state of a request as `getRequest` reports it.
///
/// # Errors
/// Returns the endpoint's error and an answer that is not the request tuple.
pub fn request_status(rpc: &EvmRpc, request_id: u64) -> Result<u8, EvmError> {
    let mut data = selector(GET_REQUEST_SIGNATURE).to_vec();
    data.extend(word(request_id));
    let answer = rpc.eth_call(XWEB_PRECOMPILE, &data)?;
    decode_request_view(&answer, request_id)
        .map(|view| view.status)
        .ok_or(EvmError::Malformed)
}

pub fn request_status_at_depth(rpc: &EvmRpc, request_id: u64, confirmations: u64) -> Result<u8, EvmError> {
    if confirmations == 0 { return request_status(rpc, request_id); }
    let safe = rpc.block_number()?.checked_sub(confirmations).ok_or(EvmError::Unavailable)?;
    let mut data = selector(GET_REQUEST_SIGNATURE).to_vec();
    data.extend(word(request_id));
    let answer = rpc.call("eth_call", &json!([{"to": hex0x(&XWEB_PRECOMPILE), "data": hex0x(&data)},
        crate::watch::quantity(u128::from(safe))]))?;
    answer.as_str().and_then(unhex0x).as_deref().and_then(|bytes| decode_request_view(bytes, request_id))
        .map(|view| view.status).ok_or(EvmError::Malformed)
}

/// The registered attestor signers and the fulfil threshold as
/// `getAttestors` reports them.
///
/// # Errors
/// Returns the endpoint's error and an answer that does not decode.
pub fn attestor_set(rpc: &EvmRpc) -> Result<AttestorSet, EvmError> {
    let answer = rpc.eth_call(XWEB_PRECOMPILE, &selector(GET_ATTESTORS_SIGNATURE))?;
    decode_attestors(&answer).ok_or(EvmError::Malformed)
}

fn decode_attestors(answer: &[u8]) -> Option<AttestorSet> {
    let threshold = u32::try_from(small(read_word(answer, 1)?)?).ok()?;
    let array = offset(answer, 0)?;
    let count = usize::try_from(small(answer.get(array..array.checked_add(32)?)?)?).ok()?;
    let elements = array.checked_add(32)?;
    if count > answer.len() / 32 {
        return None;
    }
    let mut signers = Vec::with_capacity(count);
    for index in 0..count {
        let tuple = elements.checked_add(offset(answer, elements.checked_add(index * 32)?)?)?;
        let signer_word = answer.get(tuple..tuple.checked_add(32)?)?;
        if signer_word[..12].iter().any(|byte| *byte != 0) {
            return None;
        }
        let payout = tuple.checked_add(offset(answer, tuple.checked_add(32)?)?)?;
        let length = usize::try_from(small(answer.get(payout..payout.checked_add(32)?)?)?).ok()?;
        answer.get(payout.checked_add(32)?..payout.checked_add(32)?.checked_add(length)?)?;
        let mut signer = [0; 20];
        signer.copy_from_slice(&signer_word[12..]);
        signers.push(signer);
    }
    Some(AttestorSet { signers, threshold })
}

/// Where a request's fulfilment stands in the journal.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum JournalState {
    /// The signed transaction is journalled and may not be mined yet.
    Signed,
    /// This sidecar's fulfil transaction succeeded.
    Fulfilled,
    /// Another submitter fulfilled the request first.
    AlreadyFulfilled,
    /// The request was refunded before a fulfilment.
    Refunded,
    Reorged,
}

impl JournalState {
    #[must_use]
    pub const fn code(self) -> &'static str {
        match self {
            Self::Signed => "signed",
            Self::Fulfilled => "fulfilled",
            Self::AlreadyFulfilled => "already_fulfilled",
            Self::Refunded => "refunded",
            Self::Reorged => "reorged",
        }
    }

    fn parse(code: &str) -> Option<Self> {
        [
            Self::Signed,
            Self::Fulfilled,
            Self::AlreadyFulfilled,
            Self::Refunded,
            Self::Reorged,
        ]
        .into_iter()
        .find(|state| state.code() == code)
    }

    /// Whether the request needs nothing more from this sidecar.
    #[must_use]
    pub const fn completed(self) -> bool {
        !matches!(self, Self::Signed)
    }
}

/// One journal entry: the request, its state and, once signed, the nonce,
/// the raw transaction and its hash.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct JournalEntry {
    pub request_id: u64,
    pub state: JournalState,
    pub transaction: Option<(u64, SignedTransaction)>,
}

impl JournalEntry {
    fn to_json(&self) -> Value {
        let mut value = json!({
            "request_id": self.request_id,
            "state": self.state.code(),
        });
        if let (Some(object), Some((nonce, signed))) = (value.as_object_mut(), &self.transaction) {
            object.insert("nonce".to_owned(), json!(nonce));
            object.insert("raw".to_owned(), json!(hex0x(&signed.raw)));
            object.insert("hash".to_owned(), json!(hex0x(&signed.hash)));
        }
        value
    }

    fn from_json(value: &Value) -> Option<Self> {
        let object = value.as_object()?;
        let known = ["request_id", "state", "nonce", "raw", "hash"];
        if object.keys().any(|key| !known.contains(&key.as_str())) {
            return None;
        }
        let state = JournalState::parse(object.get("state")?.as_str()?)?;
        let transaction = match (object.get("nonce"), object.get("raw"), object.get("hash")) {
            (Some(nonce), Some(raw), Some(hash)) => {
                let raw = unhex0x(raw.as_str()?)?;
                let hash: [u8; 32] = unhex0x(hash.as_str()?)?.try_into().ok()?;
                if keccak(&raw) != hash {
                    return None;
                }
                Some((nonce.as_u64()?, SignedTransaction { raw, hash }))
            }
            (None, None, None) => None,
            _ => return None,
        };
        if state == JournalState::Signed && transaction.is_none() {
            return None;
        }
        Some(Self {
            request_id: object.get("request_id")?.as_u64()?,
            state,
            transaction,
        })
    }
}

/// The submitter's journal: one file per request, written and synced before
/// any broadcast, so a restart rebroadcasts the same signed bytes.
pub struct Journal {
    directory: PathBuf,
}

impl Journal {
    /// # Errors
    /// Returns the error creating the directory.
    pub fn open(directory: &Path) -> io::Result<Self> {
        std::fs::create_dir_all(directory)?;
        Ok(Self {
            directory: directory.to_path_buf(),
        })
    }

    /// The file holding a request's entry.
    #[must_use]
    pub fn path(&self, request_id: u64) -> PathBuf {
        self.directory
            .join(format!("{request_id}.{JOURNAL_SUFFIX}"))
    }

    /// A request's entry.
    ///
    /// # Errors
    /// Refuses an unreadable file and one that is not a journal entry for
    /// the request.
    pub fn load(&self, request_id: u64) -> Result<Option<JournalEntry>, SubmitError> {
        let text = match std::fs::read(self.path(request_id)) {
            Ok(text) => text,
            Err(error) if error.kind() == io::ErrorKind::NotFound => return Ok(None),
            Err(_) => return Err(SubmitError::Journal),
        };
        serde_json::from_slice(&text)
            .ok()
            .as_ref()
            .and_then(JournalEntry::from_json)
            .filter(|entry| entry.request_id == request_id)
            .map(Some)
            .ok_or(SubmitError::Journal)
    }

    /// Writes an entry to a temporary file, syncs it, renames it into place
    /// and syncs the directory.
    ///
    /// # Errors
    /// Returns a write, sync or rename that failed.
    pub fn store(&self, entry: &JournalEntry) -> Result<(), SubmitError> {
        let path = self.path(entry.request_id);
        let temporary = path.with_extension("tmp");
        std::fs::write(&temporary, entry.to_json().to_string())
            .and_then(|()| std::fs::File::open(&temporary)?.sync_all())
            .and_then(|()| std::fs::rename(&temporary, &path))
            .and_then(|()| std::fs::File::open(&self.directory)?.sync_all())
            .map_err(|_| SubmitError::Journal)
    }

    fn remove(&self, request_id: u64) -> Result<(), SubmitError> {
        std::fs::remove_file(self.path(request_id))
            .and_then(|()| std::fs::File::open(&self.directory)?.sync_all())
            .map_err(|_| SubmitError::Journal)
    }

    /// Every request with a journalled entry, ascending.
    ///
    /// # Errors
    /// Returns an unreadable directory.
    pub fn requests(&self) -> Result<Vec<u64>, SubmitError> {
        let mut ids = Vec::new();
        for entry in std::fs::read_dir(&self.directory).map_err(|_| SubmitError::Journal)? {
            let name = entry.map_err(|_| SubmitError::Journal)?.file_name();
            let Some(id) = name
                .to_str()
                .and_then(|name| name.strip_suffix(".json"))
                .and_then(|id| id.parse().ok())
            else {
                continue;
            };
            ids.push(id);
        }
        ids.sort_unstable();
        Ok(ids)
    }
}

/// What one submit or confirm step did.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Outcome {
    /// The signed fulfil transaction was broadcast and is not mined yet.
    Sent { hash: [u8; 32] },
    /// The fulfil transaction succeeded.
    Fulfilled,
    /// Another submitter fulfilled the request first: recorded as completed.
    AlreadyFulfilled,
    /// The request was refunded: nothing is submitted.
    Refunded,
    /// The fulfil transaction reverted while the request is still pending;
    /// the journal entry is dropped so the next round signs afresh.
    Reverted,
    Reorged,
}

impl Outcome {
    /// Whether the request needs nothing more from this sidecar.
    #[must_use]
    pub const fn completed(self) -> bool {
        !matches!(self, Self::Sent { .. } | Self::Reverted)
    }
}

/// Each rebroadcast request with the result of its broadcast.
pub type Resumed = Vec<(u64, Result<Outcome, SubmitError>)>;

/// Posts fulfil through the precompile with the submitter key.
pub struct Submitter {
    rpc: EvmRpc,
    key: SigningKey,
    address: [u8; 20],
    chain_id: u64,
    journal: Journal,
    confirmations: u64,
}

impl Submitter {
    /// # Errors
    /// Returns the error opening the journal directory.
    pub fn open(
        rpc: EvmRpc,
        key: SigningKey,
        chain_id: u64,
        journal_dir: &Path,
    ) -> io::Result<Self> {
        Ok(Self {
            rpc,
            address: signer_address(&key),
            key,
            chain_id,
            journal: Journal::open(journal_dir)?,
            confirmations: 0,
        })
    }

    /// Records a fulfil as settled only once its receipt's block is
    /// `confirmations` blocks deep, the depth the request watcher follows.
    #[must_use]
    pub const fn with_confirmations(mut self, confirmations: u64) -> Self {
        self.confirmations = confirmations;
        self
    }

    /// The address the submitter pays gas from.
    #[must_use]
    pub const fn address(&self) -> [u8; 20] {
        self.address
    }

    #[must_use]
    pub const fn journal(&self) -> &Journal {
        &self.journal
    }

    pub fn bind_source(&self, request: &crate::watch::WebRequest, source: &crate::watch::SourceIdentity) -> Result<(), SubmitError> {
        if source.chain_id != self.chain_id { return Err(SubmitError::Source); }
        let path = self.journal.directory.join(format!("{}.source.json", request.request_id));
        let value = json!({"request": request, "source": source});
        match std::fs::read(&path) {
            Ok(bytes) => {
                let old: Value = serde_json::from_slice(&bytes).map_err(|_| SubmitError::Journal)?;
                if old != value { return Err(SubmitError::Source); }
                return Ok(());
            }
            Err(error) if error.kind() == io::ErrorKind::NotFound => {},
            Err(_) => return Err(SubmitError::Journal),
        }
        let temporary = path.with_extension("tmp");
        std::fs::write(&temporary, value.to_string())
            .and_then(|()| std::fs::File::open(&temporary)?.sync_all())
            .and_then(|()| std::fs::rename(&temporary, &path))
            .and_then(|()| std::fs::File::open(&self.journal.directory)?.sync_all())
            .map_err(|_| SubmitError::Journal)
    }

    fn canonical_source(&self, request_id: u64) -> Result<bool, SubmitError> {
        if self.confirmations == 0 { return Ok(true); }
        let path = self.journal.directory.join(format!("{request_id}.source.json"));
        let value: Value = serde_json::from_slice(&std::fs::read(path).map_err(|_| SubmitError::Source)?)
            .map_err(|_| SubmitError::Source)?;
        let request: crate::watch::WebRequest = serde_json::from_value(value["request"].clone()).map_err(|_| SubmitError::Source)?;
        let source: crate::watch::SourceIdentity = serde_json::from_value(value["source"].clone()).map_err(|_| SubmitError::Source)?;
        if request.request_id != request_id || source.chain_id != self.chain_id
            || self.rpc.quantity("eth_chainId", &json!([]))? != u128::from(self.chain_id) {
            return Err(SubmitError::Source);
        }
        if self.rpc.block_number()? < request.block_number.saturating_add(self.confirmations) {
            return Err(EvmError::Unavailable.into());
        }
        let logs = self.rpc.call("eth_getLogs", &json!([{
            "address": hex0x(&XWEB_PRECOMPILE),
            "fromBlock": crate::watch::quantity(u128::from(request.block_number)),
            "toBlock": crate::watch::quantity(u128::from(request.block_number)),
            "topics": [hex0x(&crate::watch::requested_topic())],
        }]))?;
        for log in logs.as_array().ok_or(EvmError::Malformed)? {
            if crate::watch::decode_requested(log)? == request
                && crate::watch::source_identity(log, self.chain_id)? == source {
                return Ok(true);
            }
        }
        Ok(false)
    }

    pub fn acknowledge_closed(&self, request_id: u64) -> Result<bool, SubmitError> {
        if let Some(outcome) = self.confirm(request_id)? {
            if outcome.completed() { return Ok(true); }
        }
        let status = request_status_at_depth(&self.rpc, request_id, self.confirmations)?;
        self.closed(request_id, status).map(|outcome| outcome.is_some_and(Outcome::completed))
    }

    pub fn abandon(&self, request_id: u64) -> Result<(), SubmitError> {
        let transaction = self.journal.load(request_id)?.and_then(|entry| entry.transaction);
        self.journal.store(&JournalEntry { request_id, state: JournalState::Reorged, transaction })
    }

    fn settle(&self, request_id: u64, state: JournalState) -> Result<Outcome, SubmitError> {
        let transaction = self.journal.load(request_id)?.and_then(|entry| entry.transaction);
        self.journal.store(&JournalEntry { request_id, state, transaction })?;
        Ok(match state {
            JournalState::Refunded => Outcome::Refunded,
            _ => Outcome::AlreadyFulfilled,
        })
    }

    /// Records a request the precompile no longer holds as pending.
    fn closed(&self, request_id: u64, status: u8) -> Result<Option<Outcome>, SubmitError> {
        if status != STATUS_PENDING && self.confirmations > 0
            && request_status_at_depth(&self.rpc, request_id, self.confirmations)? != status {
            return Err(EvmError::Unavailable.into());
        }
        match status {
            STATUS_PENDING => Ok(None),
            STATUS_FULFILLED => self
                .settle(request_id, JournalState::AlreadyFulfilled)
                .map(Some),
            STATUS_REFUNDED => self.settle(request_id, JournalState::Refunded).map(Some),
            other => Err(SubmitError::UnknownStatus(other)),
        }
    }

    fn broadcast(
        &self,
        request_id: u64,
        signed: &SignedTransaction,
    ) -> Result<Outcome, SubmitError> {
        if !self.canonical_source(request_id)? {
            self.abandon(request_id)?;
            return Ok(Outcome::Reorged);
        }
        let status = request_status(&self.rpc, request_id)?;
        if let Some(outcome) = self.closed(request_id, status)? { return Ok(outcome); }
        match self
            .rpc
            .call("eth_sendRawTransaction", &json!([hex0x(&signed.raw)]))
        {
            Ok(answer) => {
                let hash = answer.as_str().and_then(unhex0x);
                if hash.as_deref() != Some(signed.hash.as_slice()) {
                    return Err(SubmitError::HashMismatch);
                }
                Ok(Outcome::Sent { hash: signed.hash })
            }
            Err(EvmError::Rejected { code }) => {
                let status = request_status(&self.rpc, request_id)?;
                match self.closed(request_id, status)? {
                    Some(outcome) => Ok(outcome),
                    None => Err(SubmitError::Evm(EvmError::Rejected { code })),
                }
            }
            Err(error) => Err(SubmitError::Evm(error)),
        }
    }

    /// Rebroadcasts every journalled transaction that is signed and not yet
    /// settled, byte for byte as it was signed.
    ///
    /// # Errors
    /// Returns an unreadable journal. Each request's own result is returned
    /// beside its id.
    pub fn resume(&self) -> Result<Resumed, SubmitError> {
        let mut results = Vec::new();
        for request_id in self.journal.requests()? {
            let Some(entry) = self.journal.load(request_id)? else {
                continue;
            };
            if let (JournalState::Signed, Some((_, signed))) = (entry.state, &entry.transaction) {
                let outcome = match self.confirm(request_id) {
                    Ok(Some(outcome)) => Ok(outcome),
                    Ok(None) => self.broadcast(request_id, signed),
                    Err(error) => Err(error),
                };
                results.push((request_id, outcome));
            }
        }
        Ok(results)
    }

    fn fees(&self) -> Result<Fees, SubmitError> {
        let tip = self.rpc.quantity("eth_maxPriorityFeePerGas", &json!([]))?;
        let block = self
            .rpc
            .call("eth_getBlockByNumber", &json!(["latest", false]))?;
        let base = block
            .get("baseFeePerGas")
            .and_then(Value::as_str)
            .and_then(parse_quantity)
            .ok_or(EvmError::Malformed)?;
        let max_fee = base
            .checked_mul(2)
            .and_then(|doubled| doubled.checked_add(tip))
            .ok_or(EvmError::Malformed)?;
        Ok(Fees {
            max_fee_per_gas: max_fee,
            max_priority_fee_per_gas: tip,
        })
    }

    /// Posts fulfil for a request whose signatures reached the threshold.
    /// A journalled request is rebroadcast or left settled; a request the
    /// precompile already fulfilled is recorded as completed; otherwise the
    /// transaction is signed, journalled and synced, and only then
    /// broadcast.
    ///
    /// # Errors
    /// Refuses signatures out of ascending signer order and returns the
    /// endpoint, signing and journal failures.
    pub fn submit(&self, ready: &Ready) -> Result<Outcome, SubmitError> {
        if ready.signers.len() != ready.signatures.len()
            || ready.signers.windows(2).any(|pair| pair[0] >= pair[1])
        {
            return Err(SubmitError::Encode);
        }
        let request_id = ready.request_id;
        if let Some(entry) = self.journal.load(request_id)? {
            return match (entry.state, &entry.transaction) {
                (JournalState::Signed, Some((_, signed))) => match self.confirm(request_id)? {
                    Some(outcome) => Ok(outcome),
                    None => self.broadcast(request_id, signed),
                },
                (JournalState::Reorged, _) => Ok(Outcome::Reorged),
                (JournalState::Fulfilled, _) => Ok(Outcome::Fulfilled),
                (JournalState::Refunded, _) => Ok(Outcome::Refunded),
                _ => Ok(Outcome::AlreadyFulfilled),
            };
        }
        if !self.canonical_source(request_id)? {
            self.abandon(request_id)?;
            return Ok(Outcome::Reorged);
        }
        let status = request_status(&self.rpc, request_id)?;
        if let Some(outcome) = self.closed(request_id, status)? { return Ok(outcome); }
        let nonce = u64::try_from(self.rpc.quantity(
            "eth_getTransactionCount",
            &json!([hex0x(&self.address), "pending"]),
        )?)
        .map_err(|_| EvmError::Malformed)?;
        let fees = self.fees()?;
        let data = fulfil_calldata(ready);
        let transaction = Transaction {
            chain_id: self.chain_id,
            nonce,
            fees,
            gas_limit: fulfil_gas_limit(&data, ready.signatures.len(), ready.callback_gas),
            to: XWEB_PRECOMPILE,
            data,
        };
        let signed = transaction.sign(&self.key)?;
        self.journal.store(&JournalEntry {
            request_id,
            state: JournalState::Signed,
            transaction: Some((nonce, signed.clone())),
        })?;
        self.broadcast(request_id, &signed)
    }

    /// Reads the receipt of a journalled transaction. `None` means it is not
    /// mined yet or nothing is journalled.
    ///
    /// # Errors
    /// Returns the endpoint and journal failures.
    pub fn confirm(&self, request_id: u64) -> Result<Option<Outcome>, SubmitError> {
        let Some(entry) = self.journal.load(request_id)? else {
            return Ok(None);
        };
        if entry.state == JournalState::Reorged { return Ok(Some(Outcome::Reorged)); }
        if !self.canonical_source(request_id)? {
            self.abandon(request_id)?;
            return Ok(Some(Outcome::Reorged));
        }
        if entry.state != JournalState::Signed && self.confirmations > 0 {
            let expected = if entry.state == JournalState::Refunded { STATUS_REFUNDED } else { STATUS_FULFILLED };
            if request_status_at_depth(&self.rpc, request_id, self.confirmations)? != expected {
                return Err(SubmitError::Source);
            }
        }
        let (JournalState::Signed, Some((nonce, signed))) = (entry.state, entry.transaction) else {
            return Ok(Some(match entry.state {
                JournalState::Fulfilled => Outcome::Fulfilled,
                JournalState::Refunded => Outcome::Refunded,
                _ => Outcome::AlreadyFulfilled,
            }));
        };
        let receipt = self
            .rpc
            .call("eth_getTransactionReceipt", &json!([hex0x(&signed.hash)]))?;
        if receipt.is_null() {
            let status = request_status(&self.rpc, request_id)?;
            return self.closed(request_id, status);
        }
        if self.confirmations > 0 {
            if receipt.get("transactionHash").and_then(Value::as_str).and_then(unhex0x)
                .as_deref() != Some(signed.hash.as_slice()) { return Err(SubmitError::HashMismatch); }
            let mined = receipt.get("blockNumber").and_then(Value::as_str).and_then(parse_quantity)
                .and_then(|value| u64::try_from(value).ok()).ok_or(EvmError::Malformed)?;
            if self.rpc.block_number()? < mined.saturating_add(self.confirmations) { return Ok(None); }
            let block = self.rpc.call("eth_getBlockByNumber", &json!([crate::watch::quantity(u128::from(mined)), false]))?;
            let receipt_hash = receipt.get("blockHash").and_then(Value::as_str).and_then(unhex0x).ok_or(EvmError::Malformed)?;
            if receipt_hash.len() != 32 || block.get("hash").and_then(Value::as_str).and_then(unhex0x) != Some(receipt_hash) {
                return Ok(None);
            }
        }
        match receipt.get("status").and_then(Value::as_str) {
            Some("0x1") => {
                self.journal.store(&JournalEntry {
                    request_id,
                    state: JournalState::Fulfilled,
                    transaction: Some((nonce, signed)),
                })?;
                Ok(Some(Outcome::Fulfilled))
            }
            Some("0x0") => {
                let status = request_status(&self.rpc, request_id)?;
                if let Some(outcome) = self.closed(request_id, status)? {
                    return Ok(Some(outcome));
                }
                self.journal.remove(request_id)?;
                Ok(Some(Outcome::Reverted))
            }
            _ => Err(SubmitError::Evm(EvmError::Malformed)),
        }
    }
}

#[cfg(test)]
mod tests {
    use std::error::Error;
    use std::path::PathBuf;

    use serde_json::Value;

    use crate::api::{LEVEL_MAJORITY, LEVEL_SINGLE};

    use super::{
        decode_request_view, RequestView, REQUEST_ATTESTOR_WORD, REQUEST_ID_WORD,
        REQUEST_LEVEL_WORD, REQUEST_STATUS_WORD, REQUEST_VIEW_WORDS, STATUS_FULFILLED,
        STATUS_PENDING,
    };

    type Checked<T = ()> = Result<T, Box<dyn Error>>;

    const REQUEST_ID: u64 = 7;
    const REQUESTER: [u8; 20] = [0x0a; 20];
    const ATTESTOR: [u8; 20] = [0xa7; 20];

    fn fail(message: impl Into<String>) -> Box<dyn Error> {
        message.into().into()
    }

    /// The types of the `getRequest` tuple `precompiles/xweb/abi.json`
    /// publishes, in the order the precompile packs them.
    fn published_fields() -> Checked<Vec<String>> {
        let path =
            PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../../precompiles/xweb/abi.json");
        let abi: Value = serde_json::from_slice(&std::fs::read(&path)?)?;
        let entry = abi
            .as_array()
            .ok_or_else(|| fail("the published abi is not an array"))?
            .iter()
            .find(|entry| entry.get("name").and_then(Value::as_str) == Some("getRequest"))
            .ok_or_else(|| fail("the published abi declares no getRequest"))?;
        let components = entry
            .pointer("/outputs/0/components")
            .and_then(Value::as_array)
            .ok_or_else(|| fail("getRequest does not return a tuple"))?;
        Ok(components
            .iter()
            .map(|field| {
                field
                    .get("type")
                    .and_then(Value::as_str)
                    .unwrap_or_default()
                    .to_owned()
            })
            .collect())
    }

    /// One word per published field, in the abi's order.
    fn encoded_view(fields: &[String], status: u8, level: u8) -> Checked<Vec<u8>> {
        let mut out = Vec::with_capacity(fields.len() * 32);
        for (index, field) in fields.iter().enumerate() {
            let mut slot = [0_u8; 32];
            if field == "address" {
                let value = if index == REQUEST_ATTESTOR_WORD {
                    &ATTESTOR
                } else {
                    &REQUESTER
                };
                slot[12..].copy_from_slice(value);
            } else if index == REQUEST_STATUS_WORD {
                slot[31] = status;
            } else if index == REQUEST_LEVEL_WORD {
                slot[31] = level;
            } else if index == REQUEST_ID_WORD {
                slot[24..].copy_from_slice(&REQUEST_ID.to_be_bytes());
            } else {
                slot[24..].copy_from_slice(&u64::try_from(index)?.to_be_bytes());
            }
            out.extend_from_slice(&slot);
        }
        Ok(out)
    }

    #[test]
    fn request_status_reads_the_eleven_field_request_view() -> Checked {
        let fields = published_fields()?;
        assert_eq!(
            fields,
            [
                "uint64", "address", "uint8", "bytes32", "uint64", "uint256", "uint64", "uint64",
                "uint8", "uint8", "address"
            ]
        );
        assert_eq!(fields.len(), REQUEST_VIEW_WORDS);
        let answer = encoded_view(&fields, STATUS_FULFILLED, LEVEL_SINGLE)?;
        assert_eq!(answer.len(), 352);
        assert_eq!(
            decode_request_view(&answer, REQUEST_ID),
            Some(RequestView {
                request_id: REQUEST_ID,
                status: STATUS_FULFILLED,
                level: LEVEL_SINGLE,
                attestor: ATTESTOR,
            })
        );
        assert_eq!(
            decode_request_view(
                &encoded_view(&fields, STATUS_PENDING, LEVEL_MAJORITY)?,
                REQUEST_ID
            ),
            Some(RequestView {
                request_id: REQUEST_ID,
                status: STATUS_PENDING,
                level: LEVEL_MAJORITY,
                attestor: ATTESTOR,
            })
        );
        Ok(())
    }

    #[test]
    fn request_status_refuses_an_answer_that_is_not_the_published_view() -> Checked {
        let fields = published_fields()?;
        let answer = encoded_view(&fields, STATUS_FULFILLED, LEVEL_SINGLE)?;
        for words in [0_usize, 9, 10] {
            assert_eq!(decode_request_view(&answer[..words * 32], REQUEST_ID), None);
        }
        assert_eq!(
            decode_request_view(&answer[..answer.len() - 1], REQUEST_ID),
            None
        );
        let mut longer = answer.clone();
        longer.extend_from_slice(&[0; 32]);
        assert_eq!(decode_request_view(&longer, REQUEST_ID), None);
        assert_eq!(decode_request_view(&answer, REQUEST_ID + 1), None);
        let mut dirty = answer;
        dirty[REQUEST_ATTESTOR_WORD * 32] = 1;
        assert_eq!(decode_request_view(&dirty, REQUEST_ID), None);
        Ok(())
    }
}
