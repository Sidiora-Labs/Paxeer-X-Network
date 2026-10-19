//! Authenticated storage-capacity admission for the private keeper boundary.
//!
//! The sequencer node signs every capacity answer with its authorised key over
//! a network- and tag-scoped digest. This module recomputes that digest,
//! verifies the signature against the pinned sequencer key, decodes the exact
//! fixed-width bodies and evaluates admission against the committed head the
//! answer was bound to. A read never reserves anything: only a signed
//! reservation record names capacity held for a request.

use sha2::{Digest, Sha256};

pub const FORMAT_VERSION: u16 = 1;
pub const DEMAND_BYTES: usize = 4 + 8 + 4;
pub const PROFILE_BYTES: usize = 2 + 2 + 32 + DEMAND_BYTES + 8;
pub const REQUEST_BYTES: usize = 2 + 1 + 32 + 32 + 2 + 255 + DEMAND_BYTES + 8 + 32 + 8 + 8;
pub const RECORD_BYTES: usize =
    8 + 1 + 1 + 32 + 32 + 32 + 2 + 255 + DEMAND_BYTES + 8 + 32 + 8 + 8 + 8 + 4 + 32;
pub const OBSERVATION_BYTES: usize = 2 + 2 + 32 + 8 + 32 + 6 * DEMAND_BYTES + 4 + 8;
pub const PROOF_BYTES: usize = 32 + 64;
pub const MAX_ACTOR_DID_BYTES: usize = 255;

pub const OBSERVE_REQUEST: u16 = 50;
pub const OBSERVE_RESPONSE: u16 = 51;
pub const RESERVE_REQUEST: u16 = 52;
pub const RESERVE_RESPONSE: u16 = 53;
pub const RECONCILE_REQUEST: u16 = 54;
pub const RECONCILE_RESPONSE: u16 = 55;
pub const CANCEL_REQUEST: u16 = 56;
pub const CANCEL_RESPONSE: u16 = 57;
pub const INSTALL_REQUEST: u16 = 58;
pub const INSTALL_RESPONSE: u16 = 59;

/// Native bounds the kernel enforces; they are unchanged by any profile.
pub const LIMIT: Demand = Demand {
    blobs: 512,
    bytes: 67_108_864,
    kv: 512,
};

const REQUEST_DOMAIN: &[u8] = b"LayerX/storage-capacity-request/v1";
const RESPONSE_DOMAIN: &[u8] = b"LayerX/storage-capacity-response/v1";

/// Refusal of a capacity answer or request.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum AdmissionError {
    /// The body has the wrong length, format, enum or padding.
    Malformed,
    /// The proof is not the pinned sequencer key's signature over the body.
    BadSignature,
    /// The answer is bound to a different committed sequence or root.
    StaleHead,
    /// The answer belongs to a different profile than the one pinned.
    ProfileMismatch,
    /// The demand does not fit the authenticated headroom.
    InsufficientHeadroom,
    /// A reservation record does not answer the request it was asked for.
    RecordMismatch,
}

impl core::fmt::Display for AdmissionError {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        formatter.write_str(match self {
            Self::Malformed => "capacity body is malformed",
            Self::BadSignature => "capacity proof is not signed by the pinned sequencer",
            Self::StaleHead => "capacity answer is bound to another committed head",
            Self::ProfileMismatch => "capacity answer belongs to another profile",
            Self::InsufficientHeadroom => "demand does not fit authenticated headroom",
            Self::RecordMismatch => "reservation record does not answer the request",
        })
    }
}

impl std::error::Error for AdmissionError {}

/// Additions measured in native blob slots, blob bytes and module KV slots.
#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct Demand {
    pub blobs: u32,
    pub bytes: u64,
    pub kv: u32,
}

impl Demand {
    fn encode(&self, out: &mut Vec<u8>) {
        out.extend_from_slice(&self.blobs.to_be_bytes());
        out.extend_from_slice(&self.bytes.to_be_bytes());
        out.extend_from_slice(&self.kv.to_be_bytes());
    }

