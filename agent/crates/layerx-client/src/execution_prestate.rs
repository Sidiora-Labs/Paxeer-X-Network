use std::time::{Duration, Instant};

use layerx_proof::receipt::VerifiedReceipt;

use crate::evidence::{verify_native_execution_prestate_object, ExecutionPrestateEvidenceError, VerifiedExecutionPrestate, VerifiedNativeExecutionPrestate};
use crate::lni::capabilities::Capabilities;
use crate::lni::refusal::{decode_core_refusal, CoreRefusal};
use crate::lni::schema::{decode_envelope, encode_envelope, Capability, Envelope, SchemaError};
use crate::lni::transport::{FrameTransport, TransportError};
use crate::lni::schema::Version;

pub const CAPS_REQUEST_BYTES: usize = 177;
pub const CAPS_RESPONSE_HEADER_BYTES: usize = 119;
pub const MAX_CAPS_PAGE_BYTES: usize = 1_048_576;
pub const MAX_CAPS_OBJECT_BYTES: usize = 64 * 1024 * 1024;

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum ExecutionPrestateError {
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
    Evidence(ExecutionPrestateEvidenceError),
}

#[derive(Clone, Debug)]
pub enum ExecutionPrestateProgress {
    Incomplete { received_bytes: u32, total_bytes: u32 },
    Complete(VerifiedExecutionPrestate),
    Refused(ExecutionPrestateError),
    Unavailable,
}

#[derive(Clone, Debug)]
pub enum NativeExecutionPrestateProgress {
    Incomplete { received_bytes: u32, total_bytes: u32 },
    Complete(VerifiedNativeExecutionPrestate),
    Refused(ExecutionPrestateError),
    Unavailable,
}

pub struct NativeExecutionPrestateDiscovery<'a> {
    transport: &'a mut dyn FrameTransport,
    anchor: &'a VerifiedReceipt,
    interface_version: Version,
    network_id: u32,
    correlation_id: u64,
    activity_id: [u8; 32],
    receipt_digest: [u8; 32],
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

impl<'a> NativeExecutionPrestateDiscovery<'a> {
    pub fn begin(transport: &'a mut dyn FrameTransport, capabilities: &Capabilities,
        interface_version: Version, network_id: u32, correlation_id: u64, anchor: &'a VerifiedReceipt,
        page_bytes: u32, deadline: Duration) -> Result<Self, ExecutionPrestateError> {
        if !capabilities.contains(Capability::CapsDiscovery) || !capabilities.contains(Capability::ExecutionPrestate)
            || interface_version.major != 1 || interface_version.minor < 9
        { return Err(ExecutionPrestateError::Unavailable); }
        let receipt = anchor.receipt().protocol().ok_or(ExecutionPrestateError::Selection)?;
        if receipt.protocol_version() != 3 || receipt.module_id() != 9
            || receipt.activity_id() == [0; 32] || receipt.previous_state_root() == [0; 32]
            || receipt.global_sequence() == 0 || network_id == 0 || correlation_id == 0
        { return Err(ExecutionPrestateError::Selection); }
        let unsigned = layerx_wire::receipt::encode_unsigned(anchor.receipt()).map_err(|_| ExecutionPrestateError::Selection)?;
        let receipt_digest = layerx_wire::hash::receipt_digest(&unsigned).map_err(|_| ExecutionPrestateError::Selection)?;
        if page_bytes == 0 || usize::try_from(page_bytes).map_err(|_| ExecutionPrestateError::Bounds)? > MAX_CAPS_PAGE_BYTES
            || deadline.is_zero() { return Err(ExecutionPrestateError::Bounds); }
        Ok(Self { transport, anchor, interface_version, network_id, correlation_id, activity_id: receipt.activity_id(),
            receipt_digest, page_bytes, snapshot: [0; 32], root: [0; 32], cursor: [0; 32], total: None,
            bytes: Vec::new(), started: Instant::now(), deadline, terminal: false })
    }

