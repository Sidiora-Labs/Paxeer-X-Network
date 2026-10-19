use std::time::{Duration, Instant};

use crate::evidence::{verify_caps_object, CapsEvidenceError, RootSelector, VerifiedCaps};
use crate::handover::SequencerHistory;
use crate::lni::capabilities::Capabilities;
use crate::lni::refusal::{decode_core_refusal, CoreRefusal};
use crate::lni::schema::{decode_envelope, encode_envelope, Capability, Envelope, SchemaError};
use crate::lni::transport::{FrameTransport, TransportError};
use crate::read::ReadContext;

pub const CAPS_REQUEST_BYTES: usize = 177;
pub const CAPS_RESPONSE_HEADER_BYTES: usize = 119;
pub const MAX_CAPS_PAGE_BYTES: usize = 1_048_576;
pub const MAX_CAPS_OBJECT_BYTES: usize = 64 * 1024 * 1024;

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum CapsError {
    Unavailable,
    Selection,
    Bounds,
    Expired,
    Terminal,
    Transport(TransportError),
    Envelope(SchemaError),
    Core(CoreRefusal),
    UnexpectedResponse,
    Page,
    Evidence(CapsEvidenceError),
}

#[derive(Clone, Debug)]
pub enum CapsProgress {
    Incomplete { received_bytes: u32, total_bytes: u32 },
    Complete(VerifiedCaps),
    Empty(VerifiedCaps),
    Refused(CapsError),
    Unavailable,
}

pub struct CapsDiscovery<'a> {
    transport: &'a mut dyn FrameTransport,
    history: Option<&'a SequencerHistory>,
    context: ReadContext,
    did: [u8; 32],
    page_bytes: u32,
    snapshot: [u8; 32],
    root: [u8; 32],
    cursor: [u8; 32],
    total: Option<u32>,
    bytes: Vec<u8>,
    started: Instant,
    deadline: Duration,
    terminal: bool,
}

impl<'a> CapsDiscovery<'a> {
    pub fn begin(transport: &'a mut dyn FrameTransport, capabilities: &Capabilities,
        context: ReadContext, did: [u8; 32], page_bytes: u32, deadline: Duration,
        history: Option<&'a SequencerHistory>) -> Result<Self, CapsError> {
        if !capabilities.contains(Capability::CapsDiscovery) || context.interface_version.major != 1
            || context.interface_version.minor < 8 || context.expected_protocol_version != 3
            || context.requested.level().wire_rank() > 4
        { return Err(CapsError::Unavailable); }
        if did == [0; 32] || context.expected_network_id == 0 || context.correlation_id == 0
            || matches!(context.root_selector, RootSelector::Batch(0))
            || matches!(context.root_selector, RootSelector::Checkpoint(id) if id == [0; 32])
        { return Err(CapsError::Selection); }
        if page_bytes == 0 || usize::try_from(page_bytes).map_err(|_| CapsError::Bounds)? > MAX_CAPS_PAGE_BYTES
            || deadline.is_zero() { return Err(CapsError::Bounds); }
        Ok(Self { transport, history, context, did, page_bytes, snapshot: [0; 32], root: [0; 32],
            cursor: [0; 32], total: None, bytes: Vec::new(), started: Instant::now(), deadline, terminal: false })
    }

    pub fn advance(&mut self) -> CapsProgress {
        if self.terminal { return CapsProgress::Refused(CapsError::Terminal); }
        match self.read_page() {
            Ok(value) => value,
            Err(error) => {
                self.terminal = true; self.bytes.clear(); self.bytes.shrink_to_fit();
                if error == CapsError::Unavailable { CapsProgress::Unavailable } else { CapsProgress::Refused(error) }
            }
        }
    }

    fn request(&self) -> Result<[u8; CAPS_REQUEST_BYTES], CapsError> {
        let mut request = [0; CAPS_REQUEST_BYTES];
        request[..2].copy_from_slice(&1_u16.to_be_bytes());
        request[2] = u8::from(self.total.is_some());
        request[3..7].copy_from_slice(&self.context.expected_network_id.to_be_bytes());
        request[7..39].copy_from_slice(&self.did);
        match self.context.root_selector {
            RootSelector::Latest => request[39] = 1,
            RootSelector::Batch(batch) => { request[39] = 2; request[40..48].copy_from_slice(&batch.to_be_bytes()); }
            RootSelector::Checkpoint(id) => { request[39] = 3; request[40..72].copy_from_slice(&id); }
        }
        request[72] = self.context.requested.level().wire_rank();
        request[73..77].copy_from_slice(&self.page_bytes.to_be_bytes());
        request[77..109].copy_from_slice(&self.snapshot);
        request[109..141].copy_from_slice(&self.root);
        request[141..145].copy_from_slice(&u32::try_from(self.bytes.len()).map_err(|_| CapsError::Bounds)?.to_be_bytes());
        request[145..177].copy_from_slice(&self.cursor);
        Ok(request)
    }

