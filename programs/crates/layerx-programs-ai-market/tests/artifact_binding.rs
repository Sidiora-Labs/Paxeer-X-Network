//! AI.F09-T02 `SealEvidence` over the complete shared state value: one evaluator seal per
//! opened epoch inside the commit window, bound to the frozen policy, rubric, mode and the
//! explicit F01 task-set seal, plus owning-record root binding and the minimal evidence proof
//! conclusions. Every market is produced by the real F01/F02/F03/F06/F08 producers,
//! `OPEN_EPOCH` and the F01 task-set lifecycle.
use ed25519_dalek::{Signer, SigningKey};
use layerx_programs_ai_market::{
    admission::{
        admit_evaluator, Admission, AdmissionContext, AdmissionTable, ApprovalTerms,
        EvaluatorConsent, Participant, TABLE_MAX_BYTES,
    },
    aggregation_codec::{EpochAggregation, QualityStatus, WorkerAggregate},
    codec::{
        self, decode_envelope, derive_evaluator, derive_market, derive_task, derive_worker,
        encode_envelope, Envelope, ReportBody, ScoreVector,
    },
    dispatch::{self, Operation},
    epoch::{self, Frozen, Outcome as Opening, ADVANCE_SCRATCH_BYTES, OPEN_SCRATCH_BYTES},
    errors::{
        ApplicationError, CodecResult, ARITHMETIC, BAD_SIGNATURE, BAD_VERSION, CAPACITY, CONFLICT,
        EVIDENCE_BINDING, EXPIRED, F09_EVIDENCE_SEAL_CONFLICT, F09_EVIDENCE_TASK_SET_UNSEALED,
        KEY_MISMATCH, NON_CANONICAL, NOT_FOUND, REPLAY_CONFLICT, REVOKED, SEQUENCE_GAP,
        UNAUTHORIZED, UNKNOWN_OPERATION, WRONG_CONFIG, WRONG_EPOCH, WRONG_MARKET, WRONG_PHASE,
        WRONG_ROSTER,
    },
    evaluators::{
        authority::{
            self, split_identity_section, AuthorityContext, EvaluatorRecord, EvaluatorRegion,
            LastRequest,
        },
        model::{EvaluatorGrant, GrantTerms, RegisteredEvidence},
    },
    evidence::{
        self, bind_root, check_root_binding, chunk_count, encode_envelope as encode_publisher,
        encode_evidence_manifest, encode_manifest, evidence_root, manifest_root,
        object_content_root, publisher_digest, sealed_evidence, verify_evidence_proof,
        ArtifactContext, ArtifactError, ArtifactKind, ArtifactManifest, ArtifactManifestRoot,
        AssessmentMode, Conclusion, Conclusions, EvidenceManifest, EvidencePolicy, EvidenceProof,
        EvidenceSeal, EvidenceTask, Items, Outcome as Sealing, Privacy, ProvenTask,
        PublisherEnvelope, RootBind, SealRegion, SubjectContext, TerminalStatus, WorkerGroup,
        MAX_ENVELOPE_BYTES, MAX_EVIDENCE_BYTES, MAX_MANIFEST_BYTES, SEAL_PAYLOAD_BYTES,
        SEAL_RECORD_BYTES, SEAL_REGION_MAX_BYTES, SEAL_SCRATCH_BYTES,
    },
    policy::{PolicyCommitments, TaskPolicyV1, TASK_POLICY_BYTES},
    registry::{derive_rewards_account, MarketHeader, F01_SECTION_CAP},
    registry_ops::{self, CallContext, Outcome, PolicySection},
    reward_math::allocate,
    rewards::{
        decode_reward_state, FundReplay, FundRequest, FundingAuthority, FundingPhase, RewardLedger,
        RewardState, FUNDING_POLICY_VERSION, REWARD_STATE_BYTES,
    },
    state::{
        decode_shared_state, encode_shared_state, ActorSlot, Control, ReplayRequest, ReplayTable,
        Section, SharedState, ACTOR_SLOTS, REPLAY_RESULT_BYTES,
    },
    tasks::{self, Outcome as Task, SetBinding, TaskSet},
    types::{
        AccountId, AssetId, Authentication, ChainDomain, Digest32, EvaluatorBinding, EvaluatorId,
        EvidenceRoot, FrozenBinding, MarketId, MetadataDigest, PolicyDigest, Presence, PrincipalId,
        ProgramId, PublicKey32, RequestDigest, RequestId, ResultDigest, RosterDigest, RubricDigest,
        Score, ScoreEntry, Signature64, TaskId, Version, WorkerId, WorkerRosterEntry,
    },
    workers::{WorkerCurrent, WorkerState, WorkerTable, WORKER_TABLE_MAX_BYTES},
    MAX_EVENT_BYTES, MAX_STATE_BYTES,
};

type TestResult = CodecResult<()>;

const CHAIN: [u8; 32] = [10; 32];
const PROGRAM: [u8; 32] = [11; 32];
const OWNER: [u8; 32] = [12; 32];
const ASSET: [u8; 32] = [13; 32];
const REFUND: [u8; 32] = [14; 32];
const KEEPER: u8 = 0x7f;
const ORIGIN: u64 = 1000;
const METADATA: [u8; 32] = [0x44; 32];
const ROLE_EXPIRY: u64 = 5000;
const TOPIC: &[u8] = b"PAXAI/v1/SealEvidence";

fn principal(n: u8) -> CodecResult<PrincipalId> {
    let mut bytes = [0x50; 32];
    bytes[0] = n;
    PrincipalId::new(bytes)
}
fn version() -> CodecResult<Version> {
    Version::new(1)
}
fn rubric() -> CodecResult<RubricDigest> {
    RubricDigest::new([4; 32])
}
fn policy(config: u64, minimum_evaluators: u8) -> CodecResult<TaskPolicyV1> {
    let mut policy = TaskPolicyV1::bounded_default(
        config,
        1,
        PolicyCommitments {
            model_artifact: Digest32::new([1; 32])?,
            dataset_artifact: [2; 32],
            benchmark_suite: Digest32::new([3; 32])?,
            rubric: rubric()?,
            task_schema: Digest32::new([5; 32])?,
            result_schema: Digest32::new([6; 32])?,
            service_terms: Digest32::new([7; 32])?,
        },
        100,
        1,
    )?;
    policy.minimum_evaluator_count = minimum_evaluators;
    Ok(policy)
}
/// The ed25519 delegate of worker `n`.
fn delegate_key(n: u8) -> SigningKey {
    SigningKey::from_bytes(&[0x90 + n; 32])
}
/// The frozen signing key of evaluator `n`.
fn evaluator_key(n: u8) -> SigningKey {
    SigningKey::from_bytes(&[n; 32])
}
fn public(key: &SigningKey) -> PublicKey32 {
    PublicKey32(key.verifying_key().to_bytes())
}
/// The artifact service error space is independent of the protocol one; a fixture that fails
/// to build is reported as a non-canonical fixture.
fn artifact(_: ArtifactError) -> ApplicationError {
    NON_CANONICAL
}

fn encode(state: &SharedState<'_>) -> CodecResult<Vec<u8>> {
    let mut out = vec![0; state.encoded_len()?];
    let mut scratch = vec![0; Section::Control.payload_cap()];
    let n = encode_shared_state(state, &mut out, &mut scratch)?;
    out.truncate(n);
    Ok(out)
}

/// One request envelope; `delegate` is (claimed key, signer).
#[derive(Clone)]
struct Req {
    operation: Operation,
    actor: PrincipalId,
    epoch: u64,
    config: u64,
    roster: Presence<RosterDigest>,
    sequence: u64,
    request: [u8; 32],
    expiry: u64,
    market: Option<MarketId>,
    delegate: Option<(SigningKey, SigningKey)>,
    payload: Vec<u8>,
}
fn req(operation: Operation, actor: PrincipalId, payload: Vec<u8>) -> Req {
    Req {
        operation,
        actor,
        epoch: 0,
        config: 1,
        roster: Presence::Absent,
        sequence: 0,
        request: [1; 32],
        expiry: u64::MAX,
        market: None,
        delegate: None,
        payload,
    }
}
impl Req {
    fn encode(&self) -> CodecResult<Vec<u8>> {
        let chain = ChainDomain::new(CHAIN)?;
        let program = ProgramId::new(PROGRAM)?;
        let market = match self.market {
            Some(market) => market,
            None => derive_market(chain, program)?,
        };
        let mut envelope = Envelope {
            operation: self.operation,
            chain,
            program,
            market,
            actor: self.actor,
            epoch: self.epoch,
            config: self.config,
            roster: self.roster,
            sequence: self.sequence,
            expiry: self.expiry,
            request: RequestId::new(self.request)?,
            payload: &self.payload,
            authentication: Authentication::Native,
        };
        let mut out = vec![0; 32_768];
        if let Some((claimed, signer)) = &self.delegate {
            envelope.authentication = Authentication::Delegate {
                key: public(claimed),
                signature: Signature64([0; 64]),
            };
            let n = encode_envelope(&envelope, &mut out)?;
            let digest = decode_envelope(&out[..n])?.request_digest()?;
            envelope.authentication = Authentication::Delegate {
                key: public(claimed),
                signature: Signature64(signer.sign(digest.as_bytes()).to_bytes()),
            };
        }
        let n = encode_envelope(&envelope, &mut out)?;
        out.truncate(n);
        Ok(out)
    }
    fn context(&self, at: u64) -> CodecResult<CallContext> {
        Ok(CallContext {
            chain: ChainDomain::new(CHAIN)?,
            program: ProgramId::new(PROGRAM)?,
            principal: self.actor,
            height: at,
        })
    }
}

