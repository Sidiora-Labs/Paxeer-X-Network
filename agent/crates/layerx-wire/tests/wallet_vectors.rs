use std::fmt::{Debug, Write as _};
use std::path::{Path, PathBuf};

use layerx_types::account::AccountId;
use layerx_types::activity::{Authority, EnvelopeBuilder, Signature, TimestampBound};
use layerx_types::amount::Amount;
use layerx_types::ids::{Did, IdempotencyKey};
use layerx_types::payload::{ActivityType, ModuleId, ModuleRegistration, ModuleRegistry, Payload};
use layerx_wire::activity::{
    decode_signed, decode_unsigned, encode_signed, encode_signed_envelope, encode_unsigned,
    encode_unsigned_envelope,
};
use layerx_wire::encode::Encoder;
use layerx_wire::hash::{account_id_for_protocol, payload_hash_for, Domain};
use layerx_wire::limits::MAX_MESSAGE_BYTES;
use layerx_wire::sign::{preimage, preimage_unsigned};
use sha2::{Digest as _, Sha256};

type TestResult<T> = Result<T, String>;

const WRITE_VARIABLE: &str = "LAYERX_WIRE_WRITE_VECTORS";
const HEX_LIMIT: usize = 4096;
const DID_KEY: [u8; 32] = [
    0xaf, 0x06, 0xa3, 0xe3, 0x29, 0x17, 0x14, 0xe4, 0xf3, 0x56, 0xc1, 0x9c, 0x9b, 0x15, 0xcd, 0x19,
    0x51, 0xec, 0x6e, 0x66, 0x62, 0xaa, 0x77, 0xbe, 0x07, 0x54, 0x7f, 0x28, 0x93, 0x83, 0x34, 0x1d,
];
const PEER_KEY: [u8; 32] = [0x51; 32];
const NETWORK_ID: u32 = 125;

fn check<T, E: Debug>(value: Result<T, E>, what: &str) -> TestResult<T> {
    value.map_err(|error| format!("{what}: {error:?}"))
}

fn vectors_path() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/support/wallet_vectors/activities.json")
}

fn hex(bytes: &[u8]) -> String {
    let mut out = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        let _ = write!(out, "{byte:02x}");
    }
    out
}

fn sha256(parts: &[&[u8]]) -> [u8; 32] {
    let mut hasher = Sha256::new();
    for part in parts {
        hasher.update(part);
    }
    hasher.finalize().into()
}

fn did_of(key: &[u8; 32]) -> String {
    format!("did:layerx:{}", hex(key))
}

fn account_id(name: &str) -> TestResult<[u8; 32]> {
    let account = check(AccountId::parse(name), name)?;
    check(account_id_for_protocol(&account, 3), name)
}

#[derive(Clone)]
enum Field {
    Bytes(Vec<u8>),
    Repeat(u8, usize),
}

impl Field {
    fn materialise(&self) -> Vec<u8> {
        match self {
            Self::Bytes(bytes) => bytes.clone(),
            Self::Repeat(byte, count) => vec![*byte; *count],
        }
    }

    fn json(&self) -> String {
        match self {
            Self::Bytes(bytes) => format!("{{\"hex\": \"{}\"}}", hex(bytes)),
            Self::Repeat(byte, count) => {
                format!("{{\"repeat\": \"{byte:02x}\", \"count\": {count}}}")
            }
        }
    }
}

fn encoded_json(bytes: &[u8]) -> String {
    if bytes.len() <= HEX_LIMIT {
        format!("{{\"hex\": \"{}\"}}", hex(bytes))
    } else {
        format!(
            "{{\"sha256\": \"{}\", \"length\": {}}}",
            hex(&sha256(&[bytes])),
            bytes.len()
        )
    }
}

struct ActivityCase {
    name: &'static str,
    protocol_version: u16,
    module: ModuleId,
    ordinal: u16,
    actor_did: Field,
    authority: Field,
    account_sequence: u64,
    not_before: u64,
    not_after: u64,
    idempotency_key: [u8; 32],
    fee_limit: u128,
    payload: Field,
    signature: Field,
}

struct GrantCase {
    name: &'static str,
    from: [u8; 32],
    recipient: [u8; 32],
    asset: [u8; 32],
    per_draw_maximum: u128,
    allowance: u128,
    recurring: bool,
    window_length: u64,
    expiration: u64,
    purpose_hash: [u8; 32],
    has_reference: bool,
    reference_hash: [u8; 32],
    revocation_sequence: u64,
    public_key: [u8; 32],
}