    fn decode(reader: &mut Reader<'_>) -> Result<Self, AdmissionError> {
        Ok(Self {
            blobs: reader.u32()?,
            bytes: reader.u64()?,
            kv: reader.u32()?,
        })
    }

    #[must_use]
    pub fn is_zero(&self) -> bool {
        self.blobs == 0 && self.bytes == 0 && self.kv == 0
    }

    #[must_use]
    pub fn fits_within(&self, limit: &Self) -> bool {
        self.blobs <= limit.blobs && self.bytes <= limit.bytes && self.kv <= limit.kv
    }
}

/// The operator profile: reserve floor and the longest work reservation.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Profile {
    pub version: u16,
    pub digest: [u8; 32],
    pub floor: Demand,
    pub maximum_work_lifetime: u64,
}

impl Profile {
    /// Encodes the exact install body.
    ///
    /// # Errors
    ///
    /// Refuses a profile the node would refuse: zero version, digest, floor
    /// axis or lifetime, or a floor beyond the native bounds.
    pub fn encode(&self) -> Result<Vec<u8>, AdmissionError> {
        if self.version == 0
            || self.digest == [0; 32]
            || self.maximum_work_lifetime == 0
            || self.floor.blobs == 0
            || self.floor.bytes == 0
            || self.floor.kv == 0
            || !self.floor.fits_within(&LIMIT)
        {
            return Err(AdmissionError::Malformed);
        }
        let mut out = Vec::with_capacity(PROFILE_BYTES);
        out.extend_from_slice(&FORMAT_VERSION.to_be_bytes());
        out.extend_from_slice(&self.version.to_be_bytes());
        out.extend_from_slice(&self.digest);
        self.floor.encode(&mut out);
        out.extend_from_slice(&self.maximum_work_lifetime.to_be_bytes());
        Ok(out)
    }

    /// Decodes an install answer.
    ///
    /// # Errors
    ///
    /// Returns `Malformed` for any length, format or field violation.
    pub fn decode(bytes: &[u8]) -> Result<Self, AdmissionError> {
        let mut reader = Reader::exact(bytes, PROFILE_BYTES)?;
        reader.format()?;
        let profile = Self {
            version: reader.u16()?,
            digest: reader.array()?,
            floor: Demand::decode(&mut reader)?,
            maximum_work_lifetime: reader.u64()?,
        };
        reader.finish()?;
        profile.encode()?;
        Ok(profile)
    }
}

/// A capacity reservation is either routine work or a pending obligation
/// (payout, exit, terminal claim) that keeps reserve until it succeeds.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Kind {
    Work,
    Obligation,
}

impl Kind {
    fn code(self) -> u8 {
        match self {
            Self::Work => 1,
            Self::Obligation => 2,
        }
    }

