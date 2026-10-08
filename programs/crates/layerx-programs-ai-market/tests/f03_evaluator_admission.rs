//! AI.F03-T03 atomic evaluator report admission (`ValidateReportAdmission`, called by the
//! F04 `RevealScore` reveal) and the bounded advisory `ChallengeAssessment` over the complete
//! shared state value. Every market is produced by the real F01/F02/F03/F06/F08/F09
//! producers, `OPEN_EPOCH` and the F01 task-set lifecycle; reports are signed with real
//! ed25519 evaluator keys over the canonical attestation digest.
use ed25519_dalek::{Signer, SigningKey};
use layerx_programs_ai_market::{
    admission::{
        admit_evaluator, Admission, AdmissionContext, AdmissionTable, ApprovalTerms,
        EvaluatorConsent, Participant, TABLE_MAX_BYTES,
    },
    aggregation_codec::{EpochAggregation, QualityStatus, WorkerAggregate},
    codec::{
        self, decode_envelope, derive_evaluator, derive_market, derive_worker, encode_envelope,
        Envelope, ReportBody, RevealScorePayload, ScoreVector,
    },
    dispatch::{self, Operation},
    epoch::{self, Frozen, Outcome as Opening, ADVANCE_SCRATCH_BYTES, OPEN_SCRATCH_BYTES},
    errors::{
        ApplicationError, CodecResult, ARITHMETIC, BAD_SIGNATURE, CONFLICT, F03_CHALLENGE_CAPACITY,
        F03_EVIDENCE_NOT_SEALED, F03_EVIDENCE_ROOT_MISMATCH, F03_GRANT_VERSION_CONFLICT,
        F03_KEY_VERSION_CONFLICT, F03_NONCANONICAL_VECTOR, F03_NO_GRANT, F03_NO_SCORES,
        F03_REPORT_ALREADY_FINAL, F03_SCORE_RANGE, F03_UNKNOWN_WORKER, F08_MARKET_PAUSED,
        NON_CANONICAL, NOT_FOUND, REVOKED, UNAUTHORIZED, WRONG_CONFIG, WRONG_DOMAIN, WRONG_EPOCH,
        WRONG_ROSTER,
    },
    evaluators::{
        admission::{
            self as f03, AdmissionReceipt, ChallengeCategory, ChallengeOutcome, ChallengeRecord,
            RevealAdmission, CHALLENGE_SCRATCH_BYTES, MAX_CHALLENGES,
        },
        authority::{
            self, split_identity_section, AuthorityContext, EvaluatorRecord, EvaluatorRegion,
            LastRequest,
        },
        codec::encode_signed_report,
        model::{
            EvaluatorGrant, GrantTerms, RegisteredEvidence, SignedReport, VerificationError,
            SIGNED_REPORT_MAX_BYTES,
        },
    },
    evidence::{
        self, sealed_evidence, EvidenceSeal, Outcome as Sealing, SealRegion, SEAL_SCRATCH_BYTES,
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
        Section, SharedState,
    },
    tasks::{self, Outcome as Task, SetBinding, TaskSet},
    types::{
        AccountId, AssetId, Authentication, ChainDomain, Digest32, EvaluatorBinding, EvaluatorId,
        EvidenceRoot, FrozenBinding, MarketId, MetadataDigest, Presence, PrincipalId, ProgramId,
        PublicKey32, ReportDigest, RequestDigest, RequestId, ResultDigest, RosterDigest,
        RubricDigest, Salt32, Score, ScoreEntry, Signature64, Version, WorkerId, WorkerRosterEntry,
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
    fn registered(&self, n: u8) -> CodecResult<Presence<RegisteredEvidence>> {
        sealed_evidence(
            &decode_shared_state(&self.world.bytes)?,
            self.frozen.epoch,
            self.evaluator(n)?,
        )
    }
    /// One real owner F03 authority operation bound to the opened epoch. F03 advances only the
    /// shared revision, so the F01 header revision is re-bound to it.
    fn owner_authority(&mut self, operation: Operation, payload: Vec<u8>, at: u64) -> TestResult {
        let sequence = self.world.owner_sequence;
        let call = Req {
            sequence,
            request: [0x30 + u8::try_from(sequence).map_err(|_| ARITHMETIC)?; 32],
            ..self.bound(operation, PrincipalId::new(OWNER)?, payload)
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
    /// Real F03 `RevokeEvaluator` of evaluator `n` by the market owner.
    fn revoke(&mut self, n: u8, at: u64) -> TestResult {
        let mut payload = self.evaluator(n)?.as_bytes().to_vec();
        payload.extend_from_slice(&1u64.to_be_bytes());
        payload.extend_from_slice(&1u16.to_be_bytes());
        payload.extend_from_slice(&[0x5e; 32]);
        self.owner_authority(dispatch::RevokeEvaluator, payload, at)
    }
    /// Real F03 `ScheduleEvaluator` of principal `n` (nonce and key `n`), effective at the
    /// next epoch, left PENDING: no F08 approval or evaluator consent follows.
    fn schedule_pending(&mut self, n: u8, at: u64) -> TestResult {
        let mut payload = principal(n)?.as_bytes().to_vec();
        payload.extend_from_slice(&[n; 32]);
        payload.extend_from_slice(rubric()?.as_bytes());
        payload.extend_from_slice(&public(&evaluator_key(n)).0);
        for value in [1, 1, self.frozen.epoch + 1, self.frozen.epoch + 33] {
            payload.extend_from_slice(&u64::to_be_bytes(value));
        }
        self.owner_authority(dispatch::ScheduleEvaluator, payload, at)
    }
}

const REVEAL_TOPIC: &[u8] = b"PAXAI/v1/RevealScore";
const CHALLENGE_TOPIC: &[u8] = b"PAXAI/v1/ChallengeAssessment";
const MAX_SCORE: u32 = 1_000_000;
const SALT: [u8; 32] = [0x5a; 32];

/// The evidence root evaluator `n` seals in epoch 2.
const fn root(n: u8) -> u8 {
    0xe0 + n
}
/// Canonical 36-byte score entries in the given order.
fn entries(pairs: &[(WorkerId, u32)]) -> Vec<u8> {
    let mut out = Vec::new();
    for (worker, score) in pairs {
        out.extend_from_slice(worker.as_bytes());
        out.extend_from_slice(&score.to_be_bytes());
    }
    out
}
/// The evaluator signature over the attestation digest of `body`.
fn sign<'a>(body: ReportBody<'a>, key: &SigningKey) -> CodecResult<SignedReport<'a>> {
    let digest = codec::attestation_digest(codec::report_digest(&body)?)?;
    Ok(SignedReport {
        body,
        signature: Signature64(key.sign(&digest.bytes()).to_bytes()),
    })
}
/// A structurally invalid body cannot be digested, so it carries a nonzero filler signature.
const fn unsigned(body: ReportBody<'_>) -> SignedReport<'_> {
    SignedReport {
        body,
        signature: Signature64([0x11; 64]),
    }
}
/// Native verification failures are application errors.
fn application<T>(result: Result<T, VerificationError>) -> CodecResult<T> {
    result.map_err(|error| match error {
        VerificationError::Application(code) => code,
    })
}

/// Epoch 2 of the journey: worker 1 and the late worker 5 frozen, evaluators 2..=4 frozen,
/// `epoch_one` run inside epoch 1, the empty epoch-2 task set sealed at 1320 and evidence of
/// evaluators 2 and 3 sealed at 1320 under evaluator sequence 1 (evaluator 4 seals none).
/// Reveal is [1336, 1352).
fn revealing_with(epoch_one: impl FnOnce(&mut Market) -> TestResult) -> CodecResult<Market> {
    let mut m = sealed()?;
    let late = m.world.enroll(5, 1150)?;
    epoch_one(&mut m)?;
    let (frozen, entry) = (m.frozen, m.workers[0]);
    m.world.terminalize(&frozen, &entry, 1240)?;
    m.frozen = m.world.open(1256)?;
    m.workers.push(late);
    m.workers.sort_by_key(|w| w.worker);
    assert_eq!((m.frozen.epoch, m.frozen.workers), (2, 2));
    let digest = m.set_digest()?;
    m.seal_set(digest, 1320)?;
    for n in [2, 3] {
        let claim = m.claim(root(n))?;
        m.evidence(&claim, n, 1, 1320)?;
    }
    Ok(m)
}
fn revealing() -> CodecResult<Market> {
    revealing_with(|_| Ok(()))
}

/// Reports, reveal requests and the admission call.
impl Market {
    fn report_binding(&self, n: u8) -> CodecResult<EvaluatorBinding> {
        Ok(EvaluatorBinding {
            frozen: self.frozen_binding(),
            evaluator: self.evaluator(n)?,
            grant: version()?,
            key_version: version()?,
        })
    }
    /// Evaluator `n`'s report over `scores` with its sealed evidence root.
    fn body<'a>(&self, n: u8, scores: &'a [u8]) -> CodecResult<ReportBody<'a>> {
        Ok(ReportBody {
            binding: self.report_binding(n)?,
            evidence: EvidenceRoot::new([root(n); 32])?,
            scores: ScoreVector::Encoded(scores),
        })
    }
    /// Scores for both frozen workers, in worker order.
    fn both(&self, first: u32, second: u32) -> Vec<u8> {
        entries(&[
            (self.workers[0].worker, first),
            (self.workers[1].worker, second),
        ])
    }
    fn slot(&self, n: u8) -> CodecResult<ActorSlot> {
        evaluator_slot(
            &decode_shared_state(&self.world.bytes)?.control.replay,
            principal(n)?,
        )
    }
    /// The replay request of evaluator `n`'s real `RevealScore` envelope carrying `report`.
    fn request(
        &self,
        report: &SignedReport<'_>,
        n: u8,
        slot: ActorSlot,
        sequence: u64,
    ) -> CodecResult<ReplayRequest> {
        let mut payload = vec![0; 1480];
        let len = codec::encode_reveal_score(
            &RevealScorePayload {
                report: report.body,
                signature: report.signature,
                salt: Salt32::new(SALT)?,
            },
            &mut payload,
        )?;
        payload.truncate(len);
        let mut tag = [0xd0 + n; 32];
        tag[1..9].copy_from_slice(&sequence.to_be_bytes());
        let call = Req {
            sequence,
            request: tag,
            expiry: ROLE_EXPIRY,
            ..self.bound(dispatch::RevealScore, principal(n)?, payload)
        };
        let encoded = call.encode()?;
        ReplayRequest::from_envelope(slot, version()?, &decode_envelope(&encoded)?)
    }
    /// One admission composed over the committed bytes, committed only on `Applied` after the
    /// complete next state and its event are checked.
    fn reveal(
        &mut self,
        report: &SignedReport<'_>,
        request: &ReplayRequest,
        at: u64,
    ) -> CodecResult<f03::Admission> {
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; f03::ADMISSION_SCRATCH_BYTES];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let reveal = RevealAdmission {
            report: *report,
            request: *request,
            height: at,
            activity: at * 4,
        };
        let outcome = application(f03::admit_report(
            &self.world.bytes,
            &reveal,
            &mut next,
            &mut scratch,
            &mut event,
        ))?;
        if let f03::Admission::Applied {
            receipt,
            revision,
            state_len,
            event_len,
        } = outcome
        {
            let previous = core::mem::replace(&mut self.world.bytes, next[..state_len].to_vec());
            assert_eq!(self.world.revision()?, revision);
            check_admitted(&previous, &self.world.bytes, &reveal, &receipt)?;
            check_reveal_event(&event[..event_len], &reveal, &receipt, revision)?;
        }
        Ok(outcome)
    }
    /// A refused admission leaves the committed bytes unchanged.
    fn refused(
        &mut self,
        report: &SignedReport<'_>,
        request: &ReplayRequest,
        at: u64,
    ) -> CodecResult<ApplicationError> {
        let before = self.world.bytes.clone();
        let Err(code) = self.reveal(report, request, at) else {
            return Err(NON_CANONICAL);
        };
        assert_eq!(self.world.bytes, before);
        Ok(code)
    }
    /// Evaluator `n` reveals `scores` under evaluator sequence 2; returns the receipt.
    fn admit_scores(&mut self, n: u8, scores: &[u8], at: u64) -> CodecResult<AdmissionReceipt> {
        let report = sign(self.body(n, scores)?, &evaluator_key(n))?;
        let request = self.request(&report, n, self.slot(n)?, 2)?;
        match self.reveal(&report, &request, at)? {
            f03::Admission::Applied { receipt, .. } => Ok(receipt),
            _ => Err(NON_CANONICAL),
        }
    }
    fn admitted(&self, n: u8) -> CodecResult<Option<AdmissionReceipt>> {
        let state = decode_shared_state(&self.world.bytes)?;
        Ok(f03::admitted_report(&state, self.frozen.epoch, self.evaluator(n)?)?.map(|r| r.receipt))
    }
}