struct ReceiveCase {
    name: &'static str,
    grant: GrantCase,
    amount: u128,
    sequence: u64,
    idempotency_key: [u8; 32],
    authorization_kind: u8,
    network_id: u32,
    protocol_version: u16,
}

fn grant_fields(grant: &GrantCase) -> TestResult<Vec<u8>> {
    let mut encoder = Encoder::new(250);
    check(encoder.fixed(&grant.from), "grant from")?;
    check(encoder.fixed(&grant.recipient), "grant recipient")?;
    check(encoder.fixed(&grant.asset), "grant asset")?;
    check(encoder.u128(grant.per_draw_maximum), "grant per draw")?;
    check(encoder.u128(grant.allowance), "grant allowance")?;
    check(encoder.u8(u8::from(grant.recurring)), "grant recurring")?;
    check(encoder.u64(grant.window_length), "grant window")?;
    check(encoder.u64(grant.expiration), "grant expiration")?;
    check(encoder.fixed(&grant.purpose_hash), "grant purpose")?;
    check(
        encoder.u8(u8::from(grant.has_reference)),
        "grant has reference",
    )?;
    check(encoder.fixed(&grant.reference_hash), "grant reference")?;
    check(encoder.u64(grant.revocation_sequence), "grant revocation")?;
    check(encoder.fixed(&grant.public_key), "grant key")?;
    Ok(encoder.finish())
}

fn grant_preimage(grant: &GrantCase) -> TestResult<[u8; 32]> {
    let fields = grant_fields(grant)?;
    Ok(sha256(&[
        Domain::AuthorityHash.tag(),
        b"LXP:GRANT:v1",
        &fields,
    ]))
}

fn context_hash(grant: &GrantCase) -> [u8; 32] {
    if grant.has_reference {
        sha256(&[
            Domain::ContextHash.tag(),
            &grant.purpose_hash,
            &grant.reference_hash,
        ])
    } else {
        sha256(&[Domain::ContextHash.tag(), &grant.purpose_hash])
    }
}

fn receive_preimage(receive: &ReceiveCase) -> TestResult<[u8; 32]> {
    let grant_id = grant_preimage(&receive.grant)?;
    let context = context_hash(&receive.grant);
    let mut encoder = Encoder::new(512);
    check(encoder.fixed(b"LXP:RECEIVE:v1"), "receive domain")?;
    check(encoder.fixed(&receive.grant.from), "receive from")?;
    check(encoder.fixed(&receive.grant.recipient), "receive to")?;
    check(encoder.fixed(&receive.grant.asset), "receive asset")?;
    check(encoder.u128(receive.amount), "receive amount")?;
    check(encoder.fixed(&grant_id), "receive grant")?;
    check(encoder.u64(receive.sequence), "receive sequence")?;
    check(
        encoder.fixed(&receive.idempotency_key),
        "receive idempotency",
    )?;
    check(encoder.fixed(&context), "receive context")?;
    check(encoder.u8(receive.authorization_kind), "receive kind")?;
    check(
        encoder.fixed(&receive.grant.recipient),
        "receive controller",
    )?;
    check(encoder.fixed(&context), "receive signed context")?;
    check(encoder.u32(receive.network_id), "receive network")?;
    check(encoder.u16(receive.protocol_version), "receive protocol")?;
    Ok(sha256(&[
        Domain::SignaturePreimage.tag(),
        &encoder.finish(),
    ]))
}

fn grant_payload(grant: &GrantCase, signature: [u8; 64]) -> TestResult<Vec<u8>> {
    let id = grant_preimage(grant)?;
    let mut encoder = Encoder::new(346);
    check(encoder.fixed(&id), "grant id")?;
    check(encoder.fixed(&grant_fields(grant)?), "grant body")?;
    check(encoder.fixed(&signature), "grant signature")?;
    Ok(encoder.finish())
}

fn transfer_payload(from: &str, to: &str, asset: [u8; 32], amount: u128) -> TestResult<Vec<u8>> {
    let mut encoder = Encoder::new(512);
    check(encoder.fixed(&account_id(from)?), "transfer from")?;
    check(encoder.fixed(&account_id(to)?), "transfer to")?;
    check(encoder.fixed(&asset), "transfer asset")?;
    check(encoder.u128(amount), "transfer amount")?;
    Ok(encoder.finish())
}

