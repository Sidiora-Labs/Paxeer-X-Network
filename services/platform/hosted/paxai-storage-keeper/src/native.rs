use std::io::{Read, Write};
use std::os::unix::net::UnixStream;
use std::path::Path;
use std::time::Duration;

use layerx_platform_authority::ai_storage_admission::{verify_answer, AdmissionError};

const MAJOR: u16 = 1;
const MINOR: u16 = 8;
const NODE_INFO_REQUEST: u16 = 1;
const NODE_INFO_RESPONSE: u16 = 2;
const ERROR_RESPONSE: u16 = 25;
const ENVELOPE_FIXED_BYTES: usize = 22;
const MAX_RESPONSE_BYTES: usize = 1 << 20;
const IO_TIMEOUT: Duration = Duration::from_secs(15);

#[derive(Debug)]
pub enum NativeError {
    Io(std::io::Error),
    Protocol(&'static str),
    Refused { class: u8, result: i32 },
    Answer(AdmissionError),
}

impl core::fmt::Display for NativeError {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self {
            Self::Io(error) => write!(formatter, "native io: {error}"),
            Self::Protocol(what) => write!(formatter, "native protocol: {what}"),
            Self::Refused { class, result } => {
                write!(formatter, "refused class={class} result={result}")
            }
            Self::Answer(error) => write!(formatter, "answer: {error}"),
        }
    }
}

impl From<std::io::Error> for NativeError {
    fn from(error: std::io::Error) -> Self {
        Self::Io(error)
    }
}

/// A verified capacity answer: the exact payload and the proof that signs it.
pub struct Answer {
    pub payload: Vec<u8>,
    pub proof: Vec<u8>,
}

/// One authenticated LNI connection to the sequencer node.
pub struct Client {
    stream: UnixStream,
    correlation: u64,
    network_id: u32,
    sequencer: [u8; 32],
}

struct Envelope {
    tag: u16,
    correlation: u64,
    payload: Vec<u8>,
    proof: Vec<u8>,
}

impl Client {
    /// Connects and completes the node-info handshake at the pinned minor.
    ///
    /// # Errors
    ///
    /// Returns an I/O error, or a protocol error when the node does not answer
    /// the handshake at the pinned version.
    pub fn connect(
        socket: &Path,
        network_id: u32,
        sequencer: [u8; 32],
    ) -> Result<Self, NativeError> {
        let stream = UnixStream::connect(socket)?;
        stream.set_read_timeout(Some(IO_TIMEOUT))?;
        stream.set_write_timeout(Some(IO_TIMEOUT))?;
        let mut client = Self {
            stream,
            correlation: 0,
            network_id,
            sequencer,
        };
        client.send(NODE_INFO_REQUEST, 0, &[])?;
        let hello = client.receive()?;
        if hello.tag != NODE_INFO_RESPONSE || hello.correlation != 0 || !hello.proof.is_empty() {
            return Err(NativeError::Protocol("handshake"));
        }
        Ok(client)
    }

    /// Sends one capacity request and returns its verified answer.
    ///
    /// # Errors
    ///
    /// Returns `Refused` with the node's class and result, `Answer` when the
    /// proof does not verify against the pinned sequencer, or an I/O or
    /// protocol error.
    pub fn call(&mut self, tag: u16, body: &[u8]) -> Result<Answer, NativeError> {
        self.correlation = self
            .correlation
            .checked_add(1)
            .ok_or(NativeError::Protocol("correlation exhausted"))?;
        let correlation = self.correlation;
        self.send(tag, correlation, body)?;
        let answer = self.receive()?;
        if answer.correlation != correlation {
            return Err(NativeError::Protocol("correlation"));
        }
        if answer.tag == ERROR_RESPONSE {
            return match answer.payload.as_slice() {
                [class, a, b, c, d] if answer.proof.is_empty() => Err(NativeError::Refused {
                    class: *class,
                    result: i32::from_be_bytes([*a, *b, *c, *d]),
                }),
                _ => Err(NativeError::Protocol("error response")),
            };
        }
        if Some(answer.tag) != tag.checked_add(1) {
            return Err(NativeError::Protocol("response tag"));
        }
        verify_answer(
            self.network_id,
            answer.tag,
            &answer.payload,
            &answer.proof,
            &self.sequencer,
        )
        .map_err(NativeError::Answer)?;
        Ok(Answer {
            payload: answer.payload,
            proof: answer.proof,
        })
    }

