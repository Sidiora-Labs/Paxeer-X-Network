use alloc::collections::BTreeSet;
use alloc::format;
use alloc::string::String;
use core::fmt::Write;

use crate::bindgen::{BindingGenerator, Entry, Type};

impl BindingGenerator {
    #[must_use]
    pub fn generate_csharp(&self) -> String {
        let mut out = String::from("using System;\nusing System.Collections.Generic;\nusing System.Numerics;\npublic static class LayerXBindings {\n");
        let _ = writeln!(out, "public const string InterfaceDigest = \"{}\";\npublic const string CodeHash = \"{}\";", hex(&self.digest), hex(&self.code_hash));
        out.push_str(PRELUDE);
        let mut used = BTreeSet::new();
        for entry in &self.entries {
            let mut name = format!("Entry{}", pascal(&entry.name));
            if !used.insert(name.clone()) {
                name.push_str(&format!("Lx{}", hex(&entry.discriminator)));
                while !used.insert(name.clone()) {
                    name.push('_');
                }
            }
            emit_entry(&mut out, entry, &name);
        }
        out.push_str("}\n");
        out
    }
}

fn hex(bytes: &[u8]) -> String {
    let mut out = String::new();
    for byte in bytes {
        let _ = write!(out, "{byte:02x}");
    }
    out
}

fn pascal(value: &str) -> String {
    let mut out = String::new();
    let mut upper = true;
    for ch in value.chars() {
        if ch == '_' {
            upper = true;
        } else if upper {
            out.extend(ch.to_uppercase());
            upper = false;
        } else {
            out.push(ch);
        }
    }
    out
}

fn convention(value: &Type) -> u8 {
    if matches!(value, Type::EvmHead) { 2 } else { 1 }
}

fn emit_entry(out: &mut String, entry: &Entry, name: &str) {
    let _ = writeln!(out, "public static class {name} {{");
    emit_value(out, &entry.input, "Input");
    emit_value(out, &entry.output, "Output");
    for failure in &entry.failures {
        emit_value(out, &failure.detail, &format!("Detail{}", failure.code));
    }
    out.push_str("public abstract class Failure { private Failure() {} public abstract uint Code { get; } public abstract string Name { get; }\n");
    for failure in &entry.failures {
        let code = failure.code;
        let _ = writeln!(out, "public sealed class Code{code} : Failure {{ public Detail{code} Detail {{ get; }} public override uint Code => {code}u; public override string Name => \"{}\"; public Code{code}(Detail{code} detail) {{ if (detail == null) throw Refuse(Refusal.InvalidValue); Detail = detail; }} }}", failure.name);
    }
    out.push_str("}\n");
    let _ = writeln!(out, "public static Output DecodeOutput(byte[] bytes) {{ var reader = ReaderFor(bytes, {}); var value = Output.Read(reader); reader.Done(); return value; }}", convention(&entry.output));
    out.push_str("public static Failure DecodeFailure(uint code, byte[] bytes) { switch (code) {\n");
    for failure in &entry.failures {
        let code = failure.code;
        let _ = writeln!(out, "case {code}u: {{ var reader = ReaderFor(bytes, {}); var detail = Detail{code}.Read(reader); reader.Done(); return new Failure.Code{code}(detail); }}", convention(&failure.detail));
    }
    out.push_str("default: throw Refuse(Refusal.UnknownFailure); } }\n");
    out.push_str("public sealed class Call { private readonly byte[] bytes; private Call(byte[] value) { bytes = value; } public byte[] Bytes => (byte[])bytes.Clone();\n");
    let disc = entry.discriminator;
    let _ = writeln!(out, "public static Call Create(Input input, string deployedCodeHash, string publishedDigest) {{ CheckTarget(deployedCodeHash, publishedDigest); if (input == null) throw Refuse(Refusal.InvalidValue); var payload = Frame({}, input.ToBytes()); var bytes = new byte[payload.Length + 4]; bytes[0] = {}; bytes[1] = {}; bytes[2] = {}; bytes[3] = {}; Buffer.BlockCopy(payload, 0, bytes, 4, payload.Length); return new Call(bytes); }}", convention(&entry.input), disc[0], disc[1], disc[2], disc[3]);
    let _ = writeln!(out, "public Output DecodeOutput(byte[] bytes) => {name}.DecodeOutput(bytes); public Failure DecodeFailure(uint code, byte[] bytes) => {name}.DecodeFailure(code, bytes); }}");
    out.push_str("public static Call Encode(Input input, string deployedCodeHash, string publishedDigest) => Call.Create(input, deployedCodeHash, publishedDigest);\n}\n");
}

