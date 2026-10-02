//! Native caps discovery: exact request/response/cursor codecs and the consumer
//! that classifies budgets and grants only after complete prefix verification.
//!
//! The selector account is a selector, never an authenticated wallet principal.

use layerx_proof::state_range::{EmptyModuleProof, PrefixBound};
use layerx_proof::state_witness::StateWitness;
use sha2::{Digest, Sha256};

use crate::budget::ProtocolBudgetRecord;
use crate::evidence::caps::VerifiedCapsSnapshot;
use crate::grants::CommittedGrant;

/// LNI tag of a caps-discovery request (schema minor 8).
pub const CAPS_DISCOVERY_REQUEST_TAG: u16 = 42;
/// LNI tag of a caps-discovery response (schema minor 8).
pub const CAPS_DISCOVERY_RESPONSE_TAG: u16 = 43;
/// Negotiated capability name a peer must advertise.
pub const CAPS_DISCOVERY_CAPABILITY: &str = "caps_discovery";
/// First schema minor that carries caps discovery.
pub const CAPS_DISCOVERY_MINOR: u16 = 8;
/// Exact cursor length.
pub const CAPS_CURSOR_BYTES: usize = 107;
/// Caps responses carry no proof material; pages authenticate through witnesses.
pub const CAPS_PROOF_MATERIAL_BYTES: usize = 0;
/// Largest page a client may request.
pub const CAPS_PAGE_MAX_ITEMS: u16 = 64;
/// Smallest page byte budget a client may request.
pub const CAPS_PAGE_MIN_BYTES: u32 = 4096;

const FORMAT_VERSION: u16 = 1;
const OP_OPEN: u8 = 1;
const OP_PAGE: u8 = 2;
const OP_RELEASE: u8 = 3;
const PREFIX_BUDGET: u8 = 1;
const PREFIX_GRANT: u8 = 2;
const BOUND_ABSENT: u8 = 0;
const BOUND_EDGE: u8 = 1;
const BOUND_WITNESS: u8 = 2;
const BOUND_EMPTY_MODULE: u8 = 3;
const SELECTION_DOMAIN: &[u8] = b"LayerX/caps-selection/v1\0";
const PAGE_SIGNATURE_DOMAIN: &[u8] = b"LayerX/caps-discovery-page/v1\0";

/// A malformed or noncanonical caps-discovery encoding.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum CapsCodecError {
    Truncated,
    Trailing,
    Version,
    Op,
    Prefix,
    Bound,
    Limit,
    Selector,
    Witness,
}

/// Immutable 107-byte cursor binding snapshot, network, root, selection and position.
#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq)]
pub struct CapsCursor([u8; CAPS_CURSOR_BYTES]);

impl CapsCursor {
    /// # Errors
    /// Refuses a wrong version, an unknown prefix or any length other than 107.
    pub fn from_bytes(bytes: &[u8]) -> Result<Self, CapsCodecError> {
        let exact: [u8; CAPS_CURSOR_BYTES] =
            bytes.try_into().map_err(|_| CapsCodecError::Truncated)?;
        if u16::from_be_bytes([exact[0], exact[1]]) != FORMAT_VERSION {
            return Err(CapsCodecError::Version);
        }
        if exact[102] != PREFIX_BUDGET && exact[102] != PREFIX_GRANT {
            return Err(CapsCodecError::Prefix);
        }
        Ok(Self(exact))
    }

    #[must_use]
    pub const fn as_bytes(&self) -> &[u8; CAPS_CURSOR_BYTES] {
        &self.0
    }

    #[must_use]
    pub fn snapshot_id(&self) -> [u8; 32] {
        array32(&self.0[2..34])
    }

    #[must_use]
    pub fn network_id(&self) -> u32 {
        u32::from_be_bytes([self.0[34], self.0[35], self.0[36], self.0[37]])
    }

    #[must_use]
    pub fn state_root(&self) -> [u8; 32] {
        array32(&self.0[38..70])
    }

    #[must_use]
    pub fn selection_digest(&self) -> [u8; 32] {
        array32(&self.0[70..102])
    }