/// Committed state after the real F01 CREATE at `origin` (lifecycle REGISTERED).
fn create(origin: u64) -> CodecResult<Vec<u8>> {
    let program = ProgramId::new(PROGRAM)?;
    let mut policy_bytes = [0; TASK_POLICY_BYTES];
    policy(1, 3)?.encode(&mut policy_bytes)?;
    let mut payload = OWNER.to_vec();
    payload.extend_from_slice(&ASSET);
    payload.extend_from_slice(derive_rewards_account(program, AssetId::new(ASSET)?)?.as_bytes());
    payload.extend_from_slice(&REFUND);
    payload.push(0);
    payload.extend_from_slice(&policy_bytes);
    payload.extend_from_slice(&[16; 32]);
    let call = Req {
        sequence: 1,
        ..req(dispatch::CREATE, PrincipalId::new(OWNER)?, payload)
    };
    let encoded = call.encode()?;
    let mut section = vec![0; F01_SECTION_CAP];
    let mut event = vec![0; MAX_EVENT_BYTES];
    let Outcome::Applied { state, .. } = registry_ops::apply(
        &call.context(origin)?,
        None,
        &decode_envelope(&encoded)?,
        &mut section,
        &mut event,
    )?
    else {
        return Err(NON_CANONICAL);
    };
    encode(&state)
}

/// Owned, decoded sections of one committed shared state value, control feature bytes
/// included.
struct Parts {
    revision: u64,
    policy: Vec<u8>,
    workers: WorkerTable,
    region: Vec<u8>,
    reports: Vec<u8>,
    rewards: Vec<u8>,
    admission: AdmissionTable,
    replay: ReplayTable,
    features: Vec<u8>,
}
impl Parts {
    fn load(bytes: &[u8]) -> CodecResult<Self> {
        let state = decode_shared_state(bytes)?;
        let [policy, identity, reports, rewards, admission] = state.feature_sections;
        let (workers, region) = split_identity_section(identity)?;
        Ok(Self {
            revision: state.revision,
            policy: policy.to_vec(),
            workers: WorkerTable::decode(workers)?,
            region: region.to_vec(),
            reports: reports.to_vec(),
            rewards: rewards.to_vec(),
            admission: AdmissionTable::decode(admission)?,
            replay: state.control.replay,
            features: state.control.feature_bytes.to_vec(),
        })
    }
    fn market(&self) -> CodecResult<MarketHeader> {
        Ok(PolicySection::decode(&self.policy)?.header)
    }
    /// Commits at `self.revision`, keeping the F01 header revision equal to it.
    fn encode(&self) -> CodecResult<Vec<u8>> {
        let mut section = PolicySection::decode(&self.policy)?;
        section.header.state_revision = self.revision;
        let mut policy = vec![0; section.encoded_len()?];
        section.encode(&mut policy)?;
        let mut identity = vec![0; WORKER_TABLE_MAX_BYTES];
        let workers_len = self.workers.encode(&mut identity)?;
        identity.truncate(workers_len);
        identity.extend_from_slice(&self.region);
        let mut admission = vec![0; TABLE_MAX_BYTES];
        let admission_len = self.admission.encode(&mut admission)?;
        encode(&SharedState {
            revision: self.revision,
            feature_sections: [
                &policy,
                &identity,
                &self.reports,
                &self.rewards,
                &admission[..admission_len],
            ],
            control: Control {
                replay: self.replay.clone(),
                feature_bytes: &self.features,
            },
        })
    }
    fn insert_grant(&mut self, grant: EvaluatorGrant, n: u8) -> TestResult {
        let mut region = EvaluatorRegion::decode(&self.region)?;
        region.insert(&EvaluatorRecord {
            grant,
            last: LastRequest {
                sequence: 1,
                request: RequestId::new([n; 32])?,
                digest: RequestDigest::new([0x33; 32])?,
                result: ResultDigest::new([0x34; 32])?,
            },
            rekey: None,
            revocation: None,
        })?;
        let mut out = vec![0; region.encoded_len()];
        let len = region.encode(&mut out)?;
        out.truncate(len);
        self.region = out;
        Ok(())
    }
}

fn ctx(market: &MarketHeader, who: PrincipalId, at: u64) -> AdmissionContext<'_> {
    AdmissionContext {
        market,
        invoking_principal: who,
        height: at,
    }
}
/// Market-owner approval bound to the required effective epoch, expiring at its work end.
fn approve(
    table: &mut AdmissionTable,
    market: &MarketHeader,
    participant: Participant,
    owner: PrincipalId,
    delegate: PublicKey32,
    at: u64,
) -> CodecResult<(u64, Digest32)> {
    let effective = table.required_effective_epoch(&ctx(market, owner, at))?;
    let terms = ApprovalTerms {
        participant,
        owner,
        enrollment_nonce_commitment: Digest32::new([7; 32])?,
        delegate,
        delegate_generation: 1,
        identity_commitment: Digest32::new([9; 32])?,
        effective_epoch: effective,
        config_version: 1,
        request: RequestId::new(participant.bytes())?,
        expiry_height: market.origin_height + effective * 128 + 64,
    };
    let digest = table.approve(&ctx(market, market.owner_principal, at), &terms)?;
    Ok((effective, digest))
}

fn worker_record(
    worker: WorkerId,
    owner: PrincipalId,
    delegate: PublicKey32,
    slot: u8,
    at: u64,
) -> CodecResult<WorkerCurrent> {
    Ok(WorkerCurrent {
        worker,
        owner,
        delegate,
        metadata: MetadataDigest::new(METADATA)?,
        generation: 1,
        key_version: 1,
        metadata_revision: 1,
        valid_from: at,
        expiry: at + 4096,
        revocation_sequence: 0,
        effective_epoch: 0,
        last_sequence: 0,
        last_request_id: [0; 32],
        last_request_digest: [0; 32],
        last_result_digest: [0; 32],
        state: WorkerState::Enrolled,
        slot,
        last_metadata_height: at,
    })
}

fn grant(
    market: &MarketHeader,
    owner: PrincipalId,
    nonce: u8,
    signing_key: PublicKey32,
    effective: u64,
) -> CodecResult<EvaluatorGrant> {
    EvaluatorGrant::nominate(
        market.market_id,
        owner,
        [nonce; 32],
        GrantTerms {
            rubric: rubric()?,
            grant_version: version()?,
            key_version: version()?,
            signing_key,
            effective_epoch: effective,
            expiry_epoch_exclusive: effective + 32,
        },
    )
}

/// The evaluator replay slot bound to `owner`.
fn evaluator_slot(replay: &ReplayTable, owner: PrincipalId) -> CodecResult<ActorSlot> {
    for index in 0..8 {
        let slot = ActorSlot::evaluator(index)?;
        if replay
            .actor(slot)
            .is_some_and(|actor| actor.principal == owner)
        {
            return Ok(slot);
        }
    }
    Err(NOT_FOUND)
}

/// One market's committed shared state bytes and its next owner sequence.
struct World {
    bytes: Vec<u8>,
    owner_sequence: u64,
}
impl World {
    fn create(origin: u64) -> CodecResult<Self> {
        Ok(Self {
            bytes: create(origin)?,
            owner_sequence: 2,
        })
    }
    fn parts(&self) -> CodecResult<Parts> {
        Parts::load(&self.bytes)
    }
    fn revision(&self) -> CodecResult<u64> {
        Ok(decode_shared_state(&self.bytes)?.revision)
    }
    fn section(&self) -> CodecResult<PolicySection<'_>> {
        PolicySection::decode(
            decode_shared_state(&self.bytes)?.feature_sections[Section::PolicyLifecycle.index()],
        )
    }
    fn features(&self) -> CodecResult<Vec<u8>> {
        Ok(decode_shared_state(&self.bytes)?
            .control
            .feature_bytes
            .to_vec())
    }
    /// Applies `change` to the decoded sections and commits it at the next revision.
    fn edit<T>(
        &mut self,
        change: impl FnOnce(&mut Parts, &MarketHeader) -> CodecResult<T>,
    ) -> CodecResult<T> {
        let mut parts = self.parts()?;
        let market = parts.market()?;
        let out = change(&mut parts, &market)?;
        parts.revision += 1;
        self.bytes = parts.encode()?;
        Ok(out)
    }
    /// A real owner-authorized F01 registry operation (the owner role sequence advances).
    fn owner_op(&mut self, operation: Operation, payload: Vec<u8>, at: u64) -> TestResult {
        let call = Req {
            config: self.section()?.header.active_config_version,
            sequence: self.owner_sequence,
            request: [0x20 + u8::try_from(self.owner_sequence).map_err(|_| ARITHMETIC)?; 32],
            ..req(operation, PrincipalId::new(OWNER)?, payload)
        };
        let encoded = call.encode()?;
        let current = decode_shared_state(&self.bytes)?;
        let mut section = vec![0; F01_SECTION_CAP];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let Outcome::Applied { state, .. } = registry_ops::apply(
            &call.context(at)?,
            Some(&current),
            &decode_envelope(&encoded)?,
            &mut section,
            &mut event,
        )?
        else {
            return Err(NON_CANONICAL);
        };
        let next = encode(&state)?;
        self.bytes = next;
        self.owner_sequence += 1;
        Ok(())
    }
    fn schedule(&mut self, epoch: u64, at: u64) -> TestResult {
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(&epoch.to_be_bytes());
        self.owner_op(dispatch::SCHEDULE_ACTIVATION, payload, at)
    }
}

