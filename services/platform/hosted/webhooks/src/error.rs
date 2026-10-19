//! Typed webhook failures. Every refusal names the exact stage that refused so
//! delivery state is never reported as stronger than the evidence behind it.

use std::fmt::{Display, Formatter};
use std::io;

use layerx_platform_gateway::GatewayError;

/// Exact failure taxonomy for the hosted webhook surface.
#[derive(Debug)]
pub enum WebhookError {
    /// The supplied argument violated a declared bound.
    InvalidRequest,
    /// The endpoint does not exist under the authenticated principal.
    UnknownEndpoint,
    /// The delivery does not exist under the authenticated principal.
    UnknownDelivery,
    /// The delivery exists but has not been dead-lettered.
    NotDeadLettered,
    /// The endpoint is suspended and refuses new configuration.
    EndpointSuspended,
    /// The event identifier was reused with different content.
    EventConflict,
    /// The subject sequence did not advance beyond the recorded high-water mark.
    OrderViolation,
    /// The cursor was not issued for this endpoint.
    InvalidCursor,
    /// The cursor names a position that retention has already released.
    CursorExpired,
    /// A displayed fact claimed a level its evidence does not support.
    VerificationRequired,
    /// No accepted secret produced the presented signature.
    SignatureRejected,
    /// The delivery identifier was already admitted inside the replay window.
    ReplayRejected,
    /// The signature timestamp fell outside the accepted replay window.
    StaleTimestamp,
    /// The replay guard is full and refuses to admit without deduplication.
    ReplayCapacity,
    /// The durable store could not be decoded.
    CorruptStore,
    /// Secure secret generation is unavailable.
    Entropy,
    /// The durable state lock is poisoned.
    Unavailable,
    /// The hosted gateway refused an identifier the webhook surface reuses.
    Gateway(GatewayError),
    /// The durable store could not be read or written.
    Io(io::Error),
}

impl Display for WebhookError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        formatter.write_str(match self {
            Self::InvalidRequest => "webhook request is invalid",
            Self::UnknownEndpoint => "webhook endpoint is unknown",
            Self::UnknownDelivery => "webhook delivery is unknown",
            Self::NotDeadLettered => "webhook delivery has not been dead-lettered",
            Self::EndpointSuspended => "webhook endpoint is suspended",
            Self::EventConflict => "event identifier was reused with different content",
            Self::OrderViolation => "subject sequence did not advance",
            Self::InvalidCursor => "redelivery cursor was not issued for this endpoint",
            Self::CursorExpired => "redelivery cursor is older than the retained event log",
            Self::VerificationRequired => "presented fact lacks the evidence for its level",
            Self::SignatureRejected => "delivery signature did not verify",
            Self::ReplayRejected => "delivery identifier was already admitted",
            Self::StaleTimestamp => "delivery timestamp is outside the replay window",
            Self::ReplayCapacity => "replay guard is full",
            Self::CorruptStore => "webhook durable store is corrupt",
            Self::Entropy => "secure secret generation unavailable",
            Self::Unavailable => "webhook state unavailable",
            Self::Gateway(_) => "hosted gateway refused the identifier",
            Self::Io(_) => "webhook durable store unavailable",
        })
    }
}

impl std::error::Error for WebhookError {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        match self {
            Self::Gateway(error) => Some(error),
            Self::Io(error) => Some(error),
            _ => None,
        }
    }
}

impl From<io::Error> for WebhookError {
    fn from(value: io::Error) -> Self {
        Self::Io(value)
    }
}

impl From<GatewayError> for WebhookError {
    fn from(value: GatewayError) -> Self {
        Self::Gateway(value)
    }
}
