//! F03 grant authority over real shared state, the real F08 admission table and
//! roster rollover, and native Ed25519 proof of possession.
use ed25519_dalek::{Signer, SigningKey};
use layerx_programs_ai_market::{
    admission::{
        admit_evaluator, AdmissionContext, AdmissionMeta, AdmissionTable, ApprovalTerms,
        EvaluatorConsent, Participant, FLAG_ADMITTED,
    },
    codec::{
        self, derive_evaluator, derive_market, derive_worker, Envelope, ReportBody, ScoreVector,
    },
    dispatch::{self, Operation},
    errors::{
        CodecResult, CONFLICT, EXPIRED, F03_BAD_ACTIVATION, F03_EVALUATOR_CAPACITY,
        F03_GRANT_VERSION_CONFLICT, F03_KEY_REUSED, F03_KEY_VERSION_CONFLICT, F03_NO_GRANT,
        F08_BAD_CONSENT, F08_OWNER_REQUIRED, NON_CANONICAL, REPLAY_CONFLICT, REVOKED,
        ROLE_CONFLICT, SEQUENCE_GAP, UNAUTHORIZED, UNKNOWN_OPERATION, WRONG_CONFIG, WRONG_EPOCH,
        WRONG_PHASE,
    },
    evaluators::{
        authority::{
            self, AuthorityContext, EvaluatorRecord, EvaluatorRegion, LastRequest, OpenedSnapshot,
            Outcome, Revocation, RevocationReason, SnapshotContext, SCRATCH_BYTES,
        },
        codec::{check_binding, check_report_context},
        model::{EvaluatorGrant, GrantStatus, GrantTerms, ReportContext},
    },
    registry::{derive_rewards_account, MarketHeader},
    roster,
    state::{self, ActorSlot, Control, ReplayTable, Section, SharedState},
    types::{
        AccountId, AssetId, Authentication, ChainDomain, Digest32, EvaluatorBinding, EvaluatorId,
        EvidenceRoot, FrozenBinding, MetadataDigest, Presence, PrincipalId, ProgramId, PublicKey32,
        RequestDigest, RequestId, ResultDigest, RosterDigest, RubricDigest, Score, ScoreEntry,
        Version, WorkerId, WorkerRosterEntry,
    },
    workers::{WorkerCurrent, WorkerState, WorkerTable, WORKER_TABLE_MAX_BYTES},
    MAX_STATE_BYTES,
};

type TestResult = CodecResult<()>;

const OWNER: u8 = 12;
const RUBRIC: [u8; 32] = [6; 32];
const ROSTER: [u8; 32] = [0x52; 32];

fn p(b: u8) -> CodecResult<PrincipalId> {
    PrincipalId::new([b; 32])
}
fn v(n: u64) -> CodecResult<Version> {
    Version::new(n)
}
fn key(seed: u8) -> SigningKey {
    SigningKey::from_bytes(&[seed; 32])
}
fn pk(k: &SigningKey) -> PublicKey32 {
    PublicKey32(k.verifying_key().to_bytes())
}

fn header() -> CodecResult<MarketHeader> {
    let chain = ChainDomain::new([10; 32])?;
    let program = ProgramId::new([11; 32])?;
    let asset = AssetId::new([13; 32])?;
    Ok(MarketHeader {
        format_version: 1,
        market_id: derive_market(chain, program)?,
        deployment_chain_domain: chain,
        program_id: program,
        owner_principal: p(OWNER)?,
        funding_asset: asset,
        rewards_account: derive_rewards_account(program, asset)?,
        refund_recipient_account: AccountId::new([14; 32])?,
        treasury_principal: Presence::Absent,
        origin_height: 0,
        lifecycle: 2,
        state_revision: 1,
        highest_config_version: 1,
        active_config_version: 1,
        activation_epoch: 0,
        activation_scheduled: false,
        closure_requested_at: 0,
        close_phase: 0,
        close_cursor: 0,
        suspension_reason_digest: [0; 32],
        metadata_digest: MetadataDigest::new([16; 32])?,
        closing_request_digest: [0; 32],
        reserved: [0; 8],
    })
}

/// Owner-chosen nomination terms; IDs, nonces and keys derive from `n`.
#[derive(Clone)]
struct Nomination {
    principal: u8,
    nonce: u8,
    key: SigningKey,
    rubric: [u8; 32],
    grant: u64,
    key_version: u64,
    effective: u64,
    expiry: u64,
}
fn nomination(n: u8, effective: u64, expiry: u64) -> Nomination {
    Nomination {
        principal: 30 + n,
        nonce: 60 + n,
        key: key(90 + n),
        rubric: RUBRIC,
        grant: 1,
        key_version: 1,
        effective,
        expiry,
    }
}
impl Nomination {
    fn payload(&self) -> Vec<u8> {
        let mut out = vec![self.principal; 32];
        out.extend_from_slice(&[self.nonce; 32]);
        out.extend_from_slice(&self.rubric);
        out.extend_from_slice(&pk(&self.key).0);
        for value in [self.grant, self.key_version, self.effective, self.expiry] {
            out.extend_from_slice(&value.to_be_bytes());
        }
        out
    }
    fn evaluator(&self, h: &MarketHeader) -> CodecResult<EvaluatorId> {
        derive_evaluator(h.market_id, p(self.principal)?, [self.nonce; 32])
    }
}
fn rotate_payload(
    evaluator: EvaluatorId,
    new_key: PublicKey32,
    key_version: u64,
    expected_grant: u64,
    effective: u64,
) -> Vec<u8> {
    let mut out = evaluator.as_bytes().to_vec();
    out.extend_from_slice(&new_key.0);
    for value in [key_version, expected_grant, effective] {
        out.extend_from_slice(&value.to_be_bytes());
    }
    out
}
fn revoke_payload(evaluator: EvaluatorId, grant: u64, reason: u16, evidence: u8) -> Vec<u8> {
    let mut out = evaluator.as_bytes().to_vec();
    out.extend_from_slice(&grant.to_be_bytes());
    out.extend_from_slice(&reason.to_be_bytes());
    out.extend_from_slice(&[evidence; 32]);
    out
}