/// One revision and one evaluator sequence; only the F01 header revision and the admitted
/// row change: identity, rewards, admission and control feature bytes are byte-identical, so
/// no score normalization, reward or transfer follows.
fn check_admitted(
    previous: &[u8],
    current: &[u8],
    reveal: &RevealAdmission<'_>,
    receipt: &AdmissionReceipt,
) -> TestResult {
    let before = decode_shared_state(previous)?;
    let after = decode_shared_state(current)?;
    let revision = before.revision + 1;
    assert_eq!(after.revision, revision);
    let mut section = PolicySection::decode(before.feature_sections[0])?;
    section.header.state_revision = revision;
    let mut policy = vec![0; section.encoded_len()?];
    section.encode(&mut policy)?;
    assert_eq!(after.feature_sections[0], policy.as_slice());
    assert_eq!(after.feature_sections[1], before.feature_sections[1]);
    assert_eq!(after.feature_sections[3..], before.feature_sections[3..]);
    assert_eq!(after.control.feature_bytes, before.control.feature_bytes);
    let mut signed = vec![0; SIGNED_REPORT_MAX_BYTES];
    let len = encode_signed_report(&reveal.report, &mut signed)?;
    let epoch = reveal.report.body.binding.frozen.epoch;
    let row = f03::admitted_report(&after, epoch, receipt.evaluator)?.ok_or(NOT_FOUND)?;
    assert_eq!(
        (row.receipt, row.signed_bytes()),
        (*receipt, &signed[..len])
    );
    assert_eq!(row.signed()?, reveal.report);
    assert_eq!(
        (receipt.report, receipt.height, receipt.activity),
        (
            codec::report_digest(&reveal.report.body)?,
            reveal.height,
            reveal.activity
        )
    );
    let mut payload = vec![2];
    payload.extend_from_slice(receipt.evaluator.as_bytes());
    payload.extend_from_slice(receipt.report.as_bytes());
    payload.extend_from_slice(&reveal.height.to_be_bytes());
    payload.extend_from_slice(&reveal.activity.to_be_bytes());
    assert_eq!(receipt.payload()?.as_slice(), payload.as_slice());
    let retained = after
        .control
        .replay
        .actor(reveal.request.slot)
        .and_then(|actor| actor.last)
        .ok_or(NON_CANONICAL)?;
    assert_eq!(
        (
            retained.sequence,
            retained.applied_revision,
            retained.result_digest
        ),
        (
            reveal.request.sequence,
            revision,
            codec::result_digest(&payload)?
        )
    );
    Ok(())
}
/// The sole admission event is the F04 228-byte `RevealScore` event.
fn check_reveal_event(
    event: &[u8],
    reveal: &RevealAdmission<'_>,
    receipt: &AdmissionReceipt,
    revision: u64,
) -> TestResult {
    assert_eq!(event.len(), 228);
    let (operation, common, _) = codec::decode_event_frame(REVEAL_TOPIC, event)?;
    let decoded = codec::decode_reveal_event(event)?;
    let body = &reveal.report.body;
    assert_eq!(operation, dispatch::RevealScore);
    assert_eq!(
        (common.market, common.epoch, common.config, common.revision),
        (
            body.binding.frozen.market,
            body.binding.frozen.epoch,
            body.binding.frozen.config,
            revision
        )
    );
    assert_eq!(
        (common.request, common.result),
        (reveal.request.digest, receipt.result()?)
    );
    assert_eq!(
        (
            decoded.evaluator,
            decoded.report,
            decoded.evidence,
            usize::from(decoded.vector_count),
            decoded.admitted_height
        ),
        (
            receipt.evaluator,
            receipt.report,
            body.evidence,
            body.scores.len(),
            reveal.height
        )
    );
    Ok(())
}

