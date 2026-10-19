//! Pull-driven ordered event streaming with durable cursors and explicit gaps.

use crate::head::Head;
use crate::lni::schema::{decode_envelope, encode_envelope, Envelope, SchemaError, Version};
use crate::lni::transport::{FrameTransport, TransportError};

const EVENT_SUBSCRIBE_REQUEST_TAG: u16 = 21;
const EVENT_RECORD_TAG: u16 = 22;
const EVENT_GAP_TAG: u16 = 23;
const EVENT_HEARTBEAT_TAG: u16 = 24;
const CURSOR_BYTES: usize = 48;

/// Durable next-event position, bound to the head and checkpoint observed when
/// it was advanced.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Cursor {
    next_sequence: u64,
    observed_head_sequence: u64,
    observed_checkpoint: [u8; 32],
}

impl Cursor {
    #[must_use]
    pub const fn new(next_sequence: u64, head: Head) -> Self {
        Self {
            next_sequence,
            observed_head_sequence: head.chain_sequence,
            observed_checkpoint: head.finalised_checkpoint,
        }
    }

    #[must_use]
    pub const fn next_sequence(self) -> u64 {
        self.next_sequence
    }

    /// Encodes the complete stable restart token.
    #[must_use]
    pub fn encode(self) -> [u8; CURSOR_BYTES] {
        let mut bytes = [0_u8; CURSOR_BYTES];
        bytes[..8].copy_from_slice(&self.next_sequence.to_be_bytes());
        bytes[8..16].copy_from_slice(&self.observed_head_sequence.to_be_bytes());
        bytes[16..].copy_from_slice(&self.observed_checkpoint);
        bytes
    }

    /// Decodes an exact stable restart token.
    ///
    /// # Errors
    ///
    /// Refuses every non-exact token length.
    pub fn decode(bytes: &[u8]) -> Result<Self, CursorError> {
        let encoded: [u8; CURSOR_BYTES] = bytes
            .try_into()
            .map_err(|_| CursorError::Length(bytes.len()))?;
        Ok(Self {
            next_sequence: u64::from_be_bytes(
                encoded[..8]
                    .try_into()
                    .map_err(|_| CursorError::Length(bytes.len()))?,
            ),
            observed_head_sequence: u64::from_be_bytes(
                encoded[8..16]
                    .try_into()
                    .map_err(|_| CursorError::Length(bytes.len()))?,
            ),
            observed_checkpoint: encoded[16..]
                .try_into()
                .map_err(|_| CursorError::Length(bytes.len()))?,
        })
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum CursorError {
    Length(usize),
}

/// Exact missing sequence interval and the cursor used to request backfill.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Gap {
    pub missing_first: u64,
    pub missing_last: u64,
    backfill_cursor: Cursor,
}

impl Gap {
    #[must_use]
    pub const fn backfill_cursor(self) -> Cursor {
        self.backfill_cursor
    }
}

/// One unmodified core event record.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Event {
    pub global_sequence: u64,
    canonical_bytes: Vec<u8>,
    cursor: Cursor,
}

impl Event {
    #[must_use]
    pub fn canonical_bytes(&self) -> &[u8] {
        &self.canonical_bytes
    }

    #[must_use]
    pub const fn cursor(&self) -> Cursor {
        self.cursor
    }
}

/// An ordered event or an explicit barrier requiring backfill.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum StreamItem {
    Event(Event),
    Gap(Gap),
}

/// Finite stream bounds. Pull delivery is itself backpressure; these bounds
/// additionally reject an individual record or heartbeat run that exceeds the
/// consumer contract.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct StreamConfig {
    pub interface_version: Version,
    pub correlation_id: u64,
    pub maximum_buffered_events: u32,
    pub maximum_buffered_bytes: u32,
    pub maximum_heartbeats_per_poll: u16,
}

impl StreamConfig {
    fn validate(self) -> Result<Self, StreamError> {
        if self.maximum_buffered_events == 0
            || self.maximum_buffered_bytes == 0
            || self.maximum_heartbeats_per_poll == 0
        {
            Err(StreamError::InvalidLimits)
        } else {
            Ok(self)
        }
    }
}

/// Stream refusal with the exact durable position retained on disconnection or
/// backpressure.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum StreamError {
    Transport(TransportError),
    Envelope(SchemaError),
    InvalidLimits,
    UnexpectedResponse,
    MalformedRecord,
    SequenceOverflow,
    OutOfOrder {
        expected: u64,
        actual: u64,
    },
    Disconnected {
        cursor: Cursor,
        cause: TransportError,
    },
    Backpressure {
        cursor: Cursor,
        bytes: usize,
        maximum: usize,
    },
    HeartbeatLimit {
        cursor: Cursor,
    },
    UnavailableCapability,
    DisconnectedClient,
}

impl From<TransportError> for StreamError {
    fn from(value: TransportError) -> Self {
        Self::Transport(value)
    }
}

impl From<SchemaError> for StreamError {
    fn from(value: SchemaError) -> Self {
        Self::Envelope(value)
    }
}

/// Live pull stream. It never reads ahead of the consumer, so a slow consumer
/// cannot cause client-side unbounded growth or silent loss.
pub struct EventStream<'a> {
    transport: &'a mut dyn FrameTransport,
    config: StreamConfig,
    cursor: Cursor,
    stopped: bool,
}