fn encode_state(base: &[u8], identity: &[u8]) -> CodecResult<Vec<u8>> {
    let s = state::decode_shared_state(base)?;
    let mut next = s.replace_section(Section::IdentityRoster, identity)?;
    next.revision += 1;
    let mut out = vec![0u8; MAX_STATE_BYTES];
    let mut scratch = vec![0u8; Section::Control.payload_cap()];
    let n = state::encode_shared_state(&next, &mut out, &mut scratch)?;
    out.truncate(n);
    Ok(out)
}

struct Call {
    outcome: Outcome,
    env: Vec<u8>,
    event: Vec<u8>,
}

struct Market {
    header: MarketHeader,
    state: Vec<u8>,
    admission: AdmissionTable,
    sequence: u64,
    sealed: bool,
}
impl Market {
    fn new(workers: &[WorkerCurrent]) -> CodecResult<Self> {
        let mut replay = ReplayTable::new();
        replay.bind(ActorSlot::OWNER, p(OWNER)?, v(1)?)?;
        let mut table = WorkerTable::new();
        for w in workers {
            table.insert(w)?;
        }
        let mut section = vec![0u8; WORKER_TABLE_MAX_BYTES];
        let n = if workers.is_empty() {
            0
        } else {
            table.encode(&mut section)?
        };
        let mut sections: [&[u8]; 5] = [&[]; 5];
        sections[Section::IdentityRoster.index()] = &section[..n];
        let shared = SharedState {
            revision: 1,
            feature_sections: sections,
            control: Control {
                replay,
                feature_bytes: &[],
            },
        };
        let mut out = vec![0u8; MAX_STATE_BYTES];
        let mut scratch = vec![0u8; Section::Control.payload_cap()];
        let len = state::encode_shared_state(&shared, &mut out, &mut scratch)?;
        out.truncate(len);
        Ok(Self {
            header: header()?,
            state: out,
            admission: AdmissionTable::new(),
            sequence: 0,
            sealed: false,
        })
    }
    fn identity(&self) -> CodecResult<Vec<u8>> {
        let s = state::decode_shared_state(&self.state)?;
        Ok(s.section(Section::IdentityRoster)?.to_vec())
    }
    fn region(&self) -> CodecResult<EvaluatorRegion> {
        authority::evaluator_region(&self.identity()?)
    }
    fn revision(&self) -> CodecResult<u64> {
        Ok(state::decode_shared_state(&self.state)?.revision)
    }
    fn ctx(&self, height: u64) -> CodecResult<AuthorityContext<'_>> {
        Ok(AuthorityContext {
            market: &self.header,
            invoking_principal: p(OWNER)?,
            immediate_caller: Presence::Absent,
            height,
            approved_rubric: RubricDigest::new(RUBRIC)?,
            aggregate_sealed: self.sealed,
        })
    }
    /// Epoch and roster presence the current snapshot binds.
    fn binding(&self) -> CodecResult<(u64, bool)> {
        Ok(self
            .region()?
            .snapshot()
            .map_or((0, false), |s| (s.epoch, true)))
    }
    fn envelope(
        &self,
        op: Operation,
        sequence: u64,
        height: u64,
        payload: &[u8],
        (epoch, roster): (u64, bool),
    ) -> CodecResult<Vec<u8>> {
        let mut request = [1u8; 32];
        request[..2].copy_from_slice(&op.selector().to_be_bytes());
        request[2..10].copy_from_slice(&sequence.to_be_bytes());
        let env = Envelope {
            operation: op,
            chain: self.header.deployment_chain_domain,
            program: self.header.program_id,
            market: self.header.market_id,
            actor: p(OWNER)?,
            epoch,
            config: 1,
            roster: if roster {
                Presence::Present(RosterDigest::new(ROSTER)?)
            } else {
                Presence::Absent
            },
            sequence,
            expiry: height + 1000,
            request: RequestId::new(request)?,
            payload,
            authentication: Authentication::Native,
        };
        let mut out = vec![0u8; 16_384];
        let n = codec::encode_envelope(&env, &mut out)?;
        out.truncate(n);
        Ok(out)
    }
    fn apply(
        &self,
        ctx: &AuthorityContext<'_>,
        env: &[u8],
    ) -> CodecResult<(Outcome, Vec<u8>, Vec<u8>)> {
        let mut scratch = vec![0u8; SCRATCH_BYTES];
        let mut out = vec![0u8; MAX_STATE_BYTES];
        let mut event = vec![0u8; 2048];
        let outcome = authority::apply(&self.state, ctx, env, &mut scratch, &mut out, &mut event)?;
        if let Outcome::Applied {
            state_len,
            event_len,
        } = outcome
        {
            out.truncate(state_len);
            event.truncate(event_len);
        } else {
            out.clear();
            event.clear();
        }
        Ok((outcome, out, event))
    }
    /// Owner call at the next sequence; commits state only on `Applied`.
    fn call(&mut self, op: Operation, height: u64, payload: &[u8]) -> CodecResult<Call> {
        let env = self.envelope(op, self.sequence + 1, height, payload, self.binding()?)?;
        let ctx = self.ctx(height)?;
        let (outcome, state, event) = self.apply(&ctx, &env)?;
        if matches!(outcome, Outcome::Applied { .. }) {
            self.state = state;
            self.sequence += 1;
        }
        Ok(Call {
            outcome,
            env,
            event,
        })
    }
    fn schedule(&mut self, n: &Nomination, height: u64) -> CodecResult<Call> {
        self.call(dispatch::ScheduleEvaluator, height, &n.payload())
    }
    /// Market-owner approval of the stored pending grant plus the unsigned consent.
    fn consent(
        &mut self,
        n: &Nomination,
        height: u64,
    ) -> CodecResult<(EvaluatorGrant, EvaluatorConsent)> {
        let grant = self.region()?.stored_grant(n.evaluator(&self.header)?)?;
        let admin = AdmissionContext {
            market: &self.header,
            invoking_principal: p(OWNER)?,
            height,
        };
        let effective = self.admission.required_effective_epoch(&admin)?;
        let expiry_height = self.header.origin_height + effective * 128 + 64;
        let request = RequestId::new([n.nonce; 32])?;
        let approval_digest = self.admission.approve(
            &admin,
            &ApprovalTerms {
                participant: Participant::Evaluator(grant.evaluator),
                owner: grant.principal,
                enrollment_nonce_commitment: Digest32::new([7; 32])?,
                delegate: grant.signing_key,
                delegate_generation: grant.key_version.get(),
                identity_commitment: Digest32::new([9; 32])?,
                effective_epoch: effective,
                config_version: 1,
                request,
                expiry_height,
            },
        )?;
        let consent = EvaluatorConsent {
            chain: self.header.deployment_chain_domain,
            program: self.header.program_id,
            market: self.header.market_id,
            evaluator: grant.evaluator,
            owner: grant.principal,
            signing_key: grant.signing_key,
            enrollment_nonce: [n.nonce; 32],
            rubric: grant.rubric,
            approval_digest,
            request,
            grant_version: grant.grant_version.get(),
            key_version: grant.key_version.get(),
            effective_epoch: grant.effective_epoch,
            config_version: 1,
            expiry_height,
        };
        Ok((grant, consent))
    }
    fn admit(
        &mut self,
        grant: &EvaluatorGrant,
        who: PrincipalId,
        height: u64,
        consent: &EvaluatorConsent,
        signer: &SigningKey,
    ) -> CodecResult<AdmissionMeta> {
        let ctx = AdmissionContext {
            market: &self.header,
            invoking_principal: who,
            height,
        };
        admit_evaluator(
            &mut self.admission,
            &ctx,
            grant,
            consent.request,
            &sign(consent, signer)?,
        )
    }
    fn accept(&mut self, n: &Nomination, height: u64) -> CodecResult<AdmissionMeta> {
        let (grant, consent) = self.consent(n, height)?;
        self.admit(&grant, grant.principal, height, &consent, &n.key)
    }
    /// F08 rollover then the F03 snapshot; nothing is committed on refusal.
    fn open(&mut self, epoch: u64, workers: &[WorkerRosterEntry]) -> CodecResult<OpenedSnapshot> {
        let mut admission = self.admission;
        roster::open_epoch(&mut admission, epoch, |_: &AdmissionMeta| false)?;
        let mut next = vec![0u8; Section::IdentityRoster.payload_cap()];
        let opened = authority::open_epoch(
            &self.identity()?,
            &SnapshotContext {
                market: &self.header,
                epoch,
                approved_rubric: RubricDigest::new(RUBRIC)?,
                workers,
                admission: &admission,
            },
            &mut next,
        )?;
        self.state = encode_state(&self.state, &next[..opened.section_len])?;
        self.admission = admission;
        Ok(opened)
    }
}