/// The `ChallengeAssessment` payload, identity and call.
fn allegation(evaluator: EvaluatorId, report: ReportDigest, category: u8, evidence: u8) -> Vec<u8> {
    let mut out = evaluator.as_bytes().to_vec();
    out.extend_from_slice(report.as_bytes());
    out.push(category);
    out.extend_from_slice(&[evidence; 32]);
    out
}
impl Market {
    /// `H('PAXAI/evaluator-challenge/v1', chain || program || market || epoch || payload ||
    /// requester)`, the payload being `evaluator || report || category || evidence`.
    fn challenge_id(&self, payload: &[u8], requester: PrincipalId) -> CodecResult<Digest32> {
        let mut preimage = CHAIN.to_vec();
        preimage.extend_from_slice(&PROGRAM);
        preimage.extend_from_slice(self.header.market_id.as_bytes());
        preimage.extend_from_slice(&self.frozen.epoch.to_be_bytes());
        preimage.extend_from_slice(payload);
        preimage.extend_from_slice(requester.as_bytes());
        assert_eq!(preimage.len(), 233);
        codec::domain_hash("PAXAI/evaluator-challenge/v1", &preimage)
    }
    /// The permissionless native call of `requester` asserting the derived identity.
    fn challenge_call(&self, requester: u8, payload: Vec<u8>) -> CodecResult<Req> {
        let actor = principal(requester)?;
        let id = self.challenge_id(&payload, actor)?;
        Ok(Req {
            request: id.bytes(),
            ..self.bound(dispatch::ChallengeAssessment, actor, payload)
        })
    }
    /// One challenge composed over the committed bytes; committed only on `Applied` after
    /// the next state and event are checked. Nothing else writes an event.
    fn challenge(&mut self, call: &Req, ctx: &CallContext) -> CodecResult<ChallengeOutcome> {
        let encoded = call.encode()?;
        let envelope = decode_envelope(&encoded)?;
        let mut next = vec![0; MAX_STATE_BYTES];
        let mut scratch = vec![0; CHALLENGE_SCRATCH_BYTES];
        let mut event = vec![0; MAX_EVENT_BYTES];
        let before = self.world.bytes.clone();
        let outcome = f03::apply_challenge(
            ctx,
            &envelope,
            &self.world.bytes,
            &mut next,
            &mut scratch,
            &mut event,
        );
        let Ok(ChallengeOutcome::Applied {
            record,
            revision,
            result,
            state_len,
            event_len,
        }) = outcome
        else {
            assert!(event.iter().all(|b| *b == 0));
            return outcome;
        };
        self.world.bytes = next[..state_len].to_vec();
        assert_eq!(self.world.revision()?, revision);
        check_challenged(&before, &self.world.bytes, &record)?;
        let encoded_record = record.encode()?;
        assert_eq!(result, codec::result_digest(&encoded_record)?);
        assert_eq!(ChallengeRecord::decode(&encoded_record)?, record);
        let (operation, common, suffix) =
            codec::decode_event_frame(CHALLENGE_TOPIC, &event[..event_len])?;
        assert_eq!(
            (operation, common.epoch, common.revision, common.result),
            (
                dispatch::ChallengeAssessment,
                self.frozen.epoch,
                revision,
                result
            )
        );
        assert_eq!(common.request, envelope.request_digest()?);
        assert_eq!(suffix, encoded_record.as_slice());
        outcome
    }
    fn challenge_by(
        &mut self,
        requester: u8,
        payload: Vec<u8>,
        at: u64,
    ) -> CodecResult<ChallengeOutcome> {
        let call = self.challenge_call(requester, payload)?;
        self.challenge(&call, &call.context(at)?)
    }
}
/// One object-local revision: the F01 header revision and the challenge region after the
/// F09 seals change; the replay table, the seals and every other section are unchanged.
fn check_challenged(previous: &[u8], current: &[u8], record: &ChallengeRecord) -> TestResult {
    let before = decode_shared_state(previous)?;
    let after = decode_shared_state(current)?;
    let revision = before.revision + 1;
    let mut section = PolicySection::decode(before.feature_sections[0])?;
    section.header.state_revision = revision;
    let mut policy = vec![0; section.encoded_len()?];
    section.encode(&mut policy)?;
    assert_eq!(after.feature_sections[0], policy.as_slice());
    assert_eq!(after.feature_sections[1..], before.feature_sections[1..]);
    assert_eq!(after.control.replay, before.control.replay);
    let (old, new) = (
        SealRegion::decode(before.control.feature_bytes)?,
        SealRegion::decode(after.control.feature_bytes)?,
    );
    assert_eq!(
        (new.epoch, new.seals(new.epoch).count()),
        (old.epoch, old.seals(old.epoch).count())
    );
    let region = f03::challenge_region(after.control.feature_bytes)?;
    let previous = f03::challenge_region(before.control.feature_bytes)?;
    assert_eq!(region.get(region.epoch, record.id)?, Some(*record));
    assert_eq!(
        region.challenges(region.epoch).count(),
        previous.challenges(region.epoch).count() + 1
    );
    assert_eq!(region.rest(), previous.rest());
    Ok(())
}