    fn read_page(&mut self) -> Result<CapsProgress, CapsError> {
        if self.started.elapsed() >= self.deadline { return Err(CapsError::Expired); }
        let request = self.request()?;
        self.transport.send(&encode_envelope(Envelope { version: self.context.interface_version,
            message_tag: 42, correlation_id: self.context.correlation_id, canonical_payload: &request,
            proof_material: &[] }).map_err(CapsError::Envelope)?).map_err(CapsError::Transport)?;
        let frame = self.transport.receive().map_err(CapsError::Transport)?;
        if self.started.elapsed() >= self.deadline { return Err(CapsError::Expired); }
        if frame.len() > MAX_CAPS_PAGE_BYTES + CAPS_RESPONSE_HEADER_BYTES + 22 { return Err(CapsError::Bounds); }
        let response = decode_envelope(&frame).map_err(CapsError::Envelope)?;
        if response.version != self.context.interface_version || response.correlation_id != self.context.correlation_id
            || !response.proof_material.is_empty() { return Err(CapsError::UnexpectedResponse); }
        if response.message_tag == 25 {
            return Err(CapsError::Core(decode_core_refusal(response.canonical_payload).ok_or(CapsError::UnexpectedResponse)?));
        }
        if response.message_tag != 43 { return Err(CapsError::UnexpectedResponse); }
        let payload = response.canonical_payload;
        if payload.len() < CAPS_RESPONSE_HEADER_BYTES || payload[..2] != [0, 1] { return Err(CapsError::Page); }
        let snapshot: [u8; 32] = payload[2..34].try_into().map_err(|_| CapsError::Page)?;
        let network = number(payload, 34)?;
        let root: [u8; 32] = payload[38..70].try_into().map_err(|_| CapsError::Page)?;
        let offset = number(payload, 70)?; let total = number(payload, 74)?; let next = number(payload, 78)?;
        let done = payload[82];
        let cursor: [u8; 32] = payload[83..115].try_into().map_err(|_| CapsError::Page)?;
        let length = number(payload, 115)?;
        if snapshot == [0; 32] || root == [0; 32] || network != self.context.expected_network_id || done > 1
            || total == 0 || usize::try_from(total).map_err(|_| CapsError::Bounds)? > MAX_CAPS_OBJECT_BYTES
            || usize::try_from(offset).map_err(|_| CapsError::Bounds)? != self.bytes.len()
            || length == 0 || length > self.page_bytes || offset.checked_add(length) != Some(next)
            || next > total || (done == 1) != (next == total)
            || usize::try_from(length).map_err(|_| CapsError::Bounds)? != payload.len() - CAPS_RESPONSE_HEADER_BYTES
            || (done == 1 && cursor != [0; 32])
            || (done == 0 && (cursor == [0; 32] || cursor == self.cursor))
        { return Err(CapsError::Page); }
        if let Some(expected_total) = self.total {
            if snapshot != self.snapshot || root != self.root || total != expected_total { return Err(CapsError::Page); }
        }
        self.snapshot = snapshot; self.root = root; self.total = Some(total); self.cursor = cursor;
        self.bytes.try_reserve(usize::try_from(length).map_err(|_| CapsError::Bounds)?).map_err(|_| CapsError::Bounds)?;
        self.bytes.extend_from_slice(&payload[CAPS_RESPONSE_HEADER_BYTES..]);
        if done == 0 { return Ok(CapsProgress::Incomplete { received_bytes: next, total_bytes: total }); }
        self.terminal = true;
        let verified = verify_caps_object(&self.bytes, self.did, self.root, self.context, self.history).map_err(CapsError::Evidence)?;
        self.bytes.clear(); self.bytes.shrink_to_fit();
        Ok(if verified.is_empty() { CapsProgress::Empty(verified) } else { CapsProgress::Complete(verified) })
    }
}

fn number(bytes: &[u8], offset: usize) -> Result<u32, CapsError> {
    Ok(u32::from_be_bytes(bytes.get(offset..offset+4).ok_or(CapsError::Page)?.try_into().map_err(|_| CapsError::Page)?))
}

impl std::fmt::Display for CapsError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result { write!(f, "{self:?}") }
}
impl std::error::Error for CapsError {}