    pub fn advance(&mut self) -> NativeExecutionPrestateProgress {
        if self.terminal { return NativeExecutionPrestateProgress::Refused(ExecutionPrestateError::Terminal); }
        match self.read_page() {
            Ok(value) => value,
            Err(error) => {
                self.terminal = true; self.bytes.clear(); self.bytes.shrink_to_fit();
                if error == ExecutionPrestateError::Unavailable { NativeExecutionPrestateProgress::Unavailable } else { NativeExecutionPrestateProgress::Refused(error) }
            }
        }
    }

    fn request(&self) -> Result<[u8; CAPS_REQUEST_BYTES], ExecutionPrestateError> {
        let mut request = [0; CAPS_REQUEST_BYTES];
        request[..2].copy_from_slice(&2_u16.to_be_bytes());
        request[2] = u8::from(self.total.is_some());
        request[3..7].copy_from_slice(&self.network_id.to_be_bytes());
        request[7..39].copy_from_slice(&self.activity_id);
        request[39..71].copy_from_slice(&self.receipt_digest);
        request[71..75].copy_from_slice(&self.page_bytes.to_be_bytes());
        request[75..107].copy_from_slice(&self.snapshot);
        request[107..139].copy_from_slice(&self.root);
        request[139..143].copy_from_slice(&u32::try_from(self.bytes.len()).map_err(|_| ExecutionPrestateError::Bounds)?.to_be_bytes());
        request[143..175].copy_from_slice(&self.cursor);
        Ok(request)
    }

    fn read_page(&mut self) -> Result<NativeExecutionPrestateProgress, ExecutionPrestateError> {
        if self.started.elapsed() >= self.deadline { return Err(ExecutionPrestateError::Expired); }
        let request = self.request()?;
        self.transport.send(&encode_envelope(Envelope { version: self.interface_version,
            message_tag: 44, correlation_id: self.correlation_id, canonical_payload: &request,
            proof_material: &[] }).map_err(ExecutionPrestateError::Envelope)?).map_err(ExecutionPrestateError::Transport)?;
        let frame = self.transport.receive().map_err(ExecutionPrestateError::Transport)?;
        if self.started.elapsed() >= self.deadline { return Err(ExecutionPrestateError::Expired); }
        if frame.len() > MAX_CAPS_PAGE_BYTES + CAPS_RESPONSE_HEADER_BYTES + 22 { return Err(ExecutionPrestateError::Bounds); }
        let response = decode_envelope(&frame).map_err(ExecutionPrestateError::Envelope)?;
        if response.version != self.interface_version || response.correlation_id != self.correlation_id
            || !response.proof_material.is_empty() { return Err(ExecutionPrestateError::UnexpectedResponse); }
        if response.message_tag == 25 {
            return Err(ExecutionPrestateError::Core(decode_core_refusal(response.canonical_payload).ok_or(ExecutionPrestateError::UnexpectedResponse)?));
        }
        if response.message_tag != 45 { return Err(ExecutionPrestateError::UnexpectedResponse); }
        let payload = response.canonical_payload;
        if payload.len() < CAPS_RESPONSE_HEADER_BYTES || payload[..2] != [0, 2] { return Err(ExecutionPrestateError::Page); }
        let snapshot: [u8; 32] = payload[2..34].try_into().map_err(|_| ExecutionPrestateError::Page)?;
        let network = number(payload, 34)?;
        let root: [u8; 32] = payload[38..70].try_into().map_err(|_| ExecutionPrestateError::Page)?;
        let offset = number(payload, 70)?; let total = number(payload, 74)?; let next = number(payload, 78)?;
        let done = payload[82];
        let cursor: [u8; 32] = payload[83..115].try_into().map_err(|_| ExecutionPrestateError::Page)?;
        let length = number(payload, 115)?;
        if snapshot == [0; 32] || root == [0; 32] || network != self.network_id || done > 1
            || self.anchor.receipt().protocol().is_none_or(|receipt| root != receipt.previous_state_root())
            || total == 0 || usize::try_from(total).map_err(|_| ExecutionPrestateError::Bounds)? > MAX_CAPS_OBJECT_BYTES
            || usize::try_from(offset).map_err(|_| ExecutionPrestateError::Bounds)? != self.bytes.len()
            || length == 0 || length > self.page_bytes || offset.checked_add(length) != Some(next)
            || next > total || (done == 1) != (next == total)
            || usize::try_from(length).map_err(|_| ExecutionPrestateError::Bounds)? != payload.len() - CAPS_RESPONSE_HEADER_BYTES
            || (done == 1 && cursor != [0; 32])
            || (done == 0 && (cursor == [0; 32] || cursor == self.cursor))
        { return Err(ExecutionPrestateError::Page); }
        if let Some(expected_total) = self.total {
            if snapshot != self.snapshot || root != self.root || total != expected_total { return Err(ExecutionPrestateError::Page); }
        }
        self.snapshot = snapshot; self.root = root; self.total = Some(total); self.cursor = cursor;
        self.bytes.try_reserve(usize::try_from(length).map_err(|_| ExecutionPrestateError::Bounds)?).map_err(|_| ExecutionPrestateError::Bounds)?;
        self.bytes.extend_from_slice(&payload[CAPS_RESPONSE_HEADER_BYTES..]);
        if done == 0 { return Ok(NativeExecutionPrestateProgress::Incomplete { received_bytes: next, total_bytes: total }); }
        self.terminal = true;
        let verified = verify_native_execution_prestate_object(&self.bytes, self.anchor, self.network_id).map_err(ExecutionPrestateError::Evidence)?;
        self.bytes.clear(); self.bytes.shrink_to_fit();
        Ok(NativeExecutionPrestateProgress::Complete(verified))
    }
}

