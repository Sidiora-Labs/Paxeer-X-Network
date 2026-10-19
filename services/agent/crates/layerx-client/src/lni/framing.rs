//! Bounded length-prefixed framing for canonical LNI envelopes.

use std::io::{ErrorKind, Read, Write};

use super::transport::{FrameViolation, TransportError};

/// Fixed width of the unsigned big-endian frame length.
pub const LENGTH_PREFIX_BYTES: usize = 4;

/// Result of parsing one frame from a borrowed receive buffer.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum DecodedFrame<'a> {
    /// More bytes are required; no body allocation was performed.
    Incomplete,
    /// One exact frame and the unconsumed buffer tail.
    Complete {
        payload: &'a [u8],
        remaining: &'a [u8],
    },
}

/// Parses one borrowed frame and rejects an excessive declared length before
/// materialising a body.
///
/// # Errors
///
/// Returns a frame violation for an over-limit length or arithmetic overflow.
pub fn decode_frame(bytes: &[u8], maximum: usize) -> Result<DecodedFrame<'_>, FrameViolation> {
    let Some(prefix) = bytes.get(..LENGTH_PREFIX_BYTES) else {
        return Ok(DecodedFrame::Incomplete);
    };
    let declared = u32::from_be_bytes(
        prefix
            .try_into()
            .map_err(|_| FrameViolation::TruncatedPrefix)?,
    );
    let length = usize::try_from(declared).map_err(|_| FrameViolation::LengthOverflow)?;
    if length > maximum {
        return Err(FrameViolation::Oversized { declared, maximum });
    }
    let end = LENGTH_PREFIX_BYTES
        .checked_add(length)
        .ok_or(FrameViolation::LengthOverflow)?;
    let Some(payload) = bytes.get(LENGTH_PREFIX_BYTES..end) else {
        return Ok(DecodedFrame::Incomplete);
    };
    let remaining = bytes.get(end..).ok_or(FrameViolation::LengthOverflow)?;
    Ok(DecodedFrame::Complete { payload, remaining })
}

/// Reads one bounded frame. The body allocation happens only after validating
/// the four-byte declared length.
///
/// # Errors
///
/// Preserves deadline, peer-shutdown, connection, and frame failures as
/// distinct transport classes.
pub fn read_frame<R: Read>(reader: &mut R, maximum: usize) -> Result<Vec<u8>, TransportError> {
    let mut prefix = [0_u8; LENGTH_PREFIX_BYTES];
    read_exact(reader, &mut prefix, true)?;
    let declared = u32::from_be_bytes(prefix);
    let length = usize::try_from(declared)
        .map_err(|_| TransportError::Frame(FrameViolation::LengthOverflow))?;
    if length > maximum {
        return Err(TransportError::Frame(FrameViolation::Oversized {
            declared,
            maximum,
        }));
    }
    let mut payload = vec![0_u8; length];
    read_exact(reader, &mut payload, false)?;
    Ok(payload)
}

/// Writes one bounded frame without changing its canonical payload.
///
/// # Errors
///
/// Returns a typed frame error before writing an excessive payload and
/// preserves deadline, shutdown, and connection failures from the writer.
pub fn write_frame<W: Write>(
    writer: &mut W,
    payload: &[u8],
    maximum: usize,
) -> Result<(), TransportError> {
    if payload.len() > maximum {
        let declared = u32::try_from(payload.len()).unwrap_or(u32::MAX);
        return Err(TransportError::Frame(FrameViolation::Oversized {
            declared,
            maximum,
        }));
    }
    let length = u32::try_from(payload.len())
        .map_err(|_| TransportError::Frame(FrameViolation::LengthOverflow))?;
    write_all(writer, &length.to_be_bytes())?;
    write_all(writer, payload)?;
    writer.flush().map_err(|error| map_io(&error))
}

fn read_exact<R: Read>(
    reader: &mut R,
    output: &mut [u8],
    prefix: bool,
) -> Result<(), TransportError> {
    let mut offset = 0;
    while offset < output.len() {
        match reader.read(&mut output[offset..]) {
            Ok(0) if offset == 0 => return Err(TransportError::PeerShutdown),
            Ok(0) => {
                let violation = if prefix {
                    FrameViolation::TruncatedPrefix
                } else {
                    FrameViolation::TruncatedBody
                };
                return Err(TransportError::Frame(violation));
            }
            Ok(read) => offset += read,
            Err(error) if error.kind() == ErrorKind::Interrupted => {}
            Err(error) => return Err(map_io(&error)),
        }
    }
    Ok(())
}

fn write_all<W: Write>(writer: &mut W, bytes: &[u8]) -> Result<(), TransportError> {
    let mut offset = 0;
    while offset < bytes.len() {
        match writer.write(&bytes[offset..]) {
            Ok(0) => return Err(TransportError::PeerShutdown),
            Ok(written) => offset += written,
            Err(error) if error.kind() == ErrorKind::Interrupted => {}
            Err(error) => return Err(map_io(&error)),
        }
    }
    Ok(())
}

fn map_io(error: &std::io::Error) -> TransportError {
    match error.kind() {
        ErrorKind::TimedOut | ErrorKind::WouldBlock => TransportError::Deadline,
        ErrorKind::BrokenPipe
        | ErrorKind::ConnectionAborted
        | ErrorKind::ConnectionReset
        | ErrorKind::NotConnected
        | ErrorKind::UnexpectedEof => TransportError::PeerShutdown,
        kind => TransportError::ConnectionFailure(kind),
    }
}