#[test]
fn a01_a02_explicit_zero_is_a_vote_and_an_omitted_worker_is_none() -> TestResult {
    let mut m = revealing()?;
    let (first, second) = (m.workers[0].worker, m.workers[1].worker);
    let zero = m.both(0, MAX_SCORE);
    let voted = m.admit_scores(2, &zero, 1336)?;
    let omitted = entries(&[(second, 7)]);
    let partial = m.admit_scores(3, &omitted, 1337)?;
    let state = decode_shared_state(&m.world.bytes)?;
    let scores_of = |n: u8| -> CodecResult<Vec<ScoreEntry>> {
        f03::admitted_report(&state, 2, m.evaluator(n)?)?
            .ok_or(NOT_FOUND)?
            .signed()?
            .body
            .scores
            .entries()
            .collect()
    };
    assert_eq!(
        scores_of(2)?,
        vec![
            ScoreEntry {
                worker: first,
                score: Score::new(0)?
            },
            ScoreEntry {
                worker: second,
                score: Score::new(MAX_SCORE)?
            }
        ]
    );
    assert_eq!(
        scores_of(3)?,
        vec![ScoreEntry {
            worker: second,
            score: Score::new(7)?
        }]
    );
    let region =
        f03::ReportRegion::decode(state.feature_sections[Section::CurrentReports.index()])?;
    let listed = region
        .rows(2)
        .map(|row| row.map(|r| r.receipt))
        .collect::<CodecResult<Vec<_>>>()?;
    let mut expected = vec![voted, partial];
    expected.sort_unstable_by_key(|r| r.evaluator);
    assert_eq!((region.epoch, listed), (2, expected));
    assert_eq!(region.rows(1).count(), 0);
    assert_eq!(
        f03::admitted_report(&state, 1, voted.evaluator),
        Err(WRONG_EPOCH)
    );
    assert_eq!(m.admitted(4)?, None);
    Ok(())
}

