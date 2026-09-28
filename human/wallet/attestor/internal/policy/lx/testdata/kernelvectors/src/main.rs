use std::fmt::{Debug, Write as _};
use std::future::Future;
use std::pin::pin;
use std::task::{Context, Poll, Waker};

use ed25519_dalek::{Signer as _, SigningKey};
use layerx_crypto::disclosure::bind;
use layerx_crypto::local::LocalSigner;
use layerx_crypto::payments::{Grant, Payment, TransferLeg};
use layerx_crypto::send::{
    encode_payment_envelope, encode_send_envelope, send_context_hash, EnvelopeOptions,
    SendCondition, SendDebit,
};
use layerx_crypto::SignatureMessage;
use layerx_types::account::AccountId;
use layerx_types::activity::{Authority, EnvelopeBuilder, TimestampBound};
use layerx_types::amount::Amount;
use layerx_types::ids::{Did, IdempotencyKey};
use layerx_types::payload::{ActivityType, ModuleId, ModuleRegistration, ModuleRegistry, Payload};
use layerx_wire::activity::encode_unsigned_envelope;
use layerx_wire::encode::Encoder;
use layerx_wire::hash::{account_id_for_protocol, Domain};
use sha2::{Digest as _, Sha256};

const OWNER_SEED: [u8; 32] = [0x07; 32];
const PEER_SEED: [u8; 32] = [0x08; 32];
const NETWORK_ID: u32 = 125;
const PROTOCOL_VERSION: u16 = 3;
const NOT_BEFORE: u64 = 1_800_000_000;
const NOT_AFTER: u64 = 1_800_000_600;
const TOKEN_ASSET: [u8; 32] = [0x7b; 32];
const GRANT_ASSET: [u8; 32] = [0x0a; 32];
const PROGRAM: [u8; 32] = [0x9c; 32];

fn ok<T, E: Debug>(value: Result<T, E>, what: &str) -> T {
    match value {
        Ok(value) => value,
        Err(error) => panic!("{what}: {error:?}"),
    }
}

fn block_on<F: Future>(future: F) -> F::Output {
    let mut future = pin!(future);
    let mut context = Context::from_waker(Waker::noop());
    loop {
        if let Poll::Ready(value) = future.as_mut().poll(&mut context) {
            return value;
        }
    }
}

fn hex(bytes: &[u8]) -> String {
    let mut out = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        let _ = write!(out, "{byte:02x}");
    }
    out
}

fn public_key(seed: &[u8; 32]) -> [u8; 32] {
    SigningKey::from_bytes(seed).verifying_key().to_bytes()
}

fn did(key: &[u8; 32]) -> String {
    format!("did:layerx:{}", hex(key))
}

fn account(name: &str) -> [u8; 32] {
    let parsed = ok(AccountId::parse(name), name);
    ok(account_id_for_protocol(&parsed, PROTOCOL_VERSION), name)
}

fn main_account(key: &[u8; 32]) -> [u8; 32] {
    account(&format!("agent:{}:main", did(key)))
}

fn asset_account(key: &[u8; 32], asset: &[u8; 32]) -> [u8; 32] {
    account(&format!("agent:{}:asset:{}", did(key), hex(asset)))
}

fn idempotency(label: &str) -> [u8; 32] {
    Sha256::digest(label.as_bytes()).into()
}

struct Case {
    name: &'static str,
    module: ModuleId,
    ordinal: u16,
    sequence: u64,
    idempotency_key: [u8; 32],
    payload: Vec<u8>,
    accepted: bool,
    from: [u8; 32],
    to: [u8; 32],
    asset: [u8; 32],
    amount: u128,
}

fn options(
    actor: &str,
    key: [u8; 32],
    sequence: u64,
    idempotency_key: [u8; 32],
) -> EnvelopeOptions<'_> {
    EnvelopeOptions {
        actor,
        public_key: key,
        protocol_version: PROTOCOL_VERSION,
        network_id: NETWORK_ID,
        identity_sequence: sequence,
        idempotency_key,
        fee_limit: 1_000,
        not_before: NOT_BEFORE,
        not_after: NOT_AFTER,
    }
}