    fn from_code(code: u8) -> Result<Self, AdmissionError> {
        match code {
            1 => Ok(Self::Work),
            2 => Ok(Self::Obligation),
            _ => Err(AdmissionError::Malformed),
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum State {
    Reserved,
    Reconciled,
    Cancelled,
    Expired,
    Superseded,
}

impl State {
    fn from_code(code: u8) -> Result<Self, AdmissionError> {
        match code {
            1 => Ok(Self::Reserved),
            2 => Ok(Self::Reconciled),
            3 => Ok(Self::Cancelled),
            4 => Ok(Self::Expired),
            5 => Ok(Self::Superseded),
            _ => Err(AdmissionError::Malformed),
        }
    }

    #[must_use]
    pub fn name(self) -> &'static str {
        match self {
            Self::Reserved => "reserved",
            Self::Reconciled => "reconciled",
            Self::Cancelled => "cancelled",
            Self::Expired => "expired",
            Self::Superseded => "superseded",
        }
    }
}

/// A reservation request bound to one native activity and one committed head.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Request {
    pub kind: Kind,
    pub activity_id: [u8; 32],
    pub idempotency_key: [u8; 32],
    pub actor_did: Vec<u8>,
    pub demand: Demand,
    pub expected_sequence: u64,
    pub expected_root: [u8; 32],
    pub lifetime: u64,
    pub supersedes: u64,
}

impl Request {
    /// Encodes the exact request body the node hashes into the request identity.
    ///
    /// # Errors
    ///
    /// Returns `Malformed` for an empty or oversized actor, zero activity,
    /// zero demand, zero head, or a lifetime/supersession that does not match
    /// the reservation kind.
    pub fn encode(&self) -> Result<Vec<u8>, AdmissionError> {
        let actor_length =
            u16::try_from(self.actor_did.len()).map_err(|_| AdmissionError::Malformed)?;
        let shape_ok = match self.kind {
            Kind::Work => self.lifetime != 0 && self.supersedes == 0,
            Kind::Obligation => self.lifetime == 0,
        };
        if self.actor_did.is_empty()
            || self.actor_did.len() > MAX_ACTOR_DID_BYTES
            || self.activity_id == [0; 32]
            || self.demand.is_zero()
            || !self.demand.fits_within(&LIMIT)
            || self.expected_sequence == 0
            || self.expected_root == [0; 32]
            || !shape_ok
        {
            return Err(AdmissionError::Malformed);
        }
        let mut out = Vec::with_capacity(REQUEST_BYTES);
        out.extend_from_slice(&FORMAT_VERSION.to_be_bytes());
        out.push(self.kind.code());
        out.extend_from_slice(&self.activity_id);
        out.extend_from_slice(&self.idempotency_key);
        out.extend_from_slice(&actor_length.to_be_bytes());
        out.extend_from_slice(&self.actor_did);
        out.resize(out.len() + MAX_ACTOR_DID_BYTES - self.actor_did.len(), 0);
        self.demand.encode(&mut out);
        out.extend_from_slice(&self.expected_sequence.to_be_bytes());
        out.extend_from_slice(&self.expected_root);
        out.extend_from_slice(&self.lifetime.to_be_bytes());
        out.extend_from_slice(&self.supersedes.to_be_bytes());
        Ok(out)
    }
}

impl Request {
    /// Decodes an exact request body.
    ///
    /// # Errors
    ///
    /// Returns `Malformed` for any length, format, padding or shape violation.
    pub fn decode(bytes: &[u8]) -> Result<Self, AdmissionError> {
        let mut reader = Reader::exact(bytes, REQUEST_BYTES)?;
        reader.format()?;
        let kind = Kind::from_code(reader.u8()?)?;
        let activity_id = reader.array()?;
        let idempotency_key = reader.array()?;
        let actor_length = usize::from(reader.u16()?);
        let padded: [u8; MAX_ACTOR_DID_BYTES] = reader.array()?;
        if actor_length > MAX_ACTOR_DID_BYTES
            || padded[actor_length..].iter().any(|byte| *byte != 0)
        {
            return Err(AdmissionError::Malformed);
        }
        let request = Self {
            kind,
            activity_id,
            idempotency_key,
            actor_did: padded[..actor_length].to_vec(),
            demand: Demand::decode(&mut reader)?,
            expected_sequence: reader.u64()?,
            expected_root: reader.array()?,
            lifetime: reader.u64()?,
            supersedes: reader.u64()?,
        };
        reader.finish()?;
        if request.encode()? == bytes {
            Ok(request)
        } else {
            Err(AdmissionError::Malformed)
        }
    }
}

/// The identity the node assigns to an exact request body.
#[must_use]
pub fn request_digest(body: &[u8]) -> [u8; 32] {
    let mut hash = Sha256::new();
    hash.update(REQUEST_DOMAIN);
    hash.update(body);
    hash.finalize().into()
}

/// A root-bound capacity observation. It describes headroom; it holds none.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Observation {
    pub profile_version: u16,
    pub profile_digest: [u8; 32],
    pub next_sequence: u64,
    pub state_root: [u8; 32],
    pub limit: Demand,
    pub committed: Demand,
    pub floor: Demand,
    pub obligations: Demand,
    pub work: Demand,
    pub available: Demand,
    pub active_reservations: u32,
    pub next_request_id: u64,
}

