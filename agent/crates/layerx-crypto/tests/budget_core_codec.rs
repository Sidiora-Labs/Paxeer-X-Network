use std::fmt::Debug;

use ed25519_dalek::SigningKey;
use layerx_crypto::disclosure::{
    bind, bind_budget_mutation, AmountRole, BudgetStateContext, Counterparty, CounterpartyRole,
    DisclosedAmount, DisclosedNativeOperation, DisclosureError,
};
use layerx_types::account::AccountId;
use layerx_types::activity::{Authority, EnvelopeBuilder, TimestampBound};
use layerx_types::amount::Amount;
use layerx_types::ids::{Did, IdempotencyKey};
use layerx_types::payload::{ActivityType, ModuleId, ModuleRegistration, ModuleRegistry, Payload};
use layerx_wire::activity::encode_unsigned_envelope;
use layerx_wire::hash;
use sha2::{Digest as _, Sha256};

const ACTOR: &str = "did:layerx:alice";
const ENVELOPE_SEQUENCE: u64 = 10;
const NOT_BEFORE_MS: u64 = 1_000;
const NOT_AFTER_MS: u64 = 2_000;
const BUDGET_ID: [u8; 32] = [0x0b; 32];
const ASSET: [u8; 32] = [0x44; 32];
const PURPOSE: [u8; 32] = [0x54; 32];
const PER_PERIOD_LIMIT: u128 = 5_000;
const INITIAL_AMOUNT: u128 = 1_200;
const PERIOD_LENGTH_MS: u64 = 60_000;
const PERIOD_START_MS: u64 = 900;
const EXPIRY_MS: u64 = 1_700_000_000_000;
const REVOCATION_COUNTER: u64 = 4;

const CREATE: u32 = 0x0003_0001;
const FUND: u32 = 0x0003_0002;
const CLOSE: u32 = 0x0003_0007;
const REVOKE: u32 = 0x0003_0009;

fn checked<T, E: Debug>(result: Result<T, E>) -> T {
    result.unwrap_or_else(|error| panic!("canonical fixture rejected: {error:?}"))
}

fn owner_key() -> [u8; 32] {
    SigningKey::from_bytes(&[0x11; 32])
        .verifying_key()
        .to_bytes()
}

fn hex(value: &[u8; 32]) -> String {
    value.iter().map(|byte| format!("{byte:02x}")).collect()
}

fn account(name: &str) -> [u8; 32] {
    checked(hash::account_id_for_protocol(
        &checked(AccountId::parse(name)),
        3,
    ))
}

fn main_account() -> [u8; 32] {
    account(&format!("agent:{ACTOR}:main"))
}

fn asset_account(asset: [u8; 32]) -> [u8; 32] {
    account(&format!("agent:{ACTOR}:asset:{}", hex(&asset)))
}

fn budget_account(budget_id: [u8; 32]) -> [u8; 32] {
    account(&format!("agent:{ACTOR}:budget:{}", hex(&budget_id)))
}

fn kind(value: u32) -> ActivityType {
    let ordinal = checked(u16::try_from(value & 0xffff));
    checked(ActivityType::new(ModuleId::Budget, ordinal))
}

fn registry() -> ModuleRegistry {
    let kinds = [CREATE, FUND, CLOSE, 0x0003_0008, REVOKE].map(kind);
    checked(ModuleRegistry::new(&[checked(ModuleRegistration::new(
        ModuleId::Budget,
        &kinds,
    ))]))
}

fn canonical(activity_type: u32, payload: &[u8]) -> Vec<u8> {
    let kind = kind(activity_type);
    let mut hasher = Sha256::new();
    hasher.update(hash::Domain::PayloadHash.tag());
    hasher.update(payload);
    let mut builder = EnvelopeBuilder::new();
    checked(builder.protocol_version(3));
    checked(builder.network_id(77));
    checked(builder.activity_type(kind));
    checked(builder.actor_did(checked(Did::new(ACTOR.as_bytes()))));
    checked(builder.authority(checked(Authority::owner(&owner_key()))));
    checked(builder.account_sequence(ENVELOPE_SEQUENCE));
    checked(builder.timestamp_bound(checked(TimestampBound::new(NOT_BEFORE_MS, NOT_AFTER_MS))));
    checked(builder.idempotency_key(IdempotencyKey::new([9; 32])));
    checked(builder.fee_limit(Amount::from_u128(4)));
    checked(builder.payload_hash(hasher.finalize().into()));
    checked(builder.payload(checked(Payload::new(&registry(), kind, payload))));
    checked(encode_unsigned_envelope(&checked(builder.build())))
}

