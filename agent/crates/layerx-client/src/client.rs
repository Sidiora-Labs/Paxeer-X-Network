//! Connection lifecycle for the sole core-boundary client.

use std::path::PathBuf;
use std::thread;
use std::time::Duration;

use layerx_proof::inclusion::SequencerAuthorization;
use layerx_proof::receipt::AuthorizedBatch;
use layerx_types::ids::Did;
use layerx_types::payload::ModuleRegistry;
use layerx_types::verify::VerificationLevel;

use crate::availability::{
    fetch, AvailabilitySelector, FetchContext, FetchError, FetchOutcome, Progress, Provider,
    ProviderSet,
};
use crate::batch::{self, BatchHeaderError, SignedBatchHeader};
use crate::evidence::{
    self, CheckpointSelector, EvidenceContext, EvidenceError, FinalityEvidenceCandidate,
    ProgramStateResponse, ProgramStateSelector, ProofBundleSelector, RegistrationAck, RootSelector,
    VerifiedCheckpoint, VerifiedProofBundle,
};
use crate::head::{Head, HeadError, HeadTracker};
use crate::lni::handshake::{perform_with_schema, Handshake, HandshakeConfig, HandshakeError};
use crate::lni::preparation::{
    preparation_state, PreparationState, PreparationStateContext, PreparationStateError,
};
use crate::lni::program_read::{
    read_program, ProgramReadContext, ProgramReadError, ProgramReadResult,
};
use crate::lni::report::capability_report_with_schema;
use crate::lni::schema::{lni_schema_arbiter_prestate_v2, lni_schema_v1, Capability, Schema};
use crate::lni::simulate::{simulate, SimulateContext, SimulateError, Simulation};
use crate::lni::transport::{ConnectionGate, Limits, TransportError, Uds};
use crate::payments::{CommittedSnapshot, FeeEstimate, SnapshotContext};
use crate::read::{
    account, balance, history, module_state, Balance, HistoryCursor, HistoryPage, ReadContext,
    ReadError, ReadValue, Requested,
};
use crate::receipt::{
    lookup, lookup_authenticated, resolve_unknown, AuthenticatedLookup, AuthenticatedLookupContext,
    Lookup, LookupContext, ReceiptError, ReceiptSelector, ReceiptWaitMode, Resolution,
};
use crate::stream::{subscribe, Cursor, EventStream, StreamConfig, StreamError};
use crate::submit::{submit_signed, Submission, SubmissionContext, SubmitError, UnknownCause};

/// Externally visible connection lifecycle state.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ConnectionState {
    Connected,
    Degraded,
    Incompatible,
    Unreachable,
}

/// Bounded deterministic jitter schedule for reconnect attempts.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ReconnectPolicy {
    pub maximum_attempts: u8,
    pub base_delay: Duration,
    pub maximum_delay: Duration,
    pub jitter_percent: u8,
}

impl ReconnectPolicy {
    /// Returns one bounded deterministic delay from the attempt and endpoint.
    #[must_use]
    pub fn delay(self, attempt: u8, endpoint: &str) -> Duration {
        let exponent = u32::from(attempt.min(31));
        let factor = 1_u32.checked_shl(exponent).unwrap_or(u32::MAX);
        let base = self
            .base_delay
            .checked_mul(factor)
            .unwrap_or(self.maximum_delay)
            .min(self.maximum_delay);
        let jitter_bound = base
            .as_millis()
            .saturating_mul(u128::from(self.jitter_percent))
            / 100;
        if jitter_bound == 0 {
            return base;
        }
        let seed = endpoint.bytes().fold(u128::from(attempt), |value, byte| {
            value.wrapping_mul(131).wrapping_add(u128::from(byte))
        });
        let jitter = seed % (jitter_bound + 1);
        base.saturating_add(Duration::from_millis(
            u64::try_from(jitter).unwrap_or(u64::MAX),
        ))
        .min(self.maximum_delay)
    }
}

/// Explicit client connection configuration.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ClientConfig {
    pub endpoint: PathBuf,
    pub handshake: HandshakeConfig,
    pub limits: Limits,
    pub reconnect: ReconnectPolicy,
}

/// Failure to establish or safely refresh the boundary.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ConnectionError {
    Transport(TransportError),
    Handshake(HandshakeError),
    Head(HeadError),
    AttemptsExhausted,
}

impl ConnectionError {
    /// Stable state corresponding to this connection failure.
    #[must_use]
    pub const fn state(self) -> ConnectionState {
        match self {
            Self::Handshake(
                HandshakeError::InterfaceIncompatible { .. }
                | HandshakeError::ProtocolVersion { .. }
                | HandshakeError::Network { .. }
                | HandshakeError::MalformedEnvelope
                | HandshakeError::MalformedNodeInfo,
            )
            | Self::Head(_) => ConnectionState::Incompatible,
            Self::Transport(_)
            | Self::Handshake(HandshakeError::Transport(_))
            | Self::AttemptsExhausted => ConnectionState::Unreachable,
        }
    }
}

/// Largest canonical encoded size the native fee meter accepts.
pub const MAX_FEE_METER_CANONICAL_BYTES: u64 = 1_048_576;

/// Hypothetical activity meter priced by the committed native fee schedule.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct FeeMeter {
    pub activity_type: u32,
    pub canonical_bytes: u64,
    pub execution_units: u64,
    pub storage_units: u64,
}

/// One committed fee estimate bound to the single authenticated head captured
/// before the read; head fields are that observation's, not the estimate's.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct FeeObservation {
    head: Head,
    snapshot: CommittedSnapshot<FeeEstimate>,
}

impl FeeObservation {
    #[must_use]
    pub const fn head(&self) -> Head {
        self.head
    }

    #[must_use]
    pub const fn observed_sequence(&self) -> u64 {
        self.snapshot.observed_sequence
    }

    #[must_use]
    pub const fn state_root(&self) -> [u8; 32] {
        self.snapshot.state_root
    }

    #[must_use]
    pub const fn estimate(&self) -> &FeeEstimate {
        &self.snapshot.value
    }

    #[must_use]
    pub fn into_parts(self) -> (Head, CommittedSnapshot<FeeEstimate>) {
        (self.head, self.snapshot)
    }
}

/// Refusal of one head-bound fee estimate.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum FeeEstimateError {
    MeterOutOfRange {
        canonical_bytes: u64,
    },
    SnapshotSkew {
        head_sequence: u64,
        observed_sequence: u64,
    },
    Read(ReadError),
}

