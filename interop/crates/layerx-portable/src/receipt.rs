use std::fmt::{Display, Formatter};

use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use base64::Engine as _;
use layerx_proof::inclusion::SequencerAuthorization;
use layerx_proof::receipt::{
    verify_outcome, verify_sequencer_signature, AuthorizedBatch, VerificationFailure,
    VerifiedReceipt,
};
use serde::{Deserialize, Serialize};
use sha2::{Digest as _, Sha256};

/// Stable media payload identifier shared with every interop adapter.
pub const PORTABLE_RECEIPT_FORMAT: &str = "layerx-receipt-proof-v1";
/// Portable form of a sequencer-signed `ProtocolReceipt` v2 verified against
/// the pinned `ReceiptKeys` list.
pub const PORTABLE_RECEIPT_FORMAT_V2: &str = "layerx-receipt-proof-v2";
const VERIFICATION_LEVEL: &str = "sequencer-signed";
const VERIFICATION_LEVEL_V2: &str = "receipted";
const RECEIPT_DIGEST_DOMAIN: &[u8] = b"LXP/v1/receipt\0";
const SIGNATURE_TAIL: [u8; 5] = [1, 0, 0, 0, 64];
const SIGNATURE_BYTES: usize = 64;
const MAX_RECEIPT_BYTES: usize = 1_048_576;
const MAX_JSON_BYTES: usize = 1_500_000;

/// Self-contained, language-neutral receipt proof.
///
/// Every byte string is unpadded base64url. The batch facts are deliberately
/// repeated outside the receipt so the verifier can compare them with its
/// independently trusted [`AuthorizedBatch`]. Carrying those fields does not
/// itself make them authoritative.
#[derive(Clone, Debug, Eq, PartialEq, Deserialize, Serialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct PortableReceipt {
    format: String,
    verification_level: String,
    canonical_receipt: String,
    receipt_digest: String,
    batch_id: String,
    asset: String,
    previous_state_root: String,
    resulting_state_root: String,
    sequencer_public_key: String,
}

impl PortableReceipt {
    /// Exports one receipt only after its exact canonical encoding, state-root
    /// chain, balance invariants and sequencer signature verify locally.
    /// Rejected protocol outcomes remain exportable as rejected outcomes;
    /// this constructor never upgrades them to success.
    ///
    /// # Errors
    ///
    /// Returns the exact receipt verification stage that failed.
    pub fn export(
        canonical_receipt: &[u8],
        authorised: &AuthorizedBatch,
    ) -> Result<Self, PortableReceiptError> {
        if canonical_receipt.is_empty() || canonical_receipt.len() > MAX_RECEIPT_BYTES {
            return Err(PortableReceiptError::ReceiptBounds);
        }
        let verified =
            verify_outcome(canonical_receipt, authorised).map_err(PortableReceiptError::Receipt)?;
        let digest = verified
            .evidence()
            .receipt_digest()
            .ok_or(PortableReceiptError::MissingReceiptDigest)?;
        Ok(Self {
            format: PORTABLE_RECEIPT_FORMAT.to_owned(),
            verification_level: VERIFICATION_LEVEL.to_owned(),
            canonical_receipt: URL_SAFE_NO_PAD.encode(canonical_receipt),
            receipt_digest: URL_SAFE_NO_PAD.encode(digest),
            batch_id: URL_SAFE_NO_PAD.encode(authorised.batch_id()),
            asset: URL_SAFE_NO_PAD.encode(authorised.asset()),
            previous_state_root: URL_SAFE_NO_PAD.encode(authorised.previous_state_root()),
            resulting_state_root: URL_SAFE_NO_PAD.encode(authorised.resulting_state_root()),
            sequencer_public_key: URL_SAFE_NO_PAD.encode(authorised.sequencer_public_key()),
        })
    }