fn envelope(case: &Case, actor: &str, key: [u8; 32]) -> (Vec<u8>, ModuleRegistry) {
    let kind = ok(
        ActivityType::new(case.module, case.ordinal),
        "activity type",
    );
    let registration = ok(
        ModuleRegistration::new(case.module, &[kind]),
        "registration",
    );
    let registry = ok(ModuleRegistry::new(&[registration]), "registry");
    let mut hash = Sha256::new();
    hash.update(Domain::PayloadHash.tag());
    hash.update(&case.payload);
    let mut builder = EnvelopeBuilder::new();
    ok(
        builder.protocol_version(PROTOCOL_VERSION),
        "protocol version",
    );
    ok(builder.network_id(NETWORK_ID), "network");
    ok(builder.activity_type(kind), "activity type");
    ok(
        builder.actor_did(ok(Did::new(actor.as_bytes()), "did")),
        "actor",
    );
    ok(
        builder.authority(ok(Authority::owner(&key), "authority")),
        "authority",
    );
    ok(builder.account_sequence(case.sequence), "sequence");
    ok(
        builder.timestamp_bound(ok(TimestampBound::new(NOT_BEFORE, NOT_AFTER), "bound")),
        "bound",
    );
    ok(
        builder.idempotency_key(IdempotencyKey::new(case.idempotency_key)),
        "idempotency",
    );
    ok(builder.fee_limit(Amount::from_u128(1_000)), "fee limit");
    ok(builder.payload_hash(hash.finalize().into()), "payload hash");
    ok(
        builder.payload(ok(Payload::new(&registry, kind, &case.payload), "payload")),
        "payload",
    );
    let built = ok(builder.build(), "envelope");
    (ok(encode_unsigned_envelope(&built), "canonical"), registry)
}

fn send_debit(
    from: [u8; 32],
    to: [u8; 32],
    asset: [u8; 32],
    amount: u128,
    source_sequence: u64,
    idempotency_key: [u8; 32],
    conditions: Vec<SendCondition>,
) -> SendDebit {
    SendDebit {
        from,
        to,
        asset,
        amount,
        source_sequence,
        idempotency_key,
        expires_at: NOT_AFTER,
        context_hash: send_context_hash(&from, &to, &asset, amount, &idempotency_key),
        conditions,
        authorization_kind: 1,
        network_id: NETWORK_ID,
        protocol_version: PROTOCOL_VERSION,
    }
}

fn grant(owner: &[u8; 32], peer: &[u8; 32]) -> Grant {
    let mut grant = Grant {
        id: [0; 32],
        from: main_account(owner),
        recipient: main_account(peer),
        asset: GRANT_ASSET,
        per_draw_maximum: 5_000,
        allowance: 50_000,
        recurring: true,
        window_length: 86_400,
        expiration: NOT_AFTER + 3_000,
        purpose_hash: [0x33; 32],
        has_reference: false,
        reference_hash: [0; 32],
        revocation_sequence: 0,
        public_key: *owner,
        signature: [0; 64],
    };
    let mut fields = Encoder::new(512);
    for part in [&grant.from, &grant.recipient, &grant.asset] {
        ok(fields.fixed(part), "grant field");
    }
    ok(fields.u128(grant.per_draw_maximum), "grant per draw");
    ok(fields.u128(grant.allowance), "grant allowance");
    ok(fields.u8(u8::from(grant.recurring)), "grant recurring");
    ok(fields.u64(grant.window_length), "grant window");
    ok(fields.u64(grant.expiration), "grant expiration");
    ok(fields.fixed(&grant.purpose_hash), "grant purpose");
    ok(
        fields.u8(u8::from(grant.has_reference)),
        "grant reference flag",
    );
    ok(fields.fixed(&grant.reference_hash), "grant reference");
    ok(fields.u64(grant.revocation_sequence), "grant revocation");
    ok(fields.fixed(&grant.public_key), "grant key");
    let mut hash = Sha256::new();
    hash.update(Domain::AuthorityHash.tag());
    hash.update(b"LXP:GRANT:v1");
    hash.update(fields.finish());
    grant.id = hash.finalize().into();
    grant.signature = SigningKey::from_bytes(&OWNER_SEED)
        .sign(&grant.id)
        .to_bytes();
    grant
}