    fn send(&mut self, tag: u16, correlation: u64, payload: &[u8]) -> Result<(), NativeError> {
        let payload_length =
            u32::try_from(payload.len()).map_err(|_| NativeError::Protocol("payload length"))?;
        let mut frame = Vec::with_capacity(4 + ENVELOPE_FIXED_BYTES + payload.len());
        let length = u32::try_from(ENVELOPE_FIXED_BYTES + payload.len())
            .map_err(|_| NativeError::Protocol("frame length"))?;
        frame.extend_from_slice(&length.to_be_bytes());
        frame.extend_from_slice(&MAJOR.to_be_bytes());
        frame.extend_from_slice(&MINOR.to_be_bytes());
        frame.extend_from_slice(&tag.to_be_bytes());
        frame.extend_from_slice(&correlation.to_be_bytes());
        frame.extend_from_slice(&payload_length.to_be_bytes());
        frame.extend_from_slice(payload);
        frame.extend_from_slice(&0_u32.to_be_bytes());
        self.stream.write_all(&frame)?;
        Ok(())
    }

    fn receive(&mut self) -> Result<Envelope, NativeError> {
        let mut prefix = [0_u8; 4];
        self.stream.read_exact(&mut prefix)?;
        let length = usize::try_from(u32::from_be_bytes(prefix))
            .map_err(|_| NativeError::Protocol("frame length"))?;
        if !(ENVELOPE_FIXED_BYTES..=MAX_RESPONSE_BYTES).contains(&length) {
            return Err(NativeError::Protocol("frame length"));
        }
        let mut frame = vec![0_u8; length];
        self.stream.read_exact(&mut frame)?;
        let field = |start: usize, end: usize| {
            frame
                .get(start..end)
                .ok_or(NativeError::Protocol("short frame"))
        };
        let major = u16::from_be_bytes([field(0, 1)?[0], field(1, 2)?[0]]);
        let minor = u16::from_be_bytes([field(2, 3)?[0], field(3, 4)?[0]]);
        let tag = u16::from_be_bytes([field(4, 5)?[0], field(5, 6)?[0]]);
        let mut correlation = [0_u8; 8];
        correlation.copy_from_slice(field(6, 14)?);
        let mut payload_length = [0_u8; 4];
        payload_length.copy_from_slice(field(14, 18)?);
        let payload_end = usize::try_from(u32::from_be_bytes(payload_length))
            .ok()
            .and_then(|value| value.checked_add(18))
            .ok_or(NativeError::Protocol("payload length"))?;
        let payload = field(18, payload_end)?.to_vec();
        let proof_start = payload_end
            .checked_add(4)
            .ok_or(NativeError::Protocol("proof length"))?;
        let mut proof_length = [0_u8; 4];
        proof_length.copy_from_slice(field(payload_end, proof_start)?);
        let proof_end = usize::try_from(u32::from_be_bytes(proof_length))
            .ok()
            .and_then(|value| value.checked_add(proof_start))
            .ok_or(NativeError::Protocol("proof length"))?;
        if proof_end != length || major != MAJOR || minor != MINOR {
            return Err(NativeError::Protocol("envelope"));
        }
        Ok(Envelope {
            tag,
            correlation: u64::from_be_bytes(correlation),
            payload,
            proof: field(proof_start, proof_end)?.to_vec(),
        })
    }
}
