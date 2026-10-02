use alloc::format;
use alloc::string::String;
use core::fmt::Write;

use crate::bindgen::{BindingGenerator, Entry, Type};

impl BindingGenerator {
    #[must_use]
    pub fn generate_swift(&self) -> String {
        let mut out = String::from(SWIFT_RUNTIME);
        let _ = writeln!(out, "public let layerXInterfaceDigest: [UInt8] = {:?}", self.digest);
        let _ = writeln!(out, "public let layerXCodeHash: [UInt8] = {:?}", self.code_hash);
        for entry in &self.entries {
            swift_entry(&mut out, entry);
        }
        out
    }
}

const SWIFT_RUNTIME: &str = r#"public enum LxBindingRefusal: Error, Equatable {
    case codeHashMismatch
    case staleInterface
    case invalidLength
    case tooLong
    case wrongConvention
    case wrongTag
    case malformed
    case truncated
    case trailingBytes
    case unknownFailure(UInt32)
}
public struct LxUInt128: Equatable, Sendable {
    public let bigEndian: [UInt8]
    public init(bigEndian: [UInt8]) throws {
        guard bigEndian.count == 16 else { throw LxBindingRefusal.invalidLength }
        self.bigEndian = bigEndian
    }
}
public struct LxInt128: Equatable, Sendable {
    public let bigEndian: [UInt8]
    public init(bigEndian: [UInt8]) throws {
        guard bigEndian.count == 16 else { throw LxBindingRefusal.invalidLength }
        self.bigEndian = bigEndian
    }
}
public struct LxUInt256: Equatable, Sendable {
    public let bigEndian: [UInt8]
    public init(bigEndian: [UInt8]) throws {
        guard bigEndian.count == 32 else { throw LxBindingRefusal.invalidLength }
        self.bigEndian = bigEndian
    }
}
public struct LxEvmHead: Equatable, Sendable {
    public let bytes: [UInt8]
    public init(_ bytes: [UInt8]) throws {
        guard bytes.count <= 1_048_575 else { throw LxBindingRefusal.tooLong }
        guard bytes.count % 32 == 0 else { throw LxBindingRefusal.invalidLength }
        self.bytes = bytes
    }
}
public struct LxCall<Output, Failure: Error> {
    public let bytes: [UInt8]
    fileprivate init(_ bytes: [UInt8]) { self.bytes = bytes }
}
fileprivate func lxCheckTarget(_ codeHash: [UInt8], _ digest: [UInt8]) throws {
    guard codeHash.count == 32 && codeHash == layerXCodeHash else { throw LxBindingRefusal.codeHashMismatch }
    guard digest.count == 32 && digest == layerXInterfaceDigest else { throw LxBindingRefusal.staleInterface }
}
fileprivate struct LxWriter {
    var bytes: [UInt8]
    init(_ convention: UInt8) { bytes = [convention] }
    mutating func byte(_ value: UInt8) throws {
        guard bytes.count < 1_048_576 else { throw LxBindingRefusal.tooLong }
        bytes.append(value)
    }
    mutating func raw(_ value: [UInt8]) throws {
        guard value.count <= 1_048_576 - bytes.count else { throw LxBindingRefusal.tooLong }
        bytes.append(contentsOf: value)
    }
    mutating func unsigned(_ value: UInt64, width: Int) throws {
        for offset in stride(from: width - 1, through: 0, by: -1) {
            try byte(UInt8(truncatingIfNeeded: value >> (offset * 8)))
        }
    }
    mutating func count(_ value: Int) throws {
        guard let count = UInt32(exactly: value) else { throw LxBindingRefusal.invalidLength }
        try unsigned(UInt64(count), width: 4)
    }
}
fileprivate struct LxReader {
    let bytes: [UInt8]
    var position: Int = 1
    var decoded: Int = 0
    var remaining: Int { bytes.count - position }
    init(_ bytes: [UInt8], convention: UInt8) throws {
        guard !bytes.isEmpty else { throw LxBindingRefusal.truncated }
        guard bytes.count <= 1_048_576 else { throw LxBindingRefusal.tooLong }
        guard bytes[0] == convention else { throw LxBindingRefusal.wrongConvention }
        self.bytes = bytes
    }
    mutating func raw(_ count: Int) throws -> [UInt8] {
        guard count >= 0 && count <= remaining else { throw LxBindingRefusal.truncated }
        guard count <= 16_777_216 - decoded else { throw LxBindingRefusal.tooLong }
        let result = Array(bytes[position..<(position + count)])
        position += count
        decoded += count
        return result
    }
    mutating func byte() throws -> UInt8 { return try raw(1)[0] }
    mutating func tag(_ expected: UInt8) throws {
        guard try byte() == expected else { throw LxBindingRefusal.wrongTag }
    }
    mutating func unsigned(width: Int) throws -> UInt64 {
        var result: UInt64 = 0
        for byte in try raw(width) { result = (result << 8) | UInt64(byte) }
        return result
    }
    mutating func count() throws -> Int {
        let value = try unsigned(width: 4)
        guard let count = Int(exactly: value) else { throw LxBindingRefusal.invalidLength }
        return count
    }
    func finish() throws {
        guard remaining == 0 else { throw LxBindingRefusal.trailingBytes }
    }
}
"#;