fn sign(consent: &EvaluatorConsent, signer: &SigningKey) -> CodecResult<Vec<u8>> {
    let mut buf = [0u8; 362];
    consent.encode(&mut buf)?;
    let mut payload = buf.to_vec();
    payload.extend_from_slice(&signer.sign(consent.digest()?.as_bytes()).to_bytes());
    Ok(payload)
}

/// Markets with `count` accepted evaluators frozen into the epoch 0 snapshot.
fn active(count: u8) -> CodecResult<(Market, Vec<Nomination>)> {
    let mut m = Market::new(&[])?;
    let mut nominations = Vec::new();
    for i in 0..count {
        let n = nomination(i, 0, 32);
        m.schedule(&n, 1 + u64::from(i))?;
        m.accept(&n, 10 + u64::from(i))?;
        nominations.push(n);
    }
    let opened = m.open(0, &[])?;
    assert_eq!(opened.activated, count);
    assert_eq!(opened.members, count);
    Ok((m, nominations))
}

fn applied(call: &Call) -> usize {
    match call.outcome {
        Outcome::Applied { event_len, .. } => event_len,
        other => panic!("expected Applied, got {other:?}"),
    }
}
fn suffix(call: &Call, n: usize) -> &[u8] {
    &call.event[call.event.len() - n..]
}