fn cases(owner: &[u8; 32], peer: &[u8; 32], actor: &str) -> Vec<Case> {
    let signer = LocalSigner::new(OWNER_SEED);
    let owner_main = main_account(owner);
    let peer_main = main_account(peer);
    let mut out = Vec::new();

    let key = idempotency("kernel vector native send");
    let debit = send_debit(
        owner_main,
        peer_main,
        [0; 32],
        5_000_000,
        7,
        key,
        Vec::new(),
    );
    out.push(Case {
        name: "native-send",
        module: ModuleId::Asset,
        ordinal: 5,
        sequence: 1,
        idempotency_key: key,
        payload: ok(block_on(debit.sign(&signer)), "native send"),
        accepted: true,
        from: owner_main,
        to: peer_main,
        asset: [0; 32],
        amount: 5_000_000,
    });

    let key = idempotency("kernel vector token send");
    let from = asset_account(owner, &TOKEN_ASSET);
    let to = asset_account(peer, &TOKEN_ASSET);
    let conditions = vec![
        SendCondition {
            kind: 1,
            timestamp: NOT_BEFORE,
        },
        SendCondition {
            kind: 2,
            timestamp: NOT_AFTER,
        },
    ];
    let debit = send_debit(from, to, TOKEN_ASSET, 75_000, 8, key, conditions);
    out.push(Case {
        name: "token-send",
        module: ModuleId::Asset,
        ordinal: 5,
        sequence: 2,
        idempotency_key: key,
        payload: ok(block_on(debit.sign(&signer)), "token send"),
        accepted: true,
        from,
        to,
        asset: TOKEN_ASSET,
        amount: 75_000,
    });

    let key = idempotency("kernel vector tampered send");
    let debit = send_debit(
        owner_main,
        peer_main,
        [0; 32],
        5_000_000,
        9,
        key,
        Vec::new(),
    );
    let mut tampered = ok(block_on(debit.sign(&signer)), "tampered send");
    let signature_offset = tampered.len() - 167 + 65;
    tampered[signature_offset] ^= 1;
    out.push(Case {
        name: "tampered-authorization-send",
        module: ModuleId::Asset,
        ordinal: 5,
        sequence: 3,
        idempotency_key: key,
        payload: tampered,
        accepted: false,
        from: owner_main,
        to: peer_main,
        asset: [0; 32],
        amount: 5_000_000,
    });

    let key = idempotency("kernel vector bare transfer");
    let mut bare = Encoder::new(512);
    ok(bare.fixed(&owner_main), "bare from");
    ok(bare.fixed(&peer_main), "bare to");
    ok(bare.fixed(&[0; 32]), "bare asset");
    ok(bare.u128(5_000_000), "bare amount");
    out.push(Case {
        name: "bare-transfer",
        module: ModuleId::Asset,
        ordinal: 5,
        sequence: 4,
        idempotency_key: key,
        payload: bare.finish(),
        accepted: false,
        from: owner_main,
        to: peer_main,
        asset: [0; 32],
        amount: 5_000_000,
    });

    let key = idempotency("kernel vector budget fund");
    let budget = account(&format!("agent:{actor}:budget:{}", hex(&[0x42; 32])));
    let mut fund = Encoder::new(512);
    ok(fund.u16(0x4202), "fund tag");
    ok(fund.u16(6), "fund fields");
    ok(fund.fixed(&[0x42; 32]), "fund budget id");
    ok(fund.fixed(&owner_main), "fund from");
    ok(fund.fixed(&budget), "fund to");
    ok(fund.fixed(&[0; 32]), "fund asset");
    ok(fund.u128(250_000), "fund amount");
    ok(fund.fixed(&key), "fund idempotency");
    out.push(Case {
        name: "budget-fund",
        module: ModuleId::Budget,
        ordinal: 2,
        sequence: 5,
        idempotency_key: key,
        payload: fund.finish(),
        accepted: true,
        from: owner_main,
        to: budget,
        asset: [0; 32],
        amount: 250_000,
    });

    let key = idempotency("kernel vector untagged allowance");
    let mut allowance = Encoder::new(512);
    ok(allowance.u16(1), "allowance version");
    ok(allowance.fixed(&budget), "allowance account");
    ok(allowance.u128(250_000), "allowance amount");
    ok(allowance.u64(86_400), "allowance period");
    out.push(Case {
        name: "untagged-allowance",
        module: ModuleId::Budget,
        ordinal: 2,
        sequence: 6,
        idempotency_key: key,
        payload: allowance.finish(),
        accepted: false,
        from: budget,
        to: budget,
        asset: [0; 32],
        amount: 250_000,
    });

    let key = idempotency("kernel vector grant issue");
    let issued = grant(owner, peer);
    out.push(Case {
        name: "grant-issue",
        module: ModuleId::Asset,
        ordinal: 7,
        sequence: 7,
        idempotency_key: key,
        payload: ok(
            Payment::IssueGrant(issued.clone()).encode(actor.as_bytes()),
            "grant issue",
        ),
        accepted: true,
        from: issued.from,
        to: issued.recipient,
        asset: issued.asset,
        amount: issued.allowance,
    });

    let key = idempotency("kernel vector program transfer");
    let transfer = Payment::ProgramTransfer {
        program: PROGRAM,
        legs: vec![TransferLeg {
            from: owner_main,
            asset: [0; 32],
            to: peer_main,
            amount: 1_000,
        }],
    };
    out.push(Case {
        name: "program-transfer",
        module: ModuleId::Programs,
        ordinal: 5,
        sequence: 8,
        idempotency_key: key,
        payload: ok(transfer.encode(actor.as_bytes()), "program transfer"),
        accepted: true,
        from: owner_main,
        to: peer_main,
        asset: [0; 32],
        amount: 1_000,
    });
    out
}

