//! The registry routes the developer CLI calls.
//!
//! Deployment ingestion verifies native activity, receipt and Programs-state proofs.
//! Reading checks the derived local projection and current
//! independently verified protocol state. Verifying source rebuilds mirrored source in
//! the pinned toolchain environment and compares the rebuilt artifact with the
//! registered on-chain code hash, so a mismatch is reported as a mismatch and
//! never as a verified source.

use std::collections::{BTreeMap, BTreeSet, HashMap};
use std::time::Instant;

use layerx_client::lni::head_attestation::ProgramDiscoveryHead;
use layerx_programs::{
    hex, programs_source_verification, AccountStateHead, BuildPlan, BuildRefusal,
    JournalReadAuthority, LifecycleReceipt, ObservedHead, ProgramId, ProgramInterface,
    ProgramLifecycle, ProtocolDeploymentVerifier, Registry, RegistryError, RegistryVersion,
    ReproducibleBuild, SourceArchive, SourceStatus, SourceVerifier, UpgradePolicy,
    VerifiedDeploymentEvidence, VerifiedProgramBalanceRead, VerifiedRegistryRead, WindDownStateAccess,
};
use layerx_programs_protocol_adapter::ProtocolProgramStateRead;
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use sha2::{Digest as _, Sha256};

use crate::builder::{HermeticBuilder, HermeticBuilderConfig};
use crate::head_attestation::{
    attach_discovery_proof, verified_discovery_proof, DiscoveryProofFields, ExpectedDiscoveryHead,
};
use crate::journal::{FileDeploymentJournal, QuarantinedUnit};
use crate::mirror::{MirrorRefusal, SourceMirror};
use crate::node_state::{HeadAuthority, NodeProgramStateSource, ProgramStateCursor};
use crate::program_state::FileProgramStateJournal;
use crate::verified::{
    Admission, JournalRefusal, LeaseRefusal, Publication, Reconciled, VerificationJournal,
    VerificationLease, VerificationRecord, VerificationState, VerifiedSource, VerifiedSourceDeployment, VerifiedSourceStore,
};
use crate::{Authorization, Config};

const IDEMPOTENCY_DOMAIN: &[u8] = b"LayerX/platform/registry/idempotency/v2\0";
const REQUEST_DOMAIN: &[u8] = b"LayerX/platform/registry/source-request/v2\0";
const MAX_CHANGE_PAGES: usize = 1_024;
const ROUTE_PREFIX: &str = "/v1/programs/registry/";

/// One parsed request.
#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct Request {
    pub method: String,
    pub path: String,
    pub headers: HashMap<String, String>,
    pub body: Vec<u8>,
}

/// One rendered response.
#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub struct Response {
    pub status: u16,
    pub body: String,
}

/// The hosted registry. It owns the durable evidence the routes answer from
/// and never answers a read from its own projection alone.
pub struct Registrar {
    registry: Registry,
    event_outbox: crate::event_producer::ProgramOutbox,
    journal: FileDeploymentJournal,
    deployment_lni_socket: Option<std::path::PathBuf>,
    program_state: FileProgramStateJournal,
    node_state: NodeProgramStateSource,
    mirror: SourceMirror,
    verified: VerifiedSourceStore,
    verifier: SourceVerifier<HermeticBuilder>,
    request_authority: crate::RegistryAuthority,
    publication_authority: crate::RegistryAuthority,
    staleness_ms: u64,
    balance_reads: BTreeMap<ProgramId, VerifiedProgramBalanceRead>,
    interfaces: BTreeMap<(ProgramId, u32), ProgramInterface>,
    deployments: BTreeMap<(ProgramId, u32), VerifiedDeploymentEvidence>,
    current_head: Option<AccountStateHead>,
    head_authority: Option<HeadAuthority>,
    verification: VerificationJournal,
    verification_reconciled: Reconciled,
    quarantined: Vec<QuarantinedUnit>,
}

impl Registrar {
    /// Opens every durable store and rebuilds the registry projection from the
    /// canonical journal and the completed rebuilds recorded beside it.
    ///
    /// # Errors
    ///
    /// Returns unusable directories, a corrupt journal, an inadmissible
    /// declared build environment and stored verifications that no longer
    /// decode.
    pub fn open(config: &Config, now: u64) -> Result<Self, String> {
        let builder = HermeticBuilder::new(HermeticBuilderConfig {
            workspace: config.workspace.clone(),
            builder_image_digest: config.builder_image_digest,
            environment_root: config.builder_environment_root.clone(),
            entrypoint: config.builder_entrypoint.clone(),
            isolation_runtime: config.builder_isolation_runtime.clone(),
            isolation_runtime_digest: config.builder_isolation_runtime_digest,
            job_supervisor: config.builder_job_supervisor.clone(),
            job_supervisor_digest: config.builder_job_supervisor_digest,
            cgroup_root: config.builder_cgroup_root.clone(),
            timeout_seconds: config.build_timeout_seconds,
            memory_bytes: config.build_memory_bytes,
            process_limit: config.build_process_limit,
            file_size_bytes: config.build_file_size_bytes,
        })?;
        Self::open_with_builder(config, now, builder, false)
    }

    /// Opens durable and node state with the builder verified by the listener.
    ///
    /// # Errors
    /// Refuses corrupt durable state or unavailable protocol authority.
    pub fn open_with_builder(
        config: &Config,
        now: u64,
        builder: HermeticBuilder,
        health: bool,
    ) -> Result<Self, String> {
        let verifier = SourceVerifier::new(builder, config.attempts)
            .map_err(|refused| format!("the build pipeline is not admissible: {refused}"))?;
        if config.staleness_ms == 0 {
            return Err("a registry read freshness bound is required".to_owned());
        }
        let deployment_verifier = ProtocolDeploymentVerifier::from_protected_history(
            &config.sequencer_trust_history,
            config.staleness_ms,
        )
        .map_err(|error| format!("sequencer trust history is unavailable: {error}"))?;
        let mut registrar = Self {
            registry: Registry::new(),
            event_outbox: crate::event_producer::ProgramOutbox::new(&config.journal),
            journal: FileDeploymentJournal::open(config.journal.clone())?,
            deployment_lni_socket: config.deployment_lni_socket.clone(),
            program_state: FileProgramStateJournal::open(config.journal.join("program-state"))?,
            node_state: NodeProgramStateSource::connect_with_identity(
                &config.node_endpoint,
                config.node_authorization.clone(),
                &config.outbound_ca_der,
                config.outbound_client_identity.as_ref(),
                &config.receipt_authority_endpoint,
                config.receipt_authority_authorization.clone(),
                config.receipt_authority_replica_id,
                deployment_verifier,
            )?,
            mirror: SourceMirror::open(config.mirror.clone())?,
            verified: VerifiedSourceStore::open(config.verified.clone())?,
            verifier,
            request_authority: config.request_authority.clone(),
            publication_authority: config.publication_authority.clone(),
            staleness_ms: config.staleness_ms,
            balance_reads: BTreeMap::new(),
            interfaces: BTreeMap::new(),
            deployments: BTreeMap::new(),
            current_head: None,
            head_authority: None,
            verification: VerificationJournal::open(config.journal.join("verification-requests"))?,
            verification_reconciled: Reconciled::default(),
            quarantined: Vec::new(),
        };
        registrar.verification_reconciled = registrar.verification.reconcile(now)?;
        registrar.rebuild()?;
        match registrar.verification.lease(Instant::now()) {
            Ok(lease) => {
                for record in lease.pending_publications()? {
                    let response = registrar.publish(&lease, record, now);
                    if response.status == 503 {
                        break;
                    }
                }
            }
            Err(LeaseRefusal::Busy) => {}
            Err(LeaseRefusal::Unavailable(error)) => return Err(error),
        }
        if health {
            registrar.node_state.current_head_or_pending(now)?;
            return Ok(registrar);
        }
        if registrar.awaits_first_protocol_head(now)? {
            return Ok(registrar);
        }
        registrar.synchronize_protocol_state(None, now)?;
        Ok(registrar)
    }