fn binding(h: &MarketHeader, epoch: u64, grant: &EvaluatorGrant) -> CodecResult<EvaluatorBinding> {
    Ok(EvaluatorBinding {
        frozen: FrozenBinding {
            chain: h.deployment_chain_domain,
            program: h.program_id,
            market: h.market_id,
            epoch,
            config: v(1)?,
            roster: RosterDigest::new(ROSTER)?,
        },
        evaluator: grant.evaluator,
        grant: grant.grant_version,
        key_version: grant.key_version,
    })
}
fn report_check(
    h: &MarketHeader,
    epoch: u64,
    frozen: EvaluatorGrant,
    live: EvaluatorGrant,
) -> TestResult {
    let bound = binding(h, epoch, &frozen)?;
    let scores = [ScoreEntry {
        worker: WorkerId::new([1; 32])?,
        score: Score::new(5)?,
    }];
    let body = ReportBody {
        binding: bound,
        evidence: EvidenceRoot::new([44; 32])?,
        scores: ScoreVector::Typed(&scores),
    };
    check_report_context(
        &body,
        &ReportContext {
            binding: bound,
            frozen_grant: frozen,
            live_grant: live,
            approved_rubric: RubricDigest::new(RUBRIC)?,
            market_owner: h.owner_principal,
            workers: &[],
            evidence: Presence::Absent,
        },
    )
}

fn worker_current(h: &MarketHeader, owner: u8) -> CodecResult<WorkerCurrent> {
    Ok(WorkerCurrent {
        worker: derive_worker(h.market_id, p(owner)?, [3; 32])?,
        owner: p(owner)?,
        delegate: pk(&key(150)),
        metadata: MetadataDigest::new([30; 32])?,
        generation: 1,
        key_version: 1,
        metadata_revision: 1,
        valid_from: 0,
        expiry: 400,
        revocation_sequence: 0,
        effective_epoch: 0,
        last_sequence: 0,
        last_request_id: [0; 32],
        last_request_digest: [0; 32],
        last_result_digest: [0; 32],
        state: WorkerState::Available,
        slot: 0,
        last_metadata_height: 0,
    })
}
fn worker_entry(owner: u8) -> CodecResult<WorkerRosterEntry> {
    Ok(WorkerRosterEntry {
        worker: WorkerId::new([owner; 32])?,
        owner: p(owner)?,
        recipient: AccountId::new([owner; 32])?,
        generation: v(1)?,
        key_version: v(1)?,
        public_key: pk(&key(151)),
        metadata: MetadataDigest::new([30; 32])?,
    })
}

#[test]
fn a16_nomination_stays_pending_until_f08_acceptance() -> TestResult {
    let mut m = Market::new(&[])?;
    let silent = nomination(0, 0, 32);
    let call = m.schedule(&silent, 5)?;
    let id = silent.evaluator(&m.header)?;
    assert_eq!(applied(&call), call.event.len());
    let mut expected = id.as_bytes().to_vec();
    for value in [1u64, 1, 0, 32] {
        expected.extend_from_slice(&value.to_be_bytes());
    }
    assert_eq!(suffix(&call, 64), &expected[..]);
    assert_eq!(m.region()?.stored_grant(id)?.status, GrantStatus::Pending);

    let opened = m.open(0, &[])?;
    assert_eq!((opened.activated, opened.members), (0, 0));
    assert_eq!(m.region()?.stored_grant(id)?.status, GrantStatus::Pending);
    assert_eq!(m.region()?.frozen_grant(id), Err(F03_NO_GRANT));

    let n = nomination(1, 1, 32);
    m.schedule(&n, 20)?;
    let (grant, consent) = m.consent(&n, 21)?;
    let before = m.admission;
    let region = m.region()?;
    assert_eq!(
        m.admit(&grant, p(99)?, 22, &consent, &n.key),
        Err(F08_OWNER_REQUIRED)
    );
    assert_eq!(
        m.admit(&grant, grant.principal, 22, &consent, &key(200)),
        Err(F08_BAD_CONSENT)
    );
    assert_eq!(m.admission, before);
    assert_eq!(m.region()?, region);
    let meta = m.admit(&grant, grant.principal, 22, &consent, &n.key)?;
    assert!(meta.admitted());
    assert_eq!(meta.approval, None);
    assert_eq!(meta.delegate_generation, 1);
    let id = n.evaluator(&m.header)?;
    assert_eq!(m.region()?.stored_grant(id)?.status, GrantStatus::Pending);

    let opened = m.open(1, &[])?;
    assert_eq!((opened.activated, opened.members), (1, 1));
    let region = m.region()?;
    assert_eq!(region.stored_grant(id)?.status, GrantStatus::Active);
    assert_eq!(region.frozen_grant(id)?, region.stored_grant(id)?);
    assert_eq!(
        region.stored_grant(silent.evaluator(&m.header)?)?.status,
        GrantStatus::Pending
    );

    m.header.lifecycle = 3;
    let late = nomination(2, 2, 32);
    assert_eq!(m.schedule(&late, 140).map(|c| c.outcome), Err(WRONG_PHASE));
    let rotate = rotate_payload(id, pk(&key(201)), 2, 1, 2);
    assert_eq!(
        m.call(dispatch::RotateEvaluatorKey, 140, &rotate)
            .map(|c| c.outcome),
        Err(WRONG_PHASE)
    );
    let frozen = m.region()?.frozen_grant(id)?;
    let revoke = revoke_payload(silent.evaluator(&m.header)?, 1, 4, 51);
    applied(&m.call(dispatch::RevokeEvaluator, 140, &revoke)?);
    assert_eq!(m.region()?.frozen_grant(id)?, frozen);
    assert_eq!(m.region()?.stored_grant(id)?.status, GrantStatus::Active);
    Ok(())
}