fn swift_entry(out: &mut String, entry: &Entry) {
    let mut discriminator = String::new();
    for byte in entry.discriminator {
        let _ = write!(discriminator, "{byte:02x}");
    }
    let _ = writeln!(out, "public enum Lx_{}_{discriminator} {{", entry.name);
    swift_type(out, &entry.input, "Input");
    swift_type(out, &entry.output, "Output");
    for failure in &entry.failures {
        swift_type(out, &failure.detail, &format!("Failure{}Detail", failure.code));
    }
    out.push_str("public enum Failure: Error, Equatable {\n");
    for failure in &entry.failures {
        let _ = writeln!(out, "case code{}(Failure{}Detail)", failure.code, failure.code);
    }
    if !entry.failures.is_empty() {
        out.push_str("public var code: UInt32 { switch self {\n");
        for failure in &entry.failures {
            let _ = writeln!(out, "case .code{}: return {}", failure.code, failure.code);
        }
        out.push_str("} }\npublic var name: String { switch self {\n");
        for failure in &entry.failures {
            let _ = writeln!(out, "case .code{}: return \"{}\"", failure.code, failure.name);
        }
        out.push_str("} }\n");
    }
    out.push_str("}\n");
    let _ = writeln!(out, "public static func call(_ input: Input, deployedCodeHash: [UInt8], publishedDigest: [UInt8]) throws -> LxCall<Output, Failure> {{\ntry lxCheckTarget(deployedCodeHash, publishedDigest)\nvar writer = LxWriter({})\ntry encodeInput(input, &writer)\nreturn LxCall({:?} + writer.bytes)\n}}", swift_convention(&entry.input), entry.discriminator);
    let _ = writeln!(out, "public static func decodeOutput(_ bytes: [UInt8]) throws -> Output {{\nvar reader = try LxReader(bytes, convention: {})\nlet value = try decodeOutputValue(&reader)\ntry reader.finish()\nreturn value\n}}", swift_convention(&entry.output));
    out.push_str("public static func decodeFailure(code: UInt32, bytes: [UInt8]) throws -> Failure {\nswitch code {\n");
    for failure in &entry.failures {
        let _ = writeln!(out, "case {}:\nvar reader = try LxReader(bytes, convention: {})\nlet value = try decodeFailure{}DetailValue(&reader)\ntry reader.finish()\nreturn .code{}(value)", failure.code, swift_convention(&failure.detail), failure.code, failure.code);
    }
    out.push_str("default: throw LxBindingRefusal.unknownFailure(code)\n}\n}\n");
    out.push_str("public static func decodeResponse(code: UInt32?, bytes: [UInt8]) throws -> Result<Output, Failure> {\nif let failureCode = code { return .failure(try decodeFailure(code: failureCode, bytes: bytes)) }\nreturn .success(try decodeOutput(bytes))\n}\n}\n");
}