#[derive(Clone, Copy)]
struct Create {
    version: u16,
    budget_id: [u8; 32],
    budget_account: [u8; 32],
    asset: [u8; 32],
    per_period_limit: u128,
    carry_cap: u128,
    initial_amount: u128,
    period_length_ms: u64,
    period_start_ms: u64,
    expiry_ms: u64,
    revocation_counter: u64,
    rollover: u8,
    source: Option<([u8; 32], u64)>,
}

impl Create {
    fn v1() -> Self {
        Self {
            version: 1,
            budget_id: BUDGET_ID,
            budget_account: budget_account(BUDGET_ID),
            asset: ASSET,
            per_period_limit: PER_PERIOD_LIMIT,
            carry_cap: 0,
            initial_amount: INITIAL_AMOUNT,
            period_length_ms: PERIOD_LENGTH_MS,
            period_start_ms: PERIOD_START_MS,
            expiry_ms: EXPIRY_MS,
            revocation_counter: REVOCATION_COUNTER,
            rollover: 1,
            source: None,
        }
    }

    fn v2(source_account: [u8; 32], source_sequence: u64) -> Self {
        Self {
            version: 2,
            source: Some((source_account, source_sequence)),
            ..Self::v1()
        }
    }

    fn bytes(self) -> Vec<u8> {
        let mut bytes = Vec::with_capacity(251);
        bytes.extend_from_slice(&self.version.to_be_bytes());
        bytes.extend_from_slice(&self.budget_id);
        bytes.extend_from_slice(&self.budget_account);
        bytes.extend_from_slice(&self.asset);
        bytes.extend_from_slice(&PURPOSE);
        bytes.extend_from_slice(&self.per_period_limit.to_be_bytes());
        bytes.extend_from_slice(&self.carry_cap.to_be_bytes());
        bytes.extend_from_slice(&self.initial_amount.to_be_bytes());
        bytes.extend_from_slice(&self.period_length_ms.to_be_bytes());
        bytes.extend_from_slice(&self.period_start_ms.to_be_bytes());
        bytes.extend_from_slice(&self.expiry_ms.to_be_bytes());
        bytes.extend_from_slice(&self.revocation_counter.to_be_bytes());
        bytes.push(self.rollover);
        if let Some((account, sequence)) = self.source {
            bytes.extend_from_slice(&account);
            bytes.extend_from_slice(&sequence.to_be_bytes());
        }
        bytes
    }
}

fn fund_payload(version: u16, budget_id: [u8; 32], amount: u128, sequence: Option<u64>) -> Vec<u8> {
    let mut bytes = version.to_be_bytes().to_vec();
    bytes.extend_from_slice(&budget_id);
    bytes.extend_from_slice(&amount.to_be_bytes());
    if let Some(sequence) = sequence {
        bytes.extend_from_slice(&sequence.to_be_bytes());
    }
    bytes
}

fn revoke_payload(version: u16, budget_id: [u8; 32], counter: u64) -> Vec<u8> {
    let mut bytes = version.to_be_bytes().to_vec();
    bytes.extend_from_slice(&budget_id);
    bytes.extend_from_slice(&counter.to_be_bytes());
    bytes
}

fn context(native_source: bool) -> BudgetStateContext {
    BudgetStateContext {
        budget_id: BUDGET_ID,
        owner: main_account(),
        budget_account: budget_account(BUDGET_ID),
        asset: ASSET,
        source_account: main_account(),
        native_source,
        revocation_sequence: REVOCATION_COUNTER,
        balance: 700,
        state_digest: [0x5d; 32],
        observed_head_sequence: 88,
    }
}

fn refused(activity_type: u32, payload: &[u8]) -> bool {
    bind(&canonical(activity_type, payload), &registry()).is_err()
}

fn mutation_refused(activity_type: u32, payload: &[u8], context: &BudgetStateContext) -> bool {
    bind_budget_mutation(&canonical(activity_type, payload), &registry(), context).is_err()
}