impl EventStream<'_> {
    #[must_use]
    pub const fn cursor(&self) -> Cursor {
        self.cursor
    }

    /// Delivers the next exact record or explicit gap barrier.
    ///
    /// # Errors
    ///
    /// Refuses malformed, mismatched and out-of-order records; preserves a
    /// durable cursor on transport loss and backpressure.
    pub fn next_item(&mut self) -> Result<StreamItem, StreamError> {
        if self.stopped {
            return Err(StreamError::UnexpectedResponse);
        }
        let mut heartbeats = 0_u16;
        loop {
            let response_bytes =
                self.transport
                    .receive()
                    .map_err(|cause| StreamError::Disconnected {
                        cursor: self.cursor,
                        cause,
                    })?;
            let response = decode_envelope(&response_bytes)?;
            if response.version.major != self.config.interface_version.major
                || response.correlation_id != self.config.correlation_id
            {
                self.stopped = true;
                return Err(StreamError::UnexpectedResponse);
            }
            match response.message_tag {
                EVENT_RECORD_TAG => return self.record(response.canonical_payload),
                EVENT_GAP_TAG => return self.gap(response.canonical_payload),
                EVENT_HEARTBEAT_TAG => {
                    heartbeats = heartbeats.saturating_add(1);
                    if heartbeats > self.config.maximum_heartbeats_per_poll {
                        return Err(StreamError::HeartbeatLimit {
                            cursor: self.cursor,
                        });
                    }
                }
                _ => {
                    self.stopped = true;
                    return Err(StreamError::UnexpectedResponse);
                }
            }
        }
    }

    fn record(&mut self, bytes: &[u8]) -> Result<StreamItem, StreamError> {
        if bytes.len() > self.config.maximum_buffered_bytes as usize {
            return Err(StreamError::Backpressure {
                cursor: self.cursor,
                bytes: bytes.len(),
                maximum: self.config.maximum_buffered_bytes as usize,
            });
        }
        let sequence_bytes: [u8; 8] = bytes
            .get(..8)
            .ok_or(StreamError::MalformedRecord)?
            .try_into()
            .map_err(|_| StreamError::MalformedRecord)?;
        let sequence = u64::from_be_bytes(sequence_bytes);
        if sequence < self.cursor.next_sequence {
            self.stopped = true;
            return Err(StreamError::OutOfOrder {
                expected: self.cursor.next_sequence,
                actual: sequence,
            });
        }
        if sequence > self.cursor.next_sequence {
            self.stopped = true;
            return Ok(StreamItem::Gap(Gap {
                missing_first: self.cursor.next_sequence,
                missing_last: sequence.saturating_sub(1),
                backfill_cursor: self.cursor,
            }));
        }
        let canonical_bytes = bytes.get(8..).ok_or(StreamError::MalformedRecord)?.to_vec();
        let next_sequence = sequence
            .checked_add(1)
            .ok_or(StreamError::SequenceOverflow)?;
        self.cursor.next_sequence = next_sequence;
        Ok(StreamItem::Event(Event {
            global_sequence: sequence,
            canonical_bytes,
            cursor: self.cursor,
        }))
    }

    fn gap(&mut self, bytes: &[u8]) -> Result<StreamItem, StreamError> {
        let encoded: [u8; 16] = bytes.try_into().map_err(|_| StreamError::MalformedRecord)?;
        let first = u64::from_be_bytes(
            encoded[..8]
                .try_into()
                .map_err(|_| StreamError::MalformedRecord)?,
        );
        let last = u64::from_be_bytes(
            encoded[8..]
                .try_into()
                .map_err(|_| StreamError::MalformedRecord)?,
        );
        if first != self.cursor.next_sequence || last < first {
            self.stopped = true;
            return Err(StreamError::OutOfOrder {
                expected: self.cursor.next_sequence,
                actual: first,
            });
        }
        self.stopped = true;
        Ok(StreamItem::Gap(Gap {
            missing_first: first,
            missing_last: last,
            backfill_cursor: self.cursor,
        }))
    }
}

/// Starts or resumes the core event stream at exactly one durable cursor.
///
/// # Errors
///
/// Refuses disabled bounds and any request transport/schema failure.
pub fn subscribe(
    transport: &mut dyn FrameTransport,
    cursor: Cursor,
    config: StreamConfig,
) -> Result<EventStream<'_>, StreamError> {
    let config = config.validate()?;
    let mut selector = Vec::with_capacity(CURSOR_BYTES + 8);
    selector.extend_from_slice(&cursor.encode());
    selector.extend_from_slice(&config.maximum_buffered_events.to_be_bytes());
    selector.extend_from_slice(&config.maximum_buffered_bytes.to_be_bytes());
    let request = encode_envelope(Envelope {
        version: config.interface_version,
        message_tag: EVENT_SUBSCRIBE_REQUEST_TAG,
        correlation_id: config.correlation_id,
        canonical_payload: &selector,
        proof_material: &[],
    })?;
    transport.send(&request)?;
    Ok(EventStream {
        transport,
        config,
        cursor,
        stopped: false,
    })
}