    /// Returns 1 for the budget prefix and 2 for the grant prefix.
    #[must_use]
    pub const fn prefix(&self) -> u8 {
        self.0[102]
    }

    #[must_use]
    pub fn next_position(&self) -> u32 {
        u32::from_be_bytes([self.0[103], self.0[104], self.0[105], self.0[106]])
    }
}

/// Canonical leaf range hint of one prefix; authenticated only by page witnesses.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct CapsModuleSpan {
    pub module_id: u16,
    pub module_leaf_count: u32,
    pub prefix_first: u32,
    pub prefix_count: u32,
}

/// A caps-discovery request payload.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum CapsRequest {
    Open {
        network_id: u32,
        selector_account: [u8; 32],
        max_items: u16,
        max_bytes: u32,
    },
    Page {
        cursor: CapsCursor,
        max_items: u16,
        max_bytes: u32,
    },
    Release {
        snapshot_id: [u8; 32],
    },
}

/// The open response: one immutable snapshot captured at one committed root.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CapsSnapshotOpen {
    pub snapshot_id: [u8; 32],
    pub network_id: u32,
    pub state_root: [u8; 32],
    pub observed_sequence: u64,
    pub expires_at_ms: u64,
    pub selection_digest: [u8; 32],
    pub requested_rank: u8,
    /// Exact canonical account value for module 0 key `0x04 || selector_account`.
    pub account_value: Vec<u8>,
    /// Exact existing module-evidence proof material for that account value.
    pub account_evidence: Vec<u8>,
    pub budget_module: CapsModuleSpan,
    pub grant_module: CapsModuleSpan,
    pub first_cursor: CapsCursor,
}

/// One served page with its witnesses and authenticated bounds.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CapsPage {
    pub cursor_echo: CapsCursor,
    pub items: Vec<StateWitness>,
    pub lower: Option<PrefixBound>,
    pub upper: Option<PrefixBound>,
    pub exhausted: bool,
    pub next_cursor: Option<CapsCursor>,
}

/// A caps-discovery response payload.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum CapsResponse {
    Open(CapsSnapshotOpen),
    Page(CapsPage),
    Release { snapshot_id: [u8; 32] },
}

/// Why caps discovery cannot be attempted against a peer.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum CapsUnavailable {
    CapabilityNotAdvertised,
    PeerVersionUnsupported,
}

/// Why a verified traversal could not be classified.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum CapsRefusal {
    MalformedBudget,
    MalformedGrant,
    SelectorMismatch,
    NetworkMismatch,
}

/// A traversal that has not yet proven both prefixes and the account.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct CapsProgress {
    pub snapshot_id: [u8; 32],
    pub next_cursor: Option<CapsCursor>,
    pub pages_received: usize,
}

/// Account-filtered caps proven complete at one committed root.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CapsDiscovery {
    pub network_id: u32,
    pub state_root: [u8; 32],
    pub observed_sequence: u64,
    pub selector_account: [u8; 32],
    pub owned_budgets: Vec<ProtocolBudgetRecord>,
    pub delegated_budgets: Vec<ProtocolBudgetRecord>,
    pub grants_from: Vec<CommittedGrant>,
    pub grants_to: Vec<CommittedGrant>,
}

/// Explicit discovery outcome; only `Complete` and `Empty` are verified results.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum CapsOutcome {
    Incomplete(CapsProgress),
    Complete(CapsDiscovery),
    Empty(CapsDiscovery),
    Refused(CapsRefusal),
    Unavailable(CapsUnavailable),
}

/// Refuses peers that did not negotiate caps discovery.
///
/// # Errors
/// Returns the reason the peer cannot serve caps discovery.
pub fn require_caps_discovery(
    peer_capabilities: &[&str],
    peer_minor: u16,
) -> Result<(), CapsUnavailable> {
    if !peer_capabilities.contains(&CAPS_DISCOVERY_CAPABILITY) {
        return Err(CapsUnavailable::CapabilityNotAdvertised);
    }
    if peer_minor < CAPS_DISCOVERY_MINOR {
        return Err(CapsUnavailable::PeerVersionUnsupported);
    }
    Ok(())
}