#[test]
fn create_v1_is_exactly_211_core_bytes_and_discloses_every_field() {
    let payload = Create::v1().bytes();
    assert_eq!(payload.len(), 211);
    assert_eq!(&payload[..2], &[0, 1]);
    assert_eq!(&payload[2..34], &BUDGET_ID);
    assert_eq!(&payload[34..66], &budget_account(BUDGET_ID));
    assert_eq!(&payload[66..98], &ASSET);
    assert_eq!(&payload[98..130], &PURPOSE);
    assert_eq!(&payload[130..146], &PER_PERIOD_LIMIT.to_be_bytes());
    assert_eq!(&payload[146..162], &0_u128.to_be_bytes());
    assert_eq!(&payload[162..178], &INITIAL_AMOUNT.to_be_bytes());
    assert_eq!(&payload[178..186], &PERIOD_LENGTH_MS.to_be_bytes());
    assert_eq!(&payload[186..194], &PERIOD_START_MS.to_be_bytes());
    assert_eq!(&payload[194..202], &EXPIRY_MS.to_be_bytes());
    assert_eq!(&payload[202..210], &REVOCATION_COUNTER.to_be_bytes());
    assert_eq!(payload[210], 1);

    let canonical = canonical(CREATE, &payload);
    let disclosure = checked(bind(&canonical, &registry()));
    let Some(DisclosedNativeOperation::BudgetCreate(create)) = &disclosure.native_operation else {
        panic!("a core v1 create must disclose as a native budget create");
    };
    assert_eq!(create.encoding_version, 1);
    assert_eq!(create.budget_id, BUDGET_ID);
    assert_eq!(create.budget_account, budget_account(BUDGET_ID));
    assert_eq!(create.asset, ASSET);
    assert_eq!(create.purpose, PURPOSE);
    assert_eq!(create.per_period_limit, PER_PERIOD_LIMIT);
    assert_eq!(create.carry_cap, 0);
    assert_eq!(create.initial_amount, INITIAL_AMOUNT);
    assert_eq!(create.period_length_ms, PERIOD_LENGTH_MS);
    assert_eq!(create.period_start_ms, PERIOD_START_MS);
    assert_eq!(create.expiry_ms, EXPIRY_MS);
    assert_eq!(create.revocation_sequence, REVOCATION_COUNTER);
    assert_eq!(create.rollover, 1);
    assert_eq!(create.source_account, main_account());
    assert_eq!(create.source_sequence, ENVELOPE_SEQUENCE);
    assert_eq!(
        disclosure.counterparties,
        vec![
            Counterparty {
                role: CounterpartyRole::Payer,
                account: main_account(),
            },
            Counterparty {
                role: CounterpartyRole::Recipient,
                account: budget_account(BUDGET_ID),
            },
        ]
    );
    assert_eq!(
        disclosure.amounts,
        vec![
            DisclosedAmount {
                role: AmountRole::Transfer,
                value: INITIAL_AMOUNT,
            },
            DisclosedAmount {
                role: AmountRole::SpendingLimit,
                value: PER_PERIOD_LIMIT,
            },
        ]
    );
    assert_eq!(disclosure.asset, ASSET);
    assert_eq!(disclosure.expiry.payload_expires_at, EXPIRY_MS);
    assert_eq!(disclosure.reencode(), Ok(canonical));
    assert!(disclosure.audit_digest().is_ok());
}

#[test]
fn create_v2_is_exactly_251_core_bytes_and_binds_its_source() {
    let payload = Create::v2(asset_account(ASSET), 31).bytes();
    assert_eq!(payload.len(), 251);
    assert_eq!(&payload[..2], &[0, 2]);
    assert_eq!(&payload[211..243], &asset_account(ASSET));
    assert_eq!(&payload[243..251], &31_u64.to_be_bytes());
    let canonical = canonical(CREATE, &payload);
    let disclosure = checked(bind(&canonical, &registry()));
    let Some(DisclosedNativeOperation::BudgetCreate(create)) = &disclosure.native_operation else {
        panic!("a core v2 create must disclose as a native budget create");
    };
    assert_eq!(create.encoding_version, 2);
    assert_eq!(create.source_account, asset_account(ASSET));
    assert_eq!(create.source_sequence, 31);
    assert_eq!(disclosure.payload_sequence(), Ok(Some(31)));
    assert_eq!(disclosure.counterparties[0].account, asset_account(ASSET));
    assert_eq!(disclosure.reencode(), Ok(canonical));

    let main = Create::v2(main_account(), 31).bytes();
    assert!(bind(&canonical(CREATE, &main), &registry()).is_ok());
    for (source, sequence) in [
        (budget_account(BUDGET_ID), 31),
        ([0x99; 32], 31),
        ([0; 32], 31),
        (main_account(), u64::MAX),
    ] {
        assert!(
            refused(CREATE, &Create::v2(source, sequence).bytes()),
            "v2 source binding must refuse {source:?}/{sequence}"
        );
    }
}

