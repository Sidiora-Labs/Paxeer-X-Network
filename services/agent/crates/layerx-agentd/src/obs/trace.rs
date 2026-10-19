use std::fmt::{Debug, Display, Formatter};

use crate::store::TenantId;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum TraceStage {
    Caller,
    Policy,
    Preparation,
    Signing,
    Submission,
    ReceiptResolution,
}

impl TraceStage {
    pub const ALL: [Self; 6] = [
        Self::Caller,
        Self::Policy,
        Self::Preparation,
        Self::Signing,
        Self::Submission,
        Self::ReceiptResolution,
    ];
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum TraceOutcome {
    Started,
    Allowed,
    Completed,
    Refused,
    Failed,
}

#[derive(Clone, Copy, Eq, PartialEq)]
pub struct CorrelationId([u8; 32]);

impl CorrelationId {
    #[must_use]
    pub const fn bytes(self) -> [u8; 32] {
        self.0
    }
}

impl Debug for CorrelationId {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        Display::fmt(self, formatter)
    }
}

impl Display for CorrelationId {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        for byte in self.0 {
            write!(formatter, "{byte:02x}")?;
        }
        Ok(())
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct Span {
    pub stage: TraceStage,
    pub outcome: TraceOutcome,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum TraceError {
    StageOutOfOrder,
    TraceIncomplete,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Trace {
    tenant: TenantId,
    correlation_id: CorrelationId,
    spans: Vec<Span>,
}

impl Trace {
    #[must_use]
    pub const fn start(tenant: TenantId, request_id: [u8; 32]) -> Self {
        Self {
            tenant,
            correlation_id: CorrelationId(request_id),
            spans: Vec::new(),
        }
    }

    /// Records one span, which must be the next stage in the fixed order.
    ///
    /// # Errors
    ///
    /// Returns `StageOutOfOrder` when the stage is not the one that follows the recorded spans.
    pub fn enter(&mut self, stage: TraceStage, outcome: TraceOutcome) -> Result<(), TraceError> {
        if TraceStage::ALL.get(self.spans.len()).copied() != Some(stage) {
            return Err(TraceError::StageOutOfOrder);
        }
        self.spans.push(Span { stage, outcome });
        Ok(())
    }

    /// Closes a trace that has recorded every stage.
    ///
    /// # Errors
    ///
    /// Returns `TraceIncomplete` when any of the six stages has not been entered.
    pub fn finish(self) -> Result<Self, TraceError> {
        if self.spans.len() == TraceStage::ALL.len() {
            Ok(self)
        } else {
            Err(TraceError::TraceIncomplete)
        }
    }

    #[must_use]
    pub const fn tenant(&self) -> &TenantId {
        &self.tenant
    }

    #[must_use]
    pub const fn correlation_id(&self) -> CorrelationId {
        self.correlation_id
    }

    #[must_use]
    pub fn spans(&self) -> &[Span] {
        &self.spans
    }

    #[must_use]
    pub fn correlates(&self, entry: &crate::audit::Entry) -> bool {
        self.tenant == entry.tenant && self.correlation_id.bytes() == entry.request_id
    }
}