    /// Parses the bounded JSON wire form. Parsing alone establishes no trust;
    /// callers must use [`Self::verify`] before consuming any receipt fact.
    ///
    /// # Errors
    ///
    /// Refuses oversize, malformed, extended, or non-canonical JSON fields.
    pub fn from_json(bytes: &[u8]) -> Result<Self, PortableReceiptError> {
        if bytes.is_empty() || bytes.len() > MAX_JSON_BYTES {
            return Err(PortableReceiptError::JsonBounds);
        }
        let portable: Self =
            serde_json::from_slice(bytes).map_err(|_| PortableReceiptError::JsonShape)?;
        portable.decode_fields()?;
        Ok(portable)
    }

    /// Parses and verifies one JSON proof without exposing an intermediate
    /// unverified receipt value.
    ///
    /// # Errors
    ///
    /// Returns the first wire-shape, receipt, digest or signature refusal.
    pub fn verify_json(
        bytes: &[u8],
        trusted_batch: &AuthorizedBatch,
    ) -> Result<PortableVerifiedReceipt, PortableReceiptError> {
        Self::from_json(bytes)?.verify(trusted_batch)
    }

    /// Encodes the stable JSON object. Struct field order is stable for
    /// reproducible vectors, but consumers must treat JSON member order as
    /// insignificant.
    ///
    /// # Errors
    ///
    /// Returns an encoding error if the serializer refuses the object.
    pub fn to_json(&self) -> Result<Vec<u8>, PortableReceiptError> {
        self.decode_fields()?;
        serde_json::to_vec(self).map_err(|_| PortableReceiptError::JsonShape)
    }

    /// Verifies this proof against independently trusted batch authorization
    /// using the standalone `LayerX` proof library. No gateway or live
    /// infrastructure is consulted.
    ///
    /// # Errors
    ///
    /// Refuses a format/version mismatch, non-canonical base64url, mismatched
    /// receipt digest, or any canonical receipt/signature/invariant failure.
    pub fn verify(
        &self,
        trusted_batch: &AuthorizedBatch,
    ) -> Result<PortableVerifiedReceipt, PortableReceiptError> {
        let decoded = self.decode_fields()?;
        if decoded.batch_id != trusted_batch.batch_id()
            || decoded.asset != trusted_batch.asset()
            || decoded.previous_state_root != trusted_batch.previous_state_root()
            || decoded.resulting_state_root != trusted_batch.resulting_state_root()
            || decoded.sequencer_public_key != trusted_batch.sequencer_public_key()
        {
            return Err(PortableReceiptError::BatchAuthorizationMismatch);
        }
        let verified = verify_outcome(&decoded.canonical_receipt, trusted_batch)
            .map_err(PortableReceiptError::Receipt)?;
        let actual = verified
            .evidence()
            .receipt_digest()
            .ok_or(PortableReceiptError::MissingReceiptDigest)?;
        if actual != decoded.receipt_digest {
            return Err(PortableReceiptError::ReceiptDigestMismatch);
        }
        Ok(PortableVerifiedReceipt {
            receipt: verified,
            receipt_digest: actual,
            authorised_batch: *trusted_batch,
        })
    }

    /// Returns the wire-format identifier.
    #[must_use]
    pub fn format(&self) -> &str {
        &self.format
    }

    /// Returns the encoded exact canonical receipt.
    #[must_use]
    pub fn canonical_receipt(&self) -> &str {
        &self.canonical_receipt
    }

    /// Returns the encoded receipt-signature digest.
    #[must_use]
    pub fn receipt_digest(&self) -> &str {
        &self.receipt_digest
    }

