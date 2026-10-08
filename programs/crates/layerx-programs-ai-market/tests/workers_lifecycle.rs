use ed25519_dalek::{Signer, SigningKey};
use layerx_programs_ai_market::{
    codec::{self, derive_market, derive_worker, Envelope},
    dispatch::{self, Operation},
    errors::*,
    registry::*,
    state::{self, ActorSlot, Control, ReplayTable, Section, SharedState},
    types::*,
    workers::*,
    MAX_STATE_BYTES,
};

const OWNER: [u8; 32] = [12; 32];
const NOMINEE: [u8; 32] = [20; 32];
const NONCE: [u8; 32] = [21; 32];

enum Failure {
    Application(ApplicationError),
    Conversion(core::num::TryFromIntError),
    Unexpected(&'static str),
}
impl core::fmt::Debug for Failure {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self {
            Self::Application(error) => write!(f, "application refusal {error:?}"),
            Self::Conversion(error) => write!(f, "integer conversion {error}"),
            Self::Unexpected(what) => write!(f, "unexpected {what}"),
        }
    }
}
impl From<ApplicationError> for Failure {
    fn from(error: ApplicationError) -> Self {
        Self::Application(error)
    }
}
impl From<core::num::TryFromIntError> for Failure {
    fn from(error: core::num::TryFromIntError) -> Self {
        Self::Conversion(error)
    }
}
type Checked<T = ()> = Result<T, Failure>;

