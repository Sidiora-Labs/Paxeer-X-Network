use std::fmt::Write as _;
use std::io::{Read as _, Write as _};
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};

use layerx_types::payload::{ActivityType, ModuleId, ModuleRegistration, ModuleRegistry};
use layerx_types::result::{KnownResult, ResultCode};
use layerx_types::vectors::{CodecVector, Corpus};
use layerx_wire::activity::{decode_signed, encode_signed};
use layerx_wire::decode::Decoder;
use layerx_wire::encode::Encoder;
use layerx_wire::hash::{activity_id, payload_hash};
use layerx_wire::{check_ordered_keys, WireError};

const MAX_MESSAGE_BYTES: usize = 1_048_576;
const MAX_PAYLOAD_BYTES: usize = 524_288;

#[derive(Clone, Debug)]
enum Expected {
    Primitive(ResultCode),
    Activity {
        encoded: Vec<u8>,
        identifier: [u8; 32],
        payload_hash: [u8; 32],
    },
}

#[derive(Clone, Debug)]
struct Case {
    name: String,
    command: String,
    expected: Expected,
}

fn hex(bytes: &[u8]) -> String {
    let mut output = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        let _ = write!(output, "{byte:02x}");
    }
    output
}

fn decode_hex(value: &str) -> Result<Vec<u8>, String> {
    if !value.len().is_multiple_of(2) {
        return Err("odd hex width".to_owned());
    }
    value
        .as_bytes()
        .chunks_exact(2)
        .map(|pair| {
            let text = std::str::from_utf8(pair).map_err(|error| error.to_string())?;
            u8::from_str_radix(text, 16).map_err(|error| error.to_string())
        })
        .collect()
}

fn registry(activity_types: &[ActivityType]) -> Result<ModuleRegistry, String> {
    let modules = [
        ModuleId::Asset,
        ModuleId::Escrow,
        ModuleId::Budget,
        ModuleId::Stream,
        ModuleId::Service,
        ModuleId::Perps,
    ];
    let mut registrations = Vec::new();
    for module in modules {
        let values: Vec<_> = activity_types
            .iter()
            .copied()
            .filter(|activity_type| activity_type.module() == module)
            .collect();
        if values.is_empty() {
            continue;
        }
        registrations.push(
            ModuleRegistration::new(module, &values)
                .map_err(|error| format!("module registration: {error:?}"))?,
        );
    }
    ModuleRegistry::new(&registrations).map_err(|error| format!("module registry: {error:?}"))
}

fn primitive_result(vector: &CodecVector) -> ResultCode {
    let result: Result<(), WireError> = match vector.kind.as_str() {
        "u64" => {
            let mut decoder = Decoder::new(&vector.bytes, 0);
            decoder.u64().and_then(|_| decoder.finish())
        }
        "tag" => {
            let mut decoder = Decoder::new(&vector.bytes, 0);
            decoder.tag(3).map(|_| ())
        }
        "bytes4" => {
            let mut decoder = Decoder::new(&vector.bytes, 0);
            decoder.bytes(4).map(|_| ())
        }
        "seq" => {
            let mut decoder = Decoder::new(&vector.bytes, 0);
            decoder.u8().and_then(|first_length| {
                let first = decoder.fixed(usize::from(first_length))?;
                let second_length = decoder.u8()?;
                let second = decoder.fixed(usize::from(second_length))?;
                check_ordered_keys(&[first, second])
            })
        }
        _ => Err(WireError {
            result: KnownResult::InvalidTag.into(),
            offset: 0,
        }),
    };
    result.map_or_else(|error| error.result, |()| KnownResult::Ok.into())
}