fn budget_payload(budget: &str, allowance: u128, period: u64) -> TestResult<Vec<u8>> {
    let mut encoder = Encoder::new(512);
    check(encoder.u16(1), "budget version")?;
    check(encoder.fixed(&account_id(budget)?), "budget account")?;
    check(encoder.u128(allowance), "budget allowance")?;
    check(encoder.u64(period), "budget period")?;
    Ok(encoder.finish())
}

fn program_payload(
    program: [u8; 32],
    legs: &[(&str, [u8; 32], &str, u128)],
) -> TestResult<Vec<u8>> {
    let mut encoder = Encoder::new(4096);
    check(encoder.fixed(&program), "program id")?;
    let count = check(u16::try_from(legs.len()), "program leg count")?;
    check(encoder.u16(count), "program leg count")?;
    for (from, asset, to, amount) in legs {
        check(encoder.fixed(&account_id(from)?), "leg from")?;
        check(encoder.fixed(asset), "leg asset")?;
        check(encoder.fixed(&account_id(to)?), "leg to")?;
        check(encoder.u128(*amount), "leg amount")?;
    }
    Ok(encoder.finish())
}

fn base_grant(name: &'static str) -> TestResult<GrantCase> {
    let payer = did_of(&DID_KEY);
    let payee = did_of(&PEER_KEY);
    Ok(GrantCase {
        name,
        from: account_id(&format!("agent:{payer}:main"))?,
        recipient: account_id(&format!("agent:{payee}:main"))?,
        asset: [0x0a; 32],
        per_draw_maximum: 1_000,
        allowance: 50_000,
        recurring: false,
        window_length: 0,
        expiration: 1_900_000_000,
        purpose_hash: [0x64; 32],
        has_reference: false,
        reference_hash: [0; 32],
        revocation_sequence: 0,
        public_key: DID_KEY,
    })
}

fn grants() -> TestResult<Vec<GrantCase>> {
    let one_shot = base_grant("grant-one-shot")?;
    let mut recurring = base_grant("grant-recurring-with-reference")?;
    recurring.recurring = true;
    recurring.window_length = 86_400;
    recurring.has_reference = true;
    recurring.reference_hash = [0x72; 32];
    recurring.revocation_sequence = 3;
    recurring.per_draw_maximum = u128::MAX;
    recurring.allowance = u128::MAX;
    Ok(vec![one_shot, recurring])
}

fn receives() -> TestResult<Vec<ReceiveCase>> {
    let mut with_reference = base_grant("receive-with-reference")?;
    with_reference.has_reference = true;
    with_reference.reference_hash = [0x72; 32];
    Ok(vec![
        ReceiveCase {
            name: "receive-owner",
            grant: base_grant("receive-owner")?,
            amount: 250,
            sequence: 1,
            idempotency_key: [0x31; 32],
            authorization_kind: 1,
            network_id: NETWORK_ID,
            protocol_version: 2,
        },
        ReceiveCase {
            name: "receive-with-reference",
            grant: with_reference,
            amount: 1_000,
            sequence: 7,
            idempotency_key: [0x32; 32],
            authorization_kind: 2,
            network_id: NETWORK_ID,
            protocol_version: 3,
        },
    ])
}