fn integer(value: &Type) -> Option<(&'static str, u8, usize, bool)> {
    Some(match value {
        Type::U8 => ("byte", 0x10, 1, false),
        Type::U16 => ("ushort", 0x11, 2, false),
        Type::U32 => ("uint", 0x12, 4, false),
        Type::U64 => ("ulong", 0x13, 8, false),
        Type::U128 => ("BigInteger", 0x14, 16, false),
        Type::U256 => ("BigInteger", 0x15, 32, false),
        Type::I8 => ("sbyte", 0x18, 1, true),
        Type::I16 => ("short", 0x19, 2, true),
        Type::I32 => ("int", 0x1a, 4, true),
        Type::I64 => ("long", 0x1b, 8, true),
        Type::I128 => ("BigInteger", 0x1c, 16, true),
        _ => return None,
    })
}

fn emit_value(out: &mut String, value: &Type, name: &str) {
    match value {
        Type::Fixed(child, _) | Type::Variable(child, _) | Type::Option(child) => {
            emit_value(out, child, &format!("{name}Item"));
        }
        Type::Union(variants) => {
            for variant in variants {
                emit_value(out, &variant.value, &format!("{name}Variant{}", variant.tag));
            }
        }
        _ => {}
    }
    let _ = writeln!(out, "public sealed class {name} {{ private readonly byte[] encoded; public byte[] ToBytes() => (byte[])encoded.Clone();");
    if let Some((native, tag, width, signed)) = integer(value) {
        let _ = writeln!(out, "public {native} Value {{ get; }} private {name}({native} value, byte[] bytes) {{ Value = value; encoded = bytes; }} public static {name} From({native} value) => new {name}(value, EncodeInteger({tag}, {width}, value, {signed})); internal static {name} Read(Reader reader) => From(({native})ReadInteger(reader, {tag}, {width}, {signed})); }}");
        return;
    }
    match value {
        Type::Bytes(max) => {
            let _ = writeln!(out, "private readonly byte[] value; public byte[] Value => (byte[])value.Clone(); private {name}(byte[] value, byte[] bytes) {{ this.value = value; encoded = bytes; }} public static {name} From(byte[] value) {{ if (value == null || (ulong)value.Length > {max}u) throw Refuse(Refusal.InvalidValue); var copy = (byte[])value.Clone(); return new {name}(copy, Concat(new byte[] {{ 0x20 }}, U32((uint)copy.Length), copy)); }} internal static {name} Read(Reader reader) {{ reader.Expect(0x20); uint size = reader.U32(); if (size > {max}u) throw Refuse(Refusal.NonCanonical); return From(reader.Take(size)); }} }}");
        }
        Type::EvmHead => {
            let _ = writeln!(out, "private readonly byte[] value; public byte[] Value => (byte[])value.Clone(); private {name}(byte[] value) {{ this.value = value; encoded = value; }} public static {name} From(byte[] value) {{ if (value == null || value.Length > MaxCalldataBytes - 1 || value.Length % 32 != 0) throw Refuse(Refusal.InvalidValue); return new {name}((byte[])value.Clone()); }} internal static {name} Read(Reader reader) {{ if (reader.Remaining % 32 != 0) throw Refuse(Refusal.NonCanonical); return From(reader.Take((uint)reader.Remaining)); }} }}");
        }
        Type::Fixed(_, max) | Type::Variable(_, max) => {
            let fixed = matches!(value, Type::Fixed(_, _));
            let operator = if fixed { "!=" } else { ">" };
            let tag = if fixed { 0x30 } else { 0x31 };
            let _ = writeln!(out, "public IReadOnlyList<{name}Item> Items {{ get; }} private {name}({name}Item[] items, byte[] bytes) {{ Items = Array.AsReadOnly(items); encoded = bytes; }} public static {name} From({name}Item[] values) {{ if (values == null || (ulong)values.Length {operator} {max}u || values.Length > MaxCalldataBytes / 2) throw Refuse(Refusal.InvalidValue); var items = ({name}Item[])values.Clone(); var writer = new Writer(); writer.Add(new byte[] {{ {tag} }}); writer.Add(U32((uint)items.Length)); foreach (var item in items) {{ if (item == null) throw Refuse(Refusal.InvalidValue); writer.Add(item.ToBytes()); }} return new {name}(items, writer.Finish()); }} internal static {name} Read(Reader reader) {{ reader.Expect({tag}); uint count = reader.U32(); if (count {operator} {max}u || count > (uint)(reader.Remaining / 2)) throw Refuse(Refusal.NonCanonical); reader.Account((long)count * 8); var items = new {name}Item[(int)count]; for (int i = 0; i < items.Length; ++i) items[i] = {name}Item.Read(reader); return From(items); }} }}");
        }
        Type::Option(_) => {
            let _ = writeln!(out, "public bool HasValue {{ get; }} private readonly {name}Item value; public {name}Item Value {{ get {{ if (!HasValue) throw Refuse(Refusal.InvalidValue); return value; }} }} private {name}(bool present, {name}Item value, byte[] bytes) {{ HasValue = present; this.value = value; encoded = bytes; }} public static {name} None() => new {name}(false, null, new byte[] {{ 0x40, 0 }}); public static {name} Some({name}Item value) {{ if (value == null) throw Refuse(Refusal.InvalidValue); return new {name}(true, value, Concat(new byte[] {{ 0x40, 1 }}, value.ToBytes())); }} internal static {name} Read(Reader reader) {{ reader.Expect(0x40); switch (reader.Byte()) {{ case 0: return None(); case 1: return Some({name}Item.Read(reader)); default: throw Refuse(Refusal.NonCanonical); }} }} }}");
        }
        Type::Union(variants) => {
            let _ = writeln!(out, "public uint Tag {{ get; }} private readonly object value; private {name}(uint tag, object value, byte[] bytes) {{ Tag = tag; this.value = value; encoded = bytes; }}");
            for variant in variants {
                let tag = variant.tag;
                let _ = writeln!(out, "public static {name} Variant{tag}({name}Variant{tag} value) {{ if (value == null) throw Refuse(Refusal.InvalidValue); return new {name}({tag}u, value, Concat(new byte[] {{ 0x50 }}, U32({tag}u), value.ToBytes())); }} public {name}Variant{tag} AsVariant{tag}() {{ if (Tag != {tag}u) throw Refuse(Refusal.InvalidValue); return ({name}Variant{tag})value; }}");
            }
            let _ = writeln!(out, "internal static {name} Read(Reader reader) {{ reader.Expect(0x50); switch (reader.U32()) {{");
            for variant in variants {
                let tag = variant.tag;
                let _ = writeln!(out, "case {tag}u: return Variant{tag}({name}Variant{tag}.Read(reader));");
            }
            out.push_str("default: throw Refuse(Refusal.NonCanonical); } } }\n");
        }
        _ => unreachable!(),
    }
}