fn canonical_activity(
    activity_type: ActivityType,
    payload: &[u8],
    sequence: u64,
) -> Result<Vec<u8>, String> {
    let mut encoder = Encoder::new(MAX_MESSAGE_BYTES);
    encoder.structure_header(0x1001).map_err(debug)?;
    encoder.u8(12).map_err(debug)?;
    encoder.tag(1, 12).map_err(debug)?;
    encoder.u16(1).map_err(debug)?;
    encoder.tag(2, 12).map_err(debug)?;
    encoder.u32(77).map_err(debug)?;
    encoder.tag(3, 12).map_err(debug)?;
    encoder.u32(activity_type.value()).map_err(debug)?;
    encoder.tag(4, 12).map_err(debug)?;
    encoder.bytes(b"did:lx:differential", 255).map_err(debug)?;
    encoder.tag(5, 12).map_err(debug)?;
    encoder
        .bytes(&[0xa1, 1], MAX_PAYLOAD_BYTES)
        .map_err(debug)?;
    encoder.tag(6, 12).map_err(debug)?;
    encoder.u64(sequence).map_err(debug)?;
    encoder.tag(7, 12).map_err(debug)?;
    encoder.u64(1_700_000_000_000).map_err(debug)?;
    encoder.u64(1_700_000_100_000).map_err(debug)?;
    encoder.tag(8, 12).map_err(debug)?;
    encoder.bytes(&[7; 32], 32).map_err(debug)?;
    encoder.tag(9, 12).map_err(debug)?;
    encoder.u128(1).map_err(debug)?;
    encoder.tag(10, 12).map_err(debug)?;
    encoder.bytes(&[0; 32], 32).map_err(debug)?;
    encoder.tag(11, 12).map_err(debug)?;
    encoder.bytes(payload, MAX_PAYLOAD_BYTES).map_err(debug)?;
    encoder.tag(12, 12).map_err(debug)?;
    encoder.bytes(&[9; 64], 128).map_err(debug)?;
    Ok(encoder.finish())
}

fn debug(error: WireError) -> String {
    format!("wire error {:?} at {}", error.result, error.offset)
}

fn activity_case(name: String, bytes: &[u8], registry: &ModuleRegistry) -> Result<Case, String> {
    let activity = decode_signed(bytes, registry).map_err(debug)?;
    let encoded = encode_signed(&activity).map_err(debug)?;
    let identifier = activity_id(&activity).map_err(debug)?;
    let payload_hash = payload_hash(&activity).map_err(debug)?;
    Ok(Case {
        name,
        command: format!("activity signed {}", hex(bytes)),
        expected: Expected::Activity {
            encoded,
            identifier,
            payload_hash,
        },
    })
}

fn build_cases(repository_root: &Path) -> Result<Vec<Case>, String> {
    let corpus = Corpus::load(repository_root).map_err(|error| format!("corpus: {error:?}"))?;
    let registry = registry(&corpus.replay.activity_types)?;
    let mut cases = Vec::new();
    for vector in corpus.valid_codec.iter().chain(&corpus.adversarial_codec) {
        cases.push(Case {
            name: format!("codec/{}", vector.name),
            command: format!("primitive {} {}", vector.kind, hex(&vector.bytes)),
            expected: Expected::Primitive(primitive_result(vector)),
        });
    }
    for (index, bytes) in corpus.replay.canonical_activities.into_iter().enumerate() {
        cases.push(activity_case(
            format!("published/activity-{index}"),
            &bytes,
            &registry,
        )?);
    }
    let mut state = 0x243f_6a88_85a3_08d3_u64;
    for (index, activity_type) in corpus.replay.activity_types.iter().copied().enumerate() {
        state = state
            .wrapping_mul(6_364_136_223_846_793_005)
            .wrapping_add(1_442_695_040_888_963_407);
        let length = usize::from(state.to_be_bytes()[7]);
        let payload: Vec<_> = (0..length)
            .map(|offset| state.to_be_bytes()[offset % 8])
            .collect();
        let bytes = canonical_activity(
            activity_type,
            &payload,
            u64::try_from(index + 1000).map_err(|error| error.to_string())?,
        )?;
        cases.push(activity_case(
            format!("generated/type-{:#010x}", activity_type.value()),
            &bytes,
            &registry,
        )?);
    }
    let first_type = corpus.replay.activity_types[0];
    for length in [0, 1, 31, 32, 255, 256, 1024, MAX_PAYLOAD_BYTES] {
        let payload: Vec<_> = (0..length)
            .map(|offset| offset.to_be_bytes()[std::mem::size_of::<usize>() - 1])
            .collect();
        let bytes = canonical_activity(
            first_type,
            &payload,
            u64::try_from(length + 1).map_err(|error| error.to_string())?,
        )?;
        cases.push(activity_case(
            format!("generated/payload-{length}"),
            &bytes,
            &registry,
        )?);
    }
    Ok(cases)
}