#[test]
fn create_refuses_every_wrong_length_version_prefix_and_trailing_byte() {
    let v1 = Create::v1().bytes();
    let v2 = Create::v2(main_account(), 31).bytes();
    assert!(refused(CREATE, &v1[..210]));
    let mut trailing = v1.clone();
    trailing.push(0);
    assert!(refused(CREATE, &trailing));
    assert!(refused(CREATE, &v2[..250]));
    let mut trailing = v2.clone();
    trailing.push(0);
    assert!(refused(CREATE, &trailing));

    let mut v1_claiming_v2 = v1.clone();
    v1_claiming_v2[1] = 2;
    assert!(refused(CREATE, &v1_claiming_v2));
    let mut v2_claiming_v1 = v2.clone();
    v2_claiming_v1[1] = 1;
    assert!(refused(CREATE, &v2_claiming_v1));
    for prefix in [[0_u8, 0], [0, 3], [1, 1], [0x42, 0x01]] {
        let mut wrong = v1.clone();
        wrong[..2].copy_from_slice(&prefix);
        assert!(refused(CREATE, &wrong), "prefix {prefix:?} must be refused");
    }
}

#[test]
fn create_refuses_zero_identities_amounts_and_inconsistent_terms() {
    let zero_id = Create {
        budget_id: [0; 32],
        budget_account: budget_account([0; 32]),
        ..Create::v1()
    };
    assert!(refused(CREATE, &zero_id.bytes()));
    let cases = [
        Create {
            asset: [0; 32],
            ..Create::v1()
        },
        Create {
            budget_account: [0; 32],
            ..Create::v1()
        },
        Create {
            budget_account: main_account(),
            ..Create::v1()
        },
        Create {
            initial_amount: 0,
            ..Create::v1()
        },
        Create {
            per_period_limit: 0,
            ..Create::v1()
        },
        Create {
            period_length_ms: 0,
            ..Create::v1()
        },
        Create {
            expiry_ms: PERIOD_START_MS,
            ..Create::v1()
        },
        Create {
            rollover: 0,
            ..Create::v1()
        },
        Create {
            rollover: 3,
            ..Create::v1()
        },
        Create {
            carry_cap: 1,
            ..Create::v1()
        },
    ];
    for case in cases {
        assert!(refused(CREATE, &case.bytes()));
    }
    let capped = Create {
        rollover: 2,
        carry_cap: 1,
        ..Create::v1()
    };
    assert!(!refused(CREATE, &capped.bytes()));
}

#[test]
fn create_expiry_is_core_milliseconds_and_equality_with_the_bound_is_refused() {
    let at_bound = Create {
        period_start_ms: 0,
        expiry_ms: NOT_BEFORE_MS,
        ..Create::v1()
    };
    assert!(refused(CREATE, &at_bound.bytes()));
    let after_bound = Create {
        period_start_ms: 0,
        expiry_ms: NOT_BEFORE_MS + 1,
        ..Create::v1()
    };
    let disclosure = checked(bind(&canonical(CREATE, &after_bound.bytes()), &registry()));
    assert_eq!(disclosure.expiry.payload_expires_at, NOT_BEFORE_MS + 1);
}

#[test]
fn legacy_0x4201_create_payload_is_never_accepted() {
    let mut legacy = Vec::with_capacity(213);
    legacy.extend_from_slice(&0x4201_u16.to_be_bytes());
    legacy.extend_from_slice(&10_u16.to_be_bytes());
    for fixed in [BUDGET_ID, main_account(), budget_account(BUDGET_ID), ASSET] {
        legacy.extend_from_slice(&fixed);
    }
    legacy.extend_from_slice(&PER_PERIOD_LIMIT.to_be_bytes());
    legacy.extend_from_slice(&PERIOD_LENGTH_MS.to_be_bytes());
    legacy.push(1);
    legacy.extend_from_slice(&0_u128.to_be_bytes());
    legacy.extend_from_slice(&PURPOSE);
    legacy.extend_from_slice(&EXPIRY_MS.to_be_bytes());
    assert_eq!(legacy.len(), 213);
    assert!(refused(CREATE, &legacy));
}