pub struct ExecutionPrestateDiscovery<'a> {
    native: NativeExecutionPrestateDiscovery<'a>,
}

impl<'a> ExecutionPrestateDiscovery<'a> {
    pub fn begin(transport: &'a mut dyn FrameTransport, capabilities: &Capabilities,
        interface_version: Version, network_id: u32, correlation_id: u64, anchor: &'a VerifiedReceipt,
        page_bytes: u32, deadline: Duration) -> Result<Self, ExecutionPrestateError> {
        let receipt = anchor.receipt().protocol().ok_or(ExecutionPrestateError::Selection)?;
        if receipt.operation() != 3 || receipt.program_outcome().is_none_or(|outcome| outcome.encoding_version() != 4) {
            return Err(ExecutionPrestateError::Selection);
        }
        Ok(Self { native: NativeExecutionPrestateDiscovery::begin(transport, capabilities, interface_version,
            network_id, correlation_id, anchor, page_bytes, deadline)? })
    }

    pub fn advance(&mut self) -> ExecutionPrestateProgress {
        match self.native.advance() {
            NativeExecutionPrestateProgress::Incomplete { received_bytes, total_bytes } =>
                ExecutionPrestateProgress::Incomplete { received_bytes, total_bytes },
            NativeExecutionPrestateProgress::Complete(native) => match native.for_program_call(self.native.anchor) {
                Ok(call) => ExecutionPrestateProgress::Complete(call),
                Err(error) => ExecutionPrestateProgress::Refused(ExecutionPrestateError::Evidence(error)),
            },
            NativeExecutionPrestateProgress::Refused(error) => ExecutionPrestateProgress::Refused(error),
            NativeExecutionPrestateProgress::Unavailable => ExecutionPrestateProgress::Unavailable,
        }
    }
}

fn number(bytes: &[u8], offset: usize) -> Result<u32, ExecutionPrestateError> {
    Ok(u32::from_be_bytes(bytes.get(offset..offset+4).ok_or(ExecutionPrestateError::Page)?.try_into().map_err(|_| ExecutionPrestateError::Page)?))
}

impl std::fmt::Display for ExecutionPrestateError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result { write!(f, "{self:?}") }
}
impl std::error::Error for ExecutionPrestateError {}