#[test]
fn a06_rotation_is_staged_for_the_next_epoch() -> TestResult {
    let mut m = Market::new(&[])?;
    let mut n = nomination(0, 0, 32);
    n.grant = 7;
    n.key_version = 4;
    m.schedule(&n, 1)?;
    m.accept(&n, 10)?;
    m.open(0, &[])?;
    let id = n.evaluator(&m.header)?;
    let entry = m.region()?.snapshot().and_then(|s| s.get(id));
    assert_eq!(
        entry.map(|f| (f.entry.grant.get(), f.entry.key_version.get())),
        Some((7, 4))
    );

    let next_key = pk(&key(200));
    let call = m.call(
        dispatch::RotateEvaluatorKey,
        20,
        &rotate_payload(id, next_key, 5, 7, 1),
    )?;
    let mut expected = id.as_bytes().to_vec();
    expected.extend_from_slice(&5u64.to_be_bytes());
    expected.extend_from_slice(&1u64.to_be_bytes());
    assert_eq!(applied(&call), call.event.len());
    assert_eq!(suffix(&call, 48), &expected[..]);
    let region = m.region()?;
    let frozen = region.frozen_grant(id)?;
    assert_eq!(frozen.key_version.get(), 4);
    assert_eq!(region.stored_grant(id)?.key_version.get(), 4);
    let staged = region.get(id).and_then(|r| r.rekey);
    assert_eq!(
        staged.map(|k| (k.key, k.key_version.get(), k.effective_epoch)),
        Some((next_key, 5, 1))
    );
    let mut report = binding(&m.header, 0, &frozen)?;
    report.key_version = v(5)?;
    assert_eq!(
        check_binding(&report, &binding(&m.header, 0, &frozen)?),
        Err(F03_KEY_VERSION_CONFLICT)
    );

    let refusals = [
        (
            rotate_payload(id, pk(&key(201)), 5, 7, 1),
            F03_KEY_VERSION_CONFLICT,
        ),
        (rotate_payload(id, next_key, 6, 7, 1), F03_KEY_REUSED),
        (rotate_payload(id, pk(&n.key), 6, 7, 1), F03_KEY_REUSED),
        (
            rotate_payload(id, pk(&key(201)), 6, 6, 1),
            F03_GRANT_VERSION_CONFLICT,
        ),
        (
            rotate_payload(id, pk(&key(201)), 6, 7, 0),
            F03_BAD_ACTIVATION,
        ),
        (
            rotate_payload(id, pk(&key(201)), 6, 7, 2),
            F03_BAD_ACTIVATION,
        ),
        (
            rotate_payload(EvaluatorId::new([77; 32])?, pk(&key(201)), 6, 7, 1),
            F03_NO_GRANT,
        ),
        (
            rotate_payload(id, PublicKey32([0; 32]), 6, 7, 1),
            NON_CANONICAL,
        ),
    ];
    let state = m.state.clone();
    for (payload, error) in refusals {
        assert_eq!(
            m.call(dispatch::RotateEvaluatorKey, 21, &payload)
                .map(|c| c.outcome),
            Err(error)
        );
    }
    assert_eq!(m.state, state);

    let opened = m.open(1, &[])?;
    assert_eq!((opened.rekeyed, opened.members), (1, 1));
    let region = m.region()?;
    let frozen = region.frozen_grant(id)?;
    assert_eq!(
        (frozen.key_version.get(), frozen.signing_key),
        (5, next_key)
    );
    assert_eq!(region.get(id).and_then(|r| r.rekey), None);
    let mut report = binding(&m.header, 1, &frozen)?;
    report.key_version = v(5)?;
    assert_eq!(
        check_binding(&report, &binding(&m.header, 1, &frozen)?),
        Ok(())
    );

    Ok(())
}

#[test]
fn rotation_of_a_pending_grant_is_wrong_phase() -> TestResult {
    let mut m = Market::new(&[])?;
    let pending = nomination(1, 0, 32);
    m.schedule(&pending, 1)?;
    let pending_id = pending.evaluator(&m.header)?;
    assert_eq!(
        m.call(
            dispatch::RotateEvaluatorKey,
            2,
            &rotate_payload(pending_id, pk(&key(202)), 2, 1, 0)
        )
        .map(|c| c.outcome),
        Err(WRONG_PHASE)
    );
    Ok(())
}