#[test]
fn a03_a04_vector_structure_and_score_bounds() -> TestResult {
    let mut m = revealing()?;
    let (first, second) = (m.workers[0].worker, m.workers[1].worker);
    let valid = entries(&[(first, MAX_SCORE)]);
    let report = sign(m.body(2, &valid)?, &evaluator_key(2))?;
    let request = m.request(&report, 2, m.slot(2)?, 2)?;
    let stranger = derive_worker(m.header.market_id, principal(9)?, [9; 32])?;
    let mut known = [(first, 1), (stranger, 1)];
    known.sort_unstable_by_key(|(w, _)| *w);
    let mut oversized = Vec::new();
    for i in 1..=33u8 {
        oversized.push((WorkerId::new([i; 32])?, 1));
    }
    let ragged = entries(&[(first, 1)]);
    let cases = [
        (entries(&[(second, 1), (first, 2)]), F03_NONCANONICAL_VECTOR),
        (entries(&[(first, 1), (first, 2)]), F03_NONCANONICAL_VECTOR),
        (entries(&[(first, MAX_SCORE + 1)]), F03_SCORE_RANGE),
        (Vec::new(), F03_NO_SCORES),
        (entries(&oversized), NON_CANONICAL),
        (ragged[..35].to_vec(), NON_CANONICAL),
        (entries(&known), F03_UNKNOWN_WORKER),
    ];
    for (scores, code) in cases {
        let body = m.body(2, &scores)?;
        assert_eq!(m.refused(&unsigned(body), &request, 1336)?, code);
    }
    let unknown = entries(&known);
    let signed = sign(m.body(2, &unknown)?, &evaluator_key(2))?;
    assert_eq!(m.refused(&signed, &request, 1336)?, F03_UNKNOWN_WORKER);
    let f03::Admission::Applied { receipt, .. } = m.reveal(&report, &request, 1336)? else {
        return Err(NON_CANONICAL);
    };
    assert_eq!(m.admitted(2)?, Some(receipt));
    Ok(())
}