    /// A registry with no registered program, no program-state cursor and no
    /// retained head may open while the network has not yet sequenced its
    /// first receipt; every read still refuses until a verified head exists.
    ///
    /// # Errors
    /// Refuses unreadable cursors and every node refusal other than the
    /// stale-projection answer.
    fn awaits_first_protocol_head(&self, now: u64) -> Result<bool, String> {
        if self.current_head.is_some()
            || !self.registry.program_ids().is_empty()
            || self.program_state.cursor()? != ProgramStateCursor::default()
        {
            return Ok(false);
        }
        Ok(self.node_state.current_head_or_pending(now)?.is_none())
    }

    /// Returns the startup-verified builder for the bounded worker pipe.
    #[must_use]
    pub fn verified_builder(&self) -> HermeticBuilder {
        self.verifier.runner().clone()
    }

    /// Reports every incomplete deployment unit the journal quarantined when
    /// the projection was last rebuilt.
    #[must_use]
    pub fn quarantined_units(&self) -> &[QuarantinedUnit] {
        &self.quarantined
    }

    /// Reports what reconciling the durable verification request journal
    /// found when this registrar opened: live identities, corrupt records
    /// kept as evidence and interrupted writes moved aside.
    #[must_use]
    pub const fn verification_reconciled(&self) -> &Reconciled {
        &self.verification_reconciled
    }

    /// Answers one request at the supplied wall-clock millisecond.
    pub fn route(&mut self, request: &Request, now: u64, deadline: Instant) -> Response {
        if Instant::now() >= deadline {
            return refusal(
                503,
                "request_deadline_exceeded",
                "the registry request deadline expired",
            );
        }
        self.node_state.set_request_deadline(deadline);
        if self
            .verifier
            .runner()
            .set_request_deadline(deadline)
            .is_err()
        {
            return refusal(
                503,
                "builder_unavailable",
                "the builder deadline boundary is unavailable",
            );
        }
        let authorization = if request.path == "/healthz" {
            None
        } else if configures_publication_route(request) {
            if !self
                .publication_authority
                .verifies(request.headers.get("authorization").map(String::as_str))
            {
                return refusal(
                    403,
                    "publication_authority_required",
                    "source publication requires the operator authority",
                );
            }
            Some(Authorization::Publication)
        } else {
            if !self
                .request_authority
                .verifies(request.headers.get("authorization").map(String::as_str))
            {
                return refusal(
                    401,
                    "authentication_required",
                    "a valid registry bearer credential is required",
                );
            }
            Some(Authorization::Request)
        };
        let response = self.authorized_route(request, now, authorization, deadline);
        if Instant::now() >= deadline {
            return refusal(
                503,
                "request_deadline_exceeded",
                "the registry request deadline expired",
            );
        }
        response
    }

    fn authorized_route(
        &mut self,
        request: &Request,
        now: u64,
        _authorization: Option<Authorization>,
        deadline: Instant,
    ) -> Response {
        match (request.method.as_str(), request.path.as_str()) {
            ("GET", "/healthz") => Response {
                status: 200,
                body: json!({"status": "ready", "service": "program-registry"}).to_string(),
            },
            ("POST", "/__registry/deployments") => self.ingest_deployment(&request.body, deadline),
            ("POST", "/__registry/head") => self.ingest_head(now),
            ("POST", "/__registry/sources") => {
                if let Err(response) = self.publication_principal(request) {
                    return response;
                }
                self.ingest_source(&request.body, deadline)
            }
            ("GET", "/v1/programs/registry") => self.list(now),
            (
                _,
                "/healthz"
                | "/__registry/deployments"
                | "/__registry/head"
                | "/__registry/sources"
                | "/v1/programs/registry",
            ) => refusal(
                405,
                "method_not_allowed",
                "method is not supported for this route",
            ),
            _ => {
                if Instant::now() >= deadline {
                    refusal(
                        503,
                        "request_deadline_exceeded",
                        "the registry request deadline expired",
                    )
                } else {
                    self.program_route(request, now, deadline)
                }
            }
        }
    }

    /// Reconciles the hosted cache with the authenticated state and receipt
    /// authority owned by the production node that commits activities. The
    /// cursor is advanced only after every affected program has been resolved
    /// at the same current head, independently receipt-checked, replayed and
    /// persisted.
    ///
    /// # Errors
    /// Refuses unavailable, stale, inconsistent or unverified node state,
    /// exhausted scan bounds, and journal persistence failures.
    pub fn synchronize_protocol_state(
        &mut self,
        requested: Option<ProgramId>,
        now: u64,
    ) -> Result<(), String> {
        if now == 0 {
            return Err("program-state synchronization requires an observed time".to_owned());
        }
        let prior_cursor = self.program_state.cursor()?;
        let mut complete = prior_cursor;
        let mut programs = BTreeSet::new();
        let mut caught_up = false;
        let mut feed_head = 0_u64;
        for _ in 0..MAX_CHANGE_PAGES {
            if self.node_state.request_deadline_expired() {
                return Err(
                    "registry request deadline expired during protocol synchronization".to_owned(),
                );
            }
            let (notices, next, scanned, current) = self.node_state.changes(complete)?;
            programs.extend(notices.into_iter().map(|notice| notice.program));
            if next == complete && !current {
                return Err("node program-state change feed made no progress".to_owned());
            }
            complete = next;
            if scanned < feed_head {
                return Err("node program-state scan head regressed".to_owned());
            }
            feed_head = scanned;
            if current {
                caught_up = true;
                break;
            }
        }
        if !caught_up {
            return Err("node program-state change feed exceeded its page bound".to_owned());
        }
        if prior_cursor.sequence == 0 || self.balance_reads.is_empty() {
            programs.extend(self.registry.program_ids());
        }
        if let Some(program) = requested {
            programs.insert(program);
        }
        let (current_head, head_authority) = self.node_state.current_head_authority(now)?;
        if complete.sequence > feed_head || feed_head > current_head.freshness.observed_sequence {
            return Err(
                "program-state feed is ahead of the independently verified head".to_owned(),
            );
        }
        let mut registry = self.registry.clone();
        let mut staged = Vec::with_capacity(programs.len());
        for program in programs {
            let entry = registry
                .entry_for_wind_down(program)
                .map_err(|error| format!("program-state registry lookup refused: {error}"))?;
            let abi = entry.versions.last().map(|version| version.abi_version);
            if matches!(abi, Some(1 | 3 | 4)) && entry.value_accounts.is_empty() {
                continue;
            }
            if abi != Some(2) {
                return Err("program value accounts require the frozen ABI-two protocol".to_owned());
            }
            let record = self.node_state.program_state(program, current_head)?;
            let state = ProtocolProgramStateRead::restore_verified(
                &record.bytes,
                &mut registry,
                record.receipt,
                current_head,
                now,
                self.staleness_ms,
            )
            .map_err(|error| format!("protocol program-state adapter refused: {error:?}"))?;
            if state.program() != program {
                return Err("node program-state record changed program identity".to_owned());
            }
            staged.push(state);
        }

        for state in &staged {
            self.program_state.store(state)?;
        }
        self.program_state.advance(complete)?;
        self.journal.refresh_head(ObservedHead {
            sequence: current_head.freshness.observed_sequence,
            observed_at: current_head.freshness.observed_at,
        })?;
        let mut balance_reads = self.balance_reads.clone();
        for state in staged {
            let balances = state.into_balances();
            balance_reads.insert(balances.program(), balances);
        }
        self.registry = registry;
        self.balance_reads = balance_reads;
        self.current_head = Some(current_head);
        self.head_authority = Some(head_authority);
        Ok(())
    }