/// Worker, evaluator, funding and settlement producers of the market journey.
impl World {
    /// F02 ENROLLED record with an ed25519 delegate, worker replay slot and F08 approval
    /// plus owner acceptance.
    fn enroll(&mut self, n: u8, at: u64) -> CodecResult<WorkerRosterEntry> {
        self.edit(|parts, market| {
            let owner = principal(n)?;
            let worker = derive_worker(market.market_id, owner, [n; 32])?;
            let slot = parts.workers.free_slot()?;
            let record = worker_record(worker, owner, public(&delegate_key(n)), slot, at)?;
            parts.workers.insert(&record)?;
            parts
                .replay
                .bind(ActorSlot::worker(usize::from(slot))?, owner, version()?)?;
            let participant = Participant::Worker(worker);
            let (effective, digest) = approve(
                &mut parts.admission,
                market,
                participant,
                owner,
                record.delegate,
                at,
            )?;
            parts.admission.admit(
                &ctx(market, owner, at),
                &Admission {
                    participant,
                    delegate_generation: 1,
                    effective_epoch: effective,
                    config_version: 1,
                    approval_digest: digest,
                },
            )?;
            Ok(WorkerRosterEntry {
                worker,
                owner,
                recipient: AccountId::new(owner.bytes())?,
                generation: version()?,
                key_version: version()?,
                public_key: record.delegate,
                metadata: record.metadata,
            })
        })
    }
    /// F03 nomination accepted through the signed F08 evaluator consent of its delegate; the
    /// evaluator replay slot `n - 2` is bound to its owner (no producer binds it yet).
    fn evaluator(&mut self, n: u8, at: u64) -> TestResult {
        self.edit(|parts, market| {
            let owner = principal(n)?;
            let key = evaluator_key(n);
            let signing_key = public(&key);
            let participant =
                Participant::Evaluator(derive_evaluator(market.market_id, owner, [n; 32])?);
            let (effective, digest) = approve(
                &mut parts.admission,
                market,
                participant,
                owner,
                signing_key,
                at,
            )?;
            let grant = grant(market, owner, n, signing_key, effective)?;
            let consent = EvaluatorConsent {
                chain: market.deployment_chain_domain,
                program: market.program_id,
                market: market.market_id,
                evaluator: grant.evaluator,
                owner,
                signing_key,
                enrollment_nonce: [n; 32],
                rubric: grant.rubric,
                approval_digest: digest,
                request: RequestId::new([n; 32])?,
                grant_version: 1,
                key_version: 1,
                effective_epoch: effective,
                config_version: 1,
                expiry_height: market.origin_height + effective * 128 + 64,
            };
            let mut signed = [0u8; 362];
            consent.encode(&mut signed)?;
            let mut payload = signed.to_vec();
            payload.extend_from_slice(&key.sign(consent.digest()?.as_bytes()).to_bytes());
            admit_evaluator(
                &mut parts.admission,
                &ctx(market, owner, at),
                &grant,
                consent.request,
                &payload,
            )?;
            parts
                .replay
                .bind(ActorSlot::evaluator(usize::from(n - 2))?, owner, version()?)?;
            parts.insert_grant(grant, n)
        })
    }
    /// Real owner FUND of `amount` into the F06 reward state.
    fn fund(&mut self, amount: u128, at: u64) -> TestResult {
        let mut parts = self.parts()?;
        let market = parts.market()?;
        if parts.rewards.is_empty() {
            let ledger = RewardLedger::new(
                market.funding_asset,
                market.rewards_account,
                market.refund_recipient_account,
            )?;
            let mut initial = vec![0; REWARD_STATE_BYTES];
            RewardState::init(&ledger, &mut initial)?;
            parts.rewards = initial;
        }
        let tag = 0x40 + u8::try_from(self.owner_sequence).map_err(|_| ARITHMETIC)?;
        let request = ReplayRequest {
            slot: ActorSlot::OWNER,
            principal: market.owner_principal,
            authority_version: version()?,
            sequence: self.owner_sequence,
            request_id: RequestId::new([tag; 32])?,
            digest: RequestDigest::new([tag; 32])?,
            expiry_height: at + 1000,
        };
        let mut funded = vec![0; REWARD_STATE_BYTES];
        decode_reward_state(&parts.rewards)?.fund(
            &FundingAuthority {
                owner: market.owner_principal,
                treasury: Presence::Absent,
            },
            FundingPhase::Accepting,
            &FundRequest {
                amount,
                refund_recipient: market.refund_recipient_account,
                policy_version: FUNDING_POLICY_VERSION,
                consent: true,
            },
            &mut FundReplay {
                table: &mut parts.replay,
                request: &request,
                height: at,
                revision: &mut parts.revision,
                result: ResultDigest::new([tag; 32])?,
            },
            &mut funded,
        )?;
        parts.rewards = funded;
        self.bytes = parts.encode()?;
        self.owner_sequence += 1;
        Ok(())
    }
    /// F05 terminal result for the single frozen worker, then `TerminalizeRewards`.
    fn terminalize(&mut self, frozen: &Frozen, entry: &WorkerRosterEntry, at: u64) -> TestResult {
        self.edit(|parts, market| {
            let binding = FrozenBinding {
                chain: market.deployment_chain_domain,
                program: market.program_id,
                market: market.market_id,
                epoch: frozen.epoch,
                config: frozen.config,
                roster: frozen.roster,
            };
            let roster = [*entry];
            let outputs = [WorkerAggregate::new(
                entry.worker,
                entry.generation,
                3,
                QualityStatus::ScoredPositive,
                5,
                5,
            )?];
            let allocation = allocate(frozen.budget, &outputs)?;
            let aggregation =
                EpochAggregation::structural(binding, Digest32::new([5; 32])?, &roster, &outputs)?;
            let mut terminal = vec![0; REWARD_STATE_BYTES];
            decode_reward_state(&parts.rewards)?.terminalize(
                &binding,
                aggregation.root(),
                &allocation,
                &roster,
                at,
                &mut terminal,
            )?;
            parts.rewards = terminal;
            Ok(())
        })
    }
}

/// The `OPEN_EPOCH`, `ADVANCE_ACTIVATION`, task-set and `SealEvidence` calls.
impl World {
    /// Permissionless object-local `OPEN_EPOCH` of the clock epoch of `at`.
    fn open(&mut self, at: u64) -> CodecResult<Frozen> {
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; OPEN_SCRATCH_BYTES];
        let preview = epoch::preview_open(&self.bytes, at, &mut next, &mut scratch)?;
        let call = Req {
            epoch: preview.epoch,
            config: preview.config.get(),
            roster: Presence::Present(preview.roster),
            request: [0x60 + u8::try_from(preview.epoch).map_err(|_| ARITHMETIC)?; 32],
            ..req(dispatch::OPEN_EPOCH, principal(KEEPER)?, Vec::new())
        };
        let encoded = call.encode()?;
        let mut event = vec![0; MAX_EVENT_BYTES];
        let Opening::Opened {
            frozen, state_len, ..
        } = epoch::open_epoch(
            &call.context(at)?,
            &decode_envelope(&encoded)?,
            &self.bytes,
            &mut next,
            &mut scratch,
            &mut event,
        )?
        else {
            return Err(NON_CANONICAL);
        };
        next.truncate(state_len);
        self.bytes = next;
        Ok(frozen)
    }
    /// Permissionless object-local `ADVANCE_ACTIVATION`; the lifecycle becomes ACTIVE.
    fn advance(&mut self, at: u64) -> TestResult {
        let section = self.section()?;
        let mut payload = self.revision()?.to_be_bytes().to_vec();
        payload.extend_from_slice(&section.header.activation_epoch.to_be_bytes());
        let call = Req {
            config: section.header.active_config_version,
            request: [0x5f; 32],
            ..req(dispatch::ADVANCE_ACTIVATION, principal(KEEPER)?, payload)
        };
        let encoded = call.encode()?;
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; ADVANCE_SCRATCH_BYTES];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let activated = epoch::advance_activation(
            &call.context(at)?,
            &decode_envelope(&encoded)?,
            &self.bytes,
            &mut next,
            &mut scratch,
            &mut event,
        )?;
        next.truncate(activated.state_len);
        self.bytes = next;
        Ok(())
    }
    /// One F01 task-set call, committed only on `Applied`.
    fn task(&mut self, call: &Req, at: u64) -> CodecResult<Task> {
        let encoded = call.encode()?;
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; tasks::SCRATCH_BYTES];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let outcome = tasks::apply(
            &call.context(at)?,
            &decode_envelope(&encoded)?,
            &self.bytes,
            &mut next,
            &mut scratch,
            &mut event,
        )?;
        if let Task::Applied { state_len, .. } = outcome {
            next.truncate(state_len);
            self.bytes = next;
        }
        Ok(outcome)
    }
    /// One `SealEvidence` composed over the committed bytes into caller buffers; nothing is
    /// committed.
    fn compose(
        &self,
        call: &Req,
        at: u64,
        next: &mut [u8],
        event: &mut [u8],
    ) -> CodecResult<Sealing> {
        let encoded = call.encode()?;
        let mut scratch = vec![0; SEAL_SCRATCH_BYTES];
        evidence::apply(
            &call.context(at)?,
            &decode_envelope(&encoded)?,
            &self.bytes,
            next,
            &mut scratch,
            event,
        )
    }
    /// One `SealEvidence`, committed only on `Applied`. A refusal writes no event; an
    /// application is checked against the committed bytes it replaces.
    fn evidence(&mut self, call: &Req, at: u64) -> CodecResult<Sealing> {
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let outcome = self.compose(call, at, &mut next, &mut event);
        let Ok(Sealing::Applied {
            seal,
            revision,
            result,
            state_len,
            event_len,
        }) = outcome
        else {
            assert!(event.iter().all(|b| *b == 0));
            return outcome;
        };
        let previous = core::mem::replace(&mut self.bytes, next[..state_len].to_vec());
        self.check_applied(&previous, call, &event[..event_len], &seal)?;
        assert_eq!(self.revision()?, revision);
        assert_eq!(result, codec::result_digest(&seal.encode()?)?);
        outcome
    }
    /// One revision and one evaluator sequence; the F01 section changes only its header
    /// revision, the seal region its record, and every other section is byte-identical.
    fn check_applied(
        &self,
        previous: &[u8],
        call: &Req,
        event: &[u8],
        seal: &EvidenceSeal,
    ) -> TestResult {
        let before = decode_shared_state(previous)?;
        let after = decode_shared_state(&self.bytes)?;
        let revision = before.revision + 1;
        assert_eq!(after.revision, revision);
        let mut section = PolicySection::decode(before.feature_sections[0])?;
        section.header.state_revision = revision;
        let mut policy = vec![0; section.encoded_len()?];
        section.encode(&mut policy)?;
        assert_eq!(after.feature_sections[0], policy.as_slice());
        assert_eq!(after.feature_sections[1..], before.feature_sections[1..]);
        let record = seal.encode()?;
        let result = codec::result_digest(&record)?;
        let slot = evaluator_slot(&after.control.replay, call.actor)?;
        let retained = after
            .control
            .replay
            .actor(slot)
            .and_then(|actor| actor.last)
            .ok_or(NON_CANONICAL)?;
        assert_eq!(
            (
                retained.sequence,
                retained.applied_revision,
                retained.result_digest
            ),
            (call.sequence, revision, result)
        );
        let region = SealRegion::decode(after.control.feature_bytes)?;
        assert_eq!(region.get(call.epoch, seal.evaluator)?, Some(*seal));
        assert_eq!(
            region.rest(),
            SealRegion::decode(before.control.feature_bytes)?.rest()
        );
        let (operation, common, suffix) = codec::decode_event_frame(TOPIC, event)?;
        assert_eq!(
            (
                operation,
                common.epoch,
                common.config.get(),
                common.revision
            ),
            (dispatch::SealEvidence, call.epoch, call.config, revision)
        );
        assert_eq!(
            (common.request, common.result),
            (decode_envelope(&call.encode()?)?.request_digest()?, result)
        );
        assert_eq!(suffix, record.as_slice());
        Ok(())
    }
}