fn header() -> Checked<MarketHeader> {
    let chain = ChainDomain::new([10; 32])?;
    let program = ProgramId::new([11; 32])?;
    let asset = AssetId::new([13; 32])?;
    Ok(MarketHeader {
        format_version: 1,
        market_id: derive_market(chain, program)?,
        deployment_chain_domain: chain,
        program_id: program,
        owner_principal: PrincipalId::new(OWNER)?,
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

fn p(b: [u8; 32]) -> CodecResult<PrincipalId> {
    PrincipalId::new(b)
}
fn key(seed: u8) -> SigningKey {
    SigningKey::from_bytes(&[seed; 32])
}
fn pk(k: &SigningKey) -> PublicKey32 {
    PublicKey32(k.verifying_key().to_bytes())
}

fn state_with(records: &[WorkerCurrent]) -> Checked<Vec<u8>> {
    let mut replay = ReplayTable::new();
    replay.bind(ActorSlot::OWNER, p(OWNER)?, Version::new(1)?)?;
    let mut table = WorkerTable::new();
    for r in records {
        replay.bind(
            ActorSlot::worker(usize::from(r.slot))?,
            r.owner,
            Version::new(1)?,
        )?;
        table.insert(r)?;
    }
    let mut section = vec![0u8; WORKER_TABLE_MAX_BYTES];
    let n = if records.is_empty() {
        0
    } else {
        table.encode(&mut section)?
    };
    let shared = SharedState {
        revision: 1,
        feature_sections: [&[], &section[..n], &[], &[], &[]],
        control: Control {
            replay,
            feature_bytes: &[],
        },
    };
    let mut out = vec![0u8; MAX_STATE_BYTES];
    let mut scratch = vec![0u8; 24_576];
    let len = state::encode_shared_state(&shared, &mut out, &mut scratch)?;
    out.truncate(len);
    Ok(out)
}

fn table_of(state_bytes: &[u8]) -> Checked<WorkerTable> {
    let s = state::decode_shared_state(state_bytes)?;
    Ok(WorkerTable::decode(s.section(Section::IdentityRoster)?)?)
}

fn envelope(
    h: &MarketHeader,
    selector: u16,
    actor: [u8; 32],
    sequence: u64,
    height: u64,
    payload: &[u8],
    delegate: Option<&SigningKey>,
) -> Checked<Vec<u8>> {
    let mut request = [1u8; 32];
    request[..2].copy_from_slice(&selector.to_be_bytes());
    request[2..10].copy_from_slice(&sequence.to_be_bytes());
    let mut env = Envelope {
        operation: Operation::decode(selector)?,
        chain: h.deployment_chain_domain,
        program: h.program_id,
        market: h.market_id,
        actor: p(actor)?,
        epoch: 0,
        config: 1,
        roster: Presence::Absent,
        sequence,
        expiry: height + 1000,
        request: RequestId::new(request)?,
        payload,
        authentication: Authentication::Native,
    };
    let mut out = vec![0u8; 16_384];
    if let Some(k) = delegate {
        env.authentication = Authentication::Delegate {
            key: pk(k),
            signature: Signature64([0; 64]),
        };
        let n = codec::encode_envelope(&env, &mut out)?;
        let digest = codec::decode_envelope(&out[..n])?.request_digest()?;
        env.authentication = Authentication::Delegate {
            key: pk(k),
            signature: Signature64(k.sign(digest.as_bytes()).to_bytes()),
        };
    }
    let n = codec::encode_envelope(&env, &mut out)?;
    out.truncate(n);
    Ok(out)
}

struct Call {
    state: Vec<u8>,
    event: Vec<u8>,
}
fn call(
    state_bytes: &[u8],
    h: &MarketHeader,
    invoker: [u8; 32],
    height: u64,
    env: &[u8],
) -> CodecResult<(Applied, Call)> {
    let ctx = CallContext {
        market: h,
        invoking_principal: p(invoker)?,
        height,
    };
    let mut out = vec![0u8; MAX_STATE_BYTES];
    let mut event = vec![0u8; 2048];
    let mut control = vec![0u8; CONTROL_SCRATCH_BYTES];
    let applied = apply(state_bytes, &ctx, env, &mut out, &mut event, &mut control)?;
    if let Applied::Applied {
        state_len,
        event_len,
    } = applied
    {
        out.truncate(state_len);
        event.truncate(event_len);
    } else {
        out.clear();
        event.clear();
    }
    Ok((applied, Call { state: out, event }))
}

fn enroll_payload(delegate: PublicKey32, metadata: [u8; 32], expiry: u64) -> Vec<u8> {
    let mut v = Vec::new();
    v.extend_from_slice(&NOMINEE);
    v.extend_from_slice(&NONCE);
    v.extend_from_slice(&delegate.0);
    v.extend_from_slice(&metadata);
    v.extend_from_slice(&expiry.to_be_bytes());
    v
}

fn accept_payload(
    h: &MarketHeader,
    record: &WorkerCurrent,
    delegate: &SigningKey,
) -> Checked<Vec<u8>> {
    let consent = consent_digest(
        h,
        record.worker,
        record.owner,
        record.delegate,
        1,
        1,
        record.metadata,
        record.expiry,
    )?;
    let mut v = Vec::new();
    v.extend_from_slice(record.worker.as_bytes());
    v.extend_from_slice(&1u64.to_be_bytes());
    v.extend_from_slice(&1u64.to_be_bytes());
    v.extend_from_slice(record.metadata.as_bytes());
    v.extend_from_slice(&delegate.sign(consent.as_bytes()).to_bytes());
    Ok(v)
}

fn record(slot: u8, delegate: &SigningKey) -> Checked<WorkerCurrent> {
    let h = header()?;
    Ok(WorkerCurrent {
        worker: derive_worker(h.market_id, p(NOMINEE)?, [slot + 1; 32])?,
        owner: p(NOMINEE)?,
        delegate: pk(delegate),
        metadata: MetadataDigest::new([30; 32])?,
        generation: 2,
        key_version: 2,
        metadata_revision: 4,
        valid_from: 200,
        expiry: 400,
        revocation_sequence: 0,
        effective_epoch: 1,
        last_sequence: 0,
        last_request_id: [0; 32],
        last_request_digest: [0; 32],
        last_result_digest: [0; 32],
        state: WorkerState::Available,
        slot,
        last_metadata_height: 200,
    })
}

fn frozen(r: &WorkerCurrent) -> Checked<WorkerRosterEntry> {
    Ok(WorkerRosterEntry {
        worker: r.worker,
        owner: r.owner,
        recipient: AccountId::new([40; 32])?,
        generation: Version::new(r.generation)?,
        key_version: Version::new(r.key_version)?,
        public_key: r.delegate,
        metadata: r.metadata,
    })
}

const HOST: &str = "worker.example";
fn uri(host: &str) -> String {
    format!("https://{host}/paxai/v1")
}

struct ManifestSpec {
    schema: u16,
    worker: WorkerId,
    generation: u64,
    key_version: u64,
    revision: u64,
    valid_from: u64,
    expiry: u64,
    capabilities: Vec<(u8, [u8; 32])>,
    uris: Vec<String>,
    reserved: u32,
}
impl ManifestSpec {
    fn for_record(r: &WorkerCurrent, revision: u64, valid_from: u64, expiry: u64) -> Self {
        Self {
            schema: 1,
            worker: r.worker,
            generation: r.generation,
            key_version: r.key_version,
            revision,
            valid_from,
            expiry,
            capabilities: vec![(1, [50; 32])],
            uris: vec![uri(HOST)],
            reserved: 0,
        }
    }
    fn encode(&self) -> Checked<Vec<u8>> {
        let h = header()?;
        let mut v = Vec::new();
        v.extend_from_slice(&self.schema.to_be_bytes());
        v.extend_from_slice(h.market_id.as_bytes());
        v.extend_from_slice(self.worker.as_bytes());
        v.extend_from_slice(&NOMINEE);
        for n in [
            self.generation,
            self.key_version,
            self.revision,
            self.valid_from,
            self.expiry,
        ] {
            v.extend_from_slice(&n.to_be_bytes());
        }
        v.extend_from_slice(&[60; 32]);
        v.extend_from_slice(&u16::try_from(self.capabilities.len())?.to_be_bytes());
        for (kind, model) in &self.capabilities {
            v.push(*kind);
            v.push(1);
            v.extend_from_slice(model);
            for d in 0..4u8 {
                v.extend_from_slice(&[61 + d; 32]);
            }
            for n in [1024u32, 1024, 1024, 1024] {
                v.extend_from_slice(&n.to_be_bytes());
            }
            v.push(1);
            v.extend_from_slice(&60_000u32.to_be_bytes());
            v.extend_from_slice(&4u16.to_be_bytes());
            v.push(1);
        }
        v.extend_from_slice(&u16::try_from(self.uris.len())?.to_be_bytes());
        for (i, u) in self.uris.iter().enumerate() {
            v.push(u8::try_from(i)? + 1);
            v.extend_from_slice(&u32::try_from(u.len())?.to_be_bytes());
            v.extend_from_slice(u.as_bytes());
            v.extend_from_slice(&[70; 32]);
            v.push(1);
            v.extend_from_slice(&1u16.to_be_bytes());
            v.extend_from_slice(&1u16.to_be_bytes());
        }
        v.extend_from_slice(&[71; 32]);
        v.extend_from_slice(&[72; 32]);
        v.extend_from_slice(&self.reserved.to_be_bytes());
        Ok(v)
    }
}

fn publish_payload(worker: WorkerId, expected: u64, manifest: &[u8]) -> Checked<Vec<u8>> {
    let m = decode_manifest(manifest)?;
    let mut v = Vec::new();
    v.extend_from_slice(worker.as_bytes());
    v.extend_from_slice(&expected.to_be_bytes());
    v.extend_from_slice(&m.revision.to_be_bytes());
    v.extend_from_slice(m.digest.as_bytes());
    v.extend_from_slice(&m.valid_from.to_be_bytes());
    v.extend_from_slice(&m.expiry.to_be_bytes());
    v.extend_from_slice(&u32::try_from(manifest.len())?.to_be_bytes());
    v.extend_from_slice(manifest);
    Ok(v)
}

#[test]
fn a01_two_party_enrollment_derives_worker_id_and_stages_next_epoch() -> Checked {
    let h = header()?;
    let k = key(0x41);
    let s0 = state_with(&[])?;
    let env = envelope(
        &h,
        0x0201,
        OWNER,
        1,
        130,
        &enroll_payload(pk(&k), [31; 32], 258),
        None,
    )?;
    // The nominee cannot nominate itself; only the market owner nominates.
    assert_eq!(
        call(
            &s0,
            &h,
            NOMINEE,
            130,
            &envelope(
                &h,
                0x0201,
                NOMINEE,
                1,
                130,
                &enroll_payload(pk(&k), [31; 32], 258),
                None
            )?
        )
        .err(),
        Some(UNAUTHORIZED)
    );
    let (applied, c1) = call(&s0, &h, OWNER, 130, &env)?;
    assert!(matches!(applied, Applied::Applied { .. }));
    let worker = derive_worker(h.market_id, p(NOMINEE)?, NONCE)?;
    let pending = table_of(&c1.state)?
        .get(worker)
        .ok_or(Failure::Unexpected("worker record"))?;
    assert_eq!(pending.state, WorkerState::PendingOwner);
    assert_eq!((pending.valid_from, pending.expiry), (130, 258));
    // PENDING_OWNER admits nothing but acceptance/expiry.
    let drain = envelope(&h, 0x0203, NOMINEE, 1, 130, worker.as_bytes(), None)?;
    assert_eq!(
        call(&c1.state, &h, NOMINEE, 130, &drain).err(),
        Some(WRONG_PHASE)
    );
    // A consent signed by another key never associates an unwilling delegate.
    let forged = accept_payload(&h, &pending, &key(0x42))?;
    let bad = envelope(&h, 0x0209, NOMINEE, 1, 130, &forged, None)?;
    assert_eq!(
        call(&c1.state, &h, NOMINEE, 130, &bad).err(),
        Some(BAD_SIGNATURE)
    );
    let accept = envelope(
        &h,
        0x0209,
        NOMINEE,
        1,
        130,
        &accept_payload(&h, &pending, &k)?,
        None,
    )?;
    assert_eq!(
        call(&c1.state, &h, OWNER, 130, &accept).err(),
        Some(UNAUTHORIZED)
    );
    let (_, c2) = call(&c1.state, &h, NOMINEE, 130, &accept)?;
    let enrolled = table_of(&c2.state)?
        .get(worker)
        .ok_or(Failure::Unexpected("worker record"))?;
    assert_eq!(enrolled.state, WorkerState::Enrolled);
    assert_eq!(market_clock(h.origin_height, 130)?.epoch, 1);
    assert_eq!(enrolled.effective_epoch, 2);
    assert_eq!((enrolled.generation, enrolled.key_version), (1, 1));
    let before = state::decode_shared_state(&s0)?;
    let after = state::decode_shared_state(&c2.state)?;
    for section in [
        Section::PolicyLifecycle,
        Section::CurrentReports,
        Section::SettlementClaims,
        Section::ReputationAdmission,
    ] {
        assert_eq!(before.section(section), after.section(section));
    }
    assert_eq!(after.revision, 3);
    let (_, common, suffix) = codec::decode_event_frame(b"PAXAI/v1/AcceptEnrollment", &c2.event)?;
    assert_eq!(common.revision, 3);
    assert_eq!(&suffix[..32], worker.as_bytes());
    // Exact retry returns the original receipt without a second mutation.
    let (retry, _) = call(&c2.state, &h, NOMINEE, 130, &accept)?;
    assert!(matches!(retry, Applied::AlreadyApplied(_)));
    Ok(())
}

#[test]
fn a02_a03_metadata_revision_window_and_overflow() -> Checked {
    let h = header()?;
    let k = key(0x43);
    let r = record(0, &k)?;
    let s0 = state_with(&[r])?;
    let m5 = ManifestSpec::for_record(&r, 5, 300, 428).encode()?;
    let env = envelope(
        &h,
        0x0202,
        NOMINEE,
        1,
        300,
        &publish_payload(r.worker, 4, &m5)?,
        Some(&k),
    )?;
    // Native (non-delegate) publication is refused.
    let native = envelope(
        &h,
        0x0202,
        NOMINEE,
        1,
        300,
        &publish_payload(r.worker, 4, &m5)?,
        None,
    )?;
    assert_eq!(
        call(&s0, &h, NOMINEE, 300, &native).err(),
        Some(F02_DELEGATE_CONSENT_REQUIRED)
    );
    let (_, c1) = call(&s0, &h, NOMINEE, 300, &env)?;
    let staged = table_of(&c1.state)?
        .get(r.worker)
        .ok_or(Failure::Unexpected("worker record"))?;
    assert_eq!(staged.metadata_revision, 5);
    assert_eq!((staged.valid_from, staged.expiry), (300, 428));
    assert_eq!(staged.metadata, decode_manifest(&m5)?.digest);
    let m6 = ManifestSpec::for_record(&r, 6, 310, 438).encode()?;
    let stale = envelope(
        &h,
        0x0202,
        NOMINEE,
        2,
        310,
        &publish_payload(r.worker, 4, &m6)?,
        Some(&k),
    )?;
    let snapshot = c1.state.clone();
    assert_eq!(
        call(&c1.state, &h, NOMINEE, 310, &stale).err(),
        Some(F02_WRONG_REVISION)
    );
    assert_eq!(c1.state, snapshot);
    // A03 window: inclusive start, exclusive expiry.
    assert_eq!(check_metadata_window(&staged, 300), Ok(()));
    assert_eq!(check_metadata_window(&staged, 427), Ok(()));
    assert_eq!(
        check_metadata_window(&staged, 428),
        Err(F02_METADATA_EXPIRED)
    );
    let long = ManifestSpec::for_record(&r, 5, 300, 557).encode()?;
    assert_eq!(decode_manifest(&long), Err(NON_CANONICAL));
    let mut max = r;
    max.metadata_revision = u64::MAX;
    let s_max = state_with(&[max])?;
    let m = ManifestSpec::for_record(&max, u64::MAX, 300, 428).encode()?;
    let over = envelope(
        &h,
        0x0202,
        NOMINEE,
        1,
        300,
        &publish_payload(r.worker, u64::MAX, &m)?,
        Some(&k),
    )?;
    assert_eq!(
        call(&s_max, &h, NOMINEE, 300, &over).err(),
        Some(ARITHMETIC)
    );
    Ok(())
}

#[test]
fn a04_rotation_blocks_old_generation_and_waits_for_snapshot() -> Checked {
    let h = header()?;
    let k2 = key(0x44);
    let k3 = key(0x45);
    let r = record(0, &k2)?;
    let snapshot = frozen(&r)?;
    assert_eq!(check_new_admission(&r, &snapshot, 299), Ok(()));
    let s0 = state_with(&[r])?;
    let pending = MetadataDigest::new([33; 32])?;
    let consent = consent_digest(&h, r.worker, r.owner, pk(&k3), 3, 3, pending, 400)?;
    let mut payload = Vec::new();
    payload.extend_from_slice(r.worker.as_bytes());
    payload.extend_from_slice(&2u64.to_be_bytes());
    payload.extend_from_slice(&2u64.to_be_bytes());
    payload.extend_from_slice(&pk(&k3).0);
    payload.extend_from_slice(pending.as_bytes());
    payload.extend_from_slice(&400u64.to_be_bytes());
    payload.extend_from_slice(&k3.sign(consent.as_bytes()).to_bytes());
    let env = envelope(&h, 0x0205, NOMINEE, 1, 300, &payload, None)?;
    let (_, c1) = call(&s0, &h, NOMINEE, 300, &env)?;
    let rotated = table_of(&c1.state)?
        .get(r.worker)
        .ok_or(Failure::Unexpected("worker record"))?;
    assert_eq!(rotated.worker, r.worker);
    assert_eq!((rotated.generation, rotated.key_version), (3, 3));
    assert_eq!(rotated.delegate, pk(&k3));
    assert_eq!(rotated.effective_epoch, market_clock(0, 300)?.epoch + 1);
    // Old frozen generation can no longer admit; the new one is not in the snapshot.
    assert_eq!(
        check_new_admission(&rotated, &snapshot, 300),
        Err(F02_WRONG_GENERATION)
    );
    assert_eq!(snapshot, frozen(&r)?);
    // The old delegate cannot sign new operations after rotation.
    let m = ManifestSpec::for_record(&rotated, 5, 300, 428).encode()?;
    let old = envelope(
        &h,
        0x0202,
        NOMINEE,
        2,
        300,
        &publish_payload(r.worker, 4, &m)?,
        Some(&k2),
    )?;
    assert_eq!(
        call(&c1.state, &h, NOMINEE, 300, &old).err(),
        Some(KEY_MISMATCH)
    );
    Ok(())
}

#[test]
fn a13_manifest_grammar_refusals_before_mutation() -> Checked {
    let h = header()?;
    let k = key(0x46);
    let r = record(0, &k)?;
    let s0 = state_with(&[r])?;
    let base = ManifestSpec::for_record(&r, 5, 300, 428);
    assert!(decode_manifest(&base.encode()?).is_ok());
    let mut schema = ManifestSpec::for_record(&r, 5, 300, 428);
    schema.schema = 2;
    assert_eq!(decode_manifest(&schema.encode()?), Err(BAD_VERSION));
    let mut reserved = ManifestSpec::for_record(&r, 5, 300, 428);
    reserved.reserved = 1;
    assert_eq!(decode_manifest(&reserved.encode()?), Err(NON_CANONICAL));
    let mut dup = ManifestSpec::for_record(&r, 5, 300, 428);
    dup.capabilities = vec![(1, [50; 32]), (1, [50; 32])];
    assert_eq!(decode_manifest(&dup.encode()?), Err(NON_CANONICAL));
    let mut trailing = base.encode()?;
    trailing.push(0);
    assert_eq!(decode_manifest(&trailing), Err(NON_CANONICAL));
    for bad in [
        "https://user@worker.example/paxai/v1".to_string(),
        "https://worker.example/paxai/v1?next=https://other.example".to_string(),
        "http://worker.example/paxai/v1".to_string(),
        "https://127.0.0.1/paxai/v1".to_string(),
        "https://worker.example/paxai/v1#f".to_string(),
    ] {
        let mut m = ManifestSpec::for_record(&r, 5, 300, 428);
        m.uris = vec![bad];
        assert_eq!(decode_manifest(&m.encode()?), Err(NON_CANONICAL));
    }
    // Unbound receiver: a manifest for another worker refuses with state unchanged.
    let mut other = ManifestSpec::for_record(&r, 5, 300, 428);
    other.worker = WorkerId::new([99; 32])?;
    let bytes = other.encode()?;
    let env = envelope(
        &h,
        0x0202,
        NOMINEE,
        1,
        300,
        &publish_payload(r.worker, 4, &bytes)?,
        Some(&k),
    )?;
    assert_eq!(
        call(&s0, &h, NOMINEE, 300, &env).err(),
        Some(F02_METADATA_INTEGRITY_FAILURE)
    );
    assert_eq!(table_of(&s0)?.get(r.worker), Some(r));
    // Size bound: 8193 bytes refuse at the bound; 8192 bytes pass the bound itself.
    assert_eq!(decode_manifest(&vec![0u8; 8193]), Err(CAPACITY));
    assert_ne!(decode_manifest(&vec![0u8; 8192]), Err(CAPACITY));
    // Largest grammar-valid manifest (8 capabilities, 2 longest DNS URIs) is admitted.
    let host = format!(
        "{}.{}.{}.{}",
        "a".repeat(63),
        "b".repeat(63),
        "c".repeat(63),
        "d".repeat(61)
    );
    let mut big = ManifestSpec::for_record(&r, 5, 300, 428);
    big.capabilities = (1..=8).map(|i| (1, [i; 32])).collect();
    big.uris = vec![
        format!("https://{host}:65535/paxai/v1"),
        format!("https://{host}:65534/paxai/v1"),
    ];
    let big = big.encode()?;
    assert!(big.len() < MAX_MANIFEST_BYTES);
    assert!(decode_manifest(&big).is_ok());
    Ok(())
}

#[test]
fn a16_full_identity_table_fits_shared_allocation_and_33rd_refuses() -> Checked {
    const _: () = assert!(WORKER_RECORD_BYTES <= 384);
    let h = header()?;
    let records: Vec<WorkerCurrent> = (0..32u8)
        .map(|i| -> Checked<WorkerCurrent> {
            let mut r = record(i, &key(0x50 + i))?;
            r.owner = p([100 + i; 32])?;
            r.worker = derive_worker(h.market_id, r.owner, [1; 32])?;
            r.last_request_id = [0xff; 32];
            r.last_request_digest = [0xff; 32];
            r.last_result_digest = [0xff; 32];
            Ok(r)
        })
        .collect::<Checked<_>>()?;
    let s = state_with(&records)?;
    let table = table_of(&s)?;
    assert_eq!(table.len(), 32);
    assert!(table.encoded_len() + codec::ROSTER_MAX_BYTES <= 24_576);
    assert!(s.len() <= MAX_STATE_BYTES);
    let env = envelope(
        &h,
        0x0201,
        OWNER,
        1,
        130,
        &enroll_payload(pk(&key(0x7f)), [31; 32], 258),
        None,
    )?;
    assert_eq!(call(&s, &h, OWNER, 130, &env).err(), Some(CAPACITY));
    Ok(())
}

#[test]
fn a18_expire_enrollment_exact_cleanup_and_reserved_selector() -> Checked {
    let h = header()?;
    let k = key(0x47);
    let s0 = state_with(&[])?;
    let enroll = envelope(
        &h,
        0x0201,
        OWNER,
        1,
        500,
        &enroll_payload(pk(&k), [31; 32], 628),
        None,
    )?;
    let (_, c1) = call(&s0, &h, OWNER, 500, &enroll)?;
    let worker = derive_worker(h.market_id, p(NOMINEE)?, NONCE)?;
    let candidate = table_of(&c1.state)?
        .get(worker)
        .ok_or(Failure::Unexpected("worker record"))?;
    let digest = candidate.proposal_digest(&h)?;
    let expire = |d: [u8; 32], expiry: u64| {
        let mut v = Vec::new();
        v.extend_from_slice(worker.as_bytes());
        v.extend_from_slice(&d);
        v.extend_from_slice(&expiry.to_be_bytes());
        v
    };
    let exact = expire(digest.bytes(), 628);
    let early = envelope(&h, 0x020A, OWNER, 2, 627, &exact, None)?;
    assert_eq!(
        call(&c1.state, &h, OWNER, 627, &early).err(),
        Some(WRONG_PHASE)
    );
    assert_eq!(table_of(&c1.state)?.get(worker), Some(candidate));
    let late = envelope(
        &h,
        0x020A,
        OWNER,
        2,
        628,
        &expire(digest.bytes(), 629),
        None,
    )?;
    assert_eq!(call(&c1.state, &h, OWNER, 628, &late).err(), Some(CONFLICT));
    let mut flipped = digest.bytes();
    flipped[0] ^= 1;
    let bit = envelope(&h, 0x020A, OWNER, 2, 628, &expire(flipped, 628), None)?;
    assert_eq!(call(&c1.state, &h, OWNER, 628, &bit).err(), Some(CONFLICT));
    let stranger = envelope(&h, 0x020A, NOMINEE, 2, 628, &exact, None)?;
    assert_eq!(
        call(&c1.state, &h, NOMINEE, 628, &stranger).err(),
        Some(UNAUTHORIZED)
    );
    let ok = envelope(&h, 0x020A, OWNER, 2, 628, &exact, None)?;
    let (applied, c2) = call(&c1.state, &h, OWNER, 628, &ok)?;
    assert!(matches!(applied, Applied::Applied { .. }));
    assert!(table_of(&c2.state)?.is_empty());
    let after = state::decode_shared_state(&c2.state)?;
    assert!(after.control.replay.actor(ActorSlot::worker(0)?).is_none());
    let (_, _, suffix) = codec::decode_event_frame(b"PAXAI/v1/ExpireEnrollment", &c2.event)?;
    assert_eq!(&suffix[..32], worker.as_bytes());
    assert_eq!(&suffix[32..64], digest.as_bytes());
    assert_eq!(&suffix[64..72], &628u64.to_be_bytes());
    let (retry, r2) = call(&c2.state, &h, OWNER, 628, &ok)?;
    assert!(matches!(retry, Applied::AlreadyApplied(_)));
    assert!(r2.event.is_empty());
    // Accepted identities can no longer be cleaned up.
    let enroll2 = envelope(
        &h,
        0x0201,
        OWNER,
        3,
        700,
        &enroll_payload(pk(&k), [31; 32], 828),
        None,
    )?;
    let (_, c3) = call(&c2.state, &h, OWNER, 700, &enroll2)?;
    let rec = table_of(&c3.state)?
        .get(worker)
        .ok_or(Failure::Unexpected("worker record"))?;
    let accept = envelope(
        &h,
        0x0209,
        NOMINEE,
        1,
        700,
        &accept_payload(&h, &rec, &k)?,
        None,
    )?;
    let (_, c4) = call(&c3.state, &h, NOMINEE, 700, &accept)?;
    let d2 = rec.proposal_digest(&h)?;
    let cleanup = envelope(&h, 0x020A, OWNER, 4, 828, &expire(d2.bytes(), 828), None)?;
    assert_eq!(
        call(&c4.state, &h, OWNER, 828, &cleanup).err(),
        Some(WRONG_PHASE)
    );
    assert_eq!(table_of(&c4.state)?.len(), 1);
    // Reserved and service-only selectors never admit.
    assert_eq!(admit_selector(0x0208).err(), Some(UNKNOWN_OPERATION));
    assert_eq!(admit_selector(0x0281).err(), Some(UNKNOWN_OPERATION));
    assert_eq!(Operation::decode(0x0208).err(), Some(UNKNOWN_OPERATION));
    assert_eq!(admit_selector(0x020A), Ok(dispatch::ExpireEnrollment));
    Ok(())
}

#[test]
fn a19_revocation_blocks_new_work_but_keeps_frozen_identity() -> Checked {
    let h = header()?;
    let k = key(0x48);
    let r = record(0, &k)?;
    let snapshot = frozen(&r)?;
    let s0 = state_with(&[r])?;
    let mut payload = Vec::new();
    payload.extend_from_slice(r.worker.as_bytes());
    payload.extend_from_slice(&2u64.to_be_bytes());
    payload.push(1);
    payload.extend_from_slice(&1u64.to_be_bytes());
    let env = envelope(&h, 0x0206, NOMINEE, 1, 320, &payload, None)?;
    let (_, c1) = call(&s0, &h, NOMINEE, 320, &env)?;
    let revoked = table_of(&c1.state)?
        .get(r.worker)
        .ok_or(Failure::Unexpected("worker record"))?;
    assert_eq!(revoked.state, WorkerState::Revoked);
    assert_eq!(revoked.revocation_sequence, 1);
    assert_eq!(
        check_new_admission(&revoked, &snapshot, 320),
        Err(F02_DELEGATE_REVOKED)
    );
    let m = ManifestSpec::for_record(&r, 5, 330, 458).encode()?;
    let sign = envelope(
        &h,
        0x0202,
        NOMINEE,
        2,
        330,
        &publish_payload(r.worker, 4, &m)?,
        Some(&k),
    )?;
    assert_eq!(
        call(&c1.state, &h, NOMINEE, 330, &sign).err(),
        Some(F02_DELEGATE_REVOKED)
    );
    // Frozen identity, keys and metadata stay intact for already accepted evidence.
    assert_eq!(snapshot, frozen(&r)?);
    assert_eq!(
        (revoked.worker, revoked.owner, revoked.delegate),
        (r.worker, r.owner, r.delegate)
    );
    assert_eq!(
        (revoked.generation, revoked.key_version, revoked.metadata),
        (r.generation, r.key_version, r.metadata)
    );
    // A worker delegate cannot reactivate itself; repeating revocation is refused.
    let again = envelope(&h, 0x0206, NOMINEE, 2, 330, &payload, None)?;
    assert_eq!(
        call(&c1.state, &h, NOMINEE, 330, &again).err(),
        Some(WRONG_PHASE)
    );
    Ok(())
}