    fn program_route(&mut self, request: &Request, now: u64, deadline: Instant) -> Response {
        let Some(rest) = request.path.strip_prefix(ROUTE_PREFIX) else {
            return refusal(404, "not_found", "route does not exist");
        };
        match rest.split_once('/') {
            None if request.method == "GET" => self.read(rest, now),
            Some((program, "interface")) if request.method == "GET" => {
                self.read_interface(program, now)
            }
            Some((program, "source")) if request.method == "POST" => {
                self.verify(program, request, now, deadline)
            }
            None | Some((_, "interface" | "source")) => refusal(
                405,
                "method_not_allowed",
                "method is not supported for this route",
            ),
            Some(_) => refusal(404, "not_found", "route does not exist"),
        }
    }

    fn list(&mut self, now: u64) -> Response {
        if let Err(error) = self.synchronize_protocol_state(None, now) {
            return refusal(503, "protocol_state_unavailable", &error);
        }
        if let Err(error) = JournalReadAuthority::new(&self.journal, now, self.staleness_ms) {
            return refusal(503, "read_unverifiable", &error.to_string());
        }
        let Some(head) = self.current_head else {
            return refusal(
                503,
                "protocol_head_unavailable",
                "a current independently verified protocol head is not available",
            );
        };
        let Some(valid_through) = head.freshness.observed_at.checked_add(self.staleness_ms) else {
            return refusal(503, "stale_read", "protocol head freshness overflowed");
        };
        if now > valid_through {
            return refusal(
                503,
                "stale_read",
                "protocol head is outside its freshness bound",
            );
        }
        let program_ids: Vec<String> = self
            .registry
            .program_ids()
            .iter()
            .map(|program| hex::encode(&program.bytes()))
            .collect();
        Response {
            status: 200,
            body: json!({
                "program_ids": program_ids,
                "state_root": hex::encode(&head.state_root),
                "observed_sequence": head.freshness.observed_sequence,
                "observed_at": head.freshness.observed_at,
                "valid_through": valid_through,
                "verification": "registry-receipt-and-current-head-verified",
            })
            .to_string(),
        }
    }

    fn read(&mut self, program: &str, now: u64) -> Response {
        let Some(program) = program_id(program) else {
            return refusal(
                400,
                "invalid_argument",
                "program id must be thirty-two hexadecimal-encoded bytes",
            );
        };
        if self.registry.latest_version(program).is_err() {
            return refusal(404, "not_found", "program is not registered");
        }
        if let Err(error) = self.synchronize_protocol_state(Some(program), now) {
            return refusal(503, "protocol_state_unavailable", &error);
        }
        let authority = match JournalReadAuthority::new(&self.journal, now, self.staleness_ms) {
            Ok(authority) => authority,
            Err(error) => return refusal(503, "read_unverifiable", &error.to_string()),
        };
        match self.registry.read(program, &authority) {
            Ok(read) => self.render_read(&read, now),
            Err(RegistryError::UnknownProgram | RegistryError::UnknownVersion) => {
                refusal(404, "not_found", "program is not registered")
            }
            Err(error @ RegistryError::StaleRead) => refusal(503, "stale_read", &error.to_string()),
            Err(error) => refusal(502, "unverified_read", &error.to_string()),
        }
    }

    fn render_read(&self, read: &VerifiedRegistryRead, now: u64) -> Response {
        let Some(head) = self.current_head else {
            return refusal(
                503,
                "protocol_head_unavailable",
                "a current independently verified protocol head is not available",
            );
        };
        let Some(valid_through) = head.freshness.observed_at.checked_add(self.staleness_ms) else {
            return refusal(503, "stale_read", "protocol head freshness overflowed");
        };
        if now > valid_through {
            return refusal(
                503,
                "stale_read",
                "protocol head is outside its freshness bound",
            );
        }
        let abi = read
            .entry
            .versions
            .last()
            .map(|version| version.abi_version);
        if matches!(abi, Some(1 | 3 | 4)) && read.entry.value_accounts.is_empty() {
            return Response {
                status: 200,
                body: self
                    .with_discovery_proof(
                        registry_read_json(read, None, head, valid_through),
                        read,
                        head,
                        valid_through,
                    )
                    .to_string(),
            };
        }
        if abi != Some(2) {
            return refusal(
                502,
                "balance_protocol_unsupported",
                "program value accounts require the frozen ABI-two account protocol",
            );
        }
        let Some(balances) = self.balance_reads.get(&read.entry.program) else {
            return refusal(
                503,
                "balance_read_unavailable",
                "a current receipt-proven program balance read is not available",
            );
        };
        let freshness = balances.freshness();
        if now < freshness.observed_at
            || now.saturating_sub(freshness.observed_at) > self.staleness_ms
            || freshness.observed_sequence < read.freshness.observed_sequence
        {
            return refusal(
                503,
                "stale_balance_read",
                "the program balance proof is not current at the observed registry head",
            );
        }
        let bindings_match = balances.bindings().len() == read.entry.value_accounts.len()
            && read.entry.value_accounts.iter().all(|binding| {
                balances
                    .bindings()
                    .iter()
                    .any(|candidate| candidate == binding)
            });
        if balances.program() != read.entry.program
            || balances.lifecycle() != read.entry.lifecycle
            || !bindings_match
        {
            return refusal(
                502,
                "balance_registry_mismatch",
                "the current balance proof does not match the receipt-verified registry record",
            );
        }
        Response {
            status: 200,
            body: self
                .with_discovery_proof(
                    registry_read_json(read, Some(balances), head, valid_through),
                    read,
                    head,
                    valid_through,
                )
                .to_string(),
        }
    }

    /// Attaches the sequencer's discovery proof when the node attests exactly
    /// the head and latest version this read verified. Every other outcome
    /// publishes the document without the proof fields.
    fn with_discovery_proof(
        &self,
        mut document: Value,
        read: &VerifiedRegistryRead,
        head: AccountStateHead,
        valid_through: u64,
    ) -> Value {
        if let Some(versions) = document["versions"].as_array_mut() {
            for version in versions {
                if let Some(number) = version["version"].as_u64().and_then(|number| u32::try_from(number).ok()) {
                    if let Some(deployment) = self.deployments.get(&(read.entry.program, number)) {
                        version["deployment_provenance"] = deployment_provenance_json(deployment);
                    }
                }
            }
        }
        if let Err(error) = self
            .discovery_proof(read, head, valid_through)
            .and_then(|fields| attach_discovery_proof(&mut document, &fields))
        {
            eprintln!(
                "layerx-program-registry: program {} published without discovery proof: {error}",
                hex::encode(&read.entry.program.bytes())
            );
        }
        document
    }