/// A market with an opened epoch (epoch 1: Work [1128, 1192), Commit [1192, 1208)).
struct Market {
    world: World,
    frozen: Frozen,
    header: MarketHeader,
    workers: Vec<WorkerRosterEntry>,
}
/// Enrolls worker 1 and evaluators 2..=4, funds, schedules activation at epoch 1, opens
/// epoch 1 at 1128 and advances the lifecycle to ACTIVE at 1129.
fn opened() -> CodecResult<Market> {
    let mut world = World::create(ORIGIN)?;
    let entry = world.enroll(1, ORIGIN + 2)?;
    for n in 2..=4 {
        world.evaluator(n, ORIGIN + 2 + u64::from(n))?;
    }
    world.schedule(1, 1006)?;
    world.fund(500, 1007)?;
    let frozen = world.open(1128)?;
    assert_eq!((frozen.epoch, frozen.config.get()), (1, 1));
    world.advance(1129)?;
    let header = world.parts()?.market()?;
    Ok(Market {
        world,
        frozen,
        header,
        workers: vec![entry],
    })
}
/// `opened` with one task admitted at 1140 and the task set sealed at 1192.
fn sealed() -> CodecResult<Market> {
    let mut m = opened()?;
    m.admit(0x21, 1, 1172, 1140)?;
    let digest = m.set_digest()?;
    m.seal_set(digest, 1192)?;
    Ok(m)
}

/// A `SealEvidence` payload.
#[derive(Clone, Copy)]
struct Claim {
    version: u16,
    root: [u8; 32],
    policy: [u8; 32],
    set: [u8; 32],
    rubric: [u8; 32],
    mode: u8,
}
impl Claim {
    fn payload(&self) -> Vec<u8> {
        let mut out = self.version.to_be_bytes().to_vec();
        out.extend_from_slice(&self.root);
        out.extend_from_slice(&self.policy);
        out.extend_from_slice(&self.set);
        out.extend_from_slice(&self.rubric);
        out.push(self.mode);
        out
    }
}

fn evidence_request(n: u8, sequence: u64) -> [u8; 32] {
    let mut request = [0xc5; 32];
    request[0] = n;
    request[1..9].copy_from_slice(&sequence.to_be_bytes());
    request
}

impl Market {
    fn binding(&self) -> SetBinding {
        SetBinding {
            market: self.header.market_id,
            epoch: self.frozen.epoch,
            config: self.frozen.config,
            policy: self.frozen.policy,
            roster: self.frozen.roster,
        }
    }
    fn frozen_binding(&self) -> FrozenBinding {
        FrozenBinding {
            chain: self.header.deployment_chain_domain,
            program: self.header.program_id,
            market: self.header.market_id,
            epoch: self.frozen.epoch,
            config: self.frozen.config,
            roster: self.frozen.roster,
        }
    }
    /// An envelope bound to the opened epoch.
    fn bound(&self, operation: Operation, actor: PrincipalId, payload: Vec<u8>) -> Req {
        Req {
            epoch: self.frozen.epoch,
            config: self.frozen.config.get(),
            roster: Presence::Present(self.frozen.roster),
            ..req(operation, actor, payload)
        }
    }
    fn evaluator(&self, n: u8) -> CodecResult<EvaluatorId> {
        derive_evaluator(self.header.market_id, principal(n)?, [n; 32])
    }
    fn task_id(&self, requester: u8, nonce: u8) -> CodecResult<TaskId> {
        derive_task(
            self.header.market_id,
            self.frozen.epoch,
            principal(requester)?,
            [nonce; 32],
        )
    }
    fn region(&self) -> CodecResult<Vec<u8>> {
        Ok(self.world.section()?.task_region.to_vec())
    }
    /// The R044 digest of the current task region under the opened epoch.
    fn set_digest(&self) -> CodecResult<Digest32> {
        tasks::task_set_digest(&self.binding(), &TaskSet::decode(&self.region()?)?)
    }
    /// The explicit F01 task-set seal of the opened epoch.
    fn sealed_set(&self) -> CodecResult<Digest32> {
        tasks::sealed_task_set(&decode_shared_state(&self.world.bytes)?, self.frozen.epoch)
    }
    /// `ADMIT_TASK` of worker 1 by `requester`.
    fn admit(&mut self, requester: u8, nonce: u8, deadline: u64, at: u64) -> CodecResult<Task> {
        let mut payload = self.frozen.epoch.to_be_bytes().to_vec();
        payload.extend_from_slice(&self.frozen.config.get().to_be_bytes());
        payload.extend_from_slice(self.frozen.policy.as_bytes());
        payload.extend_from_slice(self.frozen.roster.as_bytes());
        payload.extend_from_slice(principal(requester)?.as_bytes());
        payload.extend_from_slice(self.workers[0].worker.as_bytes());
        payload.extend_from_slice(&METADATA);
        payload.extend_from_slice(&[nonce; 32]);
        payload.extend_from_slice(&[0xa0 + nonce; 32]);
        payload.extend_from_slice(&deadline.to_be_bytes());
        let call = Req {
            request: [0xb0 + nonce; 32],
            ..self.bound(dispatch::ADMIT_TASK, principal(requester)?, payload)
        };
        self.world.task(&call, at)
    }
    /// Keeper `SEAL_TASK_SET` of the opened epoch.
    fn seal_set(&mut self, digest: Digest32, at: u64) -> CodecResult<Task> {
        let mut payload = self.frozen.epoch.to_be_bytes().to_vec();
        payload.extend_from_slice(&self.frozen.config.get().to_be_bytes());
        payload.extend_from_slice(digest.as_bytes());
        let call = self.bound(dispatch::SEAL_TASK_SET, principal(KEEPER)?, payload);
        self.world.task(&call, at)
    }
    /// The claim the frozen policy and the sealed task set admit, with root `[root; 32]`.
    fn claim(&self, root: u8) -> CodecResult<Claim> {
        Ok(Claim {
            version: 1,
            root: [root; 32],
            policy: self.frozen.policy.bytes(),
            set: self.sealed_set()?.bytes(),
            rubric: rubric()?.bytes(),
            mode: 1,
        })
    }
    /// Native `SealEvidence` of evaluator `n` under its evaluator sequence.
    fn seal_call(&self, claim: &Claim, n: u8, sequence: u64) -> CodecResult<Req> {
        Ok(Req {
            sequence,
            request: evidence_request(n, sequence),
            expiry: ROLE_EXPIRY,
            ..self.bound(dispatch::SealEvidence, principal(n)?, claim.payload())
        })
    }
    fn evidence(&mut self, claim: &Claim, n: u8, sequence: u64, at: u64) -> CodecResult<Sealing> {
        let call = self.seal_call(claim, n, sequence)?;
        self.world.evidence(&call, at)
    }
    fn evidence_with(
        &mut self,
        claim: &Claim,
        n: u8,
        at: u64,
        edit: impl FnOnce(Req) -> Req,
    ) -> CodecResult<Sealing> {
        let call = edit(self.seal_call(claim, n, 1)?);
        self.world.evidence(&call, at)
    }
    /// The registration report admission reads for evaluator `n`.
    fn registered(&self, n: u8) -> CodecResult<Presence<RegisteredEvidence>> {
        sealed_evidence(
            &decode_shared_state(&self.world.bytes)?,
            self.frozen.epoch,
            self.evaluator(n)?,
        )
    }
    /// Real F03 `RevokeEvaluator` of evaluator `n` by the market owner. F03 advances only the
    /// shared revision, so the F01 header revision is re-bound to it.
    fn revoke(&mut self, n: u8, at: u64) -> TestResult {
        let mut payload = self.evaluator(n)?.as_bytes().to_vec();
        payload.extend_from_slice(&1u64.to_be_bytes());
        payload.extend_from_slice(&1u16.to_be_bytes());
        payload.extend_from_slice(&[0x5e; 32]);
        let sequence = self.world.owner_sequence;
        let call = Req {
            sequence,
            request: [0x30 + u8::try_from(sequence).map_err(|_| ARITHMETIC)?; 32],
            ..self.bound(dispatch::RevokeEvaluator, PrincipalId::new(OWNER)?, payload)
        };
        let market = self.world.parts()?.market()?;
        let mut scratch = vec![0; authority::SCRATCH_BYTES];
        let mut out = vec![0; MAX_STATE_BYTES];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let authority::Outcome::Applied { state_len, .. } = authority::apply(
            &self.world.bytes,
            &AuthorityContext {
                market: &market,
                invoking_principal: PrincipalId::new(OWNER)?,
                immediate_caller: Presence::Absent,
                height: at,
                approved_rubric: rubric()?,
                aggregate_sealed: false,
            },
            &call.encode()?,
            &mut scratch,
            &mut out,
            &mut event,
        )?
        else {
            return Err(NON_CANONICAL);
        };
        self.world.bytes = Parts::load(&out[..state_len])?.encode()?;
        self.world.owner_sequence += 1;
        Ok(())
    }
}