#[test]
fn a05_frozen_domain_epoch_config_and_roster() -> TestResult {
    let mut m = revealing()?;
    let scores = m.both(5, 6);
    let good = m.body(2, &scores)?;
    let request = m.request(&sign(good, &evaluator_key(2))?, 2, m.slot(2)?, 2)?;
    let f = good.binding.frozen;
    let bindings = [
        (
            FrozenBinding {
                chain: ChainDomain::new([0x61; 32])?,
                ..f
            },
            WRONG_DOMAIN,
        ),
        (
            FrozenBinding {
                program: ProgramId::new([0x62; 32])?,
                ..f
            },
            WRONG_DOMAIN,
        ),
        (
            FrozenBinding {
                market: MarketId::new([0x63; 32])?,
                ..f
            },
            WRONG_DOMAIN,
        ),
        (FrozenBinding { epoch: 1, ..f }, WRONG_EPOCH),
        (FrozenBinding { epoch: 3, ..f }, WRONG_EPOCH),
        (
            FrozenBinding {
                config: Version::new(2)?,
                ..f
            },
            WRONG_CONFIG,
        ),
        (
            FrozenBinding {
                roster: RosterDigest::new([0x64; 32])?,
                ..f
            },
            WRONG_ROSTER,
        ),
    ];
    for (frozen, code) in bindings {
        let body = ReportBody {
            binding: EvaluatorBinding {
                frozen,
                ..good.binding
            },
            ..good
        };
        assert_eq!(
            m.refused(&sign(body, &evaluator_key(2))?, &request, 1336)?,
            code
        );
    }
    let report = sign(good, &evaluator_key(2))?;
    assert!(matches!(
        m.reveal(&report, &request, 1336)?,
        f03::Admission::Applied { .. }
    ));
    Ok(())
}

#[test]
fn a06_versions_signature_and_requesting_principal() -> TestResult {
    let mut m = revealing()?;
    let scores = m.both(5, 6);
    let good = m.body(2, &scores)?;
    let report = sign(good, &evaluator_key(2))?;
    let request = m.request(&report, 2, m.slot(2)?, 2)?;
    let rekeyed = EvaluatorBinding {
        key_version: Version::new(2)?,
        ..good.binding
    };
    let regranted = EvaluatorBinding {
        grant: Version::new(2)?,
        ..good.binding
    };
    for (binding, code) in [
        (rekeyed, F03_KEY_VERSION_CONFLICT),
        (regranted, F03_GRANT_VERSION_CONFLICT),
    ] {
        let body = ReportBody { binding, ..good };
        assert_eq!(
            m.refused(&sign(body, &evaluator_key(2))?, &request, 1336)?,
            code
        );
    }
    let digest = codec::report_digest(&good)?;
    let forged = [
        sign(good, &evaluator_key(3))?,
        SignedReport {
            body: good,
            signature: Signature64(evaluator_key(2).sign(digest.as_bytes()).to_bytes()),
        },
    ];
    for report in forged {
        assert_eq!(m.refused(&report, &request, 1336)?, BAD_SIGNATURE);
    }
    let other = m.request(
        &sign(m.body(3, &scores)?, &evaluator_key(3))?,
        3,
        m.slot(3)?,
        2,
    )?;
    assert_eq!(m.refused(&report, &other, 1336)?, UNAUTHORIZED);
    let misslotted = ReplayRequest {
        slot: m.slot(3)?,
        ..request
    };
    assert_eq!(m.refused(&report, &misslotted, 1336)?, UNAUTHORIZED);
    assert!(matches!(
        m.reveal(&report, &request, 1336)?,
        f03::Admission::Applied { .. }
    ));
    Ok(())
}