/// SHA-256 selection digest binding a selector account to one network.
#[must_use]
pub fn selection_digest(network_id: u32, selector_account: [u8; 32]) -> [u8; 32] {
    let mut hash = Sha256::new();
    hash.update(SELECTION_DOMAIN);
    hash.update(network_id.to_be_bytes());
    hash.update(selector_account);
    hash.finalize().into()
}

/// Digest the sequencer signs over one canonical page payload.
#[must_use]
pub fn page_signing_digest(canonical_payload: &[u8]) -> [u8; 32] {
    let inner: [u8; 32] = Sha256::digest(canonical_payload).into();
    let mut hash = Sha256::new();
    hash.update(PAGE_SIGNATURE_DOMAIN);
    hash.update(inner);
    hash.finalize().into()
}

/// Reports where a traversal stands; never claims completion for partial pages.
#[must_use]
pub fn progress(open: &CapsSnapshotOpen, pages: &[CapsPage]) -> CapsOutcome {
    let next_cursor = match pages.last() {
        None => Some(open.first_cursor),
        Some(page) => page.next_cursor,
    };
    CapsOutcome::Incomplete(CapsProgress {
        snapshot_id: open.snapshot_id,
        next_cursor,
        pages_received: pages.len(),
    })
}

/// Classifies a snapshot whose prefixes, bounds and account witness verified.
/// Records are decoded with the canonical decoders and owner/delegate/payer/
/// receiver roles stay separate.
#[must_use]
pub fn classify(
    snapshot: &VerifiedCapsSnapshot,
    network_id: u32,
    selector_account: [u8; 32],
) -> CapsOutcome {
    if snapshot.network_id() != network_id {
        return CapsOutcome::Refused(CapsRefusal::NetworkMismatch);
    }
    if snapshot.selector_account() != selector_account {
        return CapsOutcome::Refused(CapsRefusal::SelectorMismatch);
    }
    let mut discovery = CapsDiscovery {
        network_id,
        state_root: snapshot.state_root(),
        observed_sequence: snapshot.observed_sequence(),
        selector_account,
        owned_budgets: Vec::new(),
        delegated_budgets: Vec::new(),
        grants_from: Vec::new(),
        grants_to: Vec::new(),
    };
    for (key, bytes) in snapshot.budget_entries() {
        let Ok(record) = ProtocolBudgetRecord::decode_state(key, bytes) else {
            return CapsOutcome::Refused(CapsRefusal::MalformedBudget);
        };
        if record.delegates.contains(&selector_account) {
            discovery.delegated_budgets.push(record.clone());
        }
        if record.owner == selector_account {
            discovery.owned_budgets.push(record);
        }
    }
    for (key, bytes) in snapshot.grant_entries() {
        let Ok(grant) = CommittedGrant::decode(key, bytes) else {
            return CapsOutcome::Refused(CapsRefusal::MalformedGrant);
        };
        if grant.grant.recipient == selector_account {
            discovery.grants_to.push(grant.clone());
        }
        if grant.grant.from == selector_account {
            discovery.grants_from.push(grant);
        }
    }
    if discovery.owned_budgets.is_empty()
        && discovery.delegated_budgets.is_empty()
        && discovery.grants_from.is_empty()
        && discovery.grants_to.is_empty()
    {
        CapsOutcome::Empty(discovery)
    } else {
        CapsOutcome::Complete(discovery)
    }
}

fn array32(bytes: &[u8]) -> [u8; 32] {
    let mut out = [0; 32];
    out.copy_from_slice(bytes);
    out
}

fn check_page_limits(max_items: u16, max_bytes: u32) -> Result<(), CapsCodecError> {
    if max_items == 0 || max_items > CAPS_PAGE_MAX_ITEMS || max_bytes < CAPS_PAGE_MIN_BYTES {
        return Err(CapsCodecError::Limit);
    }
    Ok(())
}