impl Observation {
    /// Decodes an observation body.
    ///
    /// # Errors
    ///
    /// Returns `Malformed` for any length or format violation, or a limit
    /// that differs from the native bounds.
    pub fn decode(bytes: &[u8]) -> Result<Self, AdmissionError> {
        let mut reader = Reader::exact(bytes, OBSERVATION_BYTES)?;
        reader.format()?;
        let observation = Self {
            profile_version: reader.u16()?,
            profile_digest: reader.array()?,
            next_sequence: reader.u64()?,
            state_root: reader.array()?,
            limit: Demand::decode(&mut reader)?,
            committed: Demand::decode(&mut reader)?,
            floor: Demand::decode(&mut reader)?,
            obligations: Demand::decode(&mut reader)?,
            work: Demand::decode(&mut reader)?,
            available: Demand::decode(&mut reader)?,
            active_reservations: reader.u32()?,
            next_request_id: reader.u64()?,
        };
        reader.finish()?;
        if observation.limit != LIMIT
            || observation.next_sequence == 0
            || observation.next_request_id == 0
            || observation.profile_version == 0
        {
            return Err(AdmissionError::Malformed);
        }
        Ok(observation)
    }

    /// Requires the observation to be taken at the exact expected head.
    ///
    /// # Errors
    ///
    /// Returns `StaleHead` when either the sequence or the root differs.
    pub fn require_head(
        &self,
        expected_sequence: u64,
        expected_root: &[u8; 32],
    ) -> Result<(), AdmissionError> {
        if self.next_sequence == expected_sequence && &self.state_root == expected_root {
            Ok(())
        } else {
            Err(AdmissionError::StaleHead)
        }
    }

    /// Requires the observation to belong to the pinned profile.
    ///
    /// # Errors
    ///
    /// Returns `ProfileMismatch` for any other version or digest.
    pub fn require_profile(&self, profile: &Profile) -> Result<(), AdmissionError> {
        if self.profile_version == profile.version
            && self.profile_digest == profile.digest
            && self.floor == profile.floor
        {
            Ok(())
        } else {
            Err(AdmissionError::ProfileMismatch)
        }
    }

    /// Evaluates whether `demand` of `kind` fits this observation using the
    /// node's own rule on every demanded axis: work must fit beside
    /// `max(floor, obligations)` and all active work; an obligation may draw
    /// on the floor but not past the limit.
    ///
    /// # Errors
    ///
    /// Returns `InsufficientHeadroom` when any axis would exceed the limit.
    pub fn admits(&self, kind: Kind, demand: &Demand) -> Result<(), AdmissionError> {
        let axes = [
            (
                self.limit.blobs.into(),
                self.committed.blobs.into(),
                self.floor.blobs.into(),
                self.obligations.blobs.into(),
                self.work.blobs.into(),
                demand.blobs.into(),
            ),
            (
                self.limit.bytes,
                self.committed.bytes,
                self.floor.bytes,
                self.obligations.bytes,
                self.work.bytes,
                demand.bytes,
            ),
            (
                self.limit.kv.into(),
                self.committed.kv.into(),
                self.floor.kv.into(),
                self.obligations.kv.into(),
                self.work.kv.into(),
                demand.kv.into(),
            ),
        ];
        for (limit, committed, floor, obligations, work, wanted) in axes {
            if wanted == 0 {
                continue;
            }
            let used = match kind {
                Kind::Work => committed
                    .checked_add(floor.max(obligations))
                    .and_then(|sum: u64| sum.checked_add(work))
                    .and_then(|sum| sum.checked_add(wanted)),
                Kind::Obligation => obligations
                    .checked_add(wanted)
                    .map(|held: u64| floor.max(held))
                    .and_then(|reserve| committed.checked_add(reserve))
                    .and_then(|sum| sum.checked_add(work)),
            };
            if used.is_none_or(|total| total > limit) {
                return Err(AdmissionError::InsufficientHeadroom);
            }
        }
        Ok(())
    }
}