fn swift_convention(value: &Type) -> u8 {
    if matches!(value, Type::EvmHead) { 2 } else { 1 }
}

fn swift_scalar(value: &Type) -> Option<(&'static str, u8, usize, bool)> {
    match value {
        Type::U8 => Some(("UInt8", 0x10, 1, false)),
        Type::U16 => Some(("UInt16", 0x11, 2, false)),
        Type::U32 => Some(("UInt32", 0x12, 4, false)),
        Type::U64 => Some(("UInt64", 0x13, 8, false)),
        Type::I8 => Some(("Int8", 0x18, 1, true)),
        Type::I16 => Some(("Int16", 0x19, 2, true)),
        Type::I32 => Some(("Int32", 0x1a, 4, true)),
        Type::I64 => Some(("Int64", 0x1b, 8, true)),
        _ => None,
    }
}

fn swift_type(out: &mut String, value: &Type, name: &str) {
    if let Some((scalar, tag, width, signed)) = swift_scalar(value) {
        let _ = writeln!(out, "public typealias {name} = {scalar}\nfileprivate static func encode{name}(_ value: {name}, _ writer: inout LxWriter) throws {{\ntry writer.byte({tag})\ntry writer.unsigned(UInt64(truncatingIfNeeded: value), width: {width})\n}}\nfileprivate static func decode{name}Value(_ reader: inout LxReader) throws -> {name} {{\ntry reader.tag({tag})\nlet value = try reader.unsigned(width: {width})");
        if signed {
            let unsigned = format!("UInt{}", width * 8);
            let _ = writeln!(out, "return {scalar}(bitPattern: {unsigned}(truncatingIfNeeded: value))\n}}");
        } else {
            let _ = writeln!(out, "return {scalar}(value)\n}}");
        }
        return;
    }
    match value {
        Type::U128 | Type::I128 | Type::U256 => {
            let (scalar, tag, width) = match value {
                Type::U128 => ("LxUInt128", 0x14, 16),
                Type::I128 => ("LxInt128", 0x1c, 16),
                _ => ("LxUInt256", 0x15, 32),
            };
            let _ = writeln!(out, "public typealias {name} = {scalar}\nfileprivate static func encode{name}(_ value: {name}, _ writer: inout LxWriter) throws {{\ntry writer.byte({tag})\ntry writer.raw(value.bigEndian)\n}}\nfileprivate static func decode{name}Value(_ reader: inout LxReader) throws -> {name} {{\ntry reader.tag({tag})\nreturn try {scalar}(bigEndian: reader.raw({width}))\n}}");
        }
        Type::Bytes(bound) => {
            let _ = writeln!(out, "public struct {name}: Equatable, Sendable {{\npublic let bytes: [UInt8]\npublic init(_ bytes: [UInt8]) throws {{\nguard UInt64(bytes.count) <= {bound} else {{ throw LxBindingRefusal.invalidLength }}\nself.bytes = bytes\n}}\n}}\nfileprivate static func encode{name}(_ value: {name}, _ writer: inout LxWriter) throws {{\ntry writer.byte(0x20)\ntry writer.count(value.bytes.count)\ntry writer.raw(value.bytes)\n}}\nfileprivate static func decode{name}Value(_ reader: inout LxReader) throws -> {name} {{\ntry reader.tag(0x20)\nlet count = try reader.count()\nguard UInt64(count) <= {bound} else {{ throw LxBindingRefusal.invalidLength }}\nreturn try {name}(reader.raw(count))\n}}");
        }
        Type::Fixed(element, bound) | Type::Variable(element, bound) => {
            let child = format!("{name}Element");
            swift_type(out, element, &child);
            let (tag, comparison) = if matches!(value, Type::Fixed(_, _)) { (0x30, "==") } else { (0x31, "<=") };
            let _ = writeln!(out, "public struct {name}: Equatable, Sendable {{\npublic let values: [{child}]\npublic init(_ values: [{child}]) throws {{\nguard UInt64(values.count) {comparison} {bound} else {{ throw LxBindingRefusal.invalidLength }}\nself.values = values\n}}\n}}\nfileprivate static func encode{name}(_ value: {name}, _ writer: inout LxWriter) throws {{\ntry writer.byte({tag})\ntry writer.count(value.values.count)\nfor element in value.values {{ try encode{child}(element, &writer) }}\n}}\nfileprivate static func decode{name}Value(_ reader: inout LxReader) throws -> {name} {{\ntry reader.tag({tag})\nlet count = try reader.count()\nguard UInt64(count) {comparison} {bound} && count <= reader.remaining else {{ throw LxBindingRefusal.invalidLength }}\nvar values: [{child}] = []\nfor _ in 0..<count {{ values.append(try decode{child}Value(&reader)) }}\nreturn try {name}(values)\n}}");
        }
        Type::Option(element) => {
            let child = format!("{name}Some");
            swift_type(out, element, &child);
            let _ = writeln!(out, "public typealias {name} = {child}?\nfileprivate static func encode{name}(_ value: {name}, _ writer: inout LxWriter) throws {{\ntry writer.byte(0x40)\nswitch value {{\ncase .none: try writer.byte(0)\ncase .some(let element):\ntry writer.byte(1)\ntry encode{child}(element, &writer)\n}}\n}}\nfileprivate static func decode{name}Value(_ reader: inout LxReader) throws -> {name} {{\ntry reader.tag(0x40)\nswitch try reader.byte() {{\ncase 0: return .none\ncase 1: return .some(try decode{child}Value(&reader))\ndefault: throw LxBindingRefusal.malformed\n}}\n}}");
        }
        Type::Union(variants) => {
            for variant in variants {
                swift_type(out, &variant.value, &format!("{name}Tag{}", variant.tag));
            }
            let _ = writeln!(out, "public enum {name}: Equatable, Sendable {{");
            for variant in variants {
                let _ = writeln!(out, "case tag{}({name}Tag{})", variant.tag, variant.tag);
            }
            let _ = writeln!(out, "}}\nfileprivate static func encode{name}(_ value: {name}, _ writer: inout LxWriter) throws {{\ntry writer.byte(0x50)\nswitch value {{");
            for variant in variants {
                let _ = writeln!(out, "case .tag{}(let element):\ntry writer.unsigned({}, width: 4)\ntry encode{name}Tag{}(element, &writer)", variant.tag, variant.tag, variant.tag);
            }
            let _ = writeln!(out, "}}\n}}\nfileprivate static func decode{name}Value(_ reader: inout LxReader) throws -> {name} {{\ntry reader.tag(0x50)\nswitch try reader.unsigned(width: 4) {{");
            for variant in variants {
                let _ = writeln!(out, "case {}: return .tag{}(try decode{name}Tag{}Value(&reader))", variant.tag, variant.tag, variant.tag);
            }
            out.push_str("default: throw LxBindingRefusal.malformed\n}\n}\n");
        }
        Type::EvmHead => {
            let _ = writeln!(out, "public typealias {name} = LxEvmHead\nfileprivate static func encode{name}(_ value: {name}, _ writer: inout LxWriter) throws {{ try writer.raw(value.bytes) }}\nfileprivate static func decode{name}Value(_ reader: inout LxReader) throws -> {name} {{\nlet count = reader.remaining\nreturn try LxEvmHead(reader.raw(count))\n}}");
        }
        Type::U8 | Type::U16 | Type::U32 | Type::U64 | Type::I8 | Type::I16 | Type::I32 | Type::I64 => unreachable!(),
    }
}