fn activities() -> TestResult<Vec<ActivityCase>> {
    let actor = did_of(&DID_KEY);
    let peer = did_of(&PEER_KEY);
    let main = format!("agent:{actor}:main");
    let peer_main = format!("agent:{peer}:main");
    let token = [0x7b; 32];
    let approval_grant = base_grant("approval")?;
    let fixed_did = || Field::Bytes(actor.as_bytes().to_vec());
    let owner = || Field::Bytes(DID_KEY.to_vec());
    let signature = |byte: u8| Field::Bytes(vec![byte; 64]);
    let maximum_did = format!("did:layerx:{}", "a".repeat(255 - 11));
    let fixed_bytes = 4 + 1 + 12 + 2 + 4 + 4 + 8 + 16 + 36 + 16 + 36;
    let payload_maximum = 524_288;
    let signature_maximum = 128;
    let authority_maximum = MAX_MESSAGE_BYTES
        - fixed_bytes
        - (4 + maximum_did.len())
        - (4 + payload_maximum)
        - (4 + signature_maximum)
        - 4;
    Ok(vec![
        ActivityCase {
            name: "native-send",
            protocol_version: 2,
            module: ModuleId::Asset,
            ordinal: 5,
            actor_did: fixed_did(),
            authority: owner(),
            account_sequence: 1,
            not_before: 1_800_000_000,
            not_after: 1_800_000_600,
            idempotency_key: [0x11; 32],
            fee_limit: 10_000,
            payload: Field::Bytes(transfer_payload(&main, &peer_main, [0; 32], 5_000_000)?),
            signature: signature(0x21),
        },
        ActivityCase {
            name: "token-send",
            protocol_version: 3,
            module: ModuleId::Asset,
            ordinal: 5,
            actor_did: fixed_did(),
            authority: owner(),
            account_sequence: 2,
            not_before: 1_800_000_000,
            not_after: 1_800_000_600,
            idempotency_key: [0x12; 32],
            fee_limit: 12_500,
            payload: Field::Bytes(transfer_payload(
                &format!("agent:{actor}:asset:{}", hex(&token)),
                &format!("agent:{peer}:asset:{}", hex(&token)),
                token,
                75_000,
            )?),
            signature: signature(0x22),
        },
        ActivityCase {
            name: "approval",
            protocol_version: 3,
            module: ModuleId::Asset,
            ordinal: 7,
            actor_did: fixed_did(),
            authority: owner(),
            account_sequence: 3,
            not_before: 1_800_000_000,
            not_after: 1_800_003_600,
            idempotency_key: [0x13; 32],
            fee_limit: 20_000,
            payload: Field::Bytes(grant_payload(&approval_grant, [0x5a; 64])?),
            signature: signature(0x23),
        },
        ActivityCase {
            name: "budget-change",
            protocol_version: 3,
            module: ModuleId::Budget,
            ordinal: 2,
            actor_did: fixed_did(),
            authority: Field::Bytes(PEER_KEY.to_vec()),
            account_sequence: 4,
            not_before: 1_800_000_000,
            not_after: 1_800_000_600,
            idempotency_key: [0x14; 32],
            fee_limit: 8_000,
            payload: Field::Bytes(budget_payload(
                &format!("agent:{actor}:budget:daily"),
                250_000,
                86_400,
            )?),
            signature: signature(0x24),
        },
        ActivityCase {
            name: "agent-action",
            protocol_version: 2,
            module: ModuleId::Programs,
            ordinal: 5,
            actor_did: fixed_did(),
            authority: owner(),
            account_sequence: 5,
            not_before: 0,
            not_after: u64::MAX,
            idempotency_key: [0x15; 32],
            fee_limit: 40_000,
            payload: Field::Bytes(program_payload(
                [0x9c; 32],
                &[
                    (main.as_str(), [0; 32], peer_main.as_str(), 1_000),
                    (peer_main.as_str(), token, main.as_str(), 2_000),
                ],
            )?),
            signature: signature(0x25),
        },
        ActivityCase {
            name: "maximum-field-sizes",
            protocol_version: 3,
            module: ModuleId::Web,
            ordinal: 1,
            actor_did: Field::Bytes(maximum_did.into_bytes()),
            authority: Field::Repeat(0x41, authority_maximum),
            account_sequence: u64::MAX,
            not_before: u64::MAX,
            not_after: u64::MAX,
            idempotency_key: [0xff; 32],
            fee_limit: u128::MAX,
            payload: Field::Repeat(0x50, payload_maximum),
            signature: Field::Repeat(0x53, signature_maximum),
        },
    ])
}

fn registry(cases: &[ActivityCase]) -> TestResult<ModuleRegistry> {
    let mut registrations = Vec::new();
    for module in ModuleId::ALL {
        let mut types: Vec<ActivityType> = Vec::new();
        for case in cases.iter().filter(|case| case.module == module) {
            let kind = check(ActivityType::new(module, case.ordinal), case.name)?;
            if !types.contains(&kind) {
                types.push(kind);
            }
        }
        if types.is_empty() {
            continue;
        }
        types.sort();
        registrations.push(check(
            ModuleRegistration::new(module, &types),
            "registration",
        )?);
    }
    check(ModuleRegistry::new(&registrations), "registry")
}