fn first_difference(left: &[u8], right: &[u8]) -> usize {
    left.iter()
        .zip(right)
        .position(|(left, right)| left != right)
        .unwrap_or_else(|| left.len().min(right.len()))
}

fn compare(case: &Case, line: &str) -> Result<(), String> {
    let fields: Vec<_> = line.trim_end().split('|').collect();
    if fields.len() != 4 {
        return Err(format!("{}: malformed C output {line:?}", case.name));
    }
    let status = fields[0]
        .parse::<i32>()
        .map_err(|error| error.to_string())?;
    match &case.expected {
        Expected::Primitive(expected) => {
            if status != expected.raw() {
                return Err(format!(
                    "{}: rejection divergence rust={} c={status}",
                    case.name,
                    expected.raw()
                ));
            }
        }
        Expected::Activity {
            encoded,
            identifier,
            payload_hash,
        } => {
            if status != 0 {
                return Err(format!(
                    "{}: C rejected Rust-accepted value with {status}",
                    case.name
                ));
            }
            let c_encoded = decode_hex(fields[1])?;
            let c_identifier = decode_hex(fields[2])?;
            let c_payload_hash = decode_hex(fields[3])?;
            if c_encoded != *encoded {
                let offset = first_difference(encoded, &c_encoded);
                return Err(format!(
                    "{}: encoding divergence offset={offset} rust={} c={}",
                    case.name,
                    hex(encoded),
                    fields[1]
                ));
            }
            if c_identifier != identifier {
                return Err(format!(
                    "{}: activity-id divergence rust={} c={}",
                    case.name,
                    hex(identifier),
                    fields[2]
                ));
            }
            if c_payload_hash != payload_hash {
                return Err(format!(
                    "{}: payload-hash divergence rust={} c={}",
                    case.name,
                    hex(payload_hash),
                    fields[3]
                ));
            }
        }
    }
    Ok(())
}

/// Runs the process-isolated Rust/C byte, digest, identifier, and rejection
/// differential suite.
///
/// # Errors
///
/// Returns a self-contained first-divergence report or process error.
pub fn agent_wire_differential_harness(
    repository_root: &Path,
    c_reference: &Path,
) -> Result<usize, String> {
    let cases = build_cases(repository_root)?;
    let mut child = Command::new(c_reference)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .spawn()
        .map_err(|error| format!("start C reference: {error}"))?;
    let Some(mut stdin) = child.stdin.take() else {
        return Err("C reference stdin unavailable".to_owned());
    };
    let Some(mut stdout) = child.stdout.take() else {
        return Err("C reference stdout unavailable".to_owned());
    };
    let reader = std::thread::spawn(move || {
        let mut output = String::new();
        stdout
            .read_to_string(&mut output)
            .map_err(|error| error.to_string())?;
        Ok::<_, String>(output)
    });
    for case in &cases {
        writeln!(stdin, "{}", case.command).map_err(|error| error.to_string())?;
    }
    drop(stdin);
    let status = child
        .wait()
        .map_err(|error| format!("wait for C reference: {error}"))?;
    if !status.success() {
        return Err(format!("C reference exited with {status}"));
    }
    let stdout = reader
        .join()
        .map_err(|_| "C reference reader thread panicked".to_owned())??;
    let lines: Vec<_> = stdout.lines().collect();
    if lines.len() != cases.len() {
        return Err(format!(
            "C reference produced {} results for {} cases",
            lines.len(),
            cases.len()
        ));
    }
    for (case, line) in cases.iter().zip(lines) {
        compare(case, line)?;
    }
    Ok(cases.len())
}

fn main() {
    let repository_root = std::env::var_os("LAYERX_REPOSITORY_ROOT")
        .map_or_else(|| PathBuf::from("."), PathBuf::from);
    let Some(c_reference) = std::env::var_os("LAYERX_C_REFERENCE").map(PathBuf::from) else {
        eprintln!("LAYERX_C_REFERENCE is required");
        std::process::exit(2);
    };
    match agent_wire_differential_harness(&repository_root, &c_reference) {
        Ok(count) => println!("wire parity passed: {count} Rust/C cases"),
        Err(error) => {
            eprintln!("wire parity failed: {error}");
            std::process::exit(1);
        }
    }
}
