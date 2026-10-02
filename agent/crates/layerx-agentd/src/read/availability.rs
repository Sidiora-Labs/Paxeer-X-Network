//! Provider-attributed availability retrieval, evidence retention, and replay framing.

use std::collections::BTreeMap;

use layerx_client::availability::{
    fetch, AvailabilitySelector, FetchContext, FetchError, FetchOutcome, Progress, Provider,
    ProviderFailure, ProviderReport, ProviderSet,
};
use layerx_client::lni::transport::FrameTransport;
use layerx_proof::availability::{AvailabilityCheck, ClassReport, ReassemblyReport, VerifiedChunk};

use crate::store::{ObjectKind, Store, StoreError, TenantId, TenantKey};

/// Exactly which availability slice to retrieve, under which checkpoint and
/// which trusted commitments.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct AvailabilityRequest {
    pub selector: AvailabilitySelector,
    pub checkpoint_id: [u8; 32],
    pub context: FetchContext,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AvailabilityFailure {
    pub checkpoint_id: [u8; 32],
    pub batch_number: u64,
    pub chunk_index: u32,
    pub provider: String,
    pub check: AvailabilityCheck,
    pub served_bytes: Vec<u8>,
    pub mismatching_commitment: [u8; 32],
    pub classes: ClassReport,
    pub provider_failure_count: u64,
}

#[derive(Default)]
pub struct AvailabilityAudit {
    failures: Vec<AvailabilityFailure>,
    provider_counts: BTreeMap<String, u64>,
}

impl AvailabilityAudit {
    #[must_use]
    pub fn failures(&self) -> &[AvailabilityFailure] {
        &self.failures
    }

    #[must_use]
    pub fn provider_failure_count(&self, provider: &str) -> u64 {
        self.provider_counts.get(provider).copied().unwrap_or(0)
    }
}

/// Daemon-owned availability providers and their failure audit. Transports are owned so a
/// provider set borrows them only for the duration of one fetch.
pub(crate) struct AvailabilityOwner {
    providers: Vec<(String, Box<dyn FrameTransport + Send>)>,
    audit: AvailabilityAudit,
}

impl AvailabilityOwner {
    pub(crate) fn new(providers: Vec<(String, Box<dyn FrameTransport + Send>)>) -> Self {
        Self {
            providers,
            audit: AvailabilityAudit::default(),
        }
    }

    /// Runs [`availability`] over the owned providers and the retained audit.
    ///
    /// # Errors
    ///
    /// Returns every error [`availability`] returns.
    pub(crate) fn fetch<F>(
        &mut self,
        store: &mut Store,
        tenant: &TenantId,
        request: &AvailabilityRequest,
        on_chunk: F,
    ) -> Result<AvailabilityRead, AvailabilityReadError>
    where
        F: FnMut(Progress<'_>),
    {
        let mut providers = ProviderSet::new(
            self.providers
                .iter_mut()
                .map(|(name, transport)| Provider {
                    name: name.clone(),
                    transport: transport.as_mut(),
                })
                .collect(),
        );
        availability(store, tenant, &mut self.audit, &mut providers, request, on_chunk)
    }
}

/// Stable replay framing: magic, version, then class/index/length/bytes records.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ReplayFraming {
    pub magic: [u8; 4],
    pub version: u8,
    pub record_order: &'static str,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum AvailabilityRead {
    Complete {
        provider: String,
        report: ReassemblyReport,
        replay_bytes: Vec<u8>,
        framing: ReplayFraming,
    },
    Partial {
        reports: Vec<ProviderReport>,
        failures: Vec<AvailabilityFailure>,
    },
}

#[derive(Debug)]
pub enum AvailabilityReadError {
    Fetch(FetchError),
    Store(StoreError),
    Arithmetic,
}

impl From<FetchError> for AvailabilityReadError {
    fn from(value: FetchError) -> Self {
        Self::Fetch(value)
    }
}

impl From<StoreError> for AvailabilityReadError {
    fn from(value: StoreError) -> Self {
        Self::Store(value)
    }
}

