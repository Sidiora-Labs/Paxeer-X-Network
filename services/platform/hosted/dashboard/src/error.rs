//! Typed dashboard failures. The dashboard reads two durable stores it does not
//! own, so every refusal names the store and the stage that refused rather than
//! collapsing into a generic read error.

use std::fmt::{Display, Formatter};
use std::io;

use layerx_platform_gateway::GatewayError;
use layerx_platform_webhooks::error::WebhookError;

/// Exact failure taxonomy for the developer dashboard surface.
#[derive(Debug)]
pub enum DashboardError {
    /// The supplied argument violated a declared bound.
    InvalidRequest,
    /// The configured store root is not an existing directory.
    UnknownRoot,
    /// No receipt is recorded under that idempotency key for this principal.
    UnknownReceipt,
    /// A durable store could not be decoded.
    CorruptStore,
    /// The hosted gateway refused an identifier the dashboard reuses.
    Gateway(GatewayError),
    /// The webhook store refused the read.
    Webhooks(WebhookError),
    /// A durable store could not be read.
    Io(io::Error),
}

impl Display for DashboardError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        formatter.write_str(match self {
            Self::InvalidRequest => "dashboard request is invalid",
            Self::UnknownRoot => "configured store root is not a directory",
            Self::UnknownReceipt => "no receipt is recorded under that idempotency key",
            Self::CorruptStore => "durable store is corrupt",
            Self::Gateway(_) => "hosted gateway refused the identifier",
            Self::Webhooks(_) => "webhook store refused the read",
            Self::Io(_) => "durable store unavailable",
        })
    }
}

impl std::error::Error for DashboardError {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        match self {
            Self::Gateway(error) => Some(error),
            Self::Webhooks(error) => Some(error),
            Self::Io(error) => Some(error),
            _ => None,
        }
    }
}

impl From<io::Error> for DashboardError {
    fn from(value: io::Error) -> Self {
        Self::Io(value)
    }
}

impl From<GatewayError> for DashboardError {
    fn from(value: GatewayError) -> Self {
        Self::Gateway(value)
    }
}

impl From<WebhookError> for DashboardError {
    fn from(value: WebhookError) -> Self {
        Self::Webhooks(value)
    }
}