    fn discovery_proof(
        &self,
        read: &VerifiedRegistryRead,
        head: AccountStateHead,
        valid_through: u64,
    ) -> Result<DiscoveryProofFields, String> {
        let authority = self
            .head_authority
            .ok_or_else(|| "the verified head authority is unavailable".to_owned())?;
        let version = read
            .entry
            .versions
            .last()
            .ok_or_else(|| "program has no verified version".to_owned())?;
        let expected = ExpectedDiscoveryHead {
            head: ProgramDiscoveryHead {
                program_id: read.entry.program.bytes(),
                version: version.number,
                code_hash: version.code_hash,
                abi_version: version.abi_version,
                observed_sequence: head.freshness.observed_sequence,
                observed_at: head.freshness.observed_at,
                valid_through,
                state_root: head.state_root,
            },
            head_receipt_digest: head.receipt_digest,
        };
        verified_discovery_proof(
            self.deployment_lni_socket.as_deref(),
            &self.node_state,
            &expected,
            self.staleness_ms,
            &authority,
        )
    }

    fn read_interface(&mut self, program: &str, now: u64) -> Response {
        let Some(program) = program_id(program) else {
            return refusal(
                400,
                "invalid_argument",
                "program id must be thirty-two hexadecimal-encoded bytes",
            );
        };
        if self.registry.latest_version(program).is_err() {
            return refusal(404, "not_found", "program is not registered");
        }
        if let Err(error) = self.synchronize_protocol_state(Some(program), now) {
            return refusal(503, "protocol_state_unavailable", &error);
        }
        let authority = match JournalReadAuthority::new(&self.journal, now, self.staleness_ms) {
            Ok(authority) => authority,
            Err(error) => return refusal(503, "read_unverifiable", &error.to_string()),
        };
        let read = match self.registry.read(program, &authority) {
            Ok(read) => read,
            Err(RegistryError::UnknownProgram | RegistryError::UnknownVersion) => {
                return refusal(404, "not_found", "program is not registered")
            }
            Err(error @ RegistryError::StaleRead) => {
                return refusal(503, "stale_read", &error.to_string())
            }
            Err(error) => return refusal(502, "unverified_read", &error.to_string()),
        };
        let Some(version) = read.entry.versions.last() else {
            return refusal(502, "unverified_read", "program has no verified version");
        };
        let Some(interface) = self.interfaces.get(&(program, version.number)) else {
            return refusal(
                404,
                "interface_absent",
                "program has no published interface",
            );
        };
        if interface.code_hash() != version.code_hash
            || interface.abi_version() != version.abi_version
        {
            return refusal(
                502,
                "interface_registry_mismatch",
                "published interface does not match the current verified program version",
            );
        }
        let Some(head) = self.current_head else {
            return refusal(
                503,
                "protocol_head_unavailable",
                "current protocol head is unavailable",
            );
        };
        let Some(valid_through) = head.freshness.observed_at.checked_add(self.staleness_ms) else {
            return refusal(503, "stale_read", "protocol head freshness overflowed");
        };
        if now > valid_through {
            return refusal(
                503,
                "stale_read",
                "protocol head is outside its freshness bound",
            );
        }
        Response {
            status: 200,
            body: json!({
                "program_id": hex::encode(&program.bytes()),
                "version": version.number,
                "code_hash": hex::encode(&version.code_hash),
                "abi_version": version.abi_version,
                "interface": hex::encode(interface.canonical_encoding()),
                "interface_digest": hex::encode(interface.digest().as_bytes()),
                "deployment_receipt_digest": hex::encode(&version.deployment_receipt_digest),
                "state_root": hex::encode(&head.state_root),
                "observed_sequence": head.freshness.observed_sequence,
                "observed_at": head.freshness.observed_at,
                "valid_through": valid_through,
                "source": source_json(version.source),
                "verification": "deployment-interface-and-current-head-verified",
            })
            .to_string(),
        }
    }

    fn verify(
        &mut self,
        program: &str,
        request: &Request,
        now: u64,
        deadline: Instant,
    ) -> Response {
        let Some(program) = program_id(program) else {
            return refusal(
                400,
                "invalid_argument",
                "program id must be thirty-two hexadecimal-encoded bytes",
            );
        };
        let Some(key) = request.headers.get("idempotency-key") else {
            return refusal(
                400,
                "idempotency_key_required",
                "source verification requires an Idempotency-Key header",
            );
        };
        if !valid_idempotency_key(key) {
            return refusal(
                400,
                "invalid_argument",
                "idempotency key must be 16-128 ASCII letters, digits, dashes, or underscores",
            );
        }
        let Some((source_uri, source_digest)) = source_request(&request.body) else {
            return refusal(
                400,
                "invalid_argument",
                "request must carry only source_uri and a thirty-two byte hexadecimal source_digest",
            );
        };
        let principal = match self.publication_principal(request) {
            Ok(principal) => principal,
            Err(response) => return response,
        };
        let scope = scoped_key(&principal, program, key);
        let deployment = match self.source_deployment(program, now) {
            Ok(deployment) => deployment,
            Err(response) => return response,
        };
        let digest = request_digest(&deployment, &source_uri, &source_digest);
        let lease = match self.verification.lease(deadline) {
            Ok(lease) => lease,
            Err(LeaseRefusal::Busy) => {
                return refusal(
                    503,
                    "verification_pending",
                    "another worker owns the verification request journal; retry with the same Idempotency-Key",
                )
            }
            Err(LeaseRefusal::Unavailable(error)) => {
                return refusal(503, "idempotency_store_unavailable", &error)
            }
        };
        let record = match lease.admit(&scope, &principal, program, digest, now) {
            Ok(Admission::Build(record)) => record,
            Ok(Admission::Publish(record)) => return self.publish(&lease, record, now),
            Ok(Admission::Artifact(record)) => return self.persist_artifact(&lease, record, now),
            Ok(Admission::Replay(response)) => return response,
            Ok(Admission::Conflict) => {
                return refusal(
                    409,
                    "idempotency_conflict",
                    "idempotency key was already used for a different request",
                )
            }
            Ok(Admission::QuotaExhausted) => {
                return refusal(
                    503,
                    "idempotency_quota_exhausted",
                    "every retained verification request is still live; retry later",
                )
            }
            Err(refused) => return journal_refusal(&refused),
        };
        self.build_and_settle(
            &lease,
            record,
            &deployment,
            &source_uri,
            source_digest,
            &principal,
            now,
        )
    }

    /// Runs the rebuild this lease owns and settles its durable outcome. A
    /// 503 commits nothing and stays retryable; a verified rebuild commits its
    /// publication before the response is acknowledged.
    #[allow(clippy::too_many_arguments)]
    fn build_and_settle(
        &mut self,
        lease: &VerificationLease,
        mut record: VerificationRecord,
        deployment: &VerifiedDeploymentEvidence,
        source_uri: &str,
        source_digest: [u8; 32],
        _principal: &str,
        now: u64,
    ) -> Response {
        let mut artifact = None;
        let response = self.reproduce(deployment, source_uri, source_digest, &mut artifact);
        if response.status == 503 {
            return settled(
                lease,
                &mut record,
                VerificationState::Retryable,
                now,
                response,
            );
        }
        if artifact.is_none() {
            let state = VerificationState::Completed {
                response: response.clone(),
            };
            return settled(lease, &mut record, state, now, response);
        }
        let Some(artifact) = artifact else {
            return refusal(503, "verification_pending", "completed rebuild artifact is unavailable");
        };
        let state = VerificationState::Artifact {
            response,
            source: VerifiedSourceStore::encode_record(&artifact),
        };
        if let Err(refused) = lease.settle(&mut record, state, now) {
            return journal_refusal(&refused);
        }
        self.persist_artifact(lease, record, now)
    }