#[test]
fn fund_v1_is_exactly_50_bytes_and_discloses_the_verified_context() {
    let payload = fund_payload(1, BUDGET_ID, 250, None);
    assert_eq!(payload.len(), 50);
    assert_eq!(&payload[34..50], &250_u128.to_be_bytes());
    let canonical = canonical(FUND, &payload);
    assert_eq!(
        bind(&canonical, &registry()).err(),
        Some(DisclosureError::UnsupportedActivity(FUND))
    );
    let context = context(false);
    let disclosure = checked(bind_budget_mutation(&canonical, &registry(), &context));
    let Some(DisclosedNativeOperation::BudgetFund(fund)) = &disclosure.native_operation else {
        panic!("a core v1 fund must disclose as a native budget fund");
    };
    assert_eq!(fund.encoding_version, 1);
    assert_eq!(fund.budget_id, BUDGET_ID);
    assert_eq!(fund.amount, 250);
    assert_eq!(fund.source_sequence, None);
    assert_eq!(fund.context, context);
    assert_eq!(
        disclosure.counterparties,
        vec![
            Counterparty {
                role: CounterpartyRole::Payer,
                account: main_account(),
            },
            Counterparty {
                role: CounterpartyRole::Recipient,
                account: budget_account(BUDGET_ID),
            },
        ]
    );
    assert_eq!(
        disclosure.amounts,
        vec![DisclosedAmount {
            role: AmountRole::Transfer,
            value: 250,
        }]
    );
    assert_eq!(disclosure.asset, ASSET);
    assert_eq!(disclosure.payload_sequence(), Ok(None));
    assert_eq!(disclosure.reencode(), Ok(canonical));
    assert!(disclosure.audit_digest().is_ok());
}

#[test]
fn fund_v2_is_exactly_58_bytes_and_carries_its_source_sequence() {
    let payload = fund_payload(2, BUDGET_ID, 250, Some(31));
    assert_eq!(payload.len(), 58);
    assert_eq!(&payload[50..58], &31_u64.to_be_bytes());
    let canonical = canonical(FUND, &payload);
    let disclosure = checked(bind_budget_mutation(
        &canonical,
        &registry(),
        &context(true),
    ));
    let Some(DisclosedNativeOperation::BudgetFund(fund)) = &disclosure.native_operation else {
        panic!("a core v2 fund must disclose as a native budget fund");
    };
    assert_eq!(fund.encoding_version, 2);
    assert_eq!(fund.source_sequence, Some(31));
    assert_eq!(disclosure.payload_sequence(), Ok(Some(31)));
    assert_eq!(disclosure.reencode(), Ok(canonical));
}

#[test]
fn fund_refuses_wrong_length_version_zero_fields_and_context_mismatch() {
    let v1 = fund_payload(1, BUDGET_ID, 250, None);
    let v2 = fund_payload(2, BUDGET_ID, 250, Some(31));
    let legacy = context(false);
    let native = context(true);
    assert!(mutation_refused(FUND, &v1[..49], &legacy));
    let mut trailing = v1.clone();
    trailing.push(0);
    assert!(mutation_refused(FUND, &trailing, &legacy));
    assert!(mutation_refused(FUND, &v2[..57], &native));
    let mut trailing = v2.clone();
    trailing.push(0);
    assert!(mutation_refused(FUND, &trailing, &native));
    let mut v1_claiming_v2 = v1.clone();
    v1_claiming_v2[1] = 2;
    assert!(mutation_refused(FUND, &v1_claiming_v2, &native));
    let mut v2_claiming_v1 = v2.clone();
    v2_claiming_v1[1] = 1;
    assert!(mutation_refused(FUND, &v2_claiming_v1, &legacy));
    for prefix in [[0_u8, 0], [0, 3], [1, 1], [0x42, 0x02]] {
        let mut wrong = v1.clone();
        wrong[..2].copy_from_slice(&prefix);
        assert!(mutation_refused(FUND, &wrong, &legacy));
    }
    assert!(mutation_refused(
        FUND,
        &fund_payload(1, [0; 32], 250, None),
        &legacy
    ));
    assert!(mutation_refused(
        FUND,
        &fund_payload(1, BUDGET_ID, 0, None),
        &legacy
    ));
    assert!(mutation_refused(
        FUND,
        &fund_payload(2, BUDGET_ID, 250, Some(u64::MAX)),
        &native
    ));
    assert!(mutation_refused(FUND, &v1, &native));
    assert!(mutation_refused(FUND, &v2, &legacy));
    let mismatches = [
        BudgetStateContext {
            budget_id: [0x0c; 32],
            ..legacy
        },
        BudgetStateContext {
            owner: [0x77; 32],
            ..legacy
        },
        BudgetStateContext {
            budget_account: [0x78; 32],
            ..legacy
        },
        BudgetStateContext {
            source_account: budget_account(BUDGET_ID),
            ..legacy
        },
        BudgetStateContext {
            source_account: [0x79; 32],
            ..legacy
        },
    ];
    for mismatch in mismatches {
        assert!(mutation_refused(FUND, &v1, &mismatch));
    }
}