    fn decode_fields(&self) -> Result<DecodedReceipt, PortableReceiptError> {
        if self.format != PORTABLE_RECEIPT_FORMAT {
            return Err(PortableReceiptError::UnsupportedFormat);
        }
        if self.verification_level != VERIFICATION_LEVEL {
            return Err(PortableReceiptError::UnsupportedVerificationLevel);
        }
        let canonical_receipt = decode_bounded(
            "canonicalReceipt",
            &self.canonical_receipt,
            1,
            MAX_RECEIPT_BYTES,
        )?;
        Ok(DecodedReceipt {
            canonical_receipt,
            receipt_digest: decode_fixed("receiptDigest", &self.receipt_digest)?,
            batch_id: decode_fixed("batchId", &self.batch_id)?,
            asset: decode_fixed("asset", &self.asset)?,
            previous_state_root: decode_fixed("previousStateRoot", &self.previous_state_root)?,
            resulting_state_root: decode_fixed("resultingStateRoot", &self.resulting_state_root)?,
            sequencer_public_key: decode_fixed("sequencerPublicKey", &self.sequencer_public_key)?,
        })
    }
}

struct DecodedReceipt {
    canonical_receipt: Vec<u8>,
    receipt_digest: [u8; 32],
    batch_id: [u8; 32],
    asset: [u8; 32],
    previous_state_root: [u8; 32],
    resulting_state_root: [u8; 32],
    sequencer_public_key: [u8; 32],
}

/// A receipt value produced only after the complete portable proof verifies.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PortableVerifiedReceipt {
    receipt: VerifiedReceipt,
    receipt_digest: [u8; 32],
    authorised_batch: AuthorizedBatch,
}

impl PortableVerifiedReceipt {
    /// Returns the verified canonical protocol receipt.
    #[must_use]
    pub const fn receipt(&self) -> &VerifiedReceipt {
        &self.receipt
    }

    /// Returns the verified receipt-signature digest.
    #[must_use]
    pub const fn receipt_digest(&self) -> [u8; 32] {
        self.receipt_digest
    }

    /// Returns the independently supplied batch authorization facts.
    #[must_use]
    pub const fn authorised_batch(&self) -> &AuthorizedBatch {
        &self.authorised_batch
    }
}

/// Typed portable-receipt refusal.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum PortableReceiptError {
    JsonBounds,
    JsonShape,
    ReceiptBounds,
    UnsupportedFormat,
    UnsupportedVerificationLevel,
    InvalidBase64(&'static str),
    InvalidLength(&'static str),
    Receipt(VerificationFailure),
    BatchAuthorizationMismatch,
    MissingReceiptDigest,
    ReceiptDigestMismatch,
    InvalidHex(&'static str),
    InvalidQuantity(&'static str),
    ReceiptKeyMismatch,
    InclusionHeight,
    MissingBatchAuthorization,
}

impl Display for PortableReceiptError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::JsonBounds => formatter.write_str("portable receipt JSON is outside its bounds"),
            Self::JsonShape => formatter.write_str("portable receipt JSON shape is invalid"),
            Self::ReceiptBounds => formatter.write_str("canonical receipt is outside its bounds"),
            Self::UnsupportedFormat => {
                formatter.write_str("portable receipt format is unsupported")
            }
            Self::UnsupportedVerificationLevel => {
                formatter.write_str("portable receipt verification level is unsupported")
            }
            Self::InvalidBase64(field) => write!(formatter, "{field} is not canonical base64url"),
            Self::InvalidLength(field) => write!(formatter, "{field} has an invalid length"),
            Self::Receipt(VerificationFailure { check }) => {
                write!(formatter, "receipt verification failed at {check:?}")
            }
            Self::BatchAuthorizationMismatch => {
                formatter.write_str("portable receipt does not match the trusted batch")
            }
            Self::MissingReceiptDigest => {
                formatter.write_str("verified receipt did not produce a receipt digest")
            }
            Self::ReceiptDigestMismatch => {
                formatter.write_str("portable receipt digest does not match the receipt")
            }
            Self::InvalidHex(field) => write!(formatter, "{field} is not canonical 0x hex"),
            Self::InvalidQuantity(field) => {
                write!(formatter, "{field} is not a canonical 0x quantity")
            }
            Self::ReceiptKeyMismatch => {
                formatter.write_str("portable receipt key is not the pinned key that signed")
            }
            Self::InclusionHeight => {
                formatter.write_str("inclusion height precedes the signed receipt height")
            }
            Self::MissingBatchAuthorization => {
                formatter.write_str("v1 portable receipt needs a trusted batch authorization")
            }
        }
    }
}