#[test]
fn a07_revocation_before_seal_excludes_the_frozen_epoch() -> TestResult {
    let (mut m, ns) = active(1)?;
    let id = ns[0].evaluator(&m.header)?;
    let frozen = m.region()?.frozen_grant(id)?;
    let call = m.call(
        dispatch::RevokeEvaluator,
        100,
        &revoke_payload(id, 1, 1, 50),
    )?;
    let mut expected = id.as_bytes().to_vec();
    expected.extend_from_slice(&1u64.to_be_bytes());
    expected.extend_from_slice(&1u16.to_be_bytes());
    expected.extend_from_slice(&[50; 32]);
    expected.push(1);
    assert_eq!(applied(&call), call.event.len());
    assert_eq!(suffix(&call, 75), &expected[..]);

    let region = m.region()?;
    assert!(region.excluded(id));
    assert_eq!(region.frozen_grant(id)?, frozen);
    let live = region.stored_grant(id)?;
    assert_eq!(live.status, GrantStatus::Revoked);
    assert_eq!(
        region.get(id).and_then(|r| r.revocation),
        Some(Revocation {
            reason: RevocationReason::CompromisedKey,
            evidence: Digest32::new([50; 32])?,
            height: 100,
            excludes_frozen_epoch: true,
        })
    );
    assert_eq!(report_check(&m.header, 0, frozen, live), Err(REVOKED));

    let state = m.state.clone();
    let repeat = m.call(
        dispatch::RevokeEvaluator,
        101,
        &revoke_payload(id, 1, 1, 50),
    )?;
    assert_eq!(repeat.outcome, Outcome::Idempotent);
    assert!(repeat.event.is_empty());
    assert_eq!(
        m.call(
            dispatch::RevokeEvaluator,
            101,
            &revoke_payload(id, 1, 1, 51)
        )
        .map(|c| c.outcome),
        Err(REVOKED)
    );
    assert_eq!(
        m.call(
            dispatch::RevokeEvaluator,
            101,
            &revoke_payload(id, 1, 2, 50)
        )
        .map(|c| c.outcome),
        Err(REVOKED)
    );
    assert_eq!(
        m.call(
            dispatch::RotateEvaluatorKey,
            101,
            &rotate_payload(id, pk(&key(200)), 2, 1, 1)
        )
        .map(|c| c.outcome),
        Err(REVOKED)
    );
    assert_eq!(m.state, state);

    let ctx = m.ctx(102)?;
    match m.apply(&ctx, &call.env)?.0 {
        Outcome::AlreadyApplied(last) => {
            assert_eq!(last.sequence, 2);
            assert_eq!(last.applied_revision, m.revision()?);
            assert_eq!(last.result_digest, codec::result_digest(&expected)?);
        }
        other => panic!("expected AlreadyApplied, got {other:?}"),
    }
    let conflicting = m.envelope(
        dispatch::RevokeEvaluator,
        2,
        100,
        &revoke_payload(id, 1, 3, 50),
        m.binding()?,
    )?;
    assert_eq!(
        m.apply(&ctx, &conflicting).map(|r| r.0),
        Err(REPLAY_CONFLICT)
    );

    let opened = m.open(1, &[])?;
    assert_eq!((opened.pruned, opened.members), (1, 0));
    assert_eq!(m.region()?.stored_grant(id), Err(F03_NO_GRANT));
    Ok(())
}

#[test]
fn a08_revocation_after_seal_is_prospective() -> TestResult {
    let (mut m, ns) = active(1)?;
    let id = ns[0].evaluator(&m.header)?;
    let frozen = m.region()?.frozen_grant(id)?;
    m.sealed = true;
    let call = m.call(
        dispatch::RevokeEvaluator,
        101,
        &revoke_payload(id, 1, 2, 50),
    )?;
    applied(&call);
    assert_eq!(suffix(&call, 1), &[0]);
    let region = m.region()?;
    assert!(!region.excluded(id));
    assert_eq!(region.frozen_grant(id)?, frozen);
    assert_eq!(region.stored_grant(id)?.status, GrantStatus::Revoked);

    let opened = m.open(1, &[])?;
    assert_eq!((opened.pruned, opened.members), (1, 0));
    let region = m.region()?;
    assert_eq!(region.snapshot().map(|s| (s.epoch, s.len())), Some((1, 0)));
    assert_eq!(region.frozen_grant(id), Err(F03_NO_GRANT));
    Ok(())
}

#[test]
fn a09_worker_principal_conflicts() -> TestResult {
    let h = header()?;
    let worker = worker_current(&h, 30)?;
    let mut m = Market::new(&[worker])?;
    assert_eq!(
        m.schedule(&nomination(0, 0, 32), 5).map(|c| c.outcome),
        Err(ROLE_CONFLICT)
    );
    applied(&m.schedule(&nomination(1, 0, 32), 5)?);
    let section = m.identity()?;
    let (workers, _) = authority::split_identity_section(&section)?;
    let table = WorkerTable::decode(workers)?;
    assert_eq!(table.iter().copied().collect::<Vec<_>>(), vec![worker]);
    assert_eq!(m.region()?.len(), 1);

    let (mut m, _) = active(1)?;
    let state = m.state.clone();
    let admission = m.admission;
    assert_eq!(m.open(1, &[worker_entry(30)?]), Err(ROLE_CONFLICT));
    assert_eq!(m.state, state);
    assert_eq!(m.admission, admission);
    assert_eq!(m.open(1, &[worker_entry(31)?])?.members, 1);
    Ok(())
}