fn activity_json(case: &ActivityCase, registry: &ModuleRegistry) -> TestResult<String> {
    let kind = check(ActivityType::new(case.module, case.ordinal), case.name)?;
    let payload = check(
        Payload::new(registry, kind, &case.payload.materialise()),
        case.name,
    )?;
    let payload_hash = check(payload_hash_for(&payload), case.name)?;
    let mut builder = EnvelopeBuilder::new();
    check(builder.protocol_version(case.protocol_version), case.name)?;
    check(builder.network_id(NETWORK_ID), case.name)?;
    check(builder.activity_type(kind), case.name)?;
    check(
        builder.actor_did(check(Did::new(&case.actor_did.materialise()), case.name)?),
        case.name,
    )?;
    check(
        builder.authority(check(
            Authority::owner(&case.authority.materialise()),
            case.name,
        )?),
        case.name,
    )?;
    check(builder.account_sequence(case.account_sequence), case.name)?;
    check(
        builder.timestamp_bound(check(
            TimestampBound::new(case.not_before, case.not_after),
            case.name,
        )?),
        case.name,
    )?;
    check(
        builder.idempotency_key(IdempotencyKey::new(case.idempotency_key)),
        case.name,
    )?;
    check(
        builder.fee_limit(Amount::from_u128(case.fee_limit)),
        case.name,
    )?;
    check(builder.payload_hash(payload_hash), case.name)?;
    check(builder.payload(payload), case.name)?;
    let unsigned = check(builder.build(), case.name)?;
    let unsigned_bytes = check(encode_unsigned_envelope(&unsigned), case.name)?;
    let signing = check(preimage_unsigned(&unsigned), case.name)?;
    let signed = unsigned.attach_signature(check(
        Signature::new(&case.signature.materialise()),
        case.name,
    )?);
    let signed_bytes = check(encode_signed_envelope(&signed), case.name)?;

    let decoded_signed = check(decode_signed(&signed_bytes, registry), case.name)?;
    let decoded_unsigned = check(decode_unsigned(&unsigned_bytes, registry), case.name)?;
    if check(encode_signed(&decoded_signed), case.name)? != signed_bytes
        || check(encode_unsigned(&decoded_signed), case.name)? != unsigned_bytes
        || check(encode_unsigned(&decoded_unsigned), case.name)? != unsigned_bytes
        || check(preimage(&decoded_signed), case.name)?.as_bytes() != signing.as_bytes()
        || check(preimage(&decoded_unsigned), case.name)?.as_bytes() != signing.as_bytes()
        || decoded_signed.account_sequence() != case.account_sequence
    {
        return Err(format!(
            "{}: decoded activity does not round trip",
            case.name
        ));
    }

    let mut out = String::new();
    let _ = write!(
        out,
        "    {{\n      \"name\": \"{}\",\n      \"protocol_version\": {},\n      \"network_id\": {},\n      \"activity_type\": {},\n      \"actor_did\": {},\n      \"authority\": {},\n      \"account_sequence\": \"{}\",\n      \"not_before\": \"{}\",\n      \"not_after\": \"{}\",\n      \"idempotency_key\": \"{}\",\n      \"fee_limit\": \"{}\",\n      \"payload_hash\": \"{}\",\n      \"payload\": {},\n      \"signature\": {},\n      \"unsigned\": {},\n      \"signed\": {},\n      \"signature_preimage\": \"{}\"\n    }}",
        case.name,
        case.protocol_version,
        NETWORK_ID,
        kind.value(),
        case.actor_did.json(),
        case.authority.json(),
        case.account_sequence,
        case.not_before,
        case.not_after,
        hex(&case.idempotency_key),
        case.fee_limit,
        hex(&payload_hash),
        case.payload.json(),
        case.signature.json(),
        encoded_json(&unsigned_bytes),
        encoded_json(&signed_bytes),
        hex(signing.as_bytes()),
    );
    Ok(out)
}

fn grant_json(grant: &GrantCase, indent: &str) -> TestResult<String> {
    let mut out = String::new();
    let _ = write!(
        out,
        "{indent}\"from\": \"{}\",\n{indent}\"recipient\": \"{}\",\n{indent}\"asset\": \"{}\",\n{indent}\"per_draw_maximum\": \"{}\",\n{indent}\"allowance\": \"{}\",\n{indent}\"recurring\": {},\n{indent}\"window_length\": \"{}\",\n{indent}\"expiration\": \"{}\",\n{indent}\"purpose_hash\": \"{}\",\n{indent}\"has_reference\": {},\n{indent}\"reference_hash\": \"{}\",\n{indent}\"revocation_sequence\": \"{}\",\n{indent}\"public_key\": \"{}\",\n{indent}\"grant_id\": \"{}\"",
        hex(&grant.from),
        hex(&grant.recipient),
        hex(&grant.asset),
        grant.per_draw_maximum,
        grant.allowance,
        grant.recurring,
        grant.window_length,
        grant.expiration,
        hex(&grant.purpose_hash),
        grant.has_reference,
        hex(&grant.reference_hash),
        grant.revocation_sequence,
        hex(&grant.public_key),
        hex(&grant_preimage(grant)?),
    );
    Ok(out)
}