impl BindingGenerator {
    #[must_use]
    pub fn generate_swift_consumer(&self) -> String {
        let mut out = String::from(r#"
enum LxConformanceFailure: Error { case assertion }
func lxAssert(_ value: Bool) throws { if !value { throw LxConformanceFailure.assertion } }
func lxExpectRefusal(_ expected: LxBindingRefusal, _ body: () throws -> Void) throws {
    do { try body() } catch let error as LxBindingRefusal {
        if error == expected { return }
        throw error
    }
    throw LxConformanceFailure.assertion
}
"#);
        for entry in &self.entries {
            let namespace = swift_namespace(entry);
            let (input, input_bytes) = swift_sample(&entry.input, &format!("{namespace}.Input"));
            let (output, output_bytes) = swift_sample(&entry.output, &format!("{namespace}.Output"));
            let _ = writeln!(out, "do {{\nlet input: {namespace}.Input = {input}\nlet call = try {namespace}.call(input, deployedCodeHash: layerXCodeHash, publishedDigest: layerXInterfaceDigest)\nlet expected: [UInt8] = {:?} + [{}] + ({input_bytes})\ntry lxAssert(call.bytes == expected)\nlet output: {namespace}.Output = {output}\nlet outputBytes: [UInt8] = [{}] + ({output_bytes})\ntry lxAssert(try {namespace}.decodeOutput(outputBytes) == output)\nswitch try {namespace}.decodeResponse(code: nil, bytes: outputBytes) {{\ncase .success(let decoded): try lxAssert(decoded == output)\ncase .failure: throw LxConformanceFailure.assertion\n}}", entry.discriminator, swift_convention(&entry.input), swift_convention(&entry.output));
            for failure in &entry.failures {
                let (detail, detail_bytes) = swift_sample(&failure.detail, &format!("{namespace}.Failure{}Detail", failure.code));
                let _ = writeln!(out, "do {{\nlet detail: {namespace}.Failure{}Detail = {detail}\nlet bytes: [UInt8] = [{}] + ({detail_bytes})\nlet failure = try {namespace}.decodeFailure(code: {}, bytes: bytes)\ntry lxAssert(failure == .code{}(detail))\ntry lxAssert(failure.code == {} && failure.name == \"{}\")\nswitch try {namespace}.decodeResponse(code: {}, bytes: bytes) {{\ncase .failure(let decoded): try lxAssert(decoded == failure)\ncase .success: throw LxConformanceFailure.assertion\n}}\n}}", failure.code, swift_convention(&failure.detail), failure.code, failure.code, failure.code, failure.name, failure.code);
            }
            let mut unknown_code = 0u32;
            while entry.failures.iter().any(|failure| failure.code == unknown_code) {
                unknown_code += 1;
            }
            let _ = writeln!(out, "try lxExpectRefusal(.unknownFailure({unknown_code})) {{ _ = try {namespace}.decodeFailure(code: {unknown_code}, bytes: []) }}\nvar stale = layerXInterfaceDigest\nstale[0] ^= 1\ntry lxExpectRefusal(.staleInterface) {{ _ = try {namespace}.call(input, deployedCodeHash: layerXCodeHash, publishedDigest: stale) }}\nvar wrongCode = layerXCodeHash\nwrongCode[0] ^= 1\ntry lxExpectRefusal(.codeHashMismatch) {{ _ = try {namespace}.call(input, deployedCodeHash: wrongCode, publishedDigest: layerXInterfaceDigest) }}\ntry lxExpectRefusal(.codeHashMismatch) {{ _ = try {namespace}.call(input, deployedCodeHash: [], publishedDigest: layerXInterfaceDigest) }}\ntry lxExpectRefusal(.staleInterface) {{ _ = try {namespace}.call(input, deployedCodeHash: layerXCodeHash, publishedDigest: []) }}\ntry lxExpectRefusal(.truncated) {{ _ = try {namespace}.decodeOutput([]) }}\nvar wrongConvention = outputBytes\nwrongConvention[0] = 255\ntry lxExpectRefusal(.wrongConvention) {{ _ = try {namespace}.decodeOutput(wrongConvention) }}");
            if matches!(entry.output, Type::EvmHead) {
                let _ = writeln!(out, "try lxExpectRefusal(.invalidLength) {{ _ = try {namespace}.decodeOutput([2, 0]) }}");
            } else {
                let _ = writeln!(out, "try lxExpectRefusal(.trailingBytes) {{ _ = try {namespace}.decodeOutput(outputBytes + [0]) }}\nvar wrongTag = outputBytes\nwrongTag[1] = 255\ntry lxExpectRefusal(.wrongTag) {{ _ = try {namespace}.decodeOutput(wrongTag) }}");
            }
            swift_bound_consumer(&mut out, &entry.input, &format!("{namespace}.Input"));
            swift_alternate_inputs(&mut out, entry, &namespace);
            let _ = writeln!(out, "print(\"BINDING_CASE roundtrip_{}\")\n}}", entry.name);
        }
        if self.entries.iter().any(|entry| !entry.failures.is_empty()) {
            out.push_str("print(\"BINDING_CASE typed_failure\")\n");
        }
        if !self.entries.is_empty() {
            out.push_str("print(\"BINDING_CASE stale_digest\")\nprint(\"BINDING_CASE wrong_code_hash\")\nprint(\"BINDING_CASE malformed_call\")\n");
        }
        out
    }

    #[must_use]
    pub fn generate_swift_malformed_consumer(&self) -> String {
        let mut out = String::new();
        for entry in &self.entries {
            let namespace = swift_namespace(entry);
            let _ = writeln!(out, "let malformed_{namespace} = try {namespace}.call(\"malformed\", deployedCodeHash: layerXCodeHash, publishedDigest: layerXInterfaceDigest)");
        }
        out
    }
}

fn swift_namespace(entry: &Entry) -> String {
    let mut discriminator = String::new();
    for byte in entry.discriminator {
        let _ = write!(discriminator, "{byte:02x}");
    }
    format!("Lx_{}_{discriminator}", entry.name)
}

fn swift_sample(value: &Type, name: &str) -> (String, String) {
    if let Some((_, tag, width, signed)) = swift_scalar(value) {
        let mut wire = alloc::vec![tag];
        if signed {
            wire.extend(core::iter::repeat_n(255, width));
            if let Some(last) = wire.last_mut() { *last = 253; }
            return ("-3".into(), format!("{wire:?}"));
        }
        wire.extend(core::iter::repeat_n(255, width));
        let literal = match width {
            1 => "UInt8.max",
            2 => "UInt16.max",
            4 => "UInt32.max",
            _ => "UInt64.max",
        };
        return (literal.into(), format!("{wire:?}"));
    }
    match value {
        Type::U128 | Type::I128 | Type::U256 => {
            let (tag, width) = match value {
                Type::U128 => (0x14, 16),
                Type::I128 => (0x1c, 16),
                _ => (0x15, 32),
            };
            let mut bytes = alloc::vec![255u8; width];
            if matches!(value, Type::I128) { bytes[width - 1] = 253; }
            (format!("try {name}(bigEndian: {bytes:?})"), format!("[{tag}] + {bytes:?}"))
        }
        Type::Bytes(bound) => {
            let count = core::cmp::min(*bound, 2);
            let bytes = &([18u8, 52])[..count as usize];
            (format!("try {name}({bytes:?})"), format!("[32] + {:?} + {bytes:?}", count.to_be_bytes()))
        }
        Type::Fixed(child, bound) | Type::Variable(child, bound) => {
            let (count, tag) = if matches!(value, Type::Fixed(_, _)) { (*bound, 48) } else { (core::cmp::min(*bound, 2), 49) };
            let (element, bytes) = swift_sample(child, &format!("{name}Element"));
            (format!("try {name}(Array(repeating: {element}, count: {count}))"), format!("[{tag}] + {:?} + Array(repeating: ({bytes}), count: {count}).flatMap {{ $0 }}", count.to_be_bytes()))
        }
        Type::Option(child) => {
            let (element, bytes) = swift_sample(child, &format!("{name}Some"));
            (format!("Optional<{name}Some>.some({element})"), format!("[64, 1] + ({bytes})"))
        }
        Type::Union(variants) => {
            let variant = &variants[0];
            let (element, bytes) = swift_sample(&variant.value, &format!("{name}Tag{}", variant.tag));
            (format!("{name}.tag{}({element})", variant.tag), format!("[80] + {:?} + ({bytes})", variant.tag.to_be_bytes()))
        }
        Type::EvmHead => {
            let mut bytes = [0u8; 32];
            bytes[31] = 7;
            (format!("try {name}({bytes:?})"), format!("{bytes:?}"))
        }
        Type::U8 | Type::U16 | Type::U32 | Type::U64 | Type::I8 | Type::I16 | Type::I32 | Type::I64 => unreachable!(),
    }
}

fn swift_bound_consumer(out: &mut String, value: &Type, name: &str) {
    match value {
        Type::U128 | Type::I128 | Type::U256 => {
            let _ = writeln!(out, "try lxExpectRefusal(.invalidLength) {{ _ = try {name}(bigEndian: []) }}");
        }
        Type::Bytes(bound) => {
            let count = u64::from(*bound) + 1;
            let _ = writeln!(out, "try lxExpectRefusal(.invalidLength) {{ _ = try {name}(Array(repeating: UInt8(0), count: {count})) }}");
        }
        Type::Fixed(child, _) => {
            let _ = writeln!(out, "try lxExpectRefusal(.invalidLength) {{ _ = try {name}([]) }}");
            swift_bound_consumer(out, child, &format!("{name}Element"));
        }
        Type::Variable(child, bound) => {
            let count = u64::from(*bound) + 1;
            let (element, _) = swift_sample(child, &format!("{name}Element"));
            let _ = writeln!(out, "try lxExpectRefusal(.invalidLength) {{ _ = try {name}(Array(repeating: {element}, count: {count})) }}");
            swift_bound_consumer(out, child, &format!("{name}Element"));
        }
        Type::Option(child) => swift_bound_consumer(out, child, &format!("{name}Some")),
        Type::Union(variants) => {
            for variant in variants {
                swift_bound_consumer(out, &variant.value, &format!("{name}Tag{}", variant.tag));
            }
        }
        Type::EvmHead => {
            let _ = writeln!(out, "try lxExpectRefusal(.invalidLength) {{ _ = try {name}([0]) }}\ntry lxExpectRefusal(.tooLong) {{ _ = try {name}(Array(repeating: UInt8(0), count: 1_048_576)) }}");
        }
        _ => {}
    }
}

fn swift_alternate_inputs(out: &mut String, entry: &Entry, namespace: &str) {
    match &entry.input {
        Type::Option(_) => {
            let _ = writeln!(out, "let absent: {namespace}.Input = nil\nlet absentCall = try {namespace}.call(absent, deployedCodeHash: layerXCodeHash, publishedDigest: layerXInterfaceDigest)\ntry lxAssert(absentCall.bytes == {:?} + [1, 64, 0])", entry.discriminator);
        }
        Type::Union(variants) => {
            for variant in variants {
                let (element, bytes) = swift_sample(&variant.value, &format!("{namespace}.InputTag{}", variant.tag));
                let _ = writeln!(out, "do {{\nlet variant: {namespace}.Input = .tag{}({element})\nlet variantCall = try {namespace}.call(variant, deployedCodeHash: layerXCodeHash, publishedDigest: layerXInterfaceDigest)\nlet variantBytes: [UInt8] = {:?} + [1, 80] + {:?} + ({bytes})\ntry lxAssert(variantCall.bytes == variantBytes)\n}}", variant.tag, entry.discriminator, variant.tag.to_be_bytes());
            }
        }
        _ => {}
    }
}
