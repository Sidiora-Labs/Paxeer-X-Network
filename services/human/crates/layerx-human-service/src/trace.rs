//! Trace propagation for the human plane: one identifier travels from the
//! human-api request through the service into agent-layer calls and back onto
//! every typed error response, so support can retrieve exactly the failure
//! context the user saw.

use std::fmt::{Display, Formatter};

/// The header carrying the trace identifier on inbound human-api requests and
/// on every call the service makes into the agent layer.
pub const TRACE_HEADER: &str = "x-layerx-trace";

const TRACE_PREFIX: &str = "trc_";
const TRACE_HEX_LENGTH: usize = 32;

const fn is_lower_hex(byte: u8) -> bool {
    byte.is_ascii_digit() || matches!(byte, b'a'..=b'f')
}

const fn hex_digit(nibble: u8) -> char {
    match nibble {
        0..=9 => (b'0' + nibble) as char,
        _ => (b'a' + (nibble - 10)) as char,
    }
}

/// The trace identifier shown on every error surface, stamped on every audit
/// entry and telemetry emission, and propagated end to end unchanged.
#[derive(Clone, Debug, Eq, Hash, PartialEq)]
pub struct TraceId(String);

impl TraceId {
    /// Parses an identifier of the declared `trc_` shape.
    ///
    /// # Errors
    ///
    /// Refuses values without the prefix, of the wrong length, or with
    /// characters outside lowercase hexadecimal.
    pub fn parse(value: &str) -> Result<Self, TraceError> {
        let digits = value
            .strip_prefix(TRACE_PREFIX)
            .ok_or(TraceError::Malformed)?;
        if digits.len() != TRACE_HEX_LENGTH || !digits.bytes().all(is_lower_hex) {
            return Err(TraceError::Malformed);
        }
        Ok(Self(value.to_owned()))
    }

    /// Mints a fresh identifier from caller-supplied entropy.
    #[must_use]
    pub fn mint(entropy: [u8; 16]) -> Self {
        let mut value = String::with_capacity(TRACE_PREFIX.len() + TRACE_HEX_LENGTH);
        value.push_str(TRACE_PREFIX);
        for byte in entropy {
            value.push(hex_digit(byte >> 4));
            value.push(hex_digit(byte & 0x0f));
        }
        Self(value)
    }

    /// Adopts the inbound request's identifier when it is well formed and
    /// otherwise mints a fresh one, so every request travels under exactly
    /// one trace.
    #[must_use]
    pub fn from_inbound(header: Option<&str>, entropy: [u8; 16]) -> Self {
        header
            .and_then(|value| Self::parse(value).ok())
            .unwrap_or_else(|| Self::mint(entropy))
    }

    /// Returns the identifier.
    #[must_use]
    pub fn as_str(&self) -> &str {
        &self.0
    }

    /// Returns the header name and value to attach to an agent-layer call so
    /// the trace crosses the boundary unchanged.
    #[must_use]
    pub fn outbound(&self) -> (&'static str, &str) {
        (TRACE_HEADER, &self.0)
    }

    /// Binds this trace to a typed error so the failure response carries it.
    #[must_use]
    pub fn wrap<E>(&self, error: E) -> Traced<E> {
        Traced {
            trace: self.clone(),
            error,
        }
    }
}

impl Display for TraceId {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        formatter.write_str(&self.0)
    }
}

/// Trace identifier failures.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum TraceError {
    Malformed,
}

impl Display for TraceError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Malformed => formatter.write_str("malformed trace identifier"),
        }
    }
}

impl std::error::Error for TraceError {}

/// A typed error carrying the trace identifier of the request that failed,
/// so every error response names the trace support retrieves it by.
#[derive(Debug)]
pub struct Traced<E> {
    trace: TraceId,
    error: E,
}

impl<E> Traced<E> {
    /// Binds a trace identifier to a typed error.
    #[must_use]
    pub fn new(trace: TraceId, error: E) -> Self {
        Self { trace, error }
    }

    /// Returns the trace the failure travelled under.
    #[must_use]
    pub const fn trace(&self) -> &TraceId {
        &self.trace
    }

    /// Returns the underlying typed error.
    #[must_use]
    pub const fn error(&self) -> &E {
        &self.error
    }

    /// Consumes the wrapper and returns the underlying typed error.
    #[must_use]
    pub fn into_error(self) -> E {
        self.error
    }
}

impl<E: Display> Display for Traced<E> {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        write!(formatter, "{} (trace {})", self.error, self.trace)
    }
}

impl<E: std::error::Error + 'static> std::error::Error for Traced<E> {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        Some(&self.error)
    }
}