struct Reader<'a> {
    bytes: &'a [u8],
    at: usize,
}

impl<'a> Reader<'a> {
    const fn new(bytes: &'a [u8]) -> Self {
        Self { bytes, at: 0 }
    }

    fn take(&mut self, len: usize) -> Result<&'a [u8], CapsCodecError> {
        let end = self.at.checked_add(len).ok_or(CapsCodecError::Truncated)?;
        let out = self
            .bytes
            .get(self.at..end)
            .ok_or(CapsCodecError::Truncated)?;
        self.at = end;
        Ok(out)
    }

    fn u8(&mut self) -> Result<u8, CapsCodecError> {
        Ok(self.take(1)?[0])
    }

    fn u16(&mut self) -> Result<u16, CapsCodecError> {
        let b = self.take(2)?;
        Ok(u16::from_be_bytes([b[0], b[1]]))
    }

    fn u32(&mut self) -> Result<u32, CapsCodecError> {
        let b = self.take(4)?;
        Ok(u32::from_be_bytes([b[0], b[1], b[2], b[3]]))
    }

    fn u64(&mut self) -> Result<u64, CapsCodecError> {
        let mut out = [0; 8];
        out.copy_from_slice(self.take(8)?);
        Ok(u64::from_be_bytes(out))
    }

    fn b32(&mut self) -> Result<[u8; 32], CapsCodecError> {
        Ok(array32(self.take(32)?))
    }

    fn length_prefixed(&mut self) -> Result<&'a [u8], CapsCodecError> {
        let len = usize::try_from(self.u32()?).map_err(|_| CapsCodecError::Truncated)?;
        self.take(len)
    }

    fn witness(&mut self) -> Result<StateWitness, CapsCodecError> {
        StateWitness::decode(self.length_prefixed()?).map_err(|_| CapsCodecError::Witness)
    }

    fn cursor(&mut self) -> Result<CapsCursor, CapsCodecError> {
        CapsCursor::from_bytes(self.take(CAPS_CURSOR_BYTES)?)
    }

    fn span(&mut self) -> Result<CapsModuleSpan, CapsCodecError> {
        Ok(CapsModuleSpan {
            module_id: self.u16()?,
            module_leaf_count: self.u32()?,
            prefix_first: self.u32()?,
            prefix_count: self.u32()?,
        })
    }

    fn bound(&mut self) -> Result<Option<PrefixBound>, CapsCodecError> {
        match self.u8()? {
            BOUND_ABSENT => Ok(None),
            BOUND_EDGE => Ok(Some(PrefixBound::Edge)),
            BOUND_WITNESS => Ok(Some(PrefixBound::Witness(self.witness()?))),
            BOUND_EMPTY_MODULE => {
                let module_id = self.u16()?;
                let composite_leaf_count = self.u32()?;
                let count = usize::from(self.u8()?);
                let mut siblings = Vec::with_capacity(count);
                for _ in 0..count {
                    siblings.push(self.b32()?);
                }
                Ok(Some(PrefixBound::EmptyModule(EmptyModuleProof {
                    module_id,
                    composite_leaf_count,
                    siblings,
                })))
            }
            _ => Err(CapsCodecError::Bound),
        }
    }

    fn header(&mut self) -> Result<u8, CapsCodecError> {
        if self.u16()? != FORMAT_VERSION {
            return Err(CapsCodecError::Version);
        }
        self.u8()
    }

    fn finish(&self) -> Result<(), CapsCodecError> {
        if self.at == self.bytes.len() {
            Ok(())
        } else {
            Err(CapsCodecError::Trailing)
        }
    }
}

fn put_length_prefixed(out: &mut Vec<u8>, bytes: &[u8]) -> Result<(), CapsCodecError> {
    out.extend_from_slice(
        &u32::try_from(bytes.len())
            .map_err(|_| CapsCodecError::Limit)?
            .to_be_bytes(),
    );
    out.extend_from_slice(bytes);
    Ok(())
}