    fn persist_artifact(
        &mut self,
        lease: &VerificationLease,
        mut record: VerificationRecord,
        now: u64,
    ) -> Response {
        let VerificationState::Artifact { response, source } = record.state.clone() else {
            return refusal(503, "idempotency_store_corrupt", "prepared artifact is absent");
        };
        let artifact = match VerifiedSourceStore::decode_record(&source) {
            Ok(artifact) => artifact,
            Err(error) => return refusal(503, "idempotency_store_corrupt", &error),
        };
        let program = artifact.program;
        let Some(publication_now) = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH).ok()
            .and_then(|elapsed| u64::try_from(elapsed.as_millis()).ok())
        else {
            return refusal(503, "clock_unavailable", "source publication requires a valid clock");
        };
        let deployment = match self.source_deployment(program, publication_now) {
            Ok(deployment) => deployment,
            Err(response) => return response,
        };
        if deployment.version() != artifact.version
            || deployment_provenance(&deployment) != artifact.deployment_provenance
            || deployment.code_hash() != artifact.artifact_digest
        {
            return refusal(409, "deployment_changed", "prepared source verification no longer matches its verified deployment");
        }
        if record.program != hex::encode(&program.bytes())
            || record.request_digest != hex::encode(&request_digest(
                &deployment, &artifact.source_uri, &artifact.source_digest,
            ))
        {
            return refusal(503, "idempotency_store_corrupt", "prepared artifact request differs");
        }
        if let Err(error) = self.verified.record(&artifact) {
            return refusal(503, "persistence_unavailable", &error);
        }
        let build = match ReproducibleBuild::from_record(
            artifact.source_uri, artifact.source_digest,
            artifact.plan.environment, artifact.artifact_digest,
        ) {
            Ok(build) => build,
            Err(error) => return refusal(503, "idempotency_store_corrupt", &error.to_string()),
        };
        if let Err(error) = self.registry.verify_source(program, artifact.version, &build) {
            return refusal(503, "verification_pending", &error.to_string());
        }
        if response.status != 200 {
            let state = VerificationState::Completed { response: response.clone() };
            return settled(lease, &mut record, state, now, response);
        }
        let read = self.read(&hex::encode(&program.bytes()), publication_now);
        if read.status != 200 {
            return read;
        }
        let mut publication: Value = match serde_json::from_str(&read.body) {
            Ok(body) => body,
            Err(error) => return refusal(503, "verification_pending", &error.to_string()),
        };
        let Some(versions) = publication["versions"].as_array_mut() else {
            return refusal(503, "verification_pending", "verified version evidence is absent");
        };
        versions.retain(|version| version["version"].as_u64() == Some(u64::from(artifact.version)));
        if versions.len() != 1 {
            return refusal(503, "verification_pending", "prepared artifact version evidence is absent");
        }
        let state = VerificationState::Persisted {
            response,
            publication: Publication {
                body: publication.to_string(),
                principal: record.principal.clone(),
                occurred_at: record.updated_at,
            },
        };
        if let Err(refused) = lease.settle(&mut record, state, now) {
            return journal_refusal(&refused);
        }
        self.publish(lease, record, now)
    }

    /// Queues the committed publication of a persisted verification, then
    /// durably acknowledges its terminal response. The outbox deduplicates
    /// the identical publication a recovery re-enqueues.
    fn publish(
        &self,
        lease: &VerificationLease,
        mut record: VerificationRecord,
        now: u64,
    ) -> Response {
        let VerificationState::Persisted {
            response,
            publication,
        } = record.state.clone()
        else {
            return journal_refusal(&JournalRefusal::Corrupt(
                "publication recovery requires a persisted verification".to_owned(),
            ));
        };
        if self
            .event_outbox
            .enqueue_publication(
                &publication.body,
                &publication.principal,
                publication.occurred_at,
            )
            .is_err()
        {
            return refusal(
                503,
                "program_event_unavailable",
                "verified program publication could not be queued; retry with the same Idempotency-Key",
            );
        }
        let state = VerificationState::Completed {
            response: response.clone(),
        };
        settled(lease, &mut record, state, now, response)
    }

    fn source_deployment(
        &mut self,
        program: ProgramId,
        now: u64,
    ) -> Result<VerifiedDeploymentEvidence, Response> {
        if self.registry.latest_version(program).is_err() {
            return Err(refusal(404, "not_found", "program is not registered"));
        }
        self.synchronize_protocol_state(Some(program), now)
            .map_err(|error| refusal(503, "protocol_state_unavailable", &error))?;
        let authority = JournalReadAuthority::new(&self.journal, now, self.staleness_ms)
            .map_err(|error| refusal(503, "read_unverifiable", &error.to_string()))?;
        let read = self.registry.read(program, &authority)
            .map_err(|error| refusal(502, "unverified_read", &error.to_string()))?;
        if read.entry.lifecycle != ProgramLifecycle::Active {
            return Err(refusal(422, "deployment_inactive", "source verification requires an active deployment"));
        }
        let version = read.entry.versions.last()
            .ok_or_else(|| refusal(502, "unverified_read", "program has no verified version"))?;
        let deployment = self.deployments.get(&(program, version.number))
            .ok_or_else(|| refusal(502, "deployment_unverified", "verified deployment evidence is absent"))?;
        if deployment.program() != program
            || deployment.version() != version.number
            || deployment.code_hash() != version.code_hash
            || deployment.abi_version() != version.abi_version
            || deployment.receipt_digest() != version.deployment_receipt_digest
            || !(1..=4).contains(&deployment.abi_version())
            || deployment.lifecycle() != ProgramLifecycle::Active
        {
            return Err(refusal(422, "deployment_mismatch", "verified deployment does not match the current registry version"));
        }
        let head = self.current_head
            .ok_or_else(|| refusal(503, "protocol_head_unavailable", "current protocol head is unavailable"))?;
        let valid_through = head.freshness.observed_at.checked_add(self.staleness_ms)
            .filter(|until| now >= head.freshness.observed_at && now <= *until)
            .ok_or_else(|| refusal(503, "stale_read", "current protocol head is outside its freshness bound"))?;
        self.discovery_proof(&read, head, valid_through)
            .map_err(|error| refusal(503, "deployment_head_unverified", &error))?;
        Ok(deployment.clone())
    }

    fn reproduce(&mut self, deployment: &VerifiedDeploymentEvidence, uri: &str, source_digest: [u8; 32], artifact: &mut Option<VerifiedSource>) -> Response {
        let program = deployment.program();
        let version = deployment.version();
        let mirrored = match self.mirror.fetch(uri, source_digest) {
            Ok(mirrored) => mirrored,
            Err(refused @ MirrorRefusal::NotMirrored) => {
                return refusal(404, "source_not_mirrored", &refused.to_string())
            }
            Err(refused) => return refusal(422, "source_unverifiable", &refused.to_string()),
        };
        let build = match self.verifier.reproduce_deployment(&mirrored.source, &mirrored.plan, deployment) {
            Ok(build) => build,
            Err(refused) => return rebuild_refusal(&refused),
        };
        if self.node_state.request_deadline_expired() {
            return refusal(503, "request_deadline_exceeded", "the registry request deadline expired during rebuild");
        }
        let Some(now) = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH).ok()
            .and_then(|elapsed| u64::try_from(elapsed.as_millis()).ok())
        else {
            return refusal(503, "clock_unavailable", "source verification requires a valid clock");
        };
        let current = match self.source_deployment(program, now) {
            Ok(current) => current,
            Err(response) => return response,
        };
        if current != *deployment {
            return refusal(409, "deployment_changed", "verified deployment changed during source reproduction");
        }
        let mut candidate = self.registry.clone();
        let status = match candidate.verify_source(program, version, &build) {
            Ok(status) => status,
            Err(error) => return refusal(404, "not_found", &error.to_string()),
        };
        *artifact = Some(VerifiedSource {
            program,
            version,
            source_uri: build.source_uri.clone(),
            source_digest: build.source_digest,
            artifact_digest: build.artifact_digest,
            plan: mirrored.plan.clone(),
            deployment_provenance: deployment_provenance(deployment),
        });
        verification_response(deployment, &build, status)
    }

    fn ingest_deployment(&mut self, body: &[u8], deadline: Instant) -> Response {
        let loaded = match self.journal.load() {
            Ok(loaded) => loaded,
            Err(error) => return refusal(503, "journal_unavailable", &error),
        };
        if let Some(unit) = loaded
            .units
            .iter()
            .find(|unit| unit.proof().activity == body)
        {
            let evidence = match self.node_state.verify_stored_deployment(unit.proof()) {
                Ok(evidence) => evidence,
                Err(error) => return refusal(422, "deployment_proof_refused", &error),
            };
            if let Err(error) = self.journal.export_pair(&evidence) {
                return refusal(503, "journal_export_unavailable", &error);
            }
            return deployment_response(&evidence);
        }
        if body.is_empty() {
            return deployment_ingress_unavailable(body);
        }
        let result = match &self.deployment_lni_socket {
            Some(socket) => crate::deployment::deploy(socket, body, deadline),
            None => self.node_state.deploy(body, deadline),
        };
        let proof = match result {
            Ok(proof) => proof,
            Err(error) => return refusal(503, "deployment_proof_unavailable", &error),
        };
        let Some(now) = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .ok()
            .and_then(|elapsed| u64::try_from(elapsed.as_millis()).ok())
        else {
            return refusal(
                503,
                "clock_unavailable",
                "deployment verification requires a valid clock",
            );
        };
        let evidence = match self.node_state.verify_deployment(&proof, now) {
            Ok(evidence) => evidence,
            Err(error) => return refusal(422, "deployment_proof_refused", &error),
        };
        if let Err(error) = self.node_state.verify_deployment_authority(&proof) {
            return refusal(503, "receipt_authority_unavailable", &error);
        }
        let mut candidate = self.registry.clone();
        if let Err(error) = candidate.record_verified_deployment(&evidence) {
            return refusal(422, "deployment_projection_refused", &error.to_string());
        }
        if let Err(error) = self.journal.append(&evidence) {
            return refusal(503, "journal_unavailable", &error);
        }
        self.registry = candidate;
        self.deployments.insert((evidence.program(), evidence.version()), evidence.clone());
        if let Some(interface) = evidence.interface() {
            self.interfaces
                .insert((evidence.program(), evidence.version()), interface.clone());
        }
        if let Err(error) = self.journal.export_pair(&evidence) {
            return refusal(503, "journal_export_unavailable", &error);
        }
        deployment_response(&evidence)
    }

    fn rebuild(&mut self) -> Result<(), String> {
        let mut registry = Registry::new();
        let mut interfaces = BTreeMap::new();
        let mut deployments = BTreeMap::new();
        let loaded = self.journal.load()?;
        for unit in &loaded.units {
            let evidence = self.node_state.verify_stored_deployment(unit.proof())?;
            self.journal.audit_projection(&evidence)?;
            deployments.insert((evidence.program(), evidence.version()), evidence.clone());
            self.journal.export_pair(&evidence)?;
            if let Some(interface) = evidence.interface() {
                interfaces.insert((evidence.program(), evidence.version()), interface.clone());
            }
            registry
                .record_verified_deployment(&evidence)
                .map_err(|error| {
                    format!("verified deployment history is not replayable: {error}")
                })?;
        }
        for record in self.verified.records()? {
            let deployment = deployments.get(&(record.program, record.version))
                .ok_or_else(|| "a stored source verification has no verified deployment".to_owned())?;
            if record.deployment_provenance != deployment_provenance(deployment)
                || record.artifact_digest != deployment.code_hash()
                || !(1..=4).contains(&deployment.abi_version())
            {
                return Err("a stored source verification has mismatched deployment provenance".to_owned());
            }
            let mirrored = self.mirror.fetch(&record.source_uri, record.source_digest)
                .map_err(|error| format!("a stored source verification has no matching source archive: {error}"))?;
            if mirrored.plan != record.plan {
                return Err("a stored source verification has a different pinned build plan".to_owned());
            }
            let build = ReproducibleBuild::from_record(
                record.source_uri.clone(),
                record.source_digest,
                record.plan.environment.clone(),
                record.artifact_digest,
            )
            .map_err(|error| format!("a stored verification is not admissible: {error}"))?;
            registry.verify_source(record.program, record.version, &build)
                .map_err(|error| format!("a stored verification is not replayable: {error}"))?;
        }
        self.program_state.audit()?;
        self.registry = registry;
        self.balance_reads.clear();
        self.interfaces = interfaces;
        self.deployments = deployments;
        self.current_head = None;
        self.head_authority = None;
        self.quarantined = loaded.quarantined;
        Ok(())
    }

    fn publication_principal(&self, request: &Request) -> Result<String, Response> {
        let result = request
            .headers
            .get("layerx-key")
            .ok_or_else(|| "publication key missing".to_owned())
            .and_then(|key| {
                layerx_platform_internal::principal::PrincipalClient::from_environment(
                    "LAYERX_REGISTRY_IDENTITY",
                )?
                .resolve(key)
            });
        result.map_err(|_| {
            if self.event_outbox.unbound().is_err() {
                return refusal(
                    503,
                    "program_event_store_unavailable",
                    "unbound principal counter could not be persisted",
                );
            }
            refusal(
                403,
                "publication_principal_unresolved",
                "identity could not resolve the publication key",
            )
        })
    }

    fn ingest_head(&mut self, now: u64) -> Response {
        let verified = match self.node_state.current_head(now) {
            Ok(head) => head,
            Err(error) => return refusal(503, "protocol_state_unavailable", &error),
        };
        let head = ObservedHead {
            sequence: verified.freshness.observed_sequence,
            observed_at: verified.freshness.observed_at,
        };
        match self.journal.refresh_head(head) {
            Ok(()) => Response {
                status: 200,
                body: json!({
                    "observed": true,
                    "sequence": head.sequence,
                    "observed_at": head.observed_at,
                    "receipt_digest": hex::encode(&verified.receipt_digest),
                    "state_root": hex::encode(&verified.state_root),
                })
                .to_string(),
            },
            Err(error) => refusal(400, "invalid_argument", &error),
        }
    }

    fn ingest_source(&mut self, body: &[u8], deadline: Instant) -> Response {
        let Ok(document) = serde_json::from_slice::<Value>(body) else {
            return refusal(400, "invalid_argument", "request body is not JSON");
        };
        let (Some(uri), Some(plan), Some(archive)) = (
            document["source_uri"].as_str(),
            document["plan"].as_str(),
            document["archive_hex"]
                .as_str()
                .and_then(|text| hex::decode(text).ok()),
        ) else {
            return refusal(
                400,
                "invalid_argument",
                "request must carry source_uri, plan and archive_hex",
            );
        };
        let archive = match SourceArchive::decode(&archive) {
            Ok(archive) => archive,
            Err(error) => return refusal(400, "invalid_argument", &error.to_string()),
        };
        let plan = match BuildPlan::parse(plan) {
            Ok(plan) => plan,
            Err(error) => return refusal(400, "invalid_argument", &error.to_string()),
        };
        if Instant::now() >= deadline {
            return refusal(
                503,
                "request_deadline_exceeded",
                "the registry request deadline expired before source publication",
            );
        }
        match self.mirror.publish(uri, &plan, &archive) {
            Ok(digest) => Response {
                status: 200,
                body: json!({
                    "mirrored": true,
                    "source_uri": uri,
                    "source_digest": hex::encode(&digest),
                })
                .to_string(),
            },
            Err(error) => refusal(400, "invalid_argument", &error),
        }
    }
}