impl std::error::Error for PortableReceiptError {}

fn decode_fixed<const N: usize>(
    field: &'static str,
    encoded: &str,
) -> Result<[u8; N], PortableReceiptError> {
    let decoded = decode_bounded(field, encoded, N, N)?;
    decoded
        .try_into()
        .map_err(|_| PortableReceiptError::InvalidLength(field))
}

fn decode_bounded(
    field: &'static str,
    encoded: &str,
    minimum: usize,
    maximum: usize,
) -> Result<Vec<u8>, PortableReceiptError> {
    if encoded.is_empty() || encoded.contains('=') {
        return Err(PortableReceiptError::InvalidBase64(field));
    }
    let decoded = URL_SAFE_NO_PAD
        .decode(encoded)
        .map_err(|_| PortableReceiptError::InvalidBase64(field))?;
    if decoded.len() < minimum || decoded.len() > maximum {
        return Err(PortableReceiptError::InvalidLength(field));
    }
    if URL_SAFE_NO_PAD.encode(&decoded) != encoded {
        return Err(PortableReceiptError::InvalidBase64(field));
    }
    Ok(decoded)
}

/// One pinned `ReceiptKeys` entry: keyId, ed25519 public key and the
/// inclusive block-height window in which that key signs receipts.
pub type ReceiptKeyEntry = SequencerAuthorization;

/// Self-contained portable proof of one sequencer-signed `ProtocolReceipt` v2.
///
/// Byte strings are lowercase `0x` hex and heights are `0x` quantities. The
/// carried `receiptKey` names the entry that signed; it is only trusted after
/// it equals the entry selected from the verifier's own pinned list.
#[derive(Clone, Debug, Eq, PartialEq, Deserialize, Serialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct PortableReceiptV2 {
    format: String,
    verification_level: String,
    signed_receipt: String,
    receipt_digest: String,
    receipt_key: PortableReceiptKey,
    inclusion_height: String,
}

#[derive(Clone, Debug, Eq, PartialEq, Deserialize, Serialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct PortableReceiptKey {
    key_id: String,
    public_key: String,
    first_height: String,
    last_height: String,
}

impl PortableReceiptV2 {
    /// Exports one signed receipt only after it verifies against the given
    /// `ReceiptKeys` entry and the inclusion height is not below the signed
    /// receipt height.
    ///
    /// # Errors
    ///
    /// Returns the bounds, receipt, key or inclusion-height refusal.
    pub fn export(
        signed_receipt: &[u8],
        receipt_key: &ReceiptKeyEntry,
        inclusion_height: u64,
    ) -> Result<Self, PortableReceiptError> {
        if signed_receipt.is_empty() || signed_receipt.len() > MAX_RECEIPT_BYTES {
            return Err(PortableReceiptError::ReceiptBounds);
        }
        verify_sequencer_signature(signed_receipt, std::slice::from_ref(receipt_key))
            .map_err(PortableReceiptError::Receipt)?;
        let portable = Self {
            format: PORTABLE_RECEIPT_FORMAT_V2.to_owned(),
            verification_level: VERIFICATION_LEVEL_V2.to_owned(),
            signed_receipt: encode_hex(signed_receipt),
            receipt_digest: encode_hex(&signed_receipt_digest(signed_receipt)?),
            receipt_key: PortableReceiptKey {
                key_id: encode_hex(&receipt_key.sequencer_id()),
                public_key: encode_hex(&receipt_key.public_key()),
                first_height: encode_quantity(receipt_key.first_batch_number()),
                last_height: encode_quantity(receipt_key.last_batch_number()),
            },
            inclusion_height: encode_quantity(inclusion_height),
        };
        portable.verify_v2(std::slice::from_ref(receipt_key))?;
        Ok(portable)
    }