#[test]
fn a23_one_seal_per_evaluator_in_the_commit_window() -> TestResult {
    let mut m = sealed()?;
    let claim = m.claim(0xe1)?;
    let before = m.world.bytes.clone();
    assert_eq!(m.evidence(&claim, 2, 1, 1191), Err(WRONG_PHASE));
    assert_eq!(m.evidence(&claim, 2, 1, 1208), Err(WRONG_PHASE));
    assert_eq!(m.world.bytes, before);
    assert_eq!(m.registered(2)?, Presence::Absent);
    let revision = m.world.revision()?;
    let Sealing::Applied { seal: third, .. } = m.evidence(&m.claim(0xe3)?, 3, 1, 1192)? else {
        return Err(NON_CANONICAL);
    };
    let Sealing::Applied { seal, .. } = m.evidence(&claim, 2, 1, 1207)? else {
        return Err(NON_CANONICAL);
    };
    assert_eq!(m.world.revision()?, revision + 2);
    let evaluator = m.evaluator(2)?;
    let expected = EvidenceSeal {
        evaluator,
        grant: version()?,
        key_version: version()?,
        root: EvidenceRoot::new([0xe1; 32])?,
        task_policy: m.frozen.policy,
        task_set: m.sealed_set()?,
        rubric: rubric()?,
        mode: AssessmentMode::Objective,
        height: 1207,
    };
    assert_eq!(seal, expected);
    let record = expected.encode()?;
    let mut bytes = evaluator.as_bytes().to_vec();
    bytes.extend_from_slice(&1u64.to_be_bytes());
    bytes.extend_from_slice(&1u64.to_be_bytes());
    bytes.extend_from_slice(&[0xe1; 32]);
    bytes.extend_from_slice(m.frozen.policy.as_bytes());
    bytes.extend_from_slice(m.sealed_set()?.as_bytes());
    bytes.extend_from_slice(&[4; 32]);
    bytes.push(1);
    bytes.extend_from_slice(&1207u64.to_be_bytes());
    assert_eq!(record.as_slice(), bytes.as_slice());
    assert_eq!(EvidenceSeal::decode(&record)?, expected);
    let mut ordered = [(evaluator, record), (third.evaluator, third.encode()?)];
    ordered.sort_unstable_by_key(|(id, _)| *id);
    let mut region = 1u64.to_be_bytes().to_vec();
    region.push(2);
    for (_, encoded) in &ordered {
        region.extend_from_slice(encoded);
    }
    assert_eq!(m.world.features()?, region);
    let frozen = m.frozen_binding();
    assert_eq!(
        m.registered(2)?,
        Presence::Present(RegisteredEvidence {
            binding: EvaluatorBinding {
                frozen,
                evaluator,
                grant: version()?,
                key_version: version()?,
            },
            root: EvidenceRoot::new([0xe1; 32])?,
            rubric: rubric()?,
        })
    );
    assert_eq!(
        m.registered(3)?,
        Presence::Present(third.registered(frozen))
    );
    assert_eq!(m.registered(4)?, Presence::Absent);
    let state = decode_shared_state(&m.world.bytes)?;
    assert_eq!(sealed_evidence(&state, 2, evaluator), Err(WRONG_EPOCH));
    Ok(())
}

#[test]
fn a23_repeat_retry_and_conflict_leave_the_seal() -> TestResult {
    let mut m = sealed()?;
    let claim = m.claim(0xe1)?;
    let Sealing::Applied {
        seal,
        revision,
        result,
        ..
    } = m.evidence(&claim, 2, 1, 1192)?
    else {
        return Err(NON_CANONICAL);
    };
    let bytes = m.world.bytes.clone();
    for at in [1193, 1300] {
        let Sealing::Retained(retained) = m.evidence(&claim, 2, 1, at)? else {
            return Err(NON_CANONICAL);
        };
        assert_eq!(
            (
                retained.sequence,
                retained.applied_revision,
                retained.result_digest
            ),
            (1, revision, result)
        );
    }
    assert_eq!(
        m.evidence(&claim, 2, 2, 1200)?,
        Sealing::AlreadyApplied { seal }
    );
    let other = Claim {
        root: [0xe2; 32],
        ..claim
    };
    assert_eq!(
        m.evidence(&other, 2, 2, 1200),
        Err(F09_EVIDENCE_SEAL_CONFLICT)
    );
    assert_eq!(
        m.evidence_with(&other, 2, 1200, |call| Req {
            request: [0xc6; 32],
            ..call
        }),
        Err(REPLAY_CONFLICT)
    );
    assert_eq!(m.evidence(&claim, 2, 3, 1200), Err(SEQUENCE_GAP));
    assert_eq!(m.evidence(&other, 2, 2, 1208), Err(WRONG_PHASE));
    assert_eq!(m.world.bytes, bytes);
    assert_eq!(
        m.registered(2)?,
        Presence::Present(seal.registered(m.frozen_binding()))
    );
    Ok(())
}

#[test]
fn a23_evaluator_authority_is_checked_at_sealing() -> TestResult {
    let mut m = sealed()?;
    let claim = m.claim(0xe1)?;
    let before = m.world.bytes.clone();
    assert_eq!(m.evidence(&claim, 9, 1, 1192), Err(UNAUTHORIZED));
    assert_eq!(m.evidence(&claim, 1, 1, 1192), Err(UNAUTHORIZED));
    let delegated = |claimed: u8, signer: u8| {
        let pair = (evaluator_key(claimed), evaluator_key(signer));
        move |call: Req| Req {
            delegate: Some(pair),
            ..call
        }
    };
    assert_eq!(
        m.evidence_with(&claim, 2, 1192, delegated(0x99, 0x99)),
        Err(KEY_MISMATCH)
    );
    assert_eq!(
        m.evidence_with(&claim, 2, 1192, delegated(3, 3)),
        Err(KEY_MISMATCH)
    );
    assert_eq!(
        m.evidence_with(&claim, 2, 1192, delegated(2, 3)),
        Err(BAD_SIGNATURE)
    );
    assert_eq!(m.world.bytes, before);
    let Sealing::Applied { seal, .. } = m.evidence_with(&claim, 2, 1192, delegated(2, 2))? else {
        return Err(NON_CANONICAL);
    };
    assert_eq!(seal.evaluator, m.evaluator(2)?);
    m.world
        .edit(|parts, _| parts.replay.retire(ActorSlot::evaluator(2)?))?;
    assert_eq!(m.evidence(&claim, 4, 1, 1193), Err(NOT_FOUND));
    m.revoke(3, 1193)?;
    let revoked = m.world.bytes.clone();
    assert_eq!(m.evidence(&claim, 3, 1, 1194), Err(REVOKED));
    m.revoke(2, 1195)?;
    assert_eq!(m.evidence(&claim, 2, 1, 1196), Err(REVOKED));
    assert_eq!(m.evidence(&claim, 2, 2, 1196), Err(REVOKED));
    assert_eq!(
        m.evidence_with(&claim, 2, 1196, delegated(2, 2)),
        Err(REVOKED)
    );
    assert_eq!(m.world.features()?, Parts::load(&revoked)?.features);
    Ok(())
}