#[test]
fn a10_market_owner_cannot_evaluate() -> TestResult {
    let mut m = Market::new(&[])?;
    let mut owner = nomination(0, 0, 32);
    owner.principal = OWNER;
    assert_eq!(m.schedule(&owner, 5).map(|c| c.outcome), Err(ROLE_CONFLICT));

    let grant = EvaluatorGrant::nominate(
        m.header.market_id,
        p(OWNER)?,
        [owner.nonce; 32],
        GrantTerms {
            rubric: RubricDigest::new(RUBRIC)?,
            grant_version: v(1)?,
            key_version: v(1)?,
            signing_key: pk(&owner.key),
            effective_epoch: 0,
            expiry_epoch_exclusive: 32,
        },
    )?;
    let mut region = EvaluatorRegion::new();
    region.insert(&EvaluatorRecord {
        grant,
        last: LastRequest {
            sequence: 1,
            request: RequestId::new([1; 32])?,
            digest: RequestDigest::new([2; 32])?,
            result: ResultDigest::new([3; 32])?,
        },
        rekey: None,
        revocation: None,
    })?;
    let mut section = vec![0u8; 1 + region.encoded_len()];
    let n = region.encode(&mut section[1..])?;
    assert_eq!(n, 1 + 356 + 1);
    m.state = encode_state(&m.state, &section)?;
    m.admission.insert(AdmissionMeta {
        participant: Participant::Evaluator(grant.evaluator),
        owner: p(OWNER)?,
        admitted_epoch: None,
        last_heartbeat_epoch: None,
        last_heartbeat_height: None,
        immunity_until_epoch: 0,
        pending_exit: None,
        membership_generation: 1,
        complete_missed_opened_epochs: 0,
        membership_flags: FLAG_ADMITTED,
        delegate_generation: 1,
        admission_height: Some(0),
        approval: None,
    })?;
    let state = m.state.clone();
    assert_eq!(m.open(0, &[]), Err(ROLE_CONFLICT));
    assert_eq!(m.state, state);
    assert_eq!(m.admission.current_epoch(), None);
    Ok(())
}

#[test]
fn a13_expiry_is_half_open_and_running_snapshot_is_fixed() -> TestResult {
    let mut m = Market::new(&[])?;
    let n = nomination(0, 0, 7);
    m.schedule(&n, 1)?;
    m.accept(&n, 10)?;
    m.open(0, &[])?;
    let id = n.evaluator(&m.header)?;
    assert_eq!(m.open(6, &[])?.members, 1);
    let frozen = m.region()?.frozen_grant(id)?;
    assert_eq!(frozen.expiry_epoch_exclusive, 7);
    let snapshot = m.region()?.snapshot().copied();

    let height = 6 * 128 + 10;
    assert_eq!(
        m.schedule(&nomination(1, 6, 9), height).map(|c| c.outcome),
        Err(F03_BAD_ACTIVATION)
    );
    assert_eq!(
        m.call(
            dispatch::RotateEvaluatorKey,
            height,
            &rotate_payload(id, pk(&key(200)), 2, 1, 7)
        )
        .map(|c| c.outcome),
        Err(F03_BAD_ACTIVATION)
    );
    applied(&m.schedule(&nomination(1, 7, 9), height)?);
    assert_eq!(m.region()?.snapshot().copied(), snapshot);
    assert_eq!(report_check(&m.header, 7, frozen, frozen), Err(EXPIRED));

    let opened = m.open(7, &[])?;
    assert_eq!((opened.expired, opened.members), (1, 0));
    let region = m.region()?;
    assert_eq!(region.stored_grant(id)?.status, GrantStatus::Expired);
    assert_eq!(region.frozen_grant(id), Err(F03_NO_GRANT));
    assert_eq!(
        region
            .stored_grant(nomination(1, 7, 9).evaluator(&m.header)?)?
            .status,
        GrantStatus::Pending
    );
    Ok(())
}