#[test]
fn a11_a12_sealed_evidence_root_and_a_recordable_challenge() -> TestResult {
    let mut m = revealing()?;
    let scores = m.both(5, 6);
    let unsealed = sign(m.body(4, &scores)?, &evaluator_key(4))?;
    let request = m.request(&unsealed, 4, m.slot(4)?, 1)?;
    assert_eq!(m.registered(4)?, Presence::Absent);
    assert_eq!(
        m.refused(&unsealed, &request, 1336)?,
        F03_EVIDENCE_NOT_SEALED
    );
    let Presence::Present(registered) = m.registered(2)? else {
        return Err(NOT_FOUND);
    };
    assert_eq!(registered.root, EvidenceRoot::new([root(2); 32])?);
    let moved = ReportBody {
        evidence: EvidenceRoot::new([root(3); 32])?,
        ..m.body(2, &scores)?
    };
    let moved = sign(moved, &evaluator_key(2))?;
    let request = m.request(&moved, 2, m.slot(2)?, 2)?;
    assert_eq!(
        m.refused(&moved, &request, 1336)?,
        F03_EVIDENCE_ROOT_MISMATCH
    );
    let receipt = m.admit_scores(2, &scores, 1337)?;
    let state = decode_shared_state(&m.world.bytes)?;
    let row = f03::admitted_report(&state, 2, receipt.evaluator)?.ok_or(NOT_FOUND)?;
    assert_eq!(row.signed()?.body.evidence, registered.root);
    let signed = row.signed_bytes().to_vec();
    let payload = allegation(receipt.evaluator, receipt.report, 3, 0x77);
    let ChallengeOutcome::Applied { record, .. } = m.challenge_by(9, payload, 1338)? else {
        return Err(NON_CANONICAL);
    };
    assert_eq!(
        (
            record.evaluator,
            record.report,
            record.category,
            record.requester,
            record.height
        ),
        (
            receipt.evaluator,
            receipt.report,
            ChallengeCategory::PolicyOrEvidenceMismatch,
            principal(9)?,
            1338
        )
    );
    let state = decode_shared_state(&m.world.bytes)?;
    let row = f03::admitted_report(&state, 2, receipt.evaluator)?.ok_or(NOT_FOUND)?;
    assert_eq!(
        (row.receipt, row.signed_bytes()),
        (receipt, signed.as_slice())
    );
    Ok(())
}

#[test]
fn a14_retransmission_recovers_the_receipt_and_the_slot_is_final() -> TestResult {
    let mut m = revealing()?;
    let scores = m.both(5, 6);
    let report = sign(m.body(2, &scores)?, &evaluator_key(2))?;
    let request = m.request(&report, 2, m.slot(2)?, 2)?;
    let f03::Admission::Applied { receipt, .. } = m.reveal(&report, &request, 1336)? else {
        return Err(NON_CANONICAL);
    };
    let admitted = m.world.bytes.clone();
    let f03::Admission::Retained(retained) = m.reveal(&report, &request, 1340)? else {
        return Err(NON_CANONICAL);
    };
    assert_eq!(
        (
            retained.sequence,
            retained.request_digest,
            retained.result_digest
        ),
        (2, request.digest, receipt.result()?)
    );
    let renewed = m.request(&report, 2, m.slot(2)?, 3)?;
    assert_eq!(
        m.reveal(&report, &renewed, 1341)?,
        f03::Admission::AlreadyAdmitted(receipt)
    );
    let changed = m.both(5, 7);
    let other = sign(m.body(2, &changed)?, &evaluator_key(2))?;
    let replaced = m.request(&other, 2, m.slot(2)?, 3)?;
    assert_eq!(
        m.refused(&other, &replaced, 1342)?,
        F03_REPORT_ALREADY_FINAL
    );
    assert_eq!(m.world.bytes, admitted);
    assert_eq!(m.admitted(2)?, Some(receipt));
    Ok(())
}

#[test]
fn a16_pending_grants_suspension_and_quality_challenges() -> TestResult {
    let mut m = revealing_with(|m| m.schedule_pending(6, 1160))?;
    let pending = m.evaluator(6)?;
    let state = decode_shared_state(&m.world.bytes)?;
    let region =
        authority::evaluator_region(state.feature_sections[Section::IdentityRoster.index()])?;
    assert!(region.get(pending).is_some());
    assert!(region.snapshot().ok_or(NOT_FOUND)?.get(pending).is_none());
    let scores = m.both(3, 4);
    let report = sign(m.body(6, &scores)?, &evaluator_key(6))?;
    let request = m.request(&report, 6, ActorSlot::evaluator(4)?, 1)?;
    assert_eq!(m.refused(&report, &request, 1336)?, F03_NO_GRANT);

    let mut payload = m.world.revision()?.to_be_bytes().to_vec();
    payload.extend_from_slice(&[0x50; 32]);
    m.world.owner_op(dispatch::SUSPEND, payload, 1337)?;
    let header = m.world.parts()?.market()?;
    assert_eq!(header.lifecycle, 3);
    let mut table = m.world.parts()?.admission;
    let late = Participant::Worker(derive_worker(header.market_id, principal(8)?, [8; 32])?);
    let refused = approve(
        &mut table,
        &header,
        late,
        principal(8)?,
        public(&delegate_key(8)),
        1337,
    );
    assert_eq!(refused.map(|_| ()), Err(F08_MARKET_PAUSED));

    let receipt = m.admit_scores(2, &scores, 1338)?;
    let state = decode_shared_state(&m.world.bytes)?;
    let reports = state.feature_sections[Section::CurrentReports.index()].to_vec();
    let payload = allegation(receipt.evaluator, receipt.report, 4, 0x71);
    let ChallengeOutcome::Applied { record, .. } = m.challenge_by(9, payload, 1339)? else {
        return Err(NON_CANONICAL);
    };
    assert_eq!(record.category, ChallengeCategory::QualityDisagreement);
    let state = decode_shared_state(&m.world.bytes)?;
    assert_eq!(
        state.feature_sections[Section::CurrentReports.index()],
        reports.as_slice()
    );
    assert_eq!(m.admitted(2)?, Some(receipt));
    Ok(())
}