fn render() -> TestResult<String> {
    let cases = activities()?;
    let registry = registry(&cases)?;
    let mut activity_entries = Vec::new();
    for case in &cases {
        activity_entries.push(activity_json(case, &registry)?);
    }

    let actor = did_of(&DID_KEY);
    let names = [
        format!("agent:{actor}:main"),
        format!("agent:{actor}:asset:{}", hex(&[0x7b; 32])),
        format!("agent:{actor}:budget:daily"),
        format!("agent:{actor}:escrow:order-17"),
        format!("agent:{actor}:margin:position-3"),
        format!("agent:{}:main", did_of(&PEER_KEY)),
        "system:fees".to_owned(),
        "system:insurance".to_owned(),
        "system:paxeer-reserve".to_owned(),
        "system:paxeer-withdrawals".to_owned(),
        "system:liquidity:pax-sid".to_owned(),
    ];
    let mut account_entries = Vec::new();
    for name in &names {
        account_entries.push(format!(
            "    {{\"name\": \"{name}\", \"account_id\": \"{}\"}}",
            hex(&account_id(name)?)
        ));
    }

    let mut grant_entries = Vec::new();
    for grant in grants()? {
        grant_entries.push(format!(
            "    {{\n      \"name\": \"{}\",\n{},\n      \"preimage\": \"{}\"\n    }}",
            grant.name,
            grant_json(&grant, "      ")?,
            hex(&grant_preimage(&grant)?)
        ));
    }

    let mut receive_entries = Vec::new();
    for receive in receives()? {
        receive_entries.push(format!(
            "    {{\n      \"name\": \"{}\",\n      \"grant\": {{\n{}\n      }},\n      \"amount\": \"{}\",\n      \"sequence\": \"{}\",\n      \"idempotency_key\": \"{}\",\n      \"context_hash\": \"{}\",\n      \"authorization_kind\": {},\n      \"network_id\": {},\n      \"protocol_version\": {},\n      \"preimage\": \"{}\"\n    }}",
            receive.name,
            grant_json(&receive.grant, "        ")?,
            receive.amount,
            receive.sequence,
            hex(&receive.idempotency_key),
            hex(&context_hash(&receive.grant)),
            receive.authorization_kind,
            receive.network_id,
            receive.protocol_version,
            hex(&receive_preimage(&receive)?)
        ));
    }

    Ok(format!(
        "{{\n  \"generator\": \"agent/crates/layerx-wire/tests/wallet_vectors.rs\",\n  \"activities\": [\n{}\n  ],\n  \"accounts\": [\n{}\n  ],\n  \"grants\": [\n{}\n  ],\n  \"receives\": [\n{}\n  ]\n}}\n",
        activity_entries.join(",\n"),
        account_entries.join(",\n"),
        grant_entries.join(",\n"),
        receive_entries.join(",\n"),
    ))
}

#[test]
fn wallet_vectors_match_the_canonical_encoder() -> TestResult<()> {
    let rendered = render()?;
    let path = vectors_path();
    if std::env::var_os(WRITE_VARIABLE).is_some() {
        if let Some(parent) = path.parent() {
            check(std::fs::create_dir_all(parent), "create vector directory")?;
        }
        check(std::fs::write(&path, &rendered), "write vectors")?;
    }
    let stored = check(std::fs::read_to_string(&path), "read vectors")?;
    if stored != rendered {
        return Err(format!(
            "{} differs from the canonical encoder; regenerate with {WRITE_VARIABLE}=1",
            path.display()
        ));
    }
    Ok(())
}

#[test]
fn maximum_case_fills_the_message_budget() -> TestResult<()> {
    let cases = activities()?;
    let registry = registry(&cases)?;
    let Some(maximum) = cases.iter().find(|case| case.name == "maximum-field-sizes") else {
        return Err("maximum case missing".to_owned());
    };
    let rendered = activity_json(maximum, &registry)?;
    if !rendered.contains(&format!("\"length\": {MAX_MESSAGE_BYTES}")) {
        return Err("maximum signed activity does not fill the message budget".to_owned());
    }
    Ok(())
}