fn configures_publication_route(request: &Request) -> bool {
    request.path == "/__registry/sources"
}

/// Renders one refusal in the platform's refusal envelope.
#[must_use]
pub fn refusal(status: u16, code: &str, detail: &str) -> Response {
    Response {
        status,
        body: json!({"error": {"code": code, "retry": "never", "detail": detail}}).to_string(),
    }
}

fn rebuild_refusal(refused: &BuildRefusal) -> Response {
    match refused {
        BuildRefusal::Registry(RegistryError::DeploymentMismatch) => refusal(
            409,
            "source_mismatch",
            "the rebuilt artifact does not match the verified deployment",
        ),
        BuildRefusal::SandboxUnavailable { reason } => refusal(503, "builder_unavailable", reason),
        BuildRefusal::BuilderFailed { reason } => refusal(422, "build_failed", reason),
        BuildRefusal::NondeterministicBuild { .. } => {
            refusal(422, "build_not_reproducible", &refused.to_string())
        }
        _ => refusal(422, "source_unverifiable", &refused.to_string()),
    }
}

fn verification_response(
    deployment: &VerifiedDeploymentEvidence,
    build: &ReproducibleBuild,
    status: SourceStatus,
) -> Response {
    let verified = matches!(status, SourceStatus::Verified { .. });
    let outcome = json!({
        "program_id": hex::encode(&deployment.program().bytes()),
        "version": deployment.version(),
        "abi_version": deployment.abi_version(),
        "code_hash": hex::encode(&deployment.code_hash()),
        "deployment_receipt_digest": hex::encode(&deployment.receipt_digest()),
        "deployment_provenance": deployment_provenance_json(deployment),
        "source_uri": build.source_uri,
        "source_digest": hex::encode(&build.source_digest),
        "environment_digest": hex::encode(&build.environment_digest),
        "reproduced_artifact_digest": hex::encode(&build.artifact_digest),
        "source": source_json(status),
        "pipeline": programs_source_verification(),
    });
    if verified {
        return Response {
            status: 200,
            body: outcome.to_string(),
        };
    }
    Response {
        status: 409,
        body: json!({
            "error": {
                "code": "source_mismatch",
                "retry": "never",
                "detail": "the rebuilt artifact does not hash to the registered code hash",
            },
            "verification": outcome,
        })
        .to_string(),
    }
}