/// A signed reservation record as the node holds it.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Reservation {
    pub request_id: u64,
    pub kind: Kind,
    pub state: State,
    pub request_digest: [u8; 32],
    pub activity_id: [u8; 32],
    pub idempotency_key: [u8; 32],
    pub actor_did: Vec<u8>,
    pub demand: Demand,
    pub bound_sequence: u64,
    pub bound_root: [u8; 32],
    pub expires_sequence: u64,
    pub supersedes: u64,
    pub outcome_sequence: u64,
    pub outcome_result: i32,
    pub outcome_receipt_digest: [u8; 32],
}

impl Reservation {
    /// Decodes a reservation record.
    ///
    /// # Errors
    ///
    /// Returns `Malformed` for any length, enum, padding or outcome violation.
    pub fn decode(bytes: &[u8]) -> Result<Self, AdmissionError> {
        let mut reader = Reader::exact(bytes, RECORD_BYTES)?;
        let request_id = reader.u64()?;
        let kind = Kind::from_code(reader.u8()?)?;
        let state = State::from_code(reader.u8()?)?;
        let request_digest = reader.array()?;
        let activity_id = reader.array()?;
        let idempotency_key = reader.array()?;
        let actor_length = usize::from(reader.u16()?);
        let padded: [u8; MAX_ACTOR_DID_BYTES] = reader.array()?;
        if actor_length == 0
            || actor_length > MAX_ACTOR_DID_BYTES
            || padded[actor_length..].iter().any(|byte| *byte != 0)
        {
            return Err(AdmissionError::Malformed);
        }
        let record = Self {
            request_id,
            kind,
            state,
            request_digest,
            activity_id,
            idempotency_key,
            actor_did: padded[..actor_length].to_vec(),
            demand: Demand::decode(&mut reader)?,
            bound_sequence: reader.u64()?,
            bound_root: reader.array()?,
            expires_sequence: reader.u64()?,
            supersedes: reader.u64()?,
            outcome_sequence: reader.u64()?,
            outcome_result: i32::from_be_bytes(reader.array()?),
            outcome_receipt_digest: reader.array()?,
        };
        reader.finish()?;
        let outcome_absent = record.outcome_sequence == 0;
        if record.request_id == 0
            || record.bound_sequence == 0
            || record.demand.is_zero()
            || outcome_absent != (record.outcome_receipt_digest == [0; 32])
            || (outcome_absent && record.outcome_result != 0)
            || (record.state == State::Reconciled && outcome_absent)
        {
            return Err(AdmissionError::Malformed);
        }
        Ok(record)
    }

    /// Requires this record to answer exactly `request` with identity `digest`.
    ///
    /// # Errors
    ///
    /// Returns `RecordMismatch` when any bound field differs.
    pub fn require_answers(
        &self,
        request: &Request,
        digest: &[u8; 32],
    ) -> Result<(), AdmissionError> {
        if &self.request_digest == digest
            && self.kind == request.kind
            && self.activity_id == request.activity_id
            && self.idempotency_key == request.idempotency_key
            && self.actor_did == request.actor_did
            && self.demand == request.demand
            && self.bound_sequence == request.expected_sequence
            && self.bound_root == request.expected_root
            && self.supersedes == request.supersedes
        {
            Ok(())
        } else {
            Err(AdmissionError::RecordMismatch)
        }
    }

    /// True while the record still holds capacity at the node.
    #[must_use]
    pub fn holds_capacity(&self) -> bool {
        self.state == State::Reserved
    }
}