    /// Parses the bounded JSON wire form without establishing trust.
    ///
    /// # Errors
    ///
    /// Refuses oversize, malformed, extended, or non-canonical JSON fields.
    pub fn from_json(bytes: &[u8]) -> Result<Self, PortableReceiptError> {
        if bytes.is_empty() || bytes.len() > MAX_JSON_BYTES {
            return Err(PortableReceiptError::JsonBounds);
        }
        let portable: Self =
            serde_json::from_slice(bytes).map_err(|_| PortableReceiptError::JsonShape)?;
        portable.decode_fields()?;
        Ok(portable)
    }

    /// Encodes the stable JSON object.
    ///
    /// # Errors
    ///
    /// Returns a field or encoding refusal.
    pub fn to_json(&self) -> Result<Vec<u8>, PortableReceiptError> {
        self.decode_fields()?;
        serde_json::to_vec(self).map_err(|_| PortableReceiptError::JsonShape)
    }

    /// Verifies the signed receipt offline against the pinned `ReceiptKeys`
    /// list: the entry is chosen by the receipt keyId and its signed height,
    /// the carried key must equal that entry, the digest must match and the
    /// inclusion height must not precede the signed height.
    ///
    /// # Errors
    ///
    /// Returns the first wire, receipt, key, digest or height refusal.
    pub fn verify_v2(
        &self,
        receipt_keys: &[ReceiptKeyEntry],
    ) -> Result<PortableVerifiedReceiptV2, PortableReceiptError> {
        let decoded = self.decode_fields()?;
        let receipt = verify_sequencer_signature(&decoded.signed_receipt, receipt_keys)
            .map_err(PortableReceiptError::Receipt)?;
        let protocol = receipt
            .protocol()
            .ok_or(PortableReceiptError::ReceiptKeyMismatch)?;
        let key_id = protocol.context_hash();
        let mut height = [0; 8];
        height.copy_from_slice(&protocol.batch_id()[24..]);
        let height = u64::from_be_bytes(height);
        let pinned = receipt_keys
            .iter()
            .find(|key| key.sequencer_id() == key_id)
            .ok_or(PortableReceiptError::ReceiptKeyMismatch)?;
        if *pinned != decoded.receipt_key {
            return Err(PortableReceiptError::ReceiptKeyMismatch);
        }
        if decoded.inclusion_height < height {
            return Err(PortableReceiptError::InclusionHeight);
        }
        let digest = signed_receipt_digest(&decoded.signed_receipt)?;
        if digest != decoded.receipt_digest {
            return Err(PortableReceiptError::ReceiptDigestMismatch);
        }
        Ok(PortableVerifiedReceiptV2 {
            signed_receipt: decoded.signed_receipt,
            receipt_digest: digest,
            receipt_key: *pinned,
            activity_id: protocol.activity_id(),
            global_sequence: protocol.global_sequence(),
            result_code: protocol.result_code(),
            height,
            inclusion_height: decoded.inclusion_height,
        })
    }

    /// Parses and verifies one v2 JSON proof.
    ///
    /// # Errors
    ///
    /// Returns the first wire, receipt, key, digest or height refusal.
    pub fn verify_json(
        bytes: &[u8],
        receipt_keys: &[ReceiptKeyEntry],
    ) -> Result<PortableVerifiedReceiptV2, PortableReceiptError> {
        Self::from_json(bytes)?.verify_v2(receipt_keys)
    }

    /// Returns the wire-format identifier.
    #[must_use]
    pub fn format(&self) -> &str {
        &self.format
    }