fn registry_read_json(
    read: &VerifiedRegistryRead,
    balances: Option<&VerifiedProgramBalanceRead>,
    head: AccountStateHead,
    valid_through: u64,
) -> Value {
    let lifecycle = balances.map_or(read.entry.lifecycle, VerifiedProgramBalanceRead::lifecycle);
    let value_accounts = balances.map_or_else(
        || {
            json!({
                "status": if read.entry.versions.last().is_some_and(|version| version.abi_version == 1) {
                    "account-incapable-abi1"
                } else {
                    "no-registered-accounts"
                },
                "accounts": [],
            })
        },
        |balances| {
            json!({
                "status": "current",
                "lifecycle": lifecycle_name(balances.lifecycle()),
                "accounts": balances.value_accounts().iter().map(|account| json!({
                    "account_id": hex::encode(&account.account_id),
                    "asset_id": hex::encode(&account.asset_id),
                    "balance": account.balance.to_string(),
                    "frozen": account.frozen,
                })).collect::<Vec<Value>>(),
                "receipt": {
                    "receipt_digest": hex::encode(&balances.receipt_digest()),
                    "state_root": hex::encode(&balances.state_root()),
                    "observed_sequence": balances.freshness().observed_sequence,
                    "observed_at": balances.freshness().observed_at,
                    "verification": "account-primary-and-state-proof-verified",
                },
            })
        },
    );
    json!({
        "program_id": hex::encode(&read.entry.program.bytes()),
        "upgrade_policy": policy_json(read.entry.upgrade_policy),
        "lifecycle": lifecycle_name(lifecycle),
        "state_root": hex::encode(&head.state_root),
        "observed_sequence": head.freshness.observed_sequence,
        "observed_at": head.freshness.observed_at,
        "valid_through": valid_through,
        "latest_version": read.entry.versions.last().map(|version| version.number),
        "versions": read
            .entry
            .versions
            .iter()
            .map(version_json)
            .collect::<Vec<Value>>(),
        "lifecycle_history": read
            .entry
            .lifecycle_history
            .iter()
            .map(lifecycle_json)
            .collect::<Vec<Value>>(),
        "exit_routes": read
            .entry
            .exit_routes
            .iter()
            .map(|route| json!({
                "seed_hex": hex::encode(&route.seed),
                "account_id": hex::encode(&route.account_id),
                "asset_id": hex::encode(&route.asset_id),
                "destination": hex::encode(&route.destination),
            }))
            .collect::<Vec<Value>>(),
        "value_accounts": value_accounts,
        "receipt": {
            "deployment_receipt_digest": hex::encode(&read.receipt_digest),
            "observed_sequence": read.freshness.observed_sequence,
            "observed_at": read.freshness.observed_at,
            "verification": "receipt-verified",
        },
    })
}

fn version_json(version: &RegistryVersion) -> Value {
    json!({
        "version": version.number,
        "code_hash": hex::encode(&version.code_hash),
        "abi_version": version.abi_version,
        "deployment_receipt_digest": hex::encode(&version.deployment_receipt_digest),
        "source": source_json(version.source),
    })
}

fn source_json(status: SourceStatus) -> Value {
    match status {
        SourceStatus::Unpublished => json!({"status": "unpublished"}),
        SourceStatus::Verified {
            source_digest,
            environment_digest,
        } => json!({
            "status": "verified",
            "source_digest": hex::encode(&source_digest),
            "environment_digest": hex::encode(&environment_digest),
            "pipeline": programs_source_verification(),
        }),
        SourceStatus::Mismatch {
            expected,
            reproduced,
        } => json!({
            "status": "mismatch",
            "expected_code_hash": hex::encode(&expected),
            "reproduced_artifact_digest": hex::encode(&reproduced),
        }),
    }
}