fn main() {
    let owner = public_key(&OWNER_SEED);
    let peer = public_key(&PEER_SEED);
    let actor = did(&owner);
    let mut rendered = String::new();
    let _ = writeln!(rendered, "{{");
    let _ = writeln!(
        rendered,
        "  \"generator\": \"human/wallet/attestor/internal/policy/lx/testdata/kernelvectors\","
    );
    let _ = writeln!(rendered, "  \"public_key\": \"{}\",", hex(&owner));
    let _ = writeln!(rendered, "  \"peer_public_key\": \"{}\",", hex(&peer));
    let _ = writeln!(rendered, "  \"network_id\": {NETWORK_ID},");
    let _ = writeln!(rendered, "  \"activities\": [");
    let all = cases(&owner, &peer, &actor);
    for (index, case) in all.iter().enumerate() {
        let (canonical, registry) = envelope(case, &actor, owner);
        let accepted = bind(&canonical, &registry).is_ok();
        assert_eq!(accepted, case.accepted, "{}: binder acceptance", case.name);
        if case.accepted {
            let encoded = if case.module == ModuleId::Asset && case.ordinal == 5 {
                encode_send_envelope(
                    &case.payload,
                    &options(&actor, owner, case.sequence, case.idempotency_key),
                )
            } else {
                encode_payment_envelope(
                    case.module,
                    case.ordinal,
                    &case.payload,
                    &options(&actor, owner, case.sequence, case.idempotency_key),
                )
            };
            let encoded = ok(encoded, case.name);
            assert_eq!(
                encoded.canonical, canonical,
                "{}: envelope encoder",
                case.name
            );
        }
        let preimage = ok(
            SignatureMessage::new(
                Domain::SignaturePreimage,
                PROTOCOL_VERSION,
                NETWORK_ID,
                &canonical,
            ),
            "preimage",
        )
        .digest();
        let _ = writeln!(rendered, "    {{");
        let _ = writeln!(rendered, "      \"name\": \"{}\",", case.name);
        let _ = writeln!(rendered, "      \"module\": {},", case.module as u16);
        let _ = writeln!(rendered, "      \"operation\": {},", case.ordinal);
        let _ = writeln!(rendered, "      \"accepted\": {},", case.accepted);
        let _ = writeln!(rendered, "      \"sequence\": {},", case.sequence);
        let _ = writeln!(rendered, "      \"not_before\": {NOT_BEFORE},");
        let _ = writeln!(rendered, "      \"not_after\": {NOT_AFTER},");
        let _ = writeln!(rendered, "      \"from\": \"{}\",", hex(&case.from));
        let _ = writeln!(rendered, "      \"to\": \"{}\",", hex(&case.to));
        let _ = writeln!(rendered, "      \"asset\": \"{}\",", hex(&case.asset));
        let _ = writeln!(rendered, "      \"amount\": \"{}\",", case.amount);
        let _ = writeln!(rendered, "      \"payload\": \"{}\",", hex(&case.payload));
        let _ = writeln!(rendered, "      \"unsigned\": \"{}\",", hex(&canonical));
        let _ = writeln!(
            rendered,
            "      \"signature_preimage\": \"{}\"",
            hex(&preimage)
        );
        let separator = if index + 1 == all.len() { "" } else { "," };
        let _ = writeln!(rendered, "    }}{separator}");
    }
    let _ = writeln!(rendered, "  ]");
    let _ = writeln!(rendered, "}}");
    print!("{rendered}");
}