/// Sole owner of a live core-boundary connection.
pub struct Client {
    config: ClientConfig,
    gate: ConnectionGate,
    transport: Option<Uds>,
    handshake: Handshake,
    head: HeadTracker,
    state: ConnectionState,
    schema: &'static Schema,
}

impl Client {
    pub fn start_arbiter_prestate_v2<'a>(
        &'a mut self,
        receipt: &'a layerx_proof::receipt::VerifiedReceipt,
        correlation_id: u64,
    ) -> Result<
        crate::arbiter_prestate::ArbiterPrestateDiscovery<'a>,
        crate::arbiter_prestate::ArbiterPrestateError,
    > {
        use crate::arbiter_prestate::{
            ArbiterPrestateDiscovery, ArbiterPrestateError, ARBITER_PRESTATE_REQUEST_BYTES,
            ARBITER_PRESTATE_RESPONSE_HEADER_BYTES, MAX_ARBITER_PRESTATE_PAGE_BYTES,
        };
        if !self
            .handshake
            .capabilities()
            .contains(Capability::ArbiterPrestateV2)
            || self.config.handshake.built_interface_version != crate::lni::schema::Version::V1_10
        {
            return Err(ArbiterPrestateError::Unavailable);
        }
        let limits = self.config.limits;
        if limits.maximum_frame_bytes < ARBITER_PRESTATE_REQUEST_BYTES + 22 {
            return Err(ArbiterPrestateError::Bounds);
        }
        let page_bytes = limits
            .maximum_frame_bytes
            .checked_sub(ARBITER_PRESTATE_RESPONSE_HEADER_BYTES + 22)
            .filter(|bytes| *bytes > 0)
            .ok_or(ArbiterPrestateError::Bounds)?
            .min(MAX_ARBITER_PRESTATE_PAGE_BYTES);
        let page_bytes = u32::try_from(page_bytes).map_err(|_| ArbiterPrestateError::Bounds)?;
        let node = self.handshake.node();
        if node.protocol_version != 3 {
            return Err(ArbiterPrestateError::Unavailable);
        }
        let transport = self
            .transport
            .as_mut()
            .ok_or(ArbiterPrestateError::Transport(
                TransportError::PeerShutdown,
            ))?;
        ArbiterPrestateDiscovery::begin(
            transport,
            self.handshake.capabilities(),
            node.interface_version,
            node.network_id,
            correlation_id,
            receipt,
            page_bytes,
            limits.deadline,
        )
    }

    pub fn arbiter_prestate_v2(
        &mut self,
        receipt: &layerx_proof::receipt::VerifiedReceipt,
        correlation_id: u64,
    ) -> Result<
        crate::evidence::VerifiedArbiterPrestate,
        crate::arbiter_prestate::ArbiterPrestateError,
    > {
        use crate::arbiter_prestate::{ArbiterPrestateError, ArbiterPrestateProgress};
        let mut discovery = self.start_arbiter_prestate_v2(receipt, correlation_id)?;
        loop {
            match discovery.advance() {
                ArbiterPrestateProgress::Incomplete { .. } => {}
                ArbiterPrestateProgress::Complete(prestate) => return Ok(prestate),
                ArbiterPrestateProgress::Refused(error) => return Err(error),
                ArbiterPrestateProgress::Unavailable => {
                    return Err(ArbiterPrestateError::Unavailable)
                }
            }
        }
    }

    pub fn start_execution_prestate<'a>(
        &'a mut self,
        receipt: &'a layerx_proof::receipt::VerifiedReceipt,
        correlation_id: u64,
    ) -> Result<
        crate::execution_prestate::ExecutionPrestateDiscovery<'a>,
        crate::execution_prestate::ExecutionPrestateError,
    > {
        use crate::execution_prestate::{
            ExecutionPrestateDiscovery, ExecutionPrestateError, CAPS_REQUEST_BYTES,
            CAPS_RESPONSE_HEADER_BYTES, MAX_CAPS_PAGE_BYTES,
        };
        if !self
            .handshake
            .capabilities()
            .contains(Capability::CapsDiscovery)
            || !self
                .handshake
                .capabilities()
                .contains(Capability::ExecutionPrestate)
        {
            return Err(ExecutionPrestateError::Unavailable);
        }
        let limits = self.config.limits;
        if limits.maximum_frame_bytes < CAPS_REQUEST_BYTES + 22 {
            return Err(ExecutionPrestateError::Bounds);
        }
        let page_bytes = limits
            .maximum_frame_bytes
            .checked_sub(CAPS_RESPONSE_HEADER_BYTES + 22)
            .filter(|bytes| *bytes > 0)
            .ok_or(ExecutionPrestateError::Bounds)?
            .min(MAX_CAPS_PAGE_BYTES);
        let page_bytes = u32::try_from(page_bytes).map_err(|_| ExecutionPrestateError::Bounds)?;
        let node = self.handshake.node();
        if node.protocol_version != 3 {
            return Err(ExecutionPrestateError::Unavailable);
        }
        let transport = self
            .transport
            .as_mut()
            .ok_or(ExecutionPrestateError::Transport(
                TransportError::PeerShutdown,
            ))?;
        ExecutionPrestateDiscovery::begin(
            transport,
            self.handshake.capabilities(),
            node.interface_version,
            node.network_id,
            correlation_id,
            receipt,
            page_bytes,
            limits.deadline,
        )
    }

    pub fn execution_prestate(
        &mut self,
        receipt: &layerx_proof::receipt::VerifiedReceipt,
        correlation_id: u64,
    ) -> Result<
        crate::evidence::VerifiedExecutionPrestate,
        crate::execution_prestate::ExecutionPrestateError,
    > {
        use crate::execution_prestate::{ExecutionPrestateError, ExecutionPrestateProgress};
        let mut discovery = self.start_execution_prestate(receipt, correlation_id)?;
        loop {
            match discovery.advance() {
                ExecutionPrestateProgress::Incomplete { .. } => {}
                ExecutionPrestateProgress::Complete(prestate) => return Ok(prestate),
                ExecutionPrestateProgress::Refused(error) => return Err(error),
                ExecutionPrestateProgress::Unavailable => {
                    return Err(ExecutionPrestateError::Unavailable)
                }
            }
        }
    }

    pub fn start_native_execution_prestate<'a>(
        &'a mut self,
        receipt: &'a layerx_proof::receipt::VerifiedReceipt,
        correlation_id: u64,
    ) -> Result<
        crate::execution_prestate::NativeExecutionPrestateDiscovery<'a>,
        crate::execution_prestate::ExecutionPrestateError,
    > {
        use crate::execution_prestate::{
            ExecutionPrestateError, NativeExecutionPrestateDiscovery, CAPS_REQUEST_BYTES,
            CAPS_RESPONSE_HEADER_BYTES, MAX_CAPS_PAGE_BYTES,
        };
        if !self
            .handshake
            .capabilities()
            .contains(Capability::CapsDiscovery)
            || !self
                .handshake
                .capabilities()
                .contains(Capability::ExecutionPrestate)
        {
            return Err(ExecutionPrestateError::Unavailable);
        }
        let limits = self.config.limits;
        if limits.maximum_frame_bytes < CAPS_REQUEST_BYTES + 22 {
            return Err(ExecutionPrestateError::Bounds);
        }
        let page_bytes = limits
            .maximum_frame_bytes
            .checked_sub(CAPS_RESPONSE_HEADER_BYTES + 22)
            .filter(|bytes| *bytes > 0)
            .ok_or(ExecutionPrestateError::Bounds)?
            .min(MAX_CAPS_PAGE_BYTES);
        let page_bytes = u32::try_from(page_bytes).map_err(|_| ExecutionPrestateError::Bounds)?;
        let node = self.handshake.node();
        if node.protocol_version != 3 {
            return Err(ExecutionPrestateError::Unavailable);
        }
        let transport = self
            .transport
            .as_mut()
            .ok_or(ExecutionPrestateError::Transport(
                TransportError::PeerShutdown,
            ))?;
        NativeExecutionPrestateDiscovery::begin(
            transport,
            self.handshake.capabilities(),
            node.interface_version,
            node.network_id,
            correlation_id,
            receipt,
            page_bytes,
            limits.deadline,
        )
    }

    pub fn native_execution_prestate(
        &mut self,
        receipt: &layerx_proof::receipt::VerifiedReceipt,
        correlation_id: u64,
    ) -> Result<
        crate::evidence::VerifiedNativeExecutionPrestate,
        crate::execution_prestate::ExecutionPrestateError,
    > {
        use crate::execution_prestate::{ExecutionPrestateError, NativeExecutionPrestateProgress};
        let mut discovery = self.start_native_execution_prestate(receipt, correlation_id)?;
        loop {
            match discovery.advance() {
                NativeExecutionPrestateProgress::Incomplete { .. } => {}
                NativeExecutionPrestateProgress::Complete(prestate) => return Ok(prestate),
                NativeExecutionPrestateProgress::Refused(error) => return Err(error),
                NativeExecutionPrestateProgress::Unavailable => {
                    return Err(ExecutionPrestateError::Unavailable)
                }
            }
        }
    }

    pub fn start_caps_discovery<'a>(
        &'a mut self,
        did: [u8; 32],
        requested: VerificationLevel,
        correlation_id: u64,
        authorization: SequencerAuthorization,
        history: Option<&'a crate::handover::SequencerHistory>,
    ) -> Result<crate::caps::CapsDiscovery<'a>, crate::caps::CapsError> {
        if !self
            .handshake
            .capabilities()
            .contains(Capability::CapsDiscovery)
        {
            return Err(crate::caps::CapsError::Unavailable);
        }
        let limits = self.config.limits;
        if limits.maximum_frame_bytes < crate::caps::CAPS_REQUEST_BYTES + 22 {
            return Err(crate::caps::CapsError::Bounds);
        }
        let page_bytes = limits
            .maximum_frame_bytes
            .checked_sub(crate::caps::CAPS_RESPONSE_HEADER_BYTES + 22)
            .filter(|bytes| *bytes > 0)
            .ok_or(crate::caps::CapsError::Bounds)?
            .min(crate::caps::MAX_CAPS_PAGE_BYTES);
        let page_bytes = u32::try_from(page_bytes).map_err(|_| crate::caps::CapsError::Bounds)?;
        let context = self.read_context(requested, correlation_id, authorization);
        let transport = self
            .transport
            .as_mut()
            .ok_or(crate::caps::CapsError::Transport(
                TransportError::PeerShutdown,
            ))?;
        crate::caps::CapsDiscovery::begin(
            transport,
            self.handshake.capabilities(),
            context,
            did,
            page_bytes,
            limits.deadline,
            history,
        )
    }

    pub fn caps_discovery(
        &mut self,
        did: [u8; 32],
        requested: VerificationLevel,
        correlation_id: u64,
        authorization: SequencerAuthorization,
        history: Option<&crate::handover::SequencerHistory>,
    ) -> Result<crate::evidence::VerifiedCaps, crate::caps::CapsError> {
        let mut discovery =
            self.start_caps_discovery(did, requested, correlation_id, authorization, history)?;
        loop {
            match discovery.advance() {
                crate::caps::CapsProgress::Incomplete { .. } => {}
                crate::caps::CapsProgress::Complete(caps)
                | crate::caps::CapsProgress::Empty(caps) => {
                    return Ok(caps);
                }
                crate::caps::CapsProgress::Refused(error) => return Err(error),
                crate::caps::CapsProgress::Unavailable => {
                    return Err(crate::caps::CapsError::Unavailable)
                }
            }
        }
    }

    /// Reads an account using authenticated historical term authority.
    ///
    /// # Errors
    /// Refuses stale history and all ordinary capability, selector and evidence failures.
    pub fn account_with_history(
        &mut self,
        account_id: [u8; 32],
        requested: VerificationLevel,
        correlation_id: u64,
        history: &crate::handover::SequencerHistory,
    ) -> Result<ReadValue, ReadError> {
        self.require_account_read_capabilities(requested)?;
        let authorization = history
            .authorization_for_batch(self.head().sealed_batch)
            .map_err(|_| ReadError::AuthorityRangeMismatch)?;
        let context = self.read_context(requested, correlation_id, authorization);
        let transport = self.transport.as_mut().ok_or(ReadError::Disconnected)?;
        crate::read::account_with_history(transport, account_id, context, history)
    }

    /// Reads module state with genesis-authenticated historical term authority.
    ///
    /// # Errors
    /// Refuses unavailable capabilities, stale history and every original module proof failure.
    pub fn module_state_with_history(
        &mut self,
        module_id: u16,
        key: &[u8],
        requested: VerificationLevel,
        correlation_id: u64,
        history: &crate::handover::SequencerHistory,
    ) -> Result<ReadValue, ReadError> {
        self.require_read_capability(Capability::AccountRead)?;
        let authorization = history
            .authorization_for_batch(self.head().sealed_batch)
            .map_err(|_| ReadError::AuthorityRangeMismatch)?;
        let context = self.read_context(requested, correlation_id, authorization);
        let transport = self.transport.as_mut().ok_or(ReadError::Disconnected)?;
        crate::read::module_state_with_history(transport, module_id, key, context, history)
    }

    /// Reads a bounded history page using independently authenticated signing terms.
    ///
    /// # Errors
    /// Refuses unavailable capabilities, unverified history and invalid inclusion evidence.
    pub fn history_with_history(
        &mut self,
        range: crate::read::HistoryRange,
        requested: VerificationLevel,
        correlation_id: u64,
        history: &crate::handover::SequencerHistory,
    ) -> Result<HistoryPage, ReadError> {
        self.require_read_capability(Capability::HistoryRange)?;
        let authorization = history
            .authorization_for_sequence(range.start_sequence)
            .map_err(|_| ReadError::AuthorityRangeMismatch)?;
        let context = self.read_context(requested, correlation_id, authorization);
        let transport = self.transport.as_mut().ok_or(ReadError::Disconnected)?;
        crate::read::history_with_history(transport, range, context, history)
    }

    /// Extends one caller-pinned history through the next complete native batch.
    ///
    /// # Errors
    /// Refuses domain mismatches, unavailable transport and unauthenticated history.
    pub fn advance_sequencer_history(
        &mut self,
        history: &mut crate::handover::SequencerHistory,
        correlation_id: u64,
        limits: crate::availability::RetrievalLimits,
    ) -> Result<(), crate::handover::HistoryError> {
        self.advance_sequencer_history_with_finality(history, correlation_id, limits, None)
    }

    /// Extends pinned history using independently verified Paxeer finality for each handover.
    ///
    /// # Errors
    /// Refuses unconfigured finality, domain mismatches and unauthenticated transitions.
    pub fn advance_sequencer_history_with_finality(
        &mut self,
        history: &mut crate::handover::SequencerHistory,
        correlation_id: u64,
        limits: crate::availability::RetrievalLimits,
        verifier: Option<&layerx_paxeer_verifier::PaxeerCheckpointVerifier>,
    ) -> Result<(), crate::handover::HistoryError> {
        use crate::handover::HistoryError;
        if history.network_id() != self.config.handshake.expected_network_id
            || self.config.handshake.expected_protocol_version != 3
        {
            return Err(HistoryError::Genesis);
        }
        let transport = self.transport.as_mut().ok_or(HistoryError::Transport)?;
        history.fetch_next_with_finality(
            transport,
            self.handshake.node().interface_version,
            correlation_id,
            limits,
            verifier,
        )
    }

    /// Reads a signed historical header under a caller's authenticated genesis history.
    ///
    /// # Errors
    /// Refuses unavailable transport, mismatched domains and unverified signing terms.
    pub fn batch_header_with_history(
        &mut self,
        batch_number: u64,
        correlation_id: u64,
        history: &crate::handover::SequencerHistory,
    ) -> Result<SignedBatchHeader, BatchHeaderError> {
        if !self
            .handshake
            .capabilities()
            .contains(Capability::BatchHeader)
        {
            return Err(BatchHeaderError::UnavailableCapability);
        }
        if history.network_id() != self.config.handshake.expected_network_id
            || self.config.handshake.expected_protocol_version != 3
        {
            return Err(BatchHeaderError::AuthorityMismatch);
        }
        let transport = self
            .transport
            .as_mut()
            .ok_or(BatchHeaderError::Disconnected)?;
        batch::lookup_with_history(
            transport,
            self.handshake.node().interface_version,
            batch_number,
            correlation_id,
            history,
        )
    }

    /// Retrieves a proof with historical signer authority established from genesis.
    ///
    /// # Errors
    /// Refuses unavailable capabilities, unknown terms and all existing proof failures.
    pub fn proof_bundle_with_history(
        &mut self,
        selector: ProofBundleSelector,
        correlation_id: u64,
        registry: &ModuleRegistry,
        history: &crate::handover::SequencerHistory,
    ) -> Result<VerifiedProofBundle, EvidenceError> {
        if !self
            .handshake
            .capabilities()
            .contains(Capability::ProofBundle)
        {
            return Err(EvidenceError::Unavailable);
        }
        let transport = self.transport.as_mut().ok_or(EvidenceError::Unavailable)?;
        evidence::proof_bundle_with_history(
            transport,
            selector,
            EvidenceContext {
                interface_version: self.handshake.node().interface_version,
                correlation_id,
                expected_protocol_version: self.config.handshake.expected_protocol_version,
                expected_network_id: self.config.handshake.expected_network_id,
                handshake_sequencer_key: self.handshake.node().authorised_sequencer_key,
            },
            registry,
            history,
        )
    }

    /// Opens the configured Unix boundary and performs the mandatory handshake.
    ///
    /// # Errors
    ///
    /// Returns a typed transport or startup-refusal failure before exposing a
    /// client.
    pub fn connect(config: ClientConfig) -> Result<Self, ConnectionError> {
        Self::connect_with_schema(config, lni_schema_v1())
    }

    pub fn connect_arbiter_prestate_v2(config: ClientConfig) -> Result<Self, ConnectionError> {
        Self::connect_with_schema(config, lni_schema_arbiter_prestate_v2())
    }

    fn connect_with_schema(
        config: ClientConfig,
        schema: &'static Schema,
    ) -> Result<Self, ConnectionError> {
        let gate = ConnectionGate::new(config.limits.maximum_connections);
        let mut transport = Uds::connect(&config.endpoint, &gate, config.limits)
            .map_err(ConnectionError::Transport)?;
        let handshake = perform_with_schema(&mut transport, &config.handshake, None, schema)
            .map_err(ConnectionError::Handshake)?;
        let head = HeadTracker::new(handshake.node());
        let state = state_for(&handshake, schema);
        Ok(Self {
            config,
            gate,
            transport: Some(transport),
            handshake,
            head,
            state,
            schema,
        })
    }

    /// Reopens and revalidates the boundary under bounded jittered backoff.
    ///
    /// # Errors
    ///
    /// Refuses incompatibility and head regression immediately; reports
    /// exhaustion after the configured number of transport attempts.
    pub fn reconnect(&mut self) -> Result<(), ConnectionError> {
        self.transport = None;
        self.state = ConnectionState::Unreachable;
        let endpoint = self.config.endpoint.to_string_lossy();
        for attempt in 0..self.config.reconnect.maximum_attempts {
            if attempt != 0 {
                thread::sleep(self.config.reconnect.delay(attempt, &endpoint));
            }
            let Ok(mut transport) =
                Uds::connect(&self.config.endpoint, &self.gate, self.config.limits)
            else {
                self.state = ConnectionState::Unreachable;
                continue;
            };
            let handshake = match perform_with_schema(
                &mut transport,
                &self.config.handshake,
                Some(&self.handshake),
                self.schema,
            ) {
                Ok(handshake) => handshake,
                Err(error) => {
                    let error = ConnectionError::Handshake(error);
                    self.state = error.state();
                    return Err(error);
                }
            };
            if let Err(error) = self.head.update(handshake.node()) {
                let error = ConnectionError::Head(error);
                self.state = error.state();
                return Err(error);
            }
            self.state = state_for(&handshake, self.schema);
            self.handshake = handshake;
            self.transport = Some(transport);
            return Ok(());
        }
        Err(ConnectionError::AttemptsExhausted)
    }

    /// Marks a lost in-flight request without claiming its outcome.
    pub const fn mark_transport_lost(&mut self) {
        self.state = ConnectionState::Unreachable;
    }

    #[must_use]
    pub const fn state(&self) -> ConnectionState {
        self.state
    }

    #[must_use]
    pub const fn head(&self) -> Head {
        self.head.current()
    }

    #[must_use]
    pub const fn handshake(&self) -> &Handshake {
        &self.handshake
    }

    /// Requires a receipt key to have been advertised for its batch.
    ///
    /// # Errors
    ///
    /// Refuses an unadvertised key without attempting signature verification.
    pub fn require_receipt_key(&self, batch: u64, key: [u8; 32]) -> Result<(), HeadError> {
        self.head.require_sequencer_key(batch, key)
    }

    /// Obtains the actor's complete atomic preparation snapshot from the
    /// authenticated production node boundary.
    ///
    /// # Errors
    ///
    /// Refuses a capability or connection gap and every typed core refusal,
    /// malformed response, network mismatch, or head regression.
    pub fn preparation_state(
        &mut self,
        actor: &Did,
        correlation_id: u64,
    ) -> Result<PreparationState, PreparationStateError> {
        if !self
            .handshake
            .capabilities()
            .contains(Capability::PreparationState)
        {
            return Err(PreparationStateError::UnavailableCapability);
        }
        let transport = self
            .transport
            .as_mut()
            .ok_or(PreparationStateError::Disconnected)?;
        preparation_state(
            transport,
            actor,
            PreparationStateContext {
                interface_version: self.handshake.node().interface_version,
                expected_network_id: self.config.handshake.expected_network_id,
                minimum_observed_head: self.head.current().chain_sequence,
                correlation_id,
            },
        )
    }

    /// Verifies and transmits one signed activity through the sole boundary
    /// connection.
    ///
    /// # Errors
    ///
    /// Refuses unavailable submission capability, a disconnected client, or
    /// any pre-transmission canonical/signature failure.
    pub fn submit_signed(
        &mut self,
        registry: &ModuleRegistry,
        signer_public_key: [u8; 32],
        correlation_id: u64,
        attempt: u32,
        signed_bytes: &[u8],
    ) -> Result<Submission, SubmitError> {
        if !self.handshake.capabilities().contains(Capability::Submit)
            || !self
                .handshake
                .capabilities()
                .contains(Capability::AuthenticatedDurableSubmit)
        {
            return Err(SubmitError::UnavailableCapability);
        }
        let context = SubmissionContext {
            interface_version: self.handshake.node().interface_version,
            protocol_version: self.config.handshake.expected_protocol_version,
            network_id: self.config.handshake.expected_network_id,
            correlation_id,
            signer_public_key,
            attempt,
        };
        let transport = self.transport.as_mut().ok_or(SubmitError::Disconnected)?;
        let submission = submit_signed(transport, registry, context, signed_bytes)?;
        let transport_lost = matches!(
            &submission,
            Submission::Unknown(unknown)
                if matches!(unknown.cause(), UnknownCause::Transport(_))
        );
        if transport_lost {
            self.transport = None;
            self.state = ConnectionState::Unreachable;
        }
        Ok(submission)
    }

    /// Executes one signed program call against the node's current head
    /// without committing it and verifies the sequencer-signed execution and
    /// simulation evidence.
    ///
    /// # Errors
    ///
    /// Refuses an unavailable simulation capability, a disconnected client,
    /// every typed core refusal, and any unverifiable or mismatched response.
    pub fn simulate(
        &mut self,
        registry: &ModuleRegistry,
        signed_bytes: &[u8],
        correlation_id: u64,
    ) -> Result<Simulation, SimulateError> {
        if !self.handshake.capabilities().contains(Capability::Simulate) {
            return Err(SimulateError::UnavailableCapability);
        }
        let context = SimulateContext {
            interface_version: self.handshake.node().interface_version,
            sequencer_public_key: self.handshake.node().authorised_sequencer_key,
            correlation_id,
        };
        let transport = self.transport.as_mut().ok_or(SimulateError::Disconnected)?;
        simulate(transport, registry, signed_bytes, context)
    }

    /// Executes one signed `ProgramCall` against an immutable, optionally
    /// caller-pinned snapshot without committing or submitting it.
    ///
    /// # Errors
    ///
    /// Refuses an unavailable v1.6 capability, disconnection, stale or
    /// mismatched snapshot, typed core refusal, or invalid signed evidence.
    pub fn read_program(
        &mut self,
        registry: &ModuleRegistry,
        signed_bytes: &[u8],
        correlation_id: u64,
        minimum_sequence: u64,
        expected_state_root: Option<[u8; 32]>,
    ) -> Result<ProgramReadResult, ProgramReadError> {
        if !self
            .handshake
            .capabilities()
            .contains(Capability::ProgramRead)
        {
            return Err(ProgramReadError::UnavailableCapability);
        }
        let context = ProgramReadContext {
            interface_version: self.handshake.node().interface_version,
            sequencer_public_key: self.handshake.node().authorised_sequencer_key,
            correlation_id,
            minimum_sequence,
            expected_state_root,
        };
        let transport = self
            .transport
            .as_mut()
            .ok_or(ProgramReadError::Disconnected)?;
        read_program(transport, registry, signed_bytes, context)
    }

    /// Looks up one exact activity receipt with an explicit native wait mode
    /// and handshake-pinned sequencer authentication.
    ///
    /// # Errors
    ///
    /// Refuses unavailable capability, disconnection, unsupported wait mode,
    /// malformed response, activity mismatch, or invalid receipt signature.
    pub fn lookup_authenticated_receipt(
        &mut self,
        activity_id: [u8; 32],
        correlation_id: u64,
        wait_mode: ReceiptWaitMode,
    ) -> Result<AuthenticatedLookup, ReceiptError> {
        if !self
            .handshake
            .capabilities()
            .contains(Capability::ReceiptLookup)
        {
            return Err(ReceiptError::UnavailableCapability);
        }
        let context = AuthenticatedLookupContext {
            interface_version: self.handshake.node().interface_version,
            correlation_id,
            sequencer_public_key: self.handshake.node().authorised_sequencer_key,
            wait_mode,
        };
        let transport = self.transport.as_mut().ok_or(ReceiptError::Disconnected)?;
        lookup_authenticated(transport, activity_id, context)
    }

    /// Retrieves and verifies one receipt through the active boundary.
    ///
    /// # Errors
    ///
    /// Refuses a capability gap, disconnected boundary, malformed response,
    /// selector mismatch, or proof verification failure.
    pub fn lookup_receipt(
        &mut self,
        selector: ReceiptSelector,
        correlation_id: u64,
        authorised_batch: AuthorizedBatch,
    ) -> Result<Lookup, ReceiptError> {
        if !self
            .handshake
            .capabilities()
            .contains(Capability::ReceiptLookup)
        {
            return Err(ReceiptError::UnavailableCapability);
        }
        let transport = self.transport.as_mut().ok_or(ReceiptError::Disconnected)?;
        lookup(
            transport,
            selector,
            LookupContext {
                interface_version: self.handshake.node().interface_version,
                correlation_id,
                authorised_batch,
            },
        )
    }

    /// Retrieves an owner module outcome against the retained original signing request.
    ///
    /// # Errors
    /// Refuses network or protocol mismatch, missing capability, disconnection,
    /// altered original activity and unverifiable or mismatched receipt evidence.
    pub fn lookup_native_owner_receipt(
        &mut self,
        selector: ReceiptSelector,
        correlation_id: u64,
        authorised_batch: AuthorizedBatch,
        expected: &layerx_proof::receipt::NativeOwnerOutcomeContext<'_>,
    ) -> Result<Lookup, ReceiptError> {
        if expected.network_id != self.config.handshake.expected_network_id
            || self.config.handshake.expected_protocol_version != 3
        {
            return Err(ReceiptError::NativeOwnerVerification(
                layerx_proof::receipt::NativeOwnerOutcomeFailure::ActivityBinding,
            ));
        }
        if !self
            .handshake
            .capabilities()
            .contains(Capability::ReceiptLookup)
        {
            return Err(ReceiptError::UnavailableCapability);
        }
        let transport = self.transport.as_mut().ok_or(ReceiptError::Disconnected)?;
        crate::receipt::lookup_native_owner(
            transport,
            selector,
            LookupContext {
                interface_version: self.handshake.node().interface_version,
                correlation_id,
                authorised_batch,
            },
            expected,
        )
    }

    /// Retrieves and independently verifies one canonical signed batch header.
    ///
    /// # Errors
    ///
    /// Refuses unavailable capability, disconnection and all batch-header verification errors.
    pub fn batch_header(
        &mut self,
        batch_number: u64,
        correlation_id: u64,
    ) -> Result<SignedBatchHeader, BatchHeaderError> {
        if !self
            .handshake
            .capabilities()
            .contains(Capability::BatchHeader)
        {
            return Err(BatchHeaderError::UnavailableCapability);
        }
        let transport = self
            .transport
            .as_mut()
            .ok_or(BatchHeaderError::Disconnected)?;
        batch::lookup(
            transport,
            self.handshake.node().interface_version,
            batch_number,
            correlation_id,
            self.handshake.node().authorised_sequencer_key,
        )
    }

    /// Runs bounded receipt-only resolution for an unknown submission.
    ///
    /// # Errors
    ///
    /// Refuses a capability gap, disconnected boundary, or invalid receipt
    /// evidence. Absence and transport loss remain `Resolution::Unknown`.
    pub fn resolve_unknown(
        &mut self,
        unknown: &crate::submit::Unknown,
        correlation_id: u64,
        authorised_batch: AuthorizedBatch,
        policy: ReconnectPolicy,
    ) -> Result<Resolution, ReceiptError> {
        if !self
            .handshake
            .capabilities()
            .contains(Capability::ReceiptLookup)
        {
            return Err(ReceiptError::UnavailableCapability);
        }
        let transport = self.transport.as_mut().ok_or(ReceiptError::Disconnected)?;
        resolve_unknown(
            transport,
            unknown,
            LookupContext {
                interface_version: self.handshake.node().interface_version,
                correlation_id,
                authorised_batch,
            },
            policy,
        )
    }

    /// Retrieves a proof-gated balance through the sole boundary.
    ///
    /// # Errors
    ///
    /// Refuses capability/disconnection gaps and all read verification errors.
    pub fn balance(
        &mut self,
        account_id: [u8; 32],
        asset_id: [u8; 32],
        requested: VerificationLevel,
        correlation_id: u64,
        authorization: SequencerAuthorization,
    ) -> Result<Balance, ReadError> {
        self.require_account_read_capabilities(requested)?;
        let context = self.read_context(requested, correlation_id, authorization);
        let transport = self.transport.as_mut().ok_or(ReadError::Disconnected)?;
        balance(transport, account_id, asset_id, context)
    }

    /// # Errors
    /// Refuses unavailable native policy, stale snapshots and malformed asset metadata.
    pub fn native_fee_policy(
        &mut self,
        correlation_id: u64,
    ) -> Result<crate::payments::CommittedSnapshot<crate::payments::NativeFeePolicy>, ReadError>
    {
        let context = crate::payments::SnapshotContext {
            interface_version: self.handshake.node().interface_version,
            correlation_id,
            minimum_sequence: self.head().chain_sequence,
        };
        let transport = self.transport.as_mut().ok_or(ReadError::Disconnected)?;
        crate::payments::native_fee_policy(transport, context)
    }

    /// # Errors
    /// Refuses unbound or unavailable committed session state.
    pub fn session_fee_state(
        &mut self,
        correlation_id: u64,
        grant_id: [u8; 32],
    ) -> Result<crate::payments::CommittedSnapshot<Vec<u8>>, ReadError> {
        if !self
            .handshake
            .capabilities()
            .contains(Capability::SessionFeeState)
        {
            return Err(ReadError::UnavailableCapability);
        }
        let context = crate::payments::SnapshotContext {
            interface_version: self.handshake.node().interface_version,
            correlation_id,
            minimum_sequence: self.head().chain_sequence,
        };
        let transport = self.transport.as_mut().ok_or(ReadError::Disconnected)?;
        crate::payments::session_fee_state(transport, grant_id, context)
    }

    /// Retrieves exact proof-gated account bytes.
    ///
    /// # Errors
    ///
    /// Returns the complete balance-read refusal set.
    pub fn account(
        &mut self,
        account_id: [u8; 32],
        requested: VerificationLevel,
        correlation_id: u64,
        authorization: SequencerAuthorization,
    ) -> Result<ReadValue, ReadError> {
        self.require_account_read_capabilities(requested)?;
        let context = self.read_context(requested, correlation_id, authorization);
        let transport = self.transport.as_mut().ok_or(ReadError::Disconnected)?;
        account(transport, account_id, context)
    }

    /// Retrieves exact proof-gated module state bytes.
    ///
    /// # Errors
    ///
    /// Returns the complete point-read refusal set.
    pub fn module_state(
        &mut self,
        module_id: u16,
        key: &[u8],
        requested: VerificationLevel,
        correlation_id: u64,
        authorization: SequencerAuthorization,
    ) -> Result<ReadValue, ReadError> {
        self.require_read_capability(Capability::AccountRead)?;
        let context = self.read_context(requested, correlation_id, authorization);
        let transport = self.transport.as_mut().ok_or(ReadError::Disconnected)?;
        module_state(transport, module_id, key, context)
    }

    /// Retrieves one gap-free, cursor-bound history page.
    ///
    /// # Errors
    ///
    /// Returns capability, disconnection, proof and sequence failures.
    #[allow(clippy::too_many_arguments)]
    pub fn history(
        &mut self,
        start_sequence: u64,
        end_sequence: u64,
        page_bound: u16,
        cursor: Option<HistoryCursor>,
        requested: VerificationLevel,
        correlation_id: u64,
        authorization: SequencerAuthorization,
    ) -> Result<HistoryPage, ReadError> {
        self.require_read_capability(Capability::HistoryRange)?;
        let context = self.read_context(requested, correlation_id, authorization);
        let transport = self.transport.as_mut().ok_or(ReadError::Disconnected)?;
        history(
            transport,
            start_sequence,
            end_sequence,
            page_bound,
            cursor,
            context,
        )
    }

    fn require_read_capability(&self, capability: Capability) -> Result<(), ReadError> {
        if self.handshake.capabilities().contains(capability) {
            Ok(())
        } else {
            Err(ReadError::UnavailableCapability)
        }
    }

    fn require_account_read_capabilities(
        &self,
        requested: VerificationLevel,
    ) -> Result<(), ReadError> {
        self.require_read_capability(Capability::AccountRead)?;
        if requires_historical_account_proofs(requested) {
            self.require_read_capability(Capability::HistoricalProofs)?;
        }
        Ok(())
    }

    fn read_context(
        &self,
        requested: VerificationLevel,
        correlation_id: u64,
        authorization: SequencerAuthorization,
    ) -> ReadContext {
        let head = self.head.current();
        let root_selector = if requested >= VerificationLevel::CHECKPOINT_FINALISED {
            RootSelector::Checkpoint(head.finalised_checkpoint)
        } else {
            RootSelector::Latest
        };
        ReadContext {
            interface_version: self.handshake.node().interface_version,
            correlation_id,
            expected_protocol_version: self.config.handshake.expected_protocol_version,
            expected_network_id: self.config.handshake.expected_network_id,
            requested: Requested::new(requested),
            head,
            sequencer_authorization: authorization,
            handshake_sequencer_key: self.handshake.node().authorised_sequencer_key,
            root_selector,
        }
    }

    /// Retrieves one independently verified activity, receipt, or account proof.
    ///
    /// # Errors
    ///
    /// Refuses unavailable capability, disconnection and invalid or mismatched proof evidence.
    pub fn proof_bundle(
        &mut self,
        selector: ProofBundleSelector,
        correlation_id: u64,
        registry: &ModuleRegistry,
    ) -> Result<VerifiedProofBundle, EvidenceError> {
        if !self
            .handshake
            .capabilities()
            .contains(Capability::ProofBundle)
        {
            return Err(EvidenceError::Unavailable);
        }
        let transport = self.transport.as_mut().ok_or(EvidenceError::Unavailable)?;
        evidence::proof_bundle(
            transport,
            selector,
            EvidenceContext {
                interface_version: self.handshake.node().interface_version,
                correlation_id,
                expected_protocol_version: self.config.handshake.expected_protocol_version,
                expected_network_id: self.handshake.node().network_id,
                handshake_sequencer_key: self.handshake.node().authorised_sequencer_key,
            },
            registry,
        )
    }

    /// Retrieves the kind 5 program-state answer for one pinned head.
    ///
    /// # Errors
    /// Refuses unavailable capability, disconnection and every transport refusal.
    pub fn program_state_bundle(
        &mut self,
        selector: ProgramStateSelector,
        correlation_id: u64,
    ) -> Result<ProgramStateResponse, EvidenceError> {
        if !self
            .handshake
            .capabilities()
            .contains(Capability::ProofBundle)
        {
            return Err(EvidenceError::Unavailable);
        }
        let transport = self.transport.as_mut().ok_or(EvidenceError::Unavailable)?;
        evidence::program_state_bundle(
            transport,
            selector,
            EvidenceContext {
                interface_version: self.handshake.node().interface_version,
                correlation_id,
                expected_protocol_version: self.config.handshake.expected_protocol_version,
                expected_network_id: self.handshake.node().network_id,
                handshake_sequencer_key: self.handshake.node().authorised_sequencer_key,
            },
        )
    }

    /// Prices one meter against the committed schedule at exactly the
    /// authenticated head captured once before the read.
    ///
    /// # Errors
    /// Refuses an out-of-range meter, unavailable capability, disconnection,
    /// every committed-read refusal and any snapshot not at the captured head.
    pub fn estimate_fee(
        &mut self,
        meter: FeeMeter,
        correlation_id: u64,
    ) -> Result<FeeObservation, FeeEstimateError> {
        if meter.canonical_bytes > MAX_FEE_METER_CANONICAL_BYTES {
            return Err(FeeEstimateError::MeterOutOfRange {
                canonical_bytes: meter.canonical_bytes,
            });
        }
        self.require_read_capability(Capability::FeeEstimate)
            .map_err(FeeEstimateError::Read)?;
        let head = self.head();
        let context = SnapshotContext {
            interface_version: self.handshake.node().interface_version,
            correlation_id,
            minimum_sequence: head.chain_sequence,
        };
        let transport = self
            .transport
            .as_mut()
            .ok_or(FeeEstimateError::Read(ReadError::Disconnected))?;
        let snapshot = crate::payments::estimate_fee(
            transport,
            meter.activity_type,
            meter.canonical_bytes,
            meter.execution_units,
            meter.storage_units,
            context,
        )
        .map_err(FeeEstimateError::Read)?;
        if snapshot.observed_sequence != head.chain_sequence {
            return Err(FeeEstimateError::SnapshotSkew {
                head_sequence: head.chain_sequence,
                observed_sequence: snapshot.observed_sequence,
            });
        }
        Ok(FeeObservation { head, snapshot })
    }

    /// Retrieves one independently verified finalized checkpoint.
    ///
    /// # Errors
    ///
    /// Refuses unavailable capability, disconnection and invalid checkpoint or settlement evidence.
    pub fn checkpoint_evidence(
        &mut self,
        selector: CheckpointSelector,
        correlation_id: u64,
    ) -> Result<VerifiedCheckpoint, EvidenceError> {
        if !self
            .handshake
            .capabilities()
            .contains(Capability::Checkpoint)
        {
            return Err(EvidenceError::Unavailable);
        }
        let transport = self.transport.as_mut().ok_or(EvidenceError::Unavailable)?;
        evidence::checkpoint(
            transport,
            selector,
            EvidenceContext {
                interface_version: self.handshake.node().interface_version,
                correlation_id,
                expected_protocol_version: self.config.handshake.expected_protocol_version,
                expected_network_id: self.handshake.node().network_id,
                handshake_sequencer_key: self.handshake.node().authorised_sequencer_key,
            },
        )
    }

    /// Registers a locally verified checkpoint evidence bundle and accepts only
    /// the durable idempotent acknowledgement.
    ///
    /// # Errors
    ///
    /// Refuses unavailable capability, disconnection and malformed or mismatched durable acknowledgements.
    pub fn register_finality_evidence(
        &mut self,
        evidence: &FinalityEvidenceCandidate,
        correlation_id: u64,
    ) -> Result<RegistrationAck, EvidenceError> {
        if !self
            .handshake
            .capabilities()
            .contains(Capability::FinalityEvidenceRegister)
        {
            return Err(EvidenceError::Unavailable);
        }
        let transport = self.transport.as_mut().ok_or(EvidenceError::Unavailable)?;
        evidence::register_finality_evidence(
            transport,
            evidence,
            self.handshake.node().interface_version,
            correlation_id,
        )
    }

    /// Starts or resumes the ordered core event stream through this client's
    /// sole boundary connection.
    ///
    /// # Errors
    ///
    /// Refuses capability/disconnection gaps, invalid bounds and request
    /// transport failures.
    pub fn subscribe_events(
        &mut self,
        cursor: Cursor,
        mut config: StreamConfig,
    ) -> Result<EventStream<'_>, StreamError> {
        if !self
            .handshake
            .capabilities()
            .contains(Capability::EventSubscribe)
        {
            return Err(StreamError::UnavailableCapability);
        }
        config.interface_version = self.handshake.node().interface_version;
        let transport = self
            .transport
            .as_mut()
            .ok_or(StreamError::DisconnectedClient)?;
        subscribe(transport, cursor, config)
    }

    /// Retrieves and verifies availability data from the active node provider.
    /// Additional provider transports use the same `ProviderSet` fetch path.
    ///
    /// # Errors
    ///
    /// Refuses capability/disconnection gaps and invalid retrieval context.
    pub fn fetch_availability<F>(
        &mut self,
        selector: AvailabilitySelector,
        mut context: FetchContext,
        on_chunk: F,
    ) -> Result<FetchOutcome, FetchError>
    where
        F: FnMut(Progress<'_>),
    {
        if !self
            .handshake
            .capabilities()
            .contains(Capability::AvailabilityFetch)
        {
            return Err(FetchError::UnavailableCapability);
        }
        context.interface_version = self.handshake.node().interface_version;
        let transport = self
            .transport
            .as_mut()
            .ok_or(FetchError::DisconnectedClient)?;
        let mut providers = ProviderSet::new(vec![Provider {
            name: "primary".to_owned(),
            transport,
        }]);
        fetch(&mut providers, selector, context, on_chunk)
    }
}

fn requires_historical_account_proofs(requested: VerificationLevel) -> bool {
    requested.wire_rank() >= VerificationLevel::CHECKPOINT_FINALISED.wire_rank()
}

fn state_for(handshake: &Handshake, schema: &Schema) -> ConnectionState {
    if capability_report_with_schema(handshake.capabilities(), schema)
        .gaps()
        .is_empty()
    {
        ConnectionState::Connected
    } else {
        ConnectionState::Degraded
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn finalised_account_reads_require_historical_proofs() {
        assert!(!requires_historical_account_proofs(
            VerificationLevel::STATE_PROVEN
        ));
        assert!(requires_historical_account_proofs(
            VerificationLevel::CHECKPOINT_FINALISED
        ));
        assert!(requires_historical_account_proofs(
            VerificationLevel::SETTLEMENT_ANCHORED
        ));
    }
}