fn put_witness(out: &mut Vec<u8>, witness: &StateWitness) -> Result<(), CapsCodecError> {
    put_length_prefixed(out, &witness.encode().map_err(|_| CapsCodecError::Witness)?)
}

fn put_span(out: &mut Vec<u8>, span: CapsModuleSpan) {
    out.extend_from_slice(&span.module_id.to_be_bytes());
    out.extend_from_slice(&span.module_leaf_count.to_be_bytes());
    out.extend_from_slice(&span.prefix_first.to_be_bytes());
    out.extend_from_slice(&span.prefix_count.to_be_bytes());
}

fn put_bound(out: &mut Vec<u8>, bound: Option<&PrefixBound>) -> Result<(), CapsCodecError> {
    match bound {
        None => out.push(BOUND_ABSENT),
        Some(PrefixBound::Edge) => out.push(BOUND_EDGE),
        Some(PrefixBound::Witness(witness)) => {
            out.push(BOUND_WITNESS);
            put_witness(out, witness)?;
        }
        Some(PrefixBound::EmptyModule(proof)) => {
            out.push(BOUND_EMPTY_MODULE);
            out.extend_from_slice(&proof.module_id.to_be_bytes());
            out.extend_from_slice(&proof.composite_leaf_count.to_be_bytes());
            out.push(u8::try_from(proof.siblings.len()).map_err(|_| CapsCodecError::Bound)?);
            for sibling in &proof.siblings {
                out.extend_from_slice(sibling);
            }
        }
    }
    Ok(())
}

impl CapsRequest {
    /// # Errors
    /// Refuses out-of-range page limits and a zero selector account.
    pub fn encode(&self) -> Result<Vec<u8>, CapsCodecError> {
        let mut out = FORMAT_VERSION.to_be_bytes().to_vec();
        match self {
            Self::Open {
                network_id,
                selector_account,
                max_items,
                max_bytes,
            } => {
                check_page_limits(*max_items, *max_bytes)?;
                if *selector_account == [0; 32] {
                    return Err(CapsCodecError::Selector);
                }
                out.push(OP_OPEN);
                out.extend_from_slice(&network_id.to_be_bytes());
                out.extend_from_slice(selector_account);
                out.extend_from_slice(&max_items.to_be_bytes());
                out.extend_from_slice(&max_bytes.to_be_bytes());
            }
            Self::Page {
                cursor,
                max_items,
                max_bytes,
            } => {
                check_page_limits(*max_items, *max_bytes)?;
                out.push(OP_PAGE);
                out.extend_from_slice(cursor.as_bytes());
                out.extend_from_slice(&max_items.to_be_bytes());
                out.extend_from_slice(&max_bytes.to_be_bytes());
            }
            Self::Release { snapshot_id } => {
                out.push(OP_RELEASE);
                out.extend_from_slice(snapshot_id);
            }
        }
        Ok(out)
    }

    /// # Errors
    /// Refuses wrong versions, unknown ops, out-of-range limits and trailing bytes.
    pub fn decode(bytes: &[u8]) -> Result<Self, CapsCodecError> {
        let mut reader = Reader::new(bytes);
        let request = match reader.header()? {
            OP_OPEN => {
                let network_id = reader.u32()?;
                let selector_account = reader.b32()?;
                let max_items = reader.u16()?;
                let max_bytes = reader.u32()?;
                check_page_limits(max_items, max_bytes)?;
                if selector_account == [0; 32] {
                    return Err(CapsCodecError::Selector);
                }
                Self::Open {
                    network_id,
                    selector_account,
                    max_items,
                    max_bytes,
                }
            }
            OP_PAGE => {
                let cursor = reader.cursor()?;
                let max_items = reader.u16()?;
                let max_bytes = reader.u32()?;
                check_page_limits(max_items, max_bytes)?;
                Self::Page {
                    cursor,
                    max_items,
                    max_bytes,
                }
            }
            OP_RELEASE => Self::Release {
                snapshot_id: reader.b32()?,
            },
            _ => return Err(CapsCodecError::Op),
        };
        reader.finish()?;
        Ok(request)
    }
}