    fn decode_fields(&self) -> Result<DecodedReceiptV2, PortableReceiptError> {
        if self.format != PORTABLE_RECEIPT_FORMAT_V2 {
            return Err(PortableReceiptError::UnsupportedFormat);
        }
        if self.verification_level != VERIFICATION_LEVEL_V2 {
            return Err(PortableReceiptError::UnsupportedVerificationLevel);
        }
        let first = decode_quantity("firstHeight", &self.receipt_key.first_height)?;
        let last = decode_quantity("lastHeight", &self.receipt_key.last_height)?;
        Ok(DecodedReceiptV2 {
            signed_receipt: decode_hex(
                "signedReceipt",
                &self.signed_receipt,
                1,
                MAX_RECEIPT_BYTES,
            )?,
            receipt_digest: decode_hex_fixed("receiptDigest", &self.receipt_digest)?,
            receipt_key: ReceiptKeyEntry::new(
                decode_hex_fixed("keyId", &self.receipt_key.key_id)?,
                decode_hex_fixed("publicKey", &self.receipt_key.public_key)?,
                first,
                last,
            ),
            inclusion_height: decode_quantity("inclusionHeight", &self.inclusion_height)?,
        })
    }
}

struct DecodedReceiptV2 {
    signed_receipt: Vec<u8>,
    receipt_digest: [u8; 32],
    receipt_key: ReceiptKeyEntry,
    inclusion_height: u64,
}

/// A v2 receipt value produced only after the complete portable proof verifies.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PortableVerifiedReceiptV2 {
    signed_receipt: Vec<u8>,
    receipt_digest: [u8; 32],
    receipt_key: ReceiptKeyEntry,
    activity_id: [u8; 32],
    global_sequence: u64,
    result_code: i32,
    height: u64,
    inclusion_height: u64,
}

impl PortableVerifiedReceiptV2 {
    /// Returns the verified canonical signed receipt bytes.
    #[must_use]
    pub fn signed_receipt(&self) -> &[u8] {
        &self.signed_receipt
    }

    /// Returns the verified receipt-signature digest.
    #[must_use]
    pub const fn receipt_digest(&self) -> [u8; 32] {
        self.receipt_digest
    }

    /// Returns the pinned `ReceiptKeys` entry that signed.
    #[must_use]
    pub const fn receipt_key(&self) -> &ReceiptKeyEntry {
        &self.receipt_key
    }

    /// Returns the transaction hash carried as the activity identifier.
    #[must_use]
    pub const fn activity_id(&self) -> [u8; 32] {
        self.activity_id
    }

    /// Returns the gap-free sequence position.
    #[must_use]
    pub const fn global_sequence(&self) -> u64 {
        self.global_sequence
    }

    /// Returns the signed result status.
    #[must_use]
    pub const fn result_code(&self) -> i32 {
        self.result_code
    }

    /// Returns the signed pending-block height the key window was checked at.
    #[must_use]
    pub const fn height(&self) -> u64 {
        self.height
    }

    /// Returns the carried inclusion height, not below [`Self::height`].
    #[must_use]
    pub const fn inclusion_height(&self) -> u64 {
        self.inclusion_height
    }
}

/// A verified portable receipt of either format.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum PortableVerified {
    V1(Box<PortableVerifiedReceipt>),
    V2(PortableVerifiedReceiptV2),
}

#[derive(Deserialize)]
struct FormatProbe {
    format: String,
}

/// Parses and verifies a JSON proof of either format: v1 against the trusted
/// batch authorization, v2 against the pinned `ReceiptKeys` list.
///
/// # Errors
///
/// Refuses an unknown format, a v1 proof without a trusted batch, and every
/// refusal of the selected format's verifier.
pub fn verify_portable_json(
    bytes: &[u8],
    trusted_batch: Option<&AuthorizedBatch>,
    receipt_keys: &[ReceiptKeyEntry],
) -> Result<PortableVerified, PortableReceiptError> {
    if bytes.is_empty() || bytes.len() > MAX_JSON_BYTES {
        return Err(PortableReceiptError::JsonBounds);
    }
    let probe: FormatProbe =
        serde_json::from_slice(bytes).map_err(|_| PortableReceiptError::JsonShape)?;
    match probe.format.as_str() {
        PORTABLE_RECEIPT_FORMAT => {
            let batch = trusted_batch.ok_or(PortableReceiptError::MissingBatchAuthorization)?;
            PortableReceipt::verify_json(bytes, batch)
                .map(|verified| PortableVerified::V1(Box::new(verified)))
        }
        PORTABLE_RECEIPT_FORMAT_V2 => {
            PortableReceiptV2::verify_json(bytes, receipt_keys).map(PortableVerified::V2)
        }
        _ => Err(PortableReceiptError::UnsupportedFormat),
    }
}