#[test]
fn revoke_is_exactly_42_bytes_and_requires_a_strictly_newer_counter() {
    let payload = revoke_payload(1, BUDGET_ID, REVOCATION_COUNTER + 1);
    assert_eq!(payload.len(), 42);
    assert_eq!(&payload[34..42], &(REVOCATION_COUNTER + 1).to_be_bytes());
    let canonical = canonical(REVOKE, &payload);
    assert_eq!(
        bind(&canonical, &registry()).err(),
        Some(DisclosureError::UnsupportedActivity(REVOKE))
    );
    let context = context(false);
    let disclosure = checked(bind_budget_mutation(&canonical, &registry(), &context));
    let Some(DisclosedNativeOperation::BudgetRevoke(revoke)) = &disclosure.native_operation else {
        panic!("a core revoke must disclose as a native budget revoke");
    };
    assert_eq!(revoke.budget_id, BUDGET_ID);
    assert_eq!(revoke.revocation_sequence, REVOCATION_COUNTER + 1);
    assert_eq!(revoke.context, context);
    assert_eq!(
        disclosure.amounts,
        vec![DisclosedAmount {
            role: AmountRole::Transfer,
            value: context.balance,
        }]
    );
    assert_eq!(
        disclosure.counterparties[0].account,
        budget_account(BUDGET_ID)
    );
    assert_eq!(disclosure.counterparties[1].account, main_account());
    assert_eq!(disclosure.reencode(), Ok(canonical));

    assert!(mutation_refused(
        REVOKE,
        &revoke_payload(1, BUDGET_ID, REVOCATION_COUNTER),
        &context
    ));
    assert!(mutation_refused(
        REVOKE,
        &revoke_payload(1, BUDGET_ID, REVOCATION_COUNTER - 1),
        &context
    ));
    assert!(mutation_refused(
        REVOKE,
        &revoke_payload(1, BUDGET_ID, 0),
        &context
    ));
    assert!(mutation_refused(
        REVOKE,
        &revoke_payload(1, [0; 32], REVOCATION_COUNTER + 1),
        &context
    ));
    assert!(mutation_refused(
        REVOKE,
        &revoke_payload(2, BUDGET_ID, REVOCATION_COUNTER + 1),
        &context
    ));
    assert!(mutation_refused(
        REVOKE,
        &revoke_payload(0, BUDGET_ID, REVOCATION_COUNTER + 1),
        &context
    ));
    assert!(mutation_refused(REVOKE, &payload[..41], &context));
    let mut trailing = payload.clone();
    trailing.push(0);
    assert!(mutation_refused(REVOKE, &trailing, &context));
    assert!(mutation_refused(
        REVOKE,
        &revoke_payload(1, [0x0c; 32], REVOCATION_COUNTER + 1),
        &context
    ));
}

#[test]
fn close_ordinal_is_not_decoded_as_a_defund() {
    let defund_shaped = fund_payload(1, BUDGET_ID, 250, None);
    assert!(refused(CLOSE, &defund_shaped));
    assert!(mutation_refused(CLOSE, &defund_shaped, &context(false)));
}