#[test]
fn schedule_refusals_bounds_and_replay() -> TestResult {
    let mut m = Market::new(&[])?;
    let n = nomination(0, 0, 32);
    let mut bad_rubric = n.clone();
    bad_rubric.rubric = [8; 32];
    let mut zero_nonce = n.clone();
    zero_nonce.nonce = 0;
    let refusals = [
        (bad_rubric, WRONG_CONFIG),
        (zero_nonce, NON_CANONICAL),
        (nomination(0, 0, 0), F03_BAD_ACTIVATION),
        (nomination(0, 0, 33), F03_BAD_ACTIVATION),
        (nomination(0, 1, 32), F03_BAD_ACTIVATION),
    ];
    for (payload, error) in refusals {
        assert_eq!(m.schedule(&payload, 5).map(|c| c.outcome), Err(error));
    }
    let env = m.envelope(dispatch::ScheduleEvaluator, 1, 5, &n.payload(), (0, false))?;
    let mut ctx = m.ctx(5)?;
    ctx.invoking_principal = p(30)?;
    assert_eq!(m.apply(&ctx, &env).map(|r| r.0), Err(UNAUTHORIZED));
    let mut ctx = m.ctx(5)?;
    ctx.immediate_caller = Presence::Present(ProgramId::new([40; 32])?);
    assert_eq!(m.apply(&ctx, &env).map(|r| r.0), Err(UNAUTHORIZED));
    let mut moved = m.header;
    moved.active_config_version = 2;
    let ctx = AuthorityContext {
        market: &moved,
        ..m.ctx(5)?
    };
    assert_eq!(m.apply(&ctx, &env).map(|r| r.0), Err(WRONG_CONFIG));
    let gap = m.envelope(dispatch::ScheduleEvaluator, 2, 5, &n.payload(), (0, false))?;
    assert_eq!(m.apply(&m.ctx(5)?, &gap).map(|r| r.0), Err(SEQUENCE_GAP));
    let challenge = m.envelope(dispatch::ChallengeAssessment, 0, 5, &[1; 97], (0, true))?;
    assert_eq!(
        m.apply(&m.ctx(5)?, &challenge).map(|r| r.0),
        Err(UNKNOWN_OPERATION)
    );

    applied(&m.schedule(&n, 5)?);
    assert_eq!(m.revision()?, 2);
    let mut twin = nomination(1, 0, 32);
    twin.principal = n.principal;
    assert_eq!(m.schedule(&twin, 6).map(|c| c.outcome), Err(CONFLICT));
    assert_eq!(m.schedule(&n, 6).map(|c| c.outcome), Err(CONFLICT));
    let mut shared = nomination(1, 0, 32);
    shared.key = n.key.clone();
    assert_eq!(
        m.schedule(&shared, 6).map(|c| c.outcome),
        Err(F03_KEY_REUSED)
    );
    for i in 1..8 {
        applied(&m.schedule(&nomination(i, 0, 32), 6 + u64::from(i))?);
    }
    assert_eq!(m.region()?.len(), 8);
    assert_eq!(
        m.schedule(&nomination(8, 0, 32), 20).map(|c| c.outcome),
        Err(F03_EVALUATOR_CAPACITY)
    );

    m.open(0, &[])?;
    let id = n.evaluator(&m.header)?;
    let revoke = revoke_payload(id, 1, 1, 50);
    let wrong_epoch = m.envelope(
        dispatch::RevokeEvaluator,
        m.sequence + 1,
        140,
        &revoke,
        (1, true),
    )?;
    assert_eq!(
        m.apply(&m.ctx(140)?, &wrong_epoch).map(|r| r.0),
        Err(WRONG_EPOCH)
    );
    for (payload, error) in [
        (revoke_payload(id, 1, 5, 50), NON_CANONICAL),
        (revoke_payload(id, 1, 1, 0), NON_CANONICAL),
        (revoke_payload(id, 2, 1, 50), F03_GRANT_VERSION_CONFLICT),
        (
            revoke_payload(EvaluatorId::new([77; 32])?, 1, 1, 50),
            F03_NO_GRANT,
        ),
    ] {
        assert_eq!(
            m.call(dispatch::RevokeEvaluator, 140, &payload)
                .map(|c| c.outcome),
            Err(error)
        );
    }
    m.header.lifecycle = 5;
    assert_eq!(
        m.call(dispatch::RevokeEvaluator, 140, &revoke)
            .map(|c| c.outcome),
        Err(WRONG_PHASE)
    );
    m.header.lifecycle = 4;
    applied(&m.call(dispatch::RevokeEvaluator, 140, &revoke)?);
    Ok(())
}

#[test]
fn region_codec_is_strict() -> TestResult {
    let (m, ns) = active(1)?;
    let section = m.identity()?;
    let (workers, bytes) = authority::split_identity_section(&section)?;
    assert_eq!(workers, &[0]);
    assert_eq!(bytes.len(), 1 + 356 + 1 + 8 + 1 + 152);
    let region = EvaluatorRegion::decode(bytes)?;
    let mut out = vec![0u8; bytes.len()];
    assert_eq!(region.encode(&mut out)?, bytes.len());
    assert_eq!(out, bytes);
    let id = ns[0].evaluator(&m.header)?;
    assert_eq!(region.snapshot().map(|s| (s.epoch, s.len())), Some((0, 1)));
    assert_eq!(region.get(id).map(|r| r.last.sequence), Some(1));

    let mut trailing = bytes.to_vec();
    trailing.push(0);
    assert_eq!(EvaluatorRegion::decode(&trailing), Err(NON_CANONICAL));
    assert_eq!(EvaluatorRegion::decode(&[0, 0]), Err(NON_CANONICAL));
    assert_eq!(EvaluatorRegion::decode(&[])?, EvaluatorRegion::new());
    assert_eq!(EvaluatorRegion::new().encode(&mut [])?, 0);
    let mut orphan = bytes.to_vec();
    orphan[0] = 0;
    assert_eq!(
        EvaluatorRegion::decode(&[&orphan[..1], &orphan[1 + 356..]].concat()),
        Err(NON_CANONICAL)
    );
    assert_eq!(
        authority::split_identity_section(&[1, 0, 0]),
        Err(NON_CANONICAL)
    );
    Ok(())
}