/// sha256("LXP/v1/receipt\0" ‖ unsigned preimage); the unsigned preimage is the
/// canonical signed bytes with the trailing signature replaced by flag 0.
fn signed_receipt_digest(signed: &[u8]) -> Result<[u8; 32], PortableReceiptError> {
    let tail = SIGNATURE_TAIL.len() + SIGNATURE_BYTES;
    let prefix_len = signed
        .len()
        .checked_sub(tail)
        .ok_or(PortableReceiptError::ReceiptBounds)?;
    let (prefix, rest) = signed.split_at(prefix_len);
    if !rest.starts_with(&SIGNATURE_TAIL) {
        return Err(PortableReceiptError::Receipt(VerificationFailure {
            check: layerx_proof::receipt::ReceiptCheck::MissingSignature,
        }));
    }
    let mut hasher = Sha256::new();
    hasher.update(RECEIPT_DIGEST_DOMAIN);
    hasher.update(prefix);
    hasher.update([0]);
    Ok(hasher.finalize().into())
}

fn encode_hex(bytes: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut encoded = String::with_capacity(2 + bytes.len() * 2);
    encoded.push_str("0x");
    for byte in bytes {
        encoded.push(char::from(DIGITS[usize::from(byte >> 4)]));
        encoded.push(char::from(DIGITS[usize::from(byte & 0x0f)]));
    }
    encoded
}

fn encode_quantity(value: u64) -> String {
    format!("{value:#x}")
}

fn hex_nibble(digit: u8) -> Option<u8> {
    match digit {
        b'0'..=b'9' => Some(digit - b'0'),
        b'a'..=b'f' => Some(digit - b'a' + 10),
        _ => None,
    }
}

fn decode_hex(
    field: &'static str,
    encoded: &str,
    minimum: usize,
    maximum: usize,
) -> Result<Vec<u8>, PortableReceiptError> {
    let digits = encoded
        .strip_prefix("0x")
        .ok_or(PortableReceiptError::InvalidHex(field))?
        .as_bytes();
    if digits.len() % 2 != 0 {
        return Err(PortableReceiptError::InvalidHex(field));
    }
    let length = digits.len() / 2;
    if length < minimum || length > maximum {
        return Err(PortableReceiptError::InvalidLength(field));
    }
    digits
        .chunks_exact(2)
        .map(|pair| Some((hex_nibble(pair[0])? << 4) | hex_nibble(pair[1])?))
        .collect::<Option<Vec<u8>>>()
        .ok_or(PortableReceiptError::InvalidHex(field))
}

fn decode_hex_fixed<const N: usize>(
    field: &'static str,
    encoded: &str,
) -> Result<[u8; N], PortableReceiptError> {
    decode_hex(field, encoded, N, N)?
        .try_into()
        .map_err(|_| PortableReceiptError::InvalidLength(field))
}

fn decode_quantity(field: &'static str, encoded: &str) -> Result<u64, PortableReceiptError> {
    let digits = encoded
        .strip_prefix("0x")
        .ok_or(PortableReceiptError::InvalidQuantity(field))?;
    if digits.is_empty()
        || digits.len() > 16
        || (digits.len() > 1 && digits.starts_with('0'))
        || !digits.bytes().all(|digit| hex_nibble(digit).is_some())
    {
        return Err(PortableReceiptError::InvalidQuantity(field));
    }
    u64::from_str_radix(digits, 16).map_err(|_| PortableReceiptError::InvalidQuantity(field))
}