#[test]
fn a25_seal_requires_the_explicit_task_set_seal() -> TestResult {
    let mut m = opened()?;
    m.admit(0x21, 1, 1172, 1140)?;
    let digest = m.set_digest()?;
    let claim = Claim {
        version: 1,
        root: [0xe1; 32],
        policy: m.frozen.policy.bytes(),
        set: digest.bytes(),
        rubric: rubric()?.bytes(),
        mode: 1,
    };
    let before = m.world.bytes.clone();
    for at in [1150, 1192, 1207] {
        let refused = m.evidence(&claim, 2, 1, at);
        let expected = if at < 1192 {
            WRONG_PHASE
        } else {
            F09_EVIDENCE_TASK_SET_UNSEALED
        };
        assert_eq!(refused, Err(expected));
    }
    assert_eq!(m.world.bytes, before);
    m.seal_set(digest, 1193)?;
    let region = m.region()?;
    let Sealing::Applied { seal, .. } = m.evidence(&claim, 2, 1, 1194)? else {
        return Err(NON_CANONICAL);
    };
    assert_eq!(seal.task_set, digest);
    assert_eq!(m.sealed_set()?, digest);
    assert_eq!(m.admit(0x22, 2, 1172, 1195), Err(WRONG_PHASE));
    assert_eq!(m.region()?, region);
    assert_eq!(m.sealed_set()?, digest);
    Ok(())
}

#[test]
fn clause_10_binding_mismatches_refuse_without_change() -> TestResult {
    let mut m = sealed()?;
    let base = m.claim(0xe1)?;
    let subjective = TaskPolicyV1 {
        assessment_mode: 2,
        ..policy(1, 3)?
    };
    let before = m.world.bytes.clone();
    let slot = ActorSlot::evaluator(0)?;
    for claim in [
        Claim { mode: 2, ..base },
        Claim {
            mode: 2,
            policy: subjective.digest()?.bytes(),
            ..base
        },
        Claim {
            policy: subjective.digest()?.bytes(),
            ..base
        },
        Claim {
            rubric: [9; 32],
            ..base
        },
        Claim {
            set: [9; 32],
            ..base
        },
    ] {
        assert_eq!(m.evidence(&claim, 2, 1, 1192), Err(EVIDENCE_BINDING));
    }
    assert_eq!(m.world.bytes, before);
    let state = decode_shared_state(&m.world.bytes)?;
    assert_eq!(state.control.replay.actor(slot).and_then(|a| a.last), None);
    assert!(state.control.feature_bytes.is_empty());
    assert!(matches!(
        m.evidence(&base, 2, 1, 1192)?,
        Sealing::Applied { .. }
    ));
    Ok(())
}

#[test]
fn seal_payload_and_envelope_refusals() -> TestResult {
    let mut m = sealed()?;
    let base = m.claim(0xe1)?;
    let before = m.world.bytes.clone();
    for (claim, code) in [
        (Claim { version: 2, ..base }, BAD_VERSION),
        (Claim { mode: 0, ..base }, NON_CANONICAL),
        (Claim { mode: 3, ..base }, NON_CANONICAL),
        (
            Claim {
                root: [0; 32],
                ..base
            },
            NON_CANONICAL,
        ),
        (
            Claim {
                set: [0; 32],
                ..base
            },
            NON_CANONICAL,
        ),
    ] {
        assert_eq!(m.evidence(&claim, 2, 1, 1192), Err(code));
    }
    assert_eq!(base.payload().len(), SEAL_PAYLOAD_BYTES);
    let short = base.payload()[..SEAL_PAYLOAD_BYTES - 1].to_vec();
    let mut long = base.payload();
    long.push(0);
    for payload in [short, long] {
        let refused = m.evidence_with(&base, 2, 1192, |call| Req { payload, ..call });
        assert_eq!(refused, Err(NON_CANONICAL));
    }
    let elsewhere = MarketId::new([9; 32])?;
    let roster = RosterDigest::new([9; 32])?;
    let edits: [(Rebind, ApplicationError); 7] = [
        (|c, _, _| Req { epoch: 2, ..c }, WRONG_EPOCH),
        (|c, _, _| Req { config: 2, ..c }, WRONG_CONFIG),
        (
            |c, _, r| Req {
                roster: Presence::Present(r),
                ..c
            },
            WRONG_ROSTER,
        ),
        (
            |c, _, _| Req {
                roster: Presence::Absent,
                ..c
            },
            WRONG_ROSTER,
        ),
        (
            |c, market, _| Req {
                market: Some(market),
                ..c
            },
            WRONG_MARKET,
        ),
        (|c, _, _| Req { expiry: 1192, ..c }, EXPIRED),
        (
            |c, _, _| Req {
                operation: dispatch::ADMIT_TASK,
                ..c
            },
            UNKNOWN_OPERATION,
        ),
    ];
    for (edit, code) in edits {
        let refused = m.evidence_with(&base, 2, 1192, |call| edit(call, elsewhere, roster));
        assert_eq!(refused, Err(code));
    }
    assert_eq!(m.world.bytes, before);
    Ok(())
}

/// An envelope edit over a foreign market and roster.
type Rebind = fn(Req, MarketId, RosterDigest) -> Req;

/// An eight-seal region of `epoch` with ascending evaluators `1..=count`.
fn seal_record(evaluator: u8, height: u64) -> CodecResult<EvidenceSeal> {
    Ok(EvidenceSeal {
        evaluator: EvaluatorId::new([evaluator; 32])?,
        grant: version()?,
        key_version: version()?,
        root: EvidenceRoot::new([0xe1; 32])?,
        task_policy: PolicyDigest::new([0x21; 32])?,
        task_set: Digest32::new([0x22; 32])?,
        rubric: rubric()?,
        mode: AssessmentMode::Objective,
        height,
    })
}
fn region_bytes(epoch: u64, evaluators: &[u8], rest: &[u8]) -> CodecResult<Vec<u8>> {
    let mut out = epoch.to_be_bytes().to_vec();
    out.push(u8::try_from(evaluators.len()).map_err(|_| ARITHMETIC)?);
    for &evaluator in evaluators {
        out.extend_from_slice(&seal_record(evaluator, 1200)?.encode()?);
    }
    out.extend_from_slice(rest);
    Ok(out)
}

#[test]
fn a18_seal_region_codec_bounds() -> TestResult {
    assert_eq!(
        (SEAL_PAYLOAD_BYTES, SEAL_RECORD_BYTES, SEAL_REGION_MAX_BYTES),
        (131, 185, 1489)
    );
    let full = region_bytes(1, &[1, 2, 3, 4, 5, 6, 7, 8], &[])?;
    assert_eq!(full.len(), SEAL_REGION_MAX_BYTES);
    let region = SealRegion::decode(&full)?;
    assert_eq!(region.epoch, 1);
    let seals = region.seals(1).collect::<CodecResult<Vec<_>>>()?;
    assert_eq!(seals.len(), 8);
    assert_eq!(seals[7], seal_record(8, 1200)?);
    assert_eq!(region.seals(2).count(), 0);
    assert_eq!(region.get(2, EvaluatorId::new([3; 32])?)?, None);
    assert_eq!(
        region.get(1, EvaluatorId::new([3; 32])?)?,
        Some(seal_record(3, 1200)?)
    );
    let trailing = region_bytes(1, &[], &[0xfe; 5])?;
    assert_eq!(SealRegion::decode(&trailing)?.rest(), &[0xfe; 5]);
    assert_eq!(SealRegion::decode(&[])?.seals(0).count(), 0);
    for invalid in [
        region_bytes(1, &[1, 2, 3, 4, 5, 6, 7, 8, 9], &[])?,
        region_bytes(1, &[2, 1], &[])?,
        region_bytes(1, &[2, 2], &[])?,
        region_bytes(1, &[], &[])?,
        full[..full.len() - 1].to_vec(),
        full[..8].to_vec(),
    ] {
        assert_eq!(SealRegion::decode(&invalid), Err(NON_CANONICAL));
    }
    let record = seal_record(1, 1200)?.encode()?;
    let mut mode = record;
    mode[176] = 3;
    let mut grant = record;
    grant[32..40].fill(0);
    assert_eq!(EvidenceSeal::decode(&mode), Err(NON_CANONICAL));
    assert_eq!(EvidenceSeal::decode(&grant), Err(NON_CANONICAL));
    assert_eq!(EvidenceSeal::decode(&record[..184]), Err(NON_CANONICAL));
    let mut table = ReplayTable::new();
    let mut revision = 1;
    for index in 0..ACTOR_SLOTS {
        let tag = u8::try_from(index + 1).map_err(|_| ARITHMETIC)?;
        let slot = ActorSlot::from_index(u16::from(tag - 1))?;
        let who = principal(tag)?;
        table.bind(slot, who, version()?)?;
        let request = ReplayRequest {
            slot,
            principal: who,
            authority_version: version()?,
            sequence: 1,
            request_id: RequestId::new([tag; 32])?,
            digest: RequestDigest::new([tag; 32])?,
            expiry_height: ROLE_EXPIRY,
        };
        table.record_success(&request, 1200, &mut revision, ResultDigest::new([tag; 32])?)?;
    }
    let control = Control {
        replay: table,
        feature_bytes: &full,
    };
    let bound = 8 + 2 + ACTOR_SLOTS * (43 + REPLAY_RESULT_BYTES) + SEAL_REGION_MAX_BYTES;
    assert_eq!(control.encoded_len()?, bound);
    assert!(bound <= Section::Control.payload_cap());
    Ok(())
}

/// `sealed` with the control feature bytes padded by another producer's `filler` bytes after
/// an empty epoch-1 seal region.
fn padded(filler: usize) -> CodecResult<Market> {
    let mut m = sealed()?;
    m.world.edit(|parts, _| {
        let mut features = 1u64.to_be_bytes().to_vec();
        features.push(0);
        features.resize(9 + filler, 0xfe);
        parts.features = features;
        Ok(())
    })?;
    Ok(m)
}

