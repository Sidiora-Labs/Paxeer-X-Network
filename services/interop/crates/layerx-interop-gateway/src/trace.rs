//! Trace propagation for the interoperability gateway: one identifier enters
//! with the external protocol request, crosses every translation boundary
//! unchanged, and lands on every audit entry, telemetry emission and typed
//! error response, exactly as the human plane propagates it.

use std::fmt::{Display, Formatter};

/// The header carrying the trace identifier on inbound adapter requests and
/// on every call the gateway makes across a translation boundary.
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

/// The trace identifier stamped on every audit entry and telemetry emission
/// and propagated end to end unchanged across every translation boundary.
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
    /// otherwise mints a fresh one, so every translation travels under exactly
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

    /// Returns the header name and value to attach to a boundary-crossing
    /// call so the trace crosses the translation boundary unchanged.
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

/// A typed error carrying the trace identifier of the translation that
/// failed, so every error response names the trace support retrieves it by.
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

#[cfg(test)]
mod tests {
    use super::{TraceError, TraceId, TRACE_HEADER};

    #[test]
    fn minted_identifiers_round_trip_and_cross_boundaries_unchanged() {
        let trace = TraceId::mint([0xa5; 16]);
        let parsed = TraceId::parse(trace.as_str())
            .unwrap_or_else(|error| panic!("minted trace rejected: {error}"));
        assert_eq!(parsed, trace);
        assert_eq!(trace.outbound(), (TRACE_HEADER, trace.as_str()));
        let adopted = TraceId::from_inbound(Some(trace.as_str()), [0; 16]);
        assert_eq!(adopted, trace);
    }

    #[test]
    fn malformed_inbound_identifiers_are_replaced_not_adopted() {
        assert_eq!(
            TraceId::parse("trc_XYZ"),
            Err(TraceError::Malformed),
            "uppercase digits must be refused"
        );
        assert_eq!(TraceId::parse("trc_"), Err(TraceError::Malformed));
        assert_eq!(TraceId::parse("no-prefix"), Err(TraceError::Malformed));
        let minted = TraceId::from_inbound(Some("trc_not-hex"), [0x0f; 16]);
        assert_eq!(minted, TraceId::mint([0x0f; 16]));
    }

    #[test]
    fn wrapped_errors_carry_the_trace_they_travelled_under() {
        let trace = TraceId::mint([3; 16]);
        let traced = trace.wrap(TraceError::Malformed);
        assert_eq!(traced.trace(), &trace);
        assert!(format!("{traced}").contains(trace.as_str()));
    }
}
