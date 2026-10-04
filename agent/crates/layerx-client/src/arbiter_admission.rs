use std::time::{Duration, Instant};

use layerx_proof::receipt::VerifiedReceipt;

use crate::evidence::{
    verify_arbiter_admission_v3, AdmissionEvidenceError, VerifiedAdmissionPrestate,
    MAX_ADMISSION_PRESTATE_BYTES,
};
use crate::lni::capabilities::Capabilities;
use crate::lni::refusal::{decode_core_refusal, CoreRefusal};
use crate::lni::schema::{
    decode_envelope_with_schema, encode_envelope_with_schema, lni_schema_arbiter_admission_v3,
    Capability, Envelope, SchemaError, Version,
};
use crate::lni::transport::{FrameTransport, TransportError};

pub const ADMISSION_PRESTATE_REQUEST_BYTES: usize = 187;
pub const ADMISSION_PRESTATE_RESPONSE_HEADER_BYTES: usize = 191;
pub const MAX_ADMISSION_PRESTATE_PAGE_BYTES: usize = 1_048_576;

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum AdmissionError {
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
    Evidence(AdmissionEvidenceError),
}

#[derive(Clone, Debug)]
pub enum AdmissionProgress {
    Incomplete {
        received_bytes: u32,
        total_bytes: u32,
    },
    Complete(VerifiedAdmissionPrestate),
    Refused(AdmissionError),
    Unavailable,
}

pub struct AdmissionDiscovery<'a> {
    transport: &'a mut dyn FrameTransport,
    anchor: &'a VerifiedReceipt,
    network_id: u32,
    correlation_id: u64,
    activity_id: [u8; 32],
    receipt_digest: [u8; 32],
    execution_sequence: u64,
    previous_state_root: [u8; 32],
    page_bytes: u32,
    snapshot: [u8; 32],
    cursor: [u8; 32],
    total: Option<u32>,
    bytes: Vec<u8>,
    started: Instant,
    deadline: Duration,
    terminal: bool,
}

impl<'a> AdmissionDiscovery<'a> {
    pub fn begin(
        transport: &'a mut dyn FrameTransport,
        capabilities: &Capabilities,
        interface_version: Version,
        network_id: u32,
        correlation_id: u64,
        anchor: &'a VerifiedReceipt,
        page_bytes: u32,
        deadline: Duration,
    ) -> Result<Self, AdmissionError> {
        if !capabilities.contains(Capability::ArbiterAdmissionV3)
            || interface_version.major != 1
            || interface_version.minor < 11
        {
            return Err(AdmissionError::Unavailable);
        }
        let receipt = anchor
            .receipt()
            .protocol()
            .ok_or(AdmissionError::Selection)?;
        if receipt.protocol_version() != 3
            || receipt.module_id() != 9
            || receipt.activity_id() == [0; 32]
            || receipt.previous_state_root() == [0; 32]
            || receipt.global_sequence() == 0
            || network_id == 0
            || correlation_id == 0
        {
            return Err(AdmissionError::Selection);
        }
        let unsigned = layerx_wire::receipt::encode_unsigned(anchor.receipt())
            .map_err(|_| AdmissionError::Selection)?;
        let receipt_digest =
            layerx_wire::hash::receipt_digest(&unsigned).map_err(|_| AdmissionError::Selection)?;
        if page_bytes == 0
            || usize::try_from(page_bytes).map_err(|_| AdmissionError::Bounds)?
                > MAX_ADMISSION_PRESTATE_PAGE_BYTES
            || deadline.is_zero()
        {
            return Err(AdmissionError::Bounds);
        }
        Ok(Self {
            transport,
            anchor,
            network_id,
            correlation_id,
            activity_id: receipt.activity_id(),
            receipt_digest,
            execution_sequence: receipt.global_sequence(),
            previous_state_root: receipt.previous_state_root(),
            page_bytes,
            snapshot: [0; 32],
            cursor: [0; 32],
            total: None,
            bytes: Vec::new(),
            started: Instant::now(),
            deadline,
            terminal: false,
        })
    }

    pub fn advance(&mut self) -> AdmissionProgress {
        if self.terminal {
            return AdmissionProgress::Refused(AdmissionError::Terminal);
        }
        match self.read_page() {
            Ok(progress) => progress,
            Err(error) => {
                self.terminal = true;
                self.bytes.clear();
                self.bytes.shrink_to_fit();
                if error == AdmissionError::Unavailable {
                    AdmissionProgress::Unavailable
                } else {
                    AdmissionProgress::Refused(error)
                }
            }
        }
    }

    fn request(&self) -> Result<[u8; ADMISSION_PRESTATE_REQUEST_BYTES], AdmissionError> {
        let mut request = [0; ADMISSION_PRESTATE_REQUEST_BYTES];
        request[..2].copy_from_slice(&3_u16.to_be_bytes());
        request[2] = u8::from(self.total.is_some());
        request[3..7].copy_from_slice(&self.network_id.to_be_bytes());
        request[7..39].copy_from_slice(&self.activity_id);
        request[39..71].copy_from_slice(&self.receipt_digest);
        request[71..79].copy_from_slice(&self.execution_sequence.to_be_bytes());
        request[79..111].copy_from_slice(&self.previous_state_root);
        request[111..115].copy_from_slice(&self.page_bytes.to_be_bytes());
        request[115..147].copy_from_slice(&self.snapshot);
        request[147..151].copy_from_slice(
            &u32::try_from(self.bytes.len())
                .map_err(|_| AdmissionError::Bounds)?
                .to_be_bytes(),
        );
        request[151..183].copy_from_slice(&self.cursor);
        request[183..187].copy_from_slice(&self.total.unwrap_or(0).to_be_bytes());
        Ok(request)
    }