/// Verifies a capacity answer's proof against the pinned sequencer key.
///
/// # Errors
///
/// Returns `Malformed` for a proof of the wrong size and `BadSignature` when
/// the embedded key is not the pinned one or the signature does not verify.
pub fn verify_answer(
    network_id: u32,
    tag: u16,
    payload: &[u8],
    proof: &[u8],
    pinned_sequencer: &[u8; 32],
) -> Result<(), AdmissionError> {
    if proof.len() != PROOF_BYTES {
        return Err(AdmissionError::Malformed);
    }
    let mut key = [0_u8; 32];
    key.copy_from_slice(&proof[..32]);
    let mut signature = [0_u8; 64];
    signature.copy_from_slice(&proof[32..]);
    if &key != pinned_sequencer {
        return Err(AdmissionError::BadSignature);
    }
    let mut hash = Sha256::new();
    hash.update(RESPONSE_DOMAIN);
    hash.update(network_id.to_be_bytes());
    hash.update(tag.to_be_bytes());
    hash.update(payload);
    let digest: [u8; 32] = hash.finalize().into();
    layerx_crypto::ed25519::verify_digest(&key, &signature, &digest)
        .map_err(|_| AdmissionError::BadSignature)
}

/// Encodes the body of an observe request.
#[must_use]
pub fn observe_body() -> Vec<u8> {
    FORMAT_VERSION.to_be_bytes().to_vec()
}

/// Encodes the body of a reconcile or cancel request.
#[must_use]
pub fn request_id_body(request_id: u64) -> Vec<u8> {
    let mut out = FORMAT_VERSION.to_be_bytes().to_vec();
    out.extend_from_slice(&request_id.to_be_bytes());
    out
}

/// Decodes a reserve answer into its replay flag and record.
///
/// # Errors
///
/// Returns `Malformed` for a wrong length, replay flag or record.
pub fn decode_reserve_answer(payload: &[u8]) -> Result<(bool, Reservation), AdmissionError> {
    let (flag, record) = payload.split_first().ok_or(AdmissionError::Malformed)?;
    let replayed = match flag {
        0 => false,
        1 => true,
        _ => return Err(AdmissionError::Malformed),
    };
    Ok((replayed, Reservation::decode(record)?))
}

struct Reader<'a> {
    bytes: &'a [u8],
    cursor: usize,
}

impl<'a> Reader<'a> {
    fn exact(bytes: &'a [u8], length: usize) -> Result<Self, AdmissionError> {
        if bytes.len() == length {
            Ok(Self { bytes, cursor: 0 })
        } else {
            Err(AdmissionError::Malformed)
        }
    }

    fn take(&mut self, length: usize) -> Result<&'a [u8], AdmissionError> {
        let end = self
            .cursor
            .checked_add(length)
            .ok_or(AdmissionError::Malformed)?;
        let slice = self
            .bytes
            .get(self.cursor..end)
            .ok_or(AdmissionError::Malformed)?;
        self.cursor = end;
        Ok(slice)
    }

    fn array<const N: usize>(&mut self) -> Result<[u8; N], AdmissionError> {
        let mut out = [0_u8; N];
        out.copy_from_slice(self.take(N)?);
        Ok(out)
    }

    fn u8(&mut self) -> Result<u8, AdmissionError> {
        Ok(self.array::<1>()?[0])
    }

    fn u16(&mut self) -> Result<u16, AdmissionError> {
        Ok(u16::from_be_bytes(self.array()?))
    }

    fn u32(&mut self) -> Result<u32, AdmissionError> {
        Ok(u32::from_be_bytes(self.array()?))
    }

    fn u64(&mut self) -> Result<u64, AdmissionError> {
        Ok(u64::from_be_bytes(self.array()?))
    }

    fn format(&mut self) -> Result<(), AdmissionError> {
        if self.u16()? == FORMAT_VERSION {
            Ok(())
        } else {
            Err(AdmissionError::Malformed)
        }
    }

    fn finish(&self) -> Result<(), AdmissionError> {
        if self.cursor == self.bytes.len() {
            Ok(())
        } else {
            Err(AdmissionError::Malformed)
        }
    }
}