#[test]
fn a18_control_capacity_is_exact_and_trailing_bytes_survive() -> TestResult {
    let replay = sealed()?.world.parts()?.replay.encoded_len()?;
    let cap = Section::Control.payload_cap();
    let filler = cap - 8 - (replay + REPLAY_RESULT_BYTES) - 9 - SEAL_RECORD_BYTES;
    let mut over = padded(filler + 1)?;
    let claim = over.claim(0xe1)?;
    let before = over.world.bytes.clone();
    assert_eq!(over.evidence(&claim, 2, 1, 1192), Err(CAPACITY));
    assert_eq!(over.world.bytes, before);
    let mut exact = padded(filler)?;
    let Sealing::Applied { seal, .. } = exact.evidence(&claim, 2, 1, 1192)? else {
        return Err(NON_CANONICAL);
    };
    let state = decode_shared_state(&exact.world.bytes)?;
    assert_eq!(state.control.encoded_len()?, cap);
    let region = SealRegion::decode(state.control.feature_bytes)?;
    assert_eq!(region.rest(), vec![0xfe; filler].as_slice());
    assert_eq!(
        region.seals(1).collect::<CodecResult<Vec<_>>>()?,
        vec![seal]
    );
    Ok(())
}

#[test]
fn a19_orphaned_composition_and_epoch_reset() -> TestResult {
    let mut m = sealed()?;
    let claim = m.claim(0xe1)?;
    let call = m.seal_call(&claim, 2, 1)?;
    let mut next = vec![0; MAX_STATE_BYTES];
    let mut event = vec![0; MAX_EVENT_BYTES];
    let Sealing::Applied { state_len, .. } = m.world.compose(&call, 1192, &mut next, &mut event)?
    else {
        return Err(NON_CANONICAL);
    };
    let composed = decode_shared_state(&next[..state_len])?;
    let evaluator = m.evaluator(2)?;
    assert!(matches!(
        sealed_evidence(&composed, 1, evaluator)?,
        Presence::Present(_)
    ));
    assert_eq!(m.registered(2)?, Presence::Absent);
    m.evidence(&claim, 2, 1, 1193)?;
    m.evidence(&m.claim(0xe3)?, 3, 1, 1193)?;
    let (frozen, entry) = (m.frozen, m.workers[0]);
    m.world.terminalize(&frozen, &entry, 1240)?;
    m.frozen = m.world.open(1256)?;
    assert_eq!(m.frozen.epoch, 2);
    let state = decode_shared_state(&m.world.bytes)?;
    assert_eq!(sealed_evidence(&state, 1, evaluator), Err(WRONG_EPOCH));
    assert_eq!(sealed_evidence(&state, 2, evaluator)?, Presence::Absent);
    let carried = SealRegion::decode(state.control.feature_bytes)?;
    assert_eq!((carried.epoch, carried.seals(1).count()), (1, 2));
    assert_eq!(carried.seals(2).count(), 0);
    let digest = m.set_digest()?;
    m.seal_set(digest, 1320)?;
    let renewed = m.claim(0xe4)?;
    assert_eq!(m.evidence(&renewed, 2, 2, 1319), Err(WRONG_PHASE));
    let Sealing::Applied { seal, .. } = m.evidence(&renewed, 2, 2, 1320)? else {
        return Err(NON_CANONICAL);
    };
    assert_eq!(seal.task_set, digest);
    let mut region = 2u64.to_be_bytes().to_vec();
    region.push(1);
    region.extend_from_slice(&seal.encode()?);
    assert_eq!(m.world.features()?, region);
    assert_eq!(
        m.registered(2)?,
        Presence::Present(seal.registered(m.frozen_binding()))
    );
    assert_eq!(m.registered(3)?, Presence::Absent);
    Ok(())
}

/// A result artifact published for one task: its context, manifest, root and envelope.
struct Published {
    context: ArtifactContext,
    manifest: Vec<u8>,
    root: [u8; 32],
    envelope: Vec<u8>,
}
fn publish(m: &Market, task: TaskId, signer: &SigningKey) -> CodecResult<Published> {
    let context = ArtifactContext {
        chain: m.header.deployment_chain_domain,
        program: m.header.program_id,
        market: m.header.market_id,
        policy: m.frozen.policy,
    };
    let object = b"result of one task";
    let mut leaves = vec![[0u8; 32]; 1];
    let value = ArtifactManifest {
        kind: ArtifactKind::Result,
        privacy: Privacy::Public,
        context,
        epoch: m.frozen.epoch,
        publisher: principal(1)?,
        subject: task.bytes(),
        byte_length: 18,
        chunk_count: chunk_count(18).map_err(artifact)?,
        content_root: object_content_root(object, &mut leaves).map_err(artifact)?,
        parents: Items::Typed(&[]),
        declaration_root: [0; 32],
        reproduction_root: [0; 32],
        access_policy_root: [0; 32],
        not_after_height: 0,
    };
    let mut manifest = vec![0; MAX_MANIFEST_BYTES];
    let n = encode_manifest(&value, &mut manifest).map_err(artifact)?;
    manifest.truncate(n);
    let root = manifest_root(&manifest).map_err(artifact)?;
    let digest = publisher_digest(&context, root, version()?);
    let mut envelope = vec![0; MAX_ENVELOPE_BYTES];
    let n = encode_publisher(
        &PublisherEnvelope {
            manifest: &manifest,
            generation: version()?,
            key: public(&delegate_key(1)),
            signature: Signature64(signer.sign(&digest).to_bytes()),
        },
        &mut envelope,
    )
    .map_err(artifact)?;
    envelope.truncate(n);
    Ok(Published {
        context,
        manifest,
        root: root.bytes(),
        envelope,
    })
}

#[test]
fn a06_r10_root_bindings_are_scoped_and_immutable() -> TestResult {
    let mut m = sealed()?;
    let task = m.task_id(0x21, 1)?;
    let p = publish(&m, task, &delegate_key(1))?;
    let subject = SubjectContext::Task { epoch: 1, task };
    let check = |context: &ArtifactContext, subject, kind, root| {
        check_root_binding(&p.manifest, context, subject, kind, root)
            .map(ArtifactManifestRoot::bytes)
    };
    assert_eq!(
        check(&p.context, subject, ArtifactKind::Result, p.root),
        Ok(p.root)
    );
    let elsewhere = ArtifactContext {
        market: MarketId::new([9; 32])?,
        ..p.context
    };
    let repolicied = ArtifactContext {
        policy: PolicyDigest::new([9; 32])?,
        ..p.context
    };
    let invalid = Err(ArtifactError::InvalidContext);
    assert_eq!(
        check(&elsewhere, subject, ArtifactKind::Result, p.root),
        invalid
    );
    assert_eq!(
        check(&repolicied, subject, ArtifactKind::Result, p.root),
        invalid
    );
    let other_task = SubjectContext::Task {
        epoch: 1,
        task: TaskId::new([0x5a; 32])?,
    };
    let later = SubjectContext::Task { epoch: 2, task };
    assert_eq!(
        check(&p.context, other_task, ArtifactKind::Result, p.root),
        invalid
    );
    assert_eq!(
        check(&p.context, later, ArtifactKind::Result, p.root),
        invalid
    );
    assert_eq!(
        check(&p.context, subject, ArtifactKind::Input, p.root),
        Err(ArtifactError::UnsupportedKind)
    );
    assert_eq!(
        check(&p.context, subject, ArtifactKind::Result, [9; 32]),
        Err(ArtifactError::RootMismatch)
    );
    let root = Digest32::new(p.root)?;
    let other = [0x77; 32];
    assert_eq!(bind_root(None, false, p.root)?, RootBind::Bound(root));
    assert_eq!(
        bind_root(Some(root), false, p.root)?,
        RootBind::AlreadyBound(root)
    );
    assert_eq!(
        bind_root(Some(root), true, p.root)?,
        RootBind::AlreadyBound(root)
    );
    assert_eq!(bind_root(Some(root), false, other), Err(CONFLICT));
    assert_eq!(bind_root(Some(root), true, other), Err(CONFLICT));
    assert_eq!(bind_root(None, true, p.root), Err(WRONG_PHASE));
    assert_eq!(bind_root(None, false, [0; 32]), Err(NON_CANONICAL));
    let claim = m.claim(0xe1)?;
    let market = MarketId::new([9; 32])?;
    let foreign = m.evidence_with(&claim, 2, 1192, |call| Req {
        market: Some(market),
        ..call
    });
    assert_eq!(foreign, Err(WRONG_MARKET));
    Ok(())
}