    fn read_page(&mut self) -> Result<AdmissionProgress, AdmissionError> {
        if self.started.elapsed() >= self.deadline {
            return Err(AdmissionError::Expired);
        }
        let request = self.request()?;
        let schema = lni_schema_arbiter_admission_v3();
        self.transport
            .send(
                &encode_envelope_with_schema(
                    Envelope {
                        version: Version::V1_11,
                        message_tag: 48,
                        correlation_id: self.correlation_id,
                        canonical_payload: &request,
                        proof_material: &[],
                    },
                    schema,
                )
                .map_err(AdmissionError::Envelope)?,
            )
            .map_err(AdmissionError::Transport)?;
        let frame = self
            .transport
            .receive()
            .map_err(AdmissionError::Transport)?;
        if self.started.elapsed() >= self.deadline {
            return Err(AdmissionError::Expired);
        }
        if frame.len()
            > MAX_ADMISSION_PRESTATE_PAGE_BYTES + ADMISSION_PRESTATE_RESPONSE_HEADER_BYTES + 22
        {
            return Err(AdmissionError::Bounds);
        }
        let response =
            decode_envelope_with_schema(&frame, schema).map_err(AdmissionError::Envelope)?;
        if response.version != Version::V1_11
            || response.correlation_id != self.correlation_id
            || !response.proof_material.is_empty()
        {
            return Err(AdmissionError::UnexpectedResponse);
        }
        if response.message_tag == 25 {
            return Err(AdmissionError::Core(
                decode_core_refusal(response.canonical_payload)
                    .ok_or(AdmissionError::UnexpectedResponse)?,
            ));
        }
        if response.message_tag != 49 {
            return Err(AdmissionError::UnexpectedResponse);
        }
        let payload = response.canonical_payload;
        if payload.len() < ADMISSION_PRESTATE_RESPONSE_HEADER_BYTES || payload[..2] != [0, 3] {
            return Err(AdmissionError::Page);
        }
        let snapshot: [u8; 32] = payload[2..34]
            .try_into()
            .map_err(|_| AdmissionError::Page)?;
        let network = number(payload, 34)?;
        let root: [u8; 32] = payload[38..70]
            .try_into()
            .map_err(|_| AdmissionError::Page)?;
        let offset = number(payload, 70)?;
        let total = number(payload, 74)?;
        let next = number(payload, 78)?;
        let done = payload[82];
        let cursor: [u8; 32] = payload[83..115]
            .try_into()
            .map_err(|_| AdmissionError::Page)?;
        let length = number(payload, 115)?;
        let sequence = u64::from_be_bytes(
            payload[183..191]
                .try_into()
                .map_err(|_| AdmissionError::Page)?,
        );
        if snapshot == [0; 32]
            || network != self.network_id
            || root != self.previous_state_root
            || payload[119..151] != self.activity_id
            || payload[151..183] != self.receipt_digest
            || sequence != self.execution_sequence
            || done > 1
            || total == 0
            || usize::try_from(total).map_err(|_| AdmissionError::Bounds)?
                > MAX_ADMISSION_PRESTATE_BYTES
            || usize::try_from(offset).map_err(|_| AdmissionError::Bounds)? != self.bytes.len()
            || length == 0
            || length > self.page_bytes
            || offset.checked_add(length) != Some(next)
            || next > total
            || (done == 1) != (next == total)
            || usize::try_from(length).map_err(|_| AdmissionError::Bounds)?
                != payload.len() - ADMISSION_PRESTATE_RESPONSE_HEADER_BYTES
            || (done == 1 && cursor != [0; 32])
            || (done == 0 && (cursor == [0; 32] || cursor == self.cursor))
        {
            return Err(AdmissionError::Page);
        }
        if let Some(expected_total) = self.total {
            if snapshot != self.snapshot || total != expected_total {
                return Err(AdmissionError::Page);
            }
        }
        self.snapshot = snapshot;
        self.cursor = cursor;
        self.total = Some(total);
        self.bytes
            .try_reserve(usize::try_from(length).map_err(|_| AdmissionError::Bounds)?)
            .map_err(|_| AdmissionError::Bounds)?;
        self.bytes
            .extend_from_slice(&payload[ADMISSION_PRESTATE_RESPONSE_HEADER_BYTES..]);
        if done == 0 {
            return Ok(AdmissionProgress::Incomplete {
                received_bytes: next,
                total_bytes: total,
            });
        }
        self.terminal = true;
        let verified = verify_arbiter_admission_v3(&self.bytes, self.anchor, self.network_id)
            .map_err(AdmissionError::Evidence)?;
        self.bytes.clear();
        self.bytes.shrink_to_fit();
        if self.started.elapsed() >= self.deadline {
            return Err(AdmissionError::Expired);
        }
        Ok(AdmissionProgress::Complete(verified))
    }
}

fn number(bytes: &[u8], offset: usize) -> Result<u32, AdmissionError> {
    Ok(u32::from_be_bytes(
        bytes
            .get(offset..offset + 4)
            .ok_or(AdmissionError::Page)?
            .try_into()
            .map_err(|_| AdmissionError::Page)?,
    ))
}

impl std::fmt::Display for AdmissionError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{self:?}")
    }
}
impl std::error::Error for AdmissionError {}
