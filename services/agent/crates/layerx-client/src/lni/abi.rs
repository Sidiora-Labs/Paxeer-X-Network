#![allow(unsafe_code)]
//! Optional stable C ABI carrying only opaque handles and canonical frames.

use std::ffi::c_void;
use std::io::ErrorKind;
use std::ptr::NonNull;

use super::framing::{read_frame, write_frame, LENGTH_PREFIX_BYTES};
use super::transport::{FrameTransport, FrameViolation, TransportError};

/// Stable ABI major and minor version, independent from the LNI schema version.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct AbiVersion {
    pub major: u16,
    pub minor: u16,
}

impl AbiVersion {
    pub const V1_0: Self = Self { major: 1, minor: 0 };

    #[must_use]
    pub fn packed(self) -> u32 {
        (u32::from(self.major)) << 16 | u32::from(self.minor)
    }

    #[must_use]
    pub const fn from_packed(value: u32) -> Self {
        let bytes = value.to_be_bytes();
        Self {
            major: u16::from_be_bytes([bytes[0], bytes[1]]),
            minor: u16::from_be_bytes([bytes[2], bytes[3]]),
        }
    }
}

/// Explicit refusal for an ABI major mismatch.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct AbiIncompatible {
    pub built: AbiVersion,
    pub peer: AbiVersion,
}

/// Applies the same major-equality and additive-minor rule as socket LNI.
///
/// # Errors
///
/// Returns both versions when their major components differ.
pub const fn negotiate(built: AbiVersion, peer: AbiVersion) -> Result<AbiVersion, AbiIncompatible> {
    if built.major == peer.major {
        Ok(AbiVersion {
            major: built.major,
            minor: if built.minor < peer.minor {
                built.minor
            } else {
                peer.minor
            },
        })
    } else {
        Err(AbiIncompatible { built, peer })
    }
}

/// Published ABI callbacks resolved from the operator's stable ABI provider.
#[derive(Clone, Copy)]
pub struct Functions {
    pub version: unsafe extern "C" fn() -> u32,
    pub send: unsafe extern "C" fn(*mut c_void, *const u8, usize) -> i32,
    pub receive: unsafe extern "C" fn(*mut c_void, *mut *const u8, *mut usize) -> i32,
    pub release: unsafe extern "C" fn(*mut c_void, *const u8, usize),
    pub close: unsafe extern "C" fn(*mut c_void),
}

/// Sole Rust owner of an opaque ABI connection handle.
pub struct Handle {
    raw: NonNull<c_void>,
    functions: Functions,
    maximum_frame_bytes: usize,
}

impl Handle {
    /// Takes ownership of a provider-created opaque handle after negotiating
    /// the provider's ABI version.
    ///
    /// # Safety
    ///
    /// `raw` must be a live uniquely-owned handle for all supplied functions.
    /// The functions must follow `services/agent/include/layerx_lni_abi.h`, keep returned
    /// receive buffers valid until `release`, and tolerate exactly one `close`.
    ///
    /// # Errors
    ///
    /// Refuses a null handle, zero frame bound, or incompatible ABI major.
    pub unsafe fn from_raw(
        raw: *mut c_void,
        functions: Functions,
        maximum_frame_bytes: usize,
    ) -> Result<Self, AbiOpenError> {
        let raw = NonNull::new(raw).ok_or(AbiOpenError::NullHandle)?;
        if maximum_frame_bytes == 0 {
            return Err(AbiOpenError::FrameLimit);
        }
        // SAFETY: the caller guarantees that this function pointer belongs to
        // the provider associated with `raw` and follows the published header.
        let peer = AbiVersion::from_packed(unsafe { (functions.version)() });
        negotiate(AbiVersion::V1_0, peer).map_err(AbiOpenError::Incompatible)?;
        Ok(Self {
            raw,
            functions,
            maximum_frame_bytes,
        })
    }
}

/// Failure before an opaque handle can become a transport.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum AbiOpenError {
    NullHandle,
    FrameLimit,
    Incompatible(AbiIncompatible),
}

impl FrameTransport for Handle {
    fn send(&mut self, canonical_envelope: &[u8]) -> Result<(), TransportError> {
        let mut frame = Vec::new();
        write_frame(&mut frame, canonical_envelope, self.maximum_frame_bytes)?;
        // SAFETY: `from_raw` established the handle/function association, and
        // `frame` remains alive and immutable for the duration of this call.
        let status =
            unsafe { (self.functions.send)(self.raw.as_ptr(), frame.as_ptr(), frame.len()) };
        status_result(status)
    }

    fn receive(&mut self) -> Result<Vec<u8>, TransportError> {
        let mut pointer = std::ptr::null();
        let mut length = 0_usize;
        // SAFETY: `from_raw` established this callback contract; the two out
        // pointers refer to initialized local variables with valid lifetimes.
        let status = unsafe {
            (self.functions.receive)(self.raw.as_ptr(), &raw mut pointer, &raw mut length)
        };
        status_result(status)?;
        let maximum_wire_bytes = self
            .maximum_frame_bytes
            .checked_add(LENGTH_PREFIX_BYTES)
            .ok_or(TransportError::Frame(FrameViolation::LengthOverflow))?;
        if length > maximum_wire_bytes {
            if !pointer.is_null() {
                // SAFETY: a successful receive returned this provider-owned
                // pointer and length; release is required even when over-limit.
                unsafe { (self.functions.release)(self.raw.as_ptr(), pointer, length) };
            }
            return Err(TransportError::Frame(FrameViolation::Oversized {
                declared: u32::try_from(length).unwrap_or(u32::MAX),
                maximum: maximum_wire_bytes,
            }));
        }
        if pointer.is_null() {
            return Err(TransportError::PeerShutdown);
        }
        // SAFETY: successful receive promises `length` readable bytes at the
        // non-null pointer until release; the bound was checked before slicing.
        let frame = unsafe { std::slice::from_raw_parts(pointer, length) }.to_vec();
        // SAFETY: the slice has been copied, and this is the matching provider
        // release call with the exact pointer and length returned by receive.
        unsafe { (self.functions.release)(self.raw.as_ptr(), pointer, length) };
        let mut borrowed = frame.as_slice();
        let envelope = read_frame(&mut borrowed, self.maximum_frame_bytes)?;
        if !borrowed.is_empty() {
            return Err(TransportError::Frame(FrameViolation::TruncatedBody));
        }
        Ok(envelope)
    }
}

impl Drop for Handle {
    fn drop(&mut self) {
        // SAFETY: `Handle` owns the non-null raw value and Drop runs once; the
        // constructor contract requires the matching callback to accept it.
        unsafe { (self.functions.close)(self.raw.as_ptr()) };
    }
}

fn status_result(status: i32) -> Result<(), TransportError> {
    match status {
        0 => Ok(()),
        1 => Err(TransportError::Deadline),
        2 => Err(TransportError::PeerShutdown),
        4 => Err(TransportError::Frame(FrameViolation::LengthOverflow)),
        _ => Err(TransportError::ConnectionFailure(ErrorKind::Other)),
    }
}