impl CapsResponse {
    /// # Errors
    /// Refuses unencodable witnesses, oversize bounds and an inconsistent
    /// exhausted flag / next cursor pair.
    pub fn encode(&self) -> Result<Vec<u8>, CapsCodecError> {
        let mut out = FORMAT_VERSION.to_be_bytes().to_vec();
        match self {
            Self::Open(open) => {
                out.push(OP_OPEN);
                out.extend_from_slice(&open.snapshot_id);
                out.extend_from_slice(&open.network_id.to_be_bytes());
                out.extend_from_slice(&open.state_root);
                out.extend_from_slice(&open.observed_sequence.to_be_bytes());
                out.extend_from_slice(&open.expires_at_ms.to_be_bytes());
                out.extend_from_slice(&open.selection_digest);
                out.push(open.requested_rank);
                put_length_prefixed(&mut out, &open.account_value)?;
                put_length_prefixed(&mut out, &open.account_evidence)?;
                put_span(&mut out, open.budget_module);
                put_span(&mut out, open.grant_module);
                out.extend_from_slice(open.first_cursor.as_bytes());
            }
            Self::Page(page) => {
                if page.exhausted == page.next_cursor.is_some() {
                    return Err(CapsCodecError::Bound);
                }
                out.push(OP_PAGE);
                out.extend_from_slice(page.cursor_echo.as_bytes());
                out.extend_from_slice(
                    &u16::try_from(page.items.len())
                        .map_err(|_| CapsCodecError::Limit)?
                        .to_be_bytes(),
                );
                for item in &page.items {
                    put_witness(&mut out, item)?;
                }
                put_bound(&mut out, page.lower.as_ref())?;
                put_bound(&mut out, page.upper.as_ref())?;
                out.push(u8::from(page.exhausted));
                if let Some(cursor) = &page.next_cursor {
                    out.extend_from_slice(cursor.as_bytes());
                }
            }
            Self::Release { snapshot_id } => {
                out.push(OP_RELEASE);
                out.extend_from_slice(snapshot_id);
            }
        }
        Ok(out)
    }

    /// # Errors
    /// Refuses wrong versions, unknown ops/bounds, malformed witnesses or cursors,
    /// oversize pages, a noncanonical exhausted flag and trailing bytes.
    pub fn decode(bytes: &[u8]) -> Result<Self, CapsCodecError> {
        let mut reader = Reader::new(bytes);
        let response = match reader.header()? {
            OP_OPEN => Self::Open(CapsSnapshotOpen {
                snapshot_id: reader.b32()?,
                network_id: reader.u32()?,
                state_root: reader.b32()?,
                observed_sequence: reader.u64()?,
                expires_at_ms: reader.u64()?,
                selection_digest: reader.b32()?,
                requested_rank: reader.u8()?,
                account_value: reader.length_prefixed()?.to_vec(),
                account_evidence: reader.length_prefixed()?.to_vec(),
                budget_module: reader.span()?,
                grant_module: reader.span()?,
                first_cursor: reader.cursor()?,
            }),
            OP_PAGE => {
                let cursor_echo = reader.cursor()?;
                let count = reader.u16()?;
                if count > CAPS_PAGE_MAX_ITEMS {
                    return Err(CapsCodecError::Limit);
                }
                let mut items = Vec::with_capacity(usize::from(count));
                for _ in 0..count {
                    items.push(reader.witness()?);
                }
                let lower = reader.bound()?;
                let upper = reader.bound()?;
                let exhausted = match reader.u8()? {
                    0 => false,
                    1 => true,
                    _ => return Err(CapsCodecError::Bound),
                };
                let next_cursor = if exhausted {
                    None
                } else {
                    Some(reader.cursor()?)
                };
                Self::Page(CapsPage {
                    cursor_echo,
                    items,
                    lower,
                    upper,
                    exhausted,
                    next_cursor,
                })
            }
            OP_RELEASE => Self::Release {
                snapshot_id: reader.b32()?,
            },
            _ => return Err(CapsCodecError::Op),
        };
        reader.finish()?;
        Ok(response)
    }
}