#[test]
fn revoked_evaluators_cannot_reveal() -> TestResult {
    let mut m = revealing()?;
    m.revoke(3, 1336)?;
    let scores = m.both(1, 1);
    let report = sign(m.body(3, &scores)?, &evaluator_key(3))?;
    let request = m.request(&report, 3, m.slot(3)?, 2)?;
    assert_eq!(m.refused(&report, &request, 1337)?, REVOKED);
    let receipt = m.admit_scores(2, &scores, 1338)?;
    assert_eq!(m.admitted(2)?, Some(receipt));
    Ok(())
}

#[test]
fn challenges_reference_admitted_reports_under_their_identity() -> TestResult {
    let mut m = revealing()?;
    let scores = m.both(1, 2);
    let receipt = m.admit_scores(2, &scores, 1336)?;
    let before = m.world.bytes.clone();
    let stranger = m.evaluator(9)?;
    let refusals = [
        (
            allegation(receipt.evaluator, receipt.report, 0, 1),
            NON_CANONICAL,
        ),
        (
            allegation(receipt.evaluator, receipt.report, 5, 1),
            NON_CANONICAL,
        ),
        (
            allegation(receipt.evaluator, ReportDigest::new([0x42; 32])?, 1, 1),
            NOT_FOUND,
        ),
        (allegation(m.evaluator(3)?, receipt.report, 1, 1), NOT_FOUND),
        (allegation(stranger, receipt.report, 1, 1), NOT_FOUND),
    ];
    for (payload, code) in refusals {
        assert_eq!(m.challenge_by(9, payload, 1337), Err(code));
    }
    let payload = allegation(receipt.evaluator, receipt.report, 1, 1);
    let call = m.challenge_call(9, payload)?;
    let asserted = Req {
        request: [0x99; 32],
        ..call.clone()
    };
    assert_eq!(
        m.challenge(&asserted, &asserted.context(1337)?),
        Err(CONFLICT)
    );
    let stale = Req {
        epoch: 1,
        ..call.clone()
    };
    assert_eq!(m.challenge(&stale, &stale.context(1337)?), Err(WRONG_EPOCH));
    let ctx = CallContext {
        principal: principal(8)?,
        ..call.context(1337)?
    };
    assert_eq!(m.challenge(&call, &ctx), Err(UNAUTHORIZED));
    assert_eq!(m.world.bytes, before);
    Ok(())
}

#[test]
fn challenges_are_bounded_and_idempotent() -> TestResult {
    let mut m = revealing()?;
    let scores = m.both(1, 2);
    let receipt = m.admit_scores(2, &scores, 1336)?;
    let target = |i: u8| allegation(receipt.evaluator, receipt.report, 1 + i % 4, 0x80 + i);
    let ChallengeOutcome::Applied { record: first, .. } = m.challenge_by(9, target(0), 1337)?
    else {
        return Err(NON_CANONICAL);
    };
    for i in 1..8u8 {
        assert!(matches!(
            m.challenge_by(9 + i, target(i), 1337 + u64::from(i))?,
            ChallengeOutcome::Applied { .. }
        ));
    }
    let state = decode_shared_state(&m.world.bytes)?;
    let region = f03::challenge_region(state.control.feature_bytes)?;
    assert_eq!(
        (region.epoch, region.challenges(2).count()),
        (2, MAX_CHALLENGES)
    );
    assert_eq!(region.challenges(1).count(), 0);
    assert_eq!(region.get(2, first.id)?, Some(first));
    let full = m.world.bytes.clone();
    assert_eq!(
        m.challenge_by(9, target(8), 1346),
        Err(F03_CHALLENGE_CAPACITY)
    );
    assert_eq!(
        m.challenge_by(9, target(0), 1347)?,
        ChallengeOutcome::AlreadyApplied { record: first }
    );
    assert_eq!(m.world.bytes, full);
    assert_eq!(m.admitted(2)?, Some(receipt));
    Ok(())
}