fn lifecycle_json(receipt: &LifecycleReceipt) -> Value {
    let state_access = match receipt.wind_down.state_access {
        WindDownStateAccess::ReadOnly => "read-only",
    };
    json!({
        "prior": lifecycle_name(receipt.prior),
        "current": lifecycle_name(receipt.current),
        "authority": hex::encode(&receipt.authority),
        "effective_sequence": receipt.effective_sequence,
        "live_value_accounts": receipt.live_value_accounts,
        "wind_down": {
            "exit_program": hex::encode(&receipt.wind_down.exit_program),
            "deadline": receipt.wind_down.deadline,
            "state_access": state_access,
        },
    })
}

fn policy_json(policy: UpgradePolicy) -> Value {
    match policy {
        UpgradePolicy::Immutable => json!({"kind": "immutable"}),
        UpgradePolicy::Authority(authority) => {
            json!({"kind": "upgradeable", "authority": hex::encode(&authority)})
        }
    }
}

const fn lifecycle_name(lifecycle: ProgramLifecycle) -> &'static str {
    match lifecycle {
        ProgramLifecycle::Active => "active",
        ProgramLifecycle::Deprecated => "deprecated",
        ProgramLifecycle::Tombstoned => "tombstoned",
    }
}

fn program_id(text: &str) -> Option<ProgramId> {
    hex::decode_digest(text)
        .ok()
        .and_then(|bytes| ProgramId::new(bytes).ok())
}

fn source_request(body: &[u8]) -> Option<(String, [u8; 32])> {
    #[derive(Deserialize)]
    #[serde(deny_unknown_fields)]
    struct SourceRequest {
        source_uri: String,
        source_digest: String,
    }
    let document: SourceRequest = serde_json::from_slice(body).ok()?;
    let uri = document.source_uri;
    let digest = hex::decode_digest(&document.source_digest).ok()?;
    Some((uri, digest))
}

fn deployment_ingress_unavailable(_untrusted_body: &[u8]) -> Response {
    refusal(
        503,
        "deployment_proof_unavailable",
        "the authenticated node does not expose canonical deployment proof production",
    )
}

fn valid_idempotency_key(value: &str) -> bool {
    (16..=128).contains(&value.len())
        && value
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'-' | b'_'))
}

fn scoped_key(principal: &str, program: ProgramId, key: &str) -> String {
    let digest: [u8; 32] = Sha256::digest(
        [
            IDEMPOTENCY_DOMAIN,
            principal.as_bytes(),
            b"\0",
            &program.bytes(),
            b"\0",
            key.as_bytes(),
        ]
        .concat(),
    )
    .into();
    hex::encode(&digest)
}

fn settled(
    lease: &VerificationLease,
    record: &mut VerificationRecord,
    state: VerificationState,
    now: u64,
    response: Response,
) -> Response {
    match lease.settle(record, state, now) {
        Ok(()) => response,
        Err(refused) => journal_refusal(&refused),
    }
}

fn journal_refusal(refused: &JournalRefusal) -> Response {
    match refused {
        JournalRefusal::Corrupt(defect) => refusal(503, "idempotency_record_corrupt", defect),
        JournalRefusal::Unavailable(error) => refusal(503, "idempotency_store_unavailable", error),
    }
}

fn request_digest(deployment: &VerifiedDeploymentEvidence, uri: &str, source_digest: &[u8; 32]) -> [u8; 32] {
    let binding = deployment_provenance_json(deployment).to_string();
    Sha256::digest(
        [
            REQUEST_DOMAIN,
            binding.as_bytes(),
            b"\0",
            uri.as_bytes(),
            b"\0",
            source_digest,
        ]
        .concat(),
    )
    .into()
}

fn deployment_provenance(evidence: &VerifiedDeploymentEvidence) -> VerifiedSourceDeployment {
    VerifiedSourceDeployment {
        abi_version: evidence.abi_version(),
        code_hash: evidence.code_hash(),
        activity_id: evidence.activity_id(),
        receipt_digest: evidence.receipt_digest(),
        batch_header_digest: evidence.batch_header_digest(),
        state_root: evidence.state_root(),
        programs_root: evidence.programs_root(),
        observed_sequence: evidence.freshness().observed_sequence,
        observed_at: evidence.freshness().observed_at,
    }
}

fn deployment_provenance_json(evidence: &VerifiedDeploymentEvidence) -> Value {
    json!({
        "program_id": hex::encode(&evidence.program().bytes()),
        "version": evidence.version(),
        "abi_version": evidence.abi_version(),
        "code_hash": hex::encode(&evidence.code_hash()),
        "activity_id": hex::encode(&evidence.activity_id()),
        "receipt_digest": hex::encode(&evidence.receipt_digest()),
        "batch_header_digest": hex::encode(&evidence.batch_header_digest()),
        "state_root": hex::encode(&evidence.state_root()),
        "programs_root": hex::encode(&evidence.programs_root()),
        "observed_sequence": evidence.freshness().observed_sequence,
        "observed_at": evidence.freshness().observed_at,
    })
}

fn deployment_response(evidence: &layerx_programs::VerifiedDeploymentEvidence) -> Response {
    Response {
        status: 200,
        body: json!({
            "activity_id": hex::encode(&evidence.activity_id()),
            "receipt_digest": hex::encode(&evidence.receipt_digest()),
            "state": "deployed",
        })
        .to_string(),
    }
}

#[cfg(test)]
mod tests {
    use super::{deployment_ingress_unavailable, source_request};

    #[test]
    fn source_request_refuses_caller_deployment_relabeling() {
        let valid = serde_json::json!({
            "source_uri": "https://sources.example/program.tar",
            "source_digest": "a1".repeat(32),
        });
        assert!(source_request(valid.to_string().as_bytes()).is_some());
        for field in ["abi_version", "version", "program_id", "code_hash", "deployment_provenance", "deployment_receipt_digest"] {
            let mut altered = valid.clone();
            altered[field] = serde_json::json!(2);
            assert!(source_request(altered.to_string().as_bytes()).is_none(), "accepted {field}");
        }
    }

    #[test]
    fn source_request_refuses_duplicate_identity_fields() {
        let digest = "a1".repeat(32);
        let repeated = format!(
            r#"{{"source_uri":"https://sources.example/a","source_uri":"https://sources.example/b","source_digest":"{digest}"}}"#
        );
        assert!(source_request(repeated.as_bytes()).is_none());
        let repeated = format!(
            r#"{{"source_uri":"https://sources.example/a","source_digest":"{digest}","source_digest":"{digest}"}}"#
        );
        assert!(source_request(repeated.as_bytes()).is_none());
    }

    #[test]
    fn deployment_ingress_stays_blocked_for_forgeable_record_shape() {
        let forged = br#"{"record_hex":"4c6179657258"}"#;
        assert_eq!(deployment_ingress_unavailable(forged).status, 503);
    }

    #[test]
    fn deployment_ingress_does_not_accept_caller_proof_bytes() {
        let untrusted = br#"{"proof_hex":"00"}"#;
        assert_eq!(deployment_ingress_unavailable(untrusted).status, 503);
    }
}