/// The evidence of evaluator 2 over the single task of `sealed`.
#[derive(Clone, Copy)]
struct Proof {
    binding: EvaluatorBinding,
    policy: PolicyDigest,
    set: Digest32,
    worker: WorkerId,
    generation: u64,
    model: [u8; 32],
    score: u32,
    task: TaskId,
    request: [u8; 32],
    result: [u8; 32],
    status: TerminalStatus,
}
fn evidence_policy() -> CodecResult<EvidencePolicy> {
    Ok(EvidencePolicy {
        mode: AssessmentMode::Objective,
        rubric: rubric()?,
        dataset_absence_admitted: false,
        benchmark_absence_admitted: false,
        missing_result_admitted: false,
    })
}
impl Proof {
    fn manifest(&self) -> CodecResult<Vec<u8>> {
        let tasks = [EvidenceTask {
            task: self.task,
            request_root: self.request,
            result_root: self.result,
            execution_root: [0x83; 32],
            status: self.status,
            reproduction_root: [0; 32],
        }];
        let groups = [WorkerGroup {
            worker: self.worker,
            generation: Version::new(self.generation)?,
            model_root: self.model,
            deployment_root: [0x72; 32],
            score: Score::new(self.score)?,
            reason_code: 0,
            reason_artifact_root: [0; 32],
            tasks: Items::Typed(&tasks),
        }];
        let manifest = EvidenceManifest {
            binding: self.binding,
            task_policy: self.policy,
            task_set: self.set,
            rubric: rubric()?,
            dataset_root: [0x61; 32],
            benchmark_root: [0x62; 32],
            mode: AssessmentMode::Objective,
            groups: Items::Typed(&groups),
        };
        let mut out = vec![0; MAX_EVIDENCE_BYTES];
        let n =
            encode_evidence_manifest(&manifest, &evidence_policy()?, &mut out).map_err(artifact)?;
        out.truncate(n);
        Ok(out)
    }
    fn root(&self) -> CodecResult<EvidenceRoot> {
        evidence_root(&self.manifest()?, &evidence_policy()?).map_err(artifact)
    }
    fn proven(&self) -> CodecResult<ProvenTask> {
        Ok(ProvenTask {
            task: self.task,
            worker: self.worker,
            generation: Version::new(self.generation)?,
            model_root: self.model,
            request_root: self.request,
            result_root: self.result,
        })
    }
    fn scores(&self) -> CodecResult<[ScoreEntry; 1]> {
        Ok([ScoreEntry {
            worker: self.worker,
            score: Score::new(self.score)?,
        }])
    }
}

/// A sealed market whose evaluator 2 sealed the root of `Proof` over a published result.
fn proven() -> CodecResult<(Market, Proof, EvidenceSeal, Published)> {
    let mut m = sealed()?;
    let task = m.task_id(0x21, 1)?;
    let published = publish(&m, task, &delegate_key(1))?;
    let proof = Proof {
        binding: EvaluatorBinding {
            frozen: m.frozen_binding(),
            evaluator: m.evaluator(2)?,
            grant: version()?,
            key_version: version()?,
        },
        policy: m.frozen.policy,
        set: m.sealed_set()?,
        worker: m.workers[0].worker,
        generation: 1,
        model: [0x71; 32],
        score: 900_000,
        task,
        request: [0x81; 32],
        result: published.root,
        status: TerminalStatus::Success,
    };
    let claim = Claim {
        root: proof.root()?.bytes(),
        ..m.claim(1)?
    };
    let Sealing::Applied { seal, .. } = m.evidence(&claim, 2, 1, 1192)? else {
        return Err(NON_CANONICAL);
    };
    Ok((m, proof, seal, published))
}

#[test]
fn a20_minimal_proof_conclusions_stay_separate() -> TestResult {
    let (m, proof, seal, published) = proven()?;
    let manifest = proof.manifest()?;
    let scores = proof.scores()?;
    let report = ReportBody {
        binding: proof.binding,
        evidence: proof.root()?,
        scores: ScoreVector::Typed(&scores),
    };
    let tasks = [proof.proven()?];
    let publishers = [published.envelope.as_slice()];
    let full = EvidenceProof {
        seal,
        binding: proof.binding,
        policy: evidence_policy()?,
        report: &report,
        manifest: &manifest,
        tasks: &tasks,
        publishers: &publishers,
    };
    let unproven = Conclusions {
        binding: Conclusion::Holds,
        signatures: Conclusion::Holds,
        integrity: Conclusion::Holds,
        availability: Conclusion::Unproven,
        rights: Conclusion::Unproven,
        reproduction: Conclusion::Unproven,
        quality: Conclusion::Unproven,
    };
    assert_eq!(verify_evidence_proof(&full), unproven);
    assert_eq!(
        verify_evidence_proof(&EvidenceProof {
            publishers: &[],
            ..full
        }),
        Conclusions {
            signatures: Conclusion::Unproven,
            ..unproven
        }
    );
    let Presence::Present(registered) = m.registered(2)? else {
        return Err(NON_CANONICAL);
    };
    assert_eq!(
        (registered.binding, registered.root, registered.rubric),
        (proof.binding, report.evidence, rubric()?)
    );
    let forged = publish(&m, proof.task, &delegate_key(5))?;
    let foreign = publish(&m, TaskId::new([0x5a; 32])?, &delegate_key(1))?;
    for (envelope, failure) in [
        (forged.envelope, ArtifactError::SignatureInvalid),
        (foreign.envelope, ArtifactError::InvalidContext),
    ] {
        let publishers = [published.envelope.as_slice(), envelope.as_slice()];
        let conclusions = verify_evidence_proof(&EvidenceProof {
            publishers: &publishers,
            ..full
        });
        assert_eq!(
            conclusions,
            Conclusions {
                signatures: Conclusion::Fails(failure),
                ..unproven
            }
        );
    }
    Ok(())
}

#[test]
fn a24_tampered_evidence_fails_integrity_or_binding() -> TestResult {
    let (_, proof, seal, _) = proven()?;
    let manifest = proof.manifest()?;
    let scores = proof.scores()?;
    let report = ReportBody {
        binding: proof.binding,
        evidence: proof.root()?,
        scores: ScoreVector::Typed(&scores),
    };
    let tasks = [proof.proven()?];
    let base = EvidenceProof {
        seal,
        binding: proof.binding,
        policy: evidence_policy()?,
        report: &report,
        manifest: &manifest,
        tasks: &tasks,
        publishers: &[],
    };
    let root_mismatch = Conclusion::Fails(ArtifactError::RootMismatch);
    let invalid = Conclusion::Fails(ArtifactError::InvalidContext);
    for tampered in [
        Proof {
            generation: 2,
            ..proof
        },
        Proof {
            score: 899_999,
            ..proof
        },
        Proof {
            status: TerminalStatus::Refused,
            ..proof
        },
        Proof {
            task: TaskId::new([0x5a; 32])?,
            ..proof
        },
        Proof {
            model: [0x79; 32],
            ..proof
        },
    ] {
        assert_ne!(tampered.root()?, proof.root()?);
        let bytes = tampered.manifest()?;
        let conclusions = verify_evidence_proof(&EvidenceProof {
            manifest: &bytes,
            ..base
        });
        assert_eq!(conclusions.integrity, root_mismatch);
        assert_eq!(conclusions.binding, invalid);
        assert_eq!(conclusions.quality, Conclusion::Unproven);
    }
    let stale = [ProvenTask {
        generation: Version::new(2)?,
        ..tasks[0]
    }];
    let extra = [
        tasks[0],
        ProvenTask {
            task: TaskId::new([0x5a; 32])?,
            ..tasks[0]
        },
    ];
    for records in [stale.as_slice(), extra.as_slice(), &[]] {
        let conclusions = verify_evidence_proof(&EvidenceProof {
            tasks: records,
            ..base
        });
        assert_eq!(
            (conclusions.binding, conclusions.integrity),
            (invalid, Conclusion::Holds)
        );
    }
    Ok(())
}

#[test]
fn a24_report_seal_and_policy_mismatches_fail_binding() -> TestResult {
    let (_, proof, seal, _) = proven()?;
    let manifest = proof.manifest()?;
    let scores = proof.scores()?;
    let report = ReportBody {
        binding: proof.binding,
        evidence: proof.root()?,
        scores: ScoreVector::Typed(&scores),
    };
    let tasks = [proof.proven()?];
    let base = EvidenceProof {
        seal,
        binding: proof.binding,
        policy: evidence_policy()?,
        report: &report,
        manifest: &manifest,
        tasks: &tasks,
        publishers: &[],
    };
    let root_mismatch = Conclusion::Fails(ArtifactError::RootMismatch);
    let invalid = Conclusion::Fails(ArtifactError::InvalidContext);
    let low = [ScoreEntry {
        score: Score::new(1)?,
        ..scores[0]
    }];
    let reports = [
        ReportBody {
            scores: ScoreVector::Typed(&low),
            ..report
        },
        ReportBody {
            evidence: EvidenceRoot::new([0x99; 32])?,
            ..report
        },
    ];
    let conclusions = verify_evidence_proof(&EvidenceProof {
        report: &reports[0],
        ..base
    });
    assert_eq!(
        (conclusions.binding, conclusions.integrity),
        (invalid, Conclusion::Holds)
    );
    let conclusions = verify_evidence_proof(&EvidenceProof {
        report: &reports[1],
        ..base
    });
    assert_eq!(conclusions.integrity, root_mismatch);
    for seal in [
        EvidenceSeal {
            task_set: Digest32::new([9; 32])?,
            ..seal
        },
        EvidenceSeal {
            grant: Version::new(2)?,
            ..seal
        },
        EvidenceSeal {
            mode: AssessmentMode::Subjective,
            ..seal
        },
    ] {
        let conclusions = verify_evidence_proof(&EvidenceProof { seal, ..base });
        assert_eq!(
            (conclusions.binding, conclusions.integrity),
            (invalid, Conclusion::Holds)
        );
    }
    let subjective = EvidencePolicy {
        mode: AssessmentMode::Subjective,
        ..base.policy
    };
    let conclusions = verify_evidence_proof(&EvidenceProof {
        policy: subjective,
        ..base
    });
    assert_eq!(
        (conclusions.binding, conclusions.integrity),
        (invalid, invalid)
    );
    Ok(())
}