/// Streams verified chunks, retains provider failure evidence, and emits replay framing.
///
/// # Errors
///
/// Returns `Fetch` for an empty provider set, an invalid selector or bounds, or
/// correlation-id overflow, `Arithmetic` when a chunk count, chunk length or provider
/// failure tally overflows its width, and `Store` if failure evidence cannot be persisted.
pub fn availability<F>(
    store: &mut Store,
    tenant: &TenantId,
    audit: &mut AvailabilityAudit,
    providers: &mut ProviderSet<'_>,
    request: &AvailabilityRequest,
    on_chunk: F,
) -> Result<AvailabilityRead, AvailabilityReadError>
where
    F: FnMut(Progress<'_>),
{
    let AvailabilityRequest {
        selector,
        checkpoint_id,
        context,
    } = *request;
    match fetch(providers, selector, context, on_chunk)? {
        FetchOutcome::Complete(result) => Ok(AvailabilityRead::Complete {
            provider: result.provider,
            report: result.report,
            replay_bytes: replay_stream(&result.chunks)?,
            framing: ReplayFraming {
                magic: *b"LXRP",
                version: 1,
                record_order: "flattened availability chunk index ascending",
            },
        }),
        FetchOutcome::Partial(reports) => {
            let mut failures = Vec::with_capacity(reports.len());
            for report in &reports {
                let count = audit
                    .provider_counts
                    .entry(report.provider.clone())
                    .or_insert(0);
                *count = count
                    .checked_add(1)
                    .ok_or(AvailabilityReadError::Arithmetic)?;
                let failure = failure_evidence(report, checkpoint_id, context, *count)?;
                persist_failure(store, tenant.clone(), &failure)?;
                audit.failures.push(failure.clone());
                failures.push(failure);
            }
            Ok(AvailabilityRead::Partial { reports, failures })
        }
    }
}

fn failure_evidence(
    report: &ProviderReport,
    checkpoint_id: [u8; 32],
    context: FetchContext,
    count: u64,
) -> Result<AvailabilityFailure, AvailabilityReadError> {
    let (check, served_bytes, commitment, classes) = match &report.failure {
        ProviderFailure::Chunk(failure) | ProviderFailure::Reassembly(failure) => (
            failure.check,
            failure.served_bytes.clone(),
            failure.commitment,
            failure.classes.clone(),
        ),
        _ => (
            AvailabilityCheck::MissingClass,
            Vec::new(),
            context.data_availability_root,
            report.classes.clone(),
        ),
    };
    Ok(AvailabilityFailure {
        checkpoint_id,
        batch_number: context.expected_batch_number,
        chunk_index: u32::try_from(report.verified_chunks)
            .map_err(|_| AvailabilityReadError::Arithmetic)?,
        provider: report.provider.clone(),
        check,
        served_bytes,
        mismatching_commitment: commitment,
        classes,
        provider_failure_count: count,
    })
}

fn replay_stream(chunks: &[VerifiedChunk]) -> Result<Vec<u8>, AvailabilityReadError> {
    let mut bytes = Vec::new();
    bytes.extend_from_slice(b"LXRP");
    bytes.push(1);
    let count = u32::try_from(chunks.len()).map_err(|_| AvailabilityReadError::Arithmetic)?;
    bytes.extend_from_slice(&count.to_be_bytes());
    for verified in chunks {
        let chunk = verified.chunk();
        bytes.push(chunk.class as u8);
        bytes.extend_from_slice(&chunk.index.to_be_bytes());
        let length =
            u32::try_from(chunk.bytes.len()).map_err(|_| AvailabilityReadError::Arithmetic)?;
        bytes.extend_from_slice(&length.to_be_bytes());
        bytes.extend_from_slice(&chunk.bytes);
    }
    Ok(bytes)
}

fn persist_failure(
    store: &mut Store,
    tenant: TenantId,
    failure: &AvailabilityFailure,
) -> Result<(), AvailabilityReadError> {
    let mut object_id = b"availability-failure:".to_vec();
    object_id.extend_from_slice(failure.provider.as_bytes());
    object_id.push(0);
    object_id.extend_from_slice(&failure.provider_failure_count.to_be_bytes());
    let key = TenantKey::new(tenant, ObjectKind::Audit, object_id)?;
    store.put_local(key, encode_failure(failure)?)?;
    Ok(())
}

fn encode_failure(failure: &AvailabilityFailure) -> Result<Vec<u8>, AvailabilityReadError> {
    let mut bytes = Vec::new();
    bytes.extend_from_slice(&failure.checkpoint_id);
    bytes.extend_from_slice(&failure.batch_number.to_be_bytes());
    bytes.extend_from_slice(&failure.chunk_index.to_be_bytes());
    let provider_length =
        u32::try_from(failure.provider.len()).map_err(|_| AvailabilityReadError::Arithmetic)?;
    bytes.extend_from_slice(&provider_length.to_be_bytes());
    bytes.extend_from_slice(failure.provider.as_bytes());
    bytes.push(failure.check as u8);
    bytes.extend_from_slice(&failure.mismatching_commitment);
    let served_length =
        u32::try_from(failure.served_bytes.len()).map_err(|_| AvailabilityReadError::Arithmetic)?;
    bytes.extend_from_slice(&served_length.to_be_bytes());
    bytes.extend_from_slice(&failure.served_bytes);
    bytes.extend_from_slice(&failure.provider_failure_count.to_be_bytes());
    Ok(bytes)
}