const PRELUDE: &str = r#"
public const int MaxCalldataBytes = 1048576;
private const int DecodedSizeLimit = 16777216;
public enum Refusal { CodeHashMismatch, StaleInterface, InvalidValue, NonCanonical, Truncated, TrailingBytes, UnknownFailure }
public sealed class BindingRefusal : Exception { public Refusal Code { get; } public BindingRefusal(Refusal code) : base(code.ToString()) { Code = code; } }
private static BindingRefusal Refuse(Refusal code) => new BindingRefusal(code);
private static string NormalizeHash(string value) {
    if (value == null) throw Refuse(Refusal.InvalidValue);
    if (value.StartsWith("0x", StringComparison.OrdinalIgnoreCase)) value = value.Substring(2);
    if (value.Length != 64) throw Refuse(Refusal.InvalidValue);
    foreach (char c in value) if (!((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F'))) throw Refuse(Refusal.InvalidValue);
    return value.ToLowerInvariant();
}
private static void CheckTarget(string codeHash, string interfaceDigest) {
    if (NormalizeHash(codeHash) != CodeHash) throw Refuse(Refusal.CodeHashMismatch);
    if (NormalizeHash(interfaceDigest) != InterfaceDigest) throw Refuse(Refusal.StaleInterface);
}
private static byte[] U32(uint value) => new byte[] { (byte)(value >> 24), (byte)(value >> 16), (byte)(value >> 8), (byte)value };
private sealed class Writer {
    private readonly List<byte> bytes = new List<byte>();
    public void Add(byte[] part) { if (part == null || part.Length > MaxCalldataBytes - bytes.Count) throw Refuse(Refusal.InvalidValue); bytes.AddRange(part); }
    public byte[] Finish() => bytes.ToArray();
}
private static byte[] Concat(params byte[][] parts) { var writer = new Writer(); foreach (var part in parts) writer.Add(part); return writer.Finish(); }
private static byte[] Frame(byte convention, byte[] payload) => Concat(new byte[] { convention }, payload);
private static byte[] EncodeInteger(byte tag, int width, BigInteger value, bool signed) {
    int bits = width * 8;
    BigInteger min = signed ? -(BigInteger.One << (bits - 1)) : BigInteger.Zero;
    BigInteger max = signed ? (BigInteger.One << (bits - 1)) - 1 : (BigInteger.One << bits) - 1;
    if (value < min || value > max) throw Refuse(Refusal.InvalidValue);
    if (value < 0) value += BigInteger.One << bits;
    var bytes = new byte[width + 1]; bytes[0] = tag;
    for (int i = width; i > 0; --i) { bytes[i] = (byte)(value & 255); value >>= 8; }
    return bytes;
}
internal sealed class Reader {
    private readonly byte[] bytes; private int position; private long decoded;
    internal Reader(byte[] bytes) { this.bytes = (byte[])bytes.Clone(); }
    internal int Remaining => bytes.Length - position;
    internal void Account(long size) { if (size < 0 || size > DecodedSizeLimit - decoded) throw Refuse(Refusal.NonCanonical); decoded += size; }
    internal byte Byte() { if (Remaining == 0) throw Refuse(Refusal.Truncated); return bytes[position++]; }
    internal void Expect(byte tag) { if (Byte() != tag) throw Refuse(Refusal.NonCanonical); }
    internal uint U32() { return ((uint)Byte() << 24) | ((uint)Byte() << 16) | ((uint)Byte() << 8) | Byte(); }
    internal byte[] Take(uint count) { if (count > (uint)Remaining) throw Refuse(Refusal.Truncated); Account(count); var result = new byte[(int)count]; Buffer.BlockCopy(bytes, position, result, 0, (int)count); position += (int)count; return result; }
    internal void Done() { if (Remaining != 0) throw Refuse(Refusal.TrailingBytes); }
}
private static Reader ReaderFor(byte[] bytes, byte convention) {
    if (bytes == null) throw Refuse(Refusal.InvalidValue);
    if (bytes.Length == 0) throw Refuse(Refusal.Truncated);
    if (bytes.Length > MaxCalldataBytes) throw Refuse(Refusal.NonCanonical);
    var reader = new Reader(bytes); reader.Expect(convention); return reader;
}
private static BigInteger ReadInteger(Reader reader, byte tag, int width, bool signed) {
    reader.Expect(tag); byte[] bytes = reader.Take((uint)width); BigInteger value = BigInteger.Zero;
    foreach (byte b in bytes) value = (value << 8) | b;
    if (signed && (bytes[0] & 128) != 0) value -= BigInteger.One << (width * 8);
    return value;
}
"#;

impl BindingGenerator {
    #[must_use]
    pub fn generate_csharp_consumer(&self) -> String {
        String::from(CONSUMER)
    }

    #[must_use]
    pub fn generate_csharp_malformed_consumer(&self) -> String {
        String::from("public static class Program { public static void Main() { var malformed = new LayerXBindings.EntryU8.Call(new byte[] { 0 }); } }\n")
    }
}

const CONSUMER: &str = r#"
using System;
using System.Numerics;
using L = LayerXBindings;
public static class Program {
    private static void Equal(byte[] actual, byte[] expected) {
        if (actual.Length != expected.Length) throw new Exception("length mismatch");
        for (int i = 0; i < actual.Length; ++i) if (actual[i] != expected[i]) throw new Exception("canonical byte mismatch at " + i);
    }
    private static byte[] Join(params byte[][] parts) {
        int length = 0; foreach (var part in parts) length = checked(length + part.Length);
        var result = new byte[length]; int offset = 0;
        foreach (var part in parts) { Buffer.BlockCopy(part, 0, result, offset, part.Length); offset += part.Length; }
        return result;
    }
    private static byte[] Filled(int count, byte value) { var result = new byte[count]; for (int i = 0; i < count; ++i) result[i] = value; return result; }
    private static void Refuses(Action action, params L.Refusal[] expected) {
        try { action(); } catch (L.BindingRefusal refusal) {
            foreach (var code in expected) if (refusal.Code == code) return;
            throw new Exception("unexpected refusal " + refusal.Code);
        }
        throw new Exception("expected binding refusal");
    }
    private static void Check(string name, byte discriminator, byte convention, byte[] payload, Func<byte[]> callBytes, Func<byte[], byte[]> output, Func<byte[], byte[]> failure) {
        var frame = Join(new byte[] { convention }, payload);
        var expected = Join(new byte[] { 0xa5, 0, 0, discriminator }, frame);
        Equal(callBytes(), expected);
        var copy = callBytes(); copy[0] ^= 255; Equal(callBytes(), expected);
        Equal(output(frame), payload); Equal(failure(frame), payload);
        var truncated = new byte[frame.Length - 1]; Buffer.BlockCopy(frame, 0, truncated, 0, truncated.Length);
        Refuses(() => output(truncated), L.Refusal.Truncated, L.Refusal.NonCanonical);
        Refuses(() => output(Join(frame, new byte[] { 0 })), L.Refusal.TrailingBytes, L.Refusal.NonCanonical);
        var badConvention = (byte[])frame.Clone(); badConvention[0] = 255;
        Refuses(() => output(badConvention), L.Refusal.NonCanonical);
        Console.WriteLine("BINDING_CASE roundtrip_" + name);
    }
    public static void Main() {
        string hash = L.CodeHash, digest = L.InterfaceDigest;
        {
            var source = new byte[] { 1, 2, 3 }; var input = L.EntryBytes.Input.From(source); source[0] = 99;
            var call = L.EntryBytes.Encode(input, hash, digest);
            Check("bytes", 1, 1, new byte[] { 0x20, 0, 0, 0, 3, 1, 2, 3 }, () => call.Bytes, b => L.EntryBytes.DecodeOutput(b).ToBytes(), b => ((L.EntryBytes.Failure.Code7)L.EntryBytes.DecodeFailure(7, b)).Detail.ToBytes());
            Refuses(() => L.EntryBytes.Input.From(new byte[9]), L.Refusal.InvalidValue);
        }
        {
            var source = new byte[32]; source[31] = 1; var input = L.EntryEvm.Input.From(source); source[31] = 9;
            var call = L.EntryEvm.Encode(input, hash, digest); var expected = new byte[32]; expected[31] = 1;
            Check("evm", 2, 2, expected, () => call.Bytes, b => L.EntryEvm.DecodeOutput(b).ToBytes(), b => ((L.EntryEvm.Failure.Code7)L.EntryEvm.DecodeFailure(7, b)).Detail.ToBytes());
            Refuses(() => L.EntryEvm.Input.From(new byte[31]), L.Refusal.InvalidValue);
            Refuses(() => L.EntryEvm.Input.From(new byte[L.MaxCalldataBytes]), L.Refusal.InvalidValue);
        }
        {
            var items = new[] { L.EntryFixed.InputItem.From(1), L.EntryFixed.InputItem.From(2) }; var input = L.EntryFixed.Input.From(items); items[0] = L.EntryFixed.InputItem.From(9);
            var call = L.EntryFixed.Encode(input, hash, digest);
            Check("fixed", 3, 1, new byte[] { 0x30, 0, 0, 0, 2, 0x10, 1, 0x10, 2 }, () => call.Bytes, b => L.EntryFixed.DecodeOutput(b).ToBytes(), b => ((L.EntryFixed.Failure.Code7)L.EntryFixed.DecodeFailure(7, b)).Detail.ToBytes());
            Refuses(() => L.EntryFixed.Input.From(new L.EntryFixed.InputItem[1]), L.Refusal.InvalidValue);
            Refuses(() => L.EntryFixed.Input.From(new L.EntryFixed.InputItem[2]), L.Refusal.InvalidValue);
        }
        {
            var call = L.EntryI128.Encode(L.EntryI128.Input.From(-(BigInteger.One << 127)), hash, digest);
            Check("i128", 4, 1, Join(new byte[] { 0x1c, 0x80 }, new byte[15]), () => call.Bytes, b => L.EntryI128.DecodeOutput(b).ToBytes(), b => ((L.EntryI128.Failure.Code7)L.EntryI128.DecodeFailure(7, b)).Detail.ToBytes());
            Refuses(() => L.EntryI128.Input.From(BigInteger.One << 127), L.Refusal.InvalidValue);
            Refuses(() => L.EntryI128.Input.From(-(BigInteger.One << 127) - 1), L.Refusal.InvalidValue);
        }
        {
            var call = L.EntryI16.Encode(L.EntryI16.Input.From(-2), hash, digest);
            Check("i16", 5, 1, new byte[] { 0x19, 0xff, 0xfe }, () => call.Bytes, b => L.EntryI16.DecodeOutput(b).ToBytes(), b => ((L.EntryI16.Failure.Code7)L.EntryI16.DecodeFailure(7, b)).Detail.ToBytes());
        }
        {
            var call = L.EntryI32.Encode(L.EntryI32.Input.From(-3), hash, digest);
            Check("i32", 6, 1, new byte[] { 0x1a, 0xff, 0xff, 0xff, 0xfd }, () => call.Bytes, b => L.EntryI32.DecodeOutput(b).ToBytes(), b => ((L.EntryI32.Failure.Code7)L.EntryI32.DecodeFailure(7, b)).Detail.ToBytes());
        }
        {
            var call = L.EntryI64.Encode(L.EntryI64.Input.From(long.MinValue), hash, digest);
            Check("i64", 7, 1, Join(new byte[] { 0x1b, 0x80 }, new byte[7]), () => call.Bytes, b => L.EntryI64.DecodeOutput(b).ToBytes(), b => ((L.EntryI64.Failure.Code7)L.EntryI64.DecodeFailure(7, b)).Detail.ToBytes());
        }
        {
            var call = L.EntryI8.Encode(L.EntryI8.Input.From(-1), hash, digest);
            Check("i8", 8, 1, new byte[] { 0x18, 0xff }, () => call.Bytes, b => L.EntryI8.DecodeOutput(b).ToBytes(), b => ((L.EntryI8.Failure.Code7)L.EntryI8.DecodeFailure(7, b)).Detail.ToBytes());
        }
        {
            var call = L.EntryOption.Encode(L.EntryOption.Input.Some(L.EntryOption.InputItem.From(8)), hash, digest);
            Check("option", 9, 1, new byte[] { 0x40, 1, 0x12, 0, 0, 0, 8 }, () => call.Bytes, b => L.EntryOption.DecodeOutput(b).ToBytes(), b => ((L.EntryOption.Failure.Code7)L.EntryOption.DecodeFailure(7, b)).Detail.ToBytes());
            Equal(L.EntryOption.Input.None().ToBytes(), new byte[] { 0x40, 0 });
            if (L.EntryOption.DecodeOutput(new byte[] { 1, 0x40, 0 }).HasValue) throw new Exception("option None changed");
            Refuses(() => L.EntryOption.DecodeOutput(new byte[] { 1, 0x40, 2 }), L.Refusal.NonCanonical);
            Refuses(() => L.EntryOption.Input.Some(null), L.Refusal.InvalidValue);
        }
        {
            var call = L.EntryU128.Encode(L.EntryU128.Input.From((BigInteger.One << 128) - 1), hash, digest);
            Check("u128", 10, 1, Join(new byte[] { 0x14 }, Filled(16, 255)), () => call.Bytes, b => L.EntryU128.DecodeOutput(b).ToBytes(), b => ((L.EntryU128.Failure.Code7)L.EntryU128.DecodeFailure(7, b)).Detail.ToBytes());
            Refuses(() => L.EntryU128.Input.From(BigInteger.One << 128), L.Refusal.InvalidValue);
            Refuses(() => L.EntryU128.Input.From(-1), L.Refusal.InvalidValue);
        }
        {
            var call = L.EntryU16.Encode(L.EntryU16.Input.From(0x1234), hash, digest);
            Check("u16", 11, 1, new byte[] { 0x11, 0x12, 0x34 }, () => call.Bytes, b => L.EntryU16.DecodeOutput(b).ToBytes(), b => ((L.EntryU16.Failure.Code7)L.EntryU16.DecodeFailure(7, b)).Detail.ToBytes());
        }
        {
            var call = L.EntryU256.Encode(L.EntryU256.Input.From((BigInteger.One << 256) - 1), hash, digest);
            Check("u256", 12, 1, Join(new byte[] { 0x15 }, Filled(32, 255)), () => call.Bytes, b => L.EntryU256.DecodeOutput(b).ToBytes(), b => ((L.EntryU256.Failure.Code7)L.EntryU256.DecodeFailure(7, b)).Detail.ToBytes());
            Refuses(() => L.EntryU256.Input.From(BigInteger.One << 256), L.Refusal.InvalidValue);
            Refuses(() => L.EntryU256.Input.From(-1), L.Refusal.InvalidValue);
        }
        {
            var call = L.EntryU32.Encode(L.EntryU32.Input.From(7), hash, digest);
            Check("u32", 13, 1, new byte[] { 0x12, 0, 0, 0, 7 }, () => call.Bytes, b => L.EntryU32.DecodeOutput(b).ToBytes(), b => ((L.EntryU32.Failure.Code7)L.EntryU32.DecodeFailure(7, b)).Detail.ToBytes());
        }
        {
            var call = L.EntryU64.Encode(L.EntryU64.Input.From(ulong.MaxValue), hash, digest);
            Check("u64", 14, 1, Join(new byte[] { 0x13 }, Filled(8, 255)), () => call.Bytes, b => L.EntryU64.DecodeOutput(b).ToBytes(), b => ((L.EntryU64.Failure.Code7)L.EntryU64.DecodeFailure(7, b)).Detail.ToBytes());
        }
        {
            var call = L.EntryU8.Encode(L.EntryU8.Input.From(127), hash, digest);
            Check("u8", 15, 1, new byte[] { 0x10, 127 }, () => call.Bytes, b => L.EntryU8.DecodeOutput(b).ToBytes(), b => ((L.EntryU8.Failure.Code7)L.EntryU8.DecodeFailure(7, b)).Detail.ToBytes());
            Refuses(() => L.EntryU8.DecodeOutput(new byte[] { 1, 0x11, 0, 127 }), L.Refusal.NonCanonical);
            Refuses(() => L.EntryU8.DecodeOutput(new byte[L.MaxCalldataBytes + 1]), L.Refusal.NonCanonical);
        }
        {
            var call = L.EntryUnion.Encode(L.EntryUnion.Input.Variant0(L.EntryUnion.InputVariant0.From(9)), hash, digest);
            Check("union", 16, 1, new byte[] { 0x50, 0, 0, 0, 0, 0x10, 9 }, () => call.Bytes, b => L.EntryUnion.DecodeOutput(b).ToBytes(), b => ((L.EntryUnion.Failure.Code7)L.EntryUnion.DecodeFailure(7, b)).Detail.ToBytes());
            var second = L.EntryUnion.Input.Variant7(L.EntryUnion.InputVariant7.From(10));
            Equal(second.ToBytes(), new byte[] { 0x50, 0, 0, 0, 7, 0x11, 0, 10 });
            var decoded = L.EntryUnion.DecodeOutput(new byte[] { 1, 0x50, 0, 0, 0, 7, 0x11, 0, 10 });
            if (decoded.AsVariant7().Value != 10) throw new Exception("union detail changed");
            Refuses(() => decoded.AsVariant0(), L.Refusal.InvalidValue);
            Refuses(() => L.EntryUnion.DecodeOutput(new byte[] { 1, 0x50, 0, 0, 0, 8, 0x10, 9 }), L.Refusal.NonCanonical);
        }
        {
            var call = L.EntryVariable.Encode(L.EntryVariable.Input.From(new[] { L.EntryVariable.InputItem.From(1), L.EntryVariable.InputItem.From(2) }), hash, digest);
            Check("variable", 17, 1, new byte[] { 0x31, 0, 0, 0, 2, 0x11, 0, 1, 0x11, 0, 2 }, () => call.Bytes, b => L.EntryVariable.DecodeOutput(b).ToBytes(), b => ((L.EntryVariable.Failure.Code7)L.EntryVariable.DecodeFailure(7, b)).Detail.ToBytes());
            Refuses(() => L.EntryVariable.Input.From(new L.EntryVariable.InputItem[4]), L.Refusal.InvalidValue);
            Refuses(() => L.EntryVariable.DecodeOutput(new byte[] { 1, 0x31, 255, 255, 255, 255 }), L.Refusal.NonCanonical);
        }
        Refuses(() => L.EntryU8.DecodeFailure(8, new byte[] { 1, 0x10, 1 }), L.Refusal.UnknownFailure);
        Console.WriteLine("BINDING_CASE typed_failure");
        string badDigest = (digest[0] == '0' ? "1" : "0") + digest.Substring(1);
        string badHash = (hash[0] == '0' ? "1" : "0") + hash.Substring(1);
        Refuses(() => L.EntryU8.Encode(L.EntryU8.Input.From(1), hash, badDigest), L.Refusal.StaleInterface);
        Console.WriteLine("BINDING_CASE stale_digest");
        Refuses(() => L.EntryU8.Encode(L.EntryU8.Input.From(1), badHash, digest), L.Refusal.CodeHashMismatch);
        Console.WriteLine("BINDING_CASE wrong_code_hash");
        Refuses(() => L.EntryU8.Encode(null, hash, digest), L.Refusal.InvalidValue);
        Refuses(() => L.EntryU8.Encode(null, badHash, badDigest), L.Refusal.CodeHashMismatch);
        Refuses(() => L.EntryU8.Encode(L.EntryU8.Input.From(1), "bad", digest), L.Refusal.InvalidValue);
        Console.WriteLine("BINDING_CASE malformed_call");
    }
}
"#;
