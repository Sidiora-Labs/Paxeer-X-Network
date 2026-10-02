use alloc::format;
use alloc::string::String;
use alloc::vec::Vec;
use core::fmt::Write;

use crate::bindgen::{BindingGenerator, Entry, Type};

impl BindingGenerator {
    #[must_use]
    pub fn generate_python(&self) -> String {
        let mut out = String::from(PRELUDE);
        let _ = writeln!(out, "INTERFACE_DIGEST: Final[bytes] = bytes({:?})", self.digest);
        let _ = writeln!(out, "CODE_HASH: Final[bytes] = bytes({:?})\n", self.code_hash);
        for entry in &self.entries {
            emit_entry(&mut out, entry);
        }
        out.push_str("\n@dataclass(frozen=True)\nclass Client:\n    deployed_code_hash: bytes\n    published_digest: bytes\n\n    def __post_init__(self) -> None:\n        _check_target(self.deployed_code_hash, self.published_digest)\n");
        for entry in &self.entries {
            let (stem, prefix) = names(entry);
            let _ = writeln!(out, "\n    def {stem}(self, input: {prefix}Input) -> Call[{prefix}Output, {prefix}Failure]:\n        return encode_{stem}(input, self.deployed_code_hash, self.published_digest)");
        }
        out
    }
}

fn names(entry: &Entry) -> (String, String) {
    let mut hex = String::new();
    for byte in entry.discriminator {
        let _ = write!(hex, "{byte:02x}");
    }
    (format!("entry_{}_{}", entry.name.to_ascii_lowercase(), hex), format!("Entry_{hex}"))
}

fn shape(ty: &Type, context: &str, out: &mut String) -> (String, String) {
    let primitive = match ty {
        Type::U8 => Some(("int", "(0x10, 1, False)")),
        Type::U16 => Some(("int", "(0x11, 2, False)")),
        Type::U32 => Some(("int", "(0x12, 4, False)")),
        Type::U64 => Some(("int", "(0x13, 8, False)")),
        Type::U128 => Some(("int", "(0x14, 16, False)")),
        Type::U256 => Some(("bytes", "(0x15,)")),
        Type::I8 => Some(("int", "(0x18, 1, True)")),
        Type::I16 => Some(("int", "(0x19, 2, True)")),
        Type::I32 => Some(("int", "(0x1a, 4, True)")),
        Type::I64 => Some(("int", "(0x1b, 8, True)")),
        Type::I128 => Some(("int", "(0x1c, 16, True)")),
        Type::EvmHead => Some(("bytes", "(0x60,)")),
        _ => None,
    };
    if let Some((name, schema)) = primitive {
        return (name.into(), schema.into());
    }
    match ty {
        Type::Bytes(bound) => ("bytes".into(), format!("(0x20, {bound})")),
        Type::Fixed(item, bound) | Type::Variable(item, bound) => {
            let (name, schema) = shape(item, &format!("{context}Element"), out);
            let tag = if matches!(ty, Type::Fixed(_, _)) { 0x30 } else { 0x31 };
            (format!("tuple[{name}, ...]"), format!("({tag}, {bound}, {schema})"))
        }
        Type::Option(item) => {
            let (name, schema) = shape(item, &format!("{context}Some"), out);
            (format!("Optional[Some[{name}]]"), format!("(0x40, {schema})"))
        }
        Type::Union(variants) => {
            let mut names = Vec::new();
            let mut schemas = Vec::new();
            for variant in variants {
                let variant_name = format!("{context}Variant{}", variant.tag);
                let (name, schema) = shape(&variant.value, &format!("{variant_name}Value"), out);
                let _ = writeln!(out, "@dataclass(frozen=True)\nclass {variant_name}:\n    value: {name}\n    tag: ClassVar[Literal[{}]] = {}\n\n    def __post_init__(self) -> None:\n        _frame({schema}, self.value)\n", variant.tag, variant.tag);
                schemas.push(format!("({}, {schema}, {variant_name})", variant.tag));
                names.push(variant_name);
            }
            (names.join(" | "), format!("(0x50, ({},))", schemas.join(", ")))
        }
        _ => unreachable!(),
    }
}

fn emit_entry(out: &mut String, entry: &Entry) {
    let (stem, prefix) = names(entry);
    let (input, input_schema) = shape(&entry.input, &format!("{prefix}InputValue"), out);
    let (output, output_schema) = shape(&entry.output, &format!("{prefix}OutputValue"), out);
    let _ = writeln!(out, "{prefix}Output: TypeAlias = {output}\n_{prefix}_INPUT: _Schema = {input_schema}\n_{prefix}_OUTPUT: _Schema = {output_schema}\n\n@dataclass(frozen=True)\nclass {prefix}Input:\n    value: {input}\n\n    def __post_init__(self) -> None:\n        _frame(_{prefix}_INPUT, self.value)\n");
    let mut failure_names = Vec::new();
    let mut failure_arms = String::new();
    for failure in &entry.failures {
        let class = format!("{prefix}Failure{}", failure.code);
        let (detail, schema) = shape(&failure.detail, &format!("{class}Detail"), out);
        let _ = writeln!(out, "@dataclass(frozen=True)\nclass {class}:\n    detail: {detail}\n    code: ClassVar[Literal[{}]] = {}\n    name: ClassVar[Literal[{:?}]] = {:?}\n\n    def __post_init__(self) -> None:\n        _frame({schema}, self.detail)\n", failure.code, failure.code, failure.name, failure.name);
        let _ = writeln!(failure_arms, "    if code == {}:\n        return {class}(cast({detail}, _decode_frame({schema}, detail)))", failure.code);
        failure_names.push(class);
    }
    let failure_type = if failure_names.is_empty() { "NoReturn".into() } else { failure_names.join(" | ") };
    let _ = writeln!(out, "{prefix}Failure: TypeAlias = {failure_type}\n\ndef decode_{stem}_output(data: bytes) -> {prefix}Output:\n    return cast({prefix}Output, _decode_frame(_{prefix}_OUTPUT, data))\n\ndef decode_{stem}_failure(code: int, detail: bytes) -> {prefix}Failure:\n    if type(code) is not int or not 0 <= code <= 0xffffffff:\n        raise BindingRefusal('INVALID_VALUE', 'failure code must be u32')\n{failure_arms}    raise BindingRefusal('UNKNOWN_FAILURE', 'undeclared failure code')\n\ndef encode_{stem}(input: {prefix}Input, deployed_code_hash: bytes, published_digest: bytes) -> Call[{prefix}Output, {prefix}Failure]:\n    _check_target(deployed_code_hash, published_digest)\n    if type(input) is not {prefix}Input:\n        raise BindingRefusal('INVALID_VALUE', 'entry input requires its checked builder')\n    data = bytes({:?}) + _frame(_{prefix}_INPUT, input.value)\n    return Call(data, decode_{stem}_output, decode_{stem}_failure)\n", entry.discriminator);
}

const PRELUDE: &str = r#"from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Callable, ClassVar, Final, Generic, Literal, NoReturn, Optional, TypeAlias, TypeVar, cast

MAX_CALLDATA_BYTES: Final[int] = 1_048_576
DECODED_SIZE_LIMIT: Final[int] = 16_777_216
MAX_DEPTH: Final[int] = 16
_Schema: TypeAlias = tuple[Any, ...]
T = TypeVar('T')
O = TypeVar('O')
F = TypeVar('F')

class BindingRefusal(ValueError):
    def __init__(self, code: str, message: str) -> None:
        super().__init__(message)
        self.code = code

@dataclass(frozen=True)
class Some(Generic[T]):
    value: T

@dataclass(frozen=True)
class Call(Generic[O, F]):
    _bytes: bytes
    _decode_output: Callable[[bytes], O]
    _decode_failure: Callable[[int, bytes], F]

    def as_bytes(self) -> bytes:
        return self._bytes

    def decode_output(self, data: bytes) -> O:
        return self._decode_output(data)

    def decode_failure(self, code: int, detail: bytes) -> F:
        return self._decode_failure(code, detail)

def _check_target(code_hash: bytes, digest: bytes) -> None:
    if type(code_hash) is not bytes or len(code_hash) != 32:
        raise BindingRefusal('INVALID_VALUE', 'code hash must contain exactly 32 bytes')
    if type(digest) is not bytes or len(digest) != 32:
        raise BindingRefusal('INVALID_VALUE', 'interface digest must contain exactly 32 bytes')
    if code_hash != CODE_HASH:
        raise BindingRefusal('CODE_HASH_MISMATCH', 'binding targets a different deployed code hash')
    if digest != INTERFACE_DIGEST:
        raise BindingRefusal('STALE_INTERFACE', 'binding targets a different published interface')

class _Writer:
    def __init__(self) -> None:
        self.data = bytearray()
        self.decoded = 0

    def append(self, value: bytes) -> None:
        if len(self.data) + len(value) > MAX_CALLDATA_BYTES:
            raise BindingRefusal('INVALID_VALUE', 'encoded message exceeds protocol limit')
        self.data.extend(value)

    def account(self, size: int) -> None:
        if self.decoded + size > DECODED_SIZE_LIMIT:
            raise BindingRefusal('INVALID_VALUE', 'decoded value exceeds protocol limit')
        self.decoded += size

class _Reader:
    def __init__(self, data: bytes) -> None:
        self.data = data
        self.at = 0
        self.decoded = 0

    def take(self, size: int, account: bool = False) -> bytes:
        if size < 0 or size > len(self.data) - self.at:
            raise BindingRefusal('TRUNCATED', 'truncated canonical value')
        if account:
            self.account(size)
        result = self.data[self.at:self.at + size]
        self.at += size
        return result

    def account(self, size: int) -> None:
        if self.decoded + size > DECODED_SIZE_LIMIT:
            raise BindingRefusal('NON_CANONICAL', 'decoded value exceeds protocol limit')
        self.decoded += size

    def byte(self) -> int:
        return self.take(1)[0]

    def count(self) -> int:
        return int.from_bytes(self.take(4), 'big')

    def tag(self, expected: int) -> None:
        if self.byte() != expected:
            raise BindingRefusal('NON_CANONICAL', 'type tag does not match schema')

def _write(schema: _Schema, value: object, writer: _Writer, depth: int = 0) -> None:
    if depth > MAX_DEPTH:
        raise BindingRefusal('INVALID_VALUE', 'value exceeds nesting limit')
    tag = schema[0]
    if tag in (0x10, 0x11, 0x12, 0x13, 0x14, 0x18, 0x19, 0x1a, 0x1b, 0x1c):
        width, signed = schema[1], schema[2]
        if type(value) is not int:
            raise BindingRefusal('INVALID_VALUE', 'integer required; booleans and floats are refused')
        minimum = -(1 << (width * 8 - 1)) if signed else 0
        maximum = (1 << (width * 8 - (1 if signed else 0))) - 1
        if not minimum <= value <= maximum:
            raise BindingRefusal('INVALID_VALUE', 'integer outside schema range')
        writer.account(width)
        writer.append(bytes((tag,)) + value.to_bytes(width, 'big', signed=signed))
        return
    if tag in (0x15, 0x20, 0x60):
        if type(value) is not bytes:
            raise BindingRefusal('INVALID_VALUE', 'immutable bytes required')
        if tag == 0x15:
            if len(value) != 32:
                raise BindingRefusal('INVALID_VALUE', 'u256 requires exactly 32 bytes')
            writer.append(bytes((tag,)))
        elif tag == 0x20:
            if len(value) > schema[1]:
                raise BindingRefusal('INVALID_VALUE', 'byte string exceeds schema bound')
            writer.append(bytes((tag,)) + len(value).to_bytes(4, 'big'))
        elif len(value) % 32 != 0:
            raise BindingRefusal('INVALID_VALUE', 'EVM head requires complete 32-byte words')
        writer.account(len(value))
        writer.append(value)
        return
    if tag in (0x30, 0x31):
        if type(value) is not tuple:
            raise BindingRefusal('INVALID_VALUE', 'array requires an immutable tuple')
        if (tag == 0x30 and len(value) != schema[1]) or len(value) > schema[1]:
            raise BindingRefusal('INVALID_VALUE', 'array length does not match schema bound')
        writer.append(bytes((tag,)) + len(value).to_bytes(4, 'big'))
        for item in value:
            _write(schema[2], item, writer, depth + 1)
        return
    if tag == 0x40:
        writer.append(bytes((tag,)))
        if value is None:
            writer.append(b'\x00')
        elif type(value) is Some:
            writer.append(b'\x01')
            _write(schema[1], value.value, writer, depth + 1)
        else:
            raise BindingRefusal('INVALID_VALUE', 'option requires None or Some(value)')
        return
    if tag == 0x50:
        for variant_tag, inner, variant_class in schema[1]:
            if type(value) is variant_class:
                writer.append(bytes((tag,)) + variant_tag.to_bytes(4, 'big'))
                _write(inner, getattr(value, 'value'), writer, depth + 1)
                return
        raise BindingRefusal('INVALID_VALUE', 'union requires a declared variant builder')
    raise BindingRefusal('NON_CANONICAL', 'unsupported schema tag')

def _read(schema: _Schema, reader: _Reader, depth: int = 0) -> object:
    if depth > MAX_DEPTH:
        raise BindingRefusal('NON_CANONICAL', 'value exceeds nesting limit')
    tag = schema[0]
    if tag == 0x60:
        value = reader.take(len(reader.data) - reader.at, True)
        if len(value) % 32 != 0:
            raise BindingRefusal('NON_CANONICAL', 'EVM head requires complete 32-byte words')
        return value
    reader.tag(tag)
    if tag in (0x10, 0x11, 0x12, 0x13, 0x14, 0x18, 0x19, 0x1a, 0x1b, 0x1c):
        return int.from_bytes(reader.take(schema[1], True), 'big', signed=schema[2])
    if tag == 0x15:
        return reader.take(32, True)
    if tag == 0x20:
        size = reader.count()
        if size > schema[1]:
            raise BindingRefusal('NON_CANONICAL', 'byte string exceeds schema bound')
        return reader.take(size, True)
    if tag in (0x30, 0x31):
        count = reader.count()
        if (tag == 0x30 and count != schema[1]) or count > schema[1]:
            raise BindingRefusal('NON_CANONICAL', 'array length does not match schema bound')
        if count > len(reader.data) - reader.at:
            raise BindingRefusal('TRUNCATED', 'array count exceeds remaining canonical bytes')
        return tuple(_read(schema[2], reader, depth + 1) for _ in range(count))
    if tag == 0x40:
        present = reader.byte()
        if present == 0:
            return None
        if present != 1:
            raise BindingRefusal('NON_CANONICAL', 'invalid option discriminator')
        return Some(_read(schema[1], reader, depth + 1))
    if tag == 0x50:
        variant = reader.count()
        for variant_tag, inner, variant_class in schema[1]:
            if variant == variant_tag:
                return cast(object, variant_class(_read(inner, reader, depth + 1)))
        raise BindingRefusal('NON_CANONICAL', 'undeclared union variant')
    raise BindingRefusal('NON_CANONICAL', 'unsupported schema tag')

def _frame(schema: _Schema, value: object) -> bytes:
    writer = _Writer()
    writer.append(b'\x02' if schema[0] == 0x60 else b'\x01')
    _write(schema, value, writer)
    return bytes(writer.data)

def _decode_frame(schema: _Schema, data: bytes) -> object:
    if type(data) is not bytes:
        raise BindingRefusal('INVALID_VALUE', 'encoded message requires immutable bytes')
    if not data:
        raise BindingRefusal('TRUNCATED', 'missing encoding convention')
    if len(data) > MAX_CALLDATA_BYTES:
        raise BindingRefusal('NON_CANONICAL', 'encoded message exceeds protocol limit')
    convention = 2 if schema[0] == 0x60 else 1
    if data[0] != convention:
        raise BindingRefusal('NON_CANONICAL', 'encoding convention does not match schema')
    reader = _Reader(data[1:])
    value = _read(schema, reader)
    if reader.at != len(reader.data):
        raise BindingRefusal('TRAILING_BYTES', 'trailing bytes after canonical value')
    return value

"#;

impl BindingGenerator {
    #[must_use]
    pub fn generate_python_consumer(&self) -> String {
        let mut out = String::from("from typing import Any, Callable, cast\nimport bindings as b\n\ndef expect(code: str, action: Callable[[], object]) -> None:\n    try:\n        action()\n    except b.BindingRefusal as error:\n        assert error.code == code, (error.code, code)\n    else:\n        raise AssertionError('expected ' + code)\n\n");
        for (index, entry) in self.entries.iter().enumerate() {
            let (stem, prefix) = names(entry);
            let (input, input_wire) = sample(&entry.input, &format!("b.{prefix}InputValue"));
            let (output, output_wire) = sample(&entry.output, &format!("b.{prefix}OutputValue"));
            let input_convention = if matches!(entry.input, Type::EvmHead) { 2 } else { 1 };
            let output_convention = if matches!(entry.output, Type::EvmHead) { 2 } else { 1 };
            let _ = writeln!(out, "input_{index} = b.{prefix}Input({input})\ncall_{index}: b.Call[b.{prefix}Output, b.{prefix}Failure] = b.encode_{stem}(input_{index}, b.CODE_HASH, b.INTERFACE_DIGEST)\nassert call_{index}.as_bytes() == bytes({:?}) + bytes([{input_convention}]) + {input_wire}\nassert call_{index}.decode_output(bytes([{output_convention}]) + {output_wire}) == {output}\nassert b.Client(b.CODE_HASH, b.INTERFACE_DIGEST).{stem}(input_{index}).as_bytes() == call_{index}.as_bytes()\nprint('BINDING_CASE roundtrip_{}')", entry.discriminator, entry.name);
            for failure in &entry.failures {
                let (detail, wire) = sample(&failure.detail, &format!("b.{prefix}Failure{}Detail", failure.code));
                let convention = if matches!(failure.detail, Type::EvmHead) { 2 } else { 1 };
                let _ = writeln!(out, "failure_{index}_{}: b.{prefix}Failure = call_{index}.decode_failure({}, bytes([{convention}]) + {wire})\nassert failure_{index}_{} == b.{prefix}Failure{}({detail})", failure.code, failure.code, failure.code, failure.code);
            }
            let _ = writeln!(out, "expect('STALE_INTERFACE', lambda: b.encode_{stem}(input_{index}, b.CODE_HASH, bytes([b.INTERFACE_DIGEST[0] ^ 1]) + b.INTERFACE_DIGEST[1:]))\nexpect('CODE_HASH_MISMATCH', lambda: b.encode_{stem}(input_{index}, bytes([b.CODE_HASH[0] ^ 1]) + b.CODE_HASH[1:], b.INTERFACE_DIGEST))\nexpect('INVALID_VALUE', lambda: b.encode_{stem}(cast(Any, object()), b.CODE_HASH, b.INTERFACE_DIGEST))\nexpect('INVALID_VALUE', lambda: b.{prefix}Input(cast(Any, 'wrong-native-type')))\nexpect('TRUNCATED', lambda: call_{index}.decode_output(b''))\nexpect('NON_CANONICAL', lambda: call_{index}.decode_output(bytes([3]) + {output_wire}))\nexpect('NON_CANONICAL', lambda: call_{index}.decode_output(bytes(b.MAX_CALLDATA_BYTES + 1)))\nexpect('INVALID_VALUE', lambda: call_{index}.decode_failure(-1, b''))");
            if !matches!(entry.output, Type::EvmHead) {
                let _ = writeln!(out, "expect('TRAILING_BYTES', lambda: call_{index}.decode_output(bytes([{output_convention}]) + {output_wire} + b'\\x00'))\nexpect('NON_CANONICAL', lambda: call_{index}.decode_output(bytes([{output_convention}, 0xff])))");
            }
            for bad in invalid_samples(&entry.input) {
                let _ = writeln!(out, "expect('INVALID_VALUE', lambda: b.{prefix}Input(cast(Any, {bad})))");
            }
            if matches!(entry.output, Type::Option(_)) {
                let _ = writeln!(out, "assert call_{index}.decode_output(bytes([1, 0x40, 0])) is None\nexpect('NON_CANONICAL', lambda: call_{index}.decode_output(bytes([1, 0x40, 2])))");
            }
            if let Type::Union(variants) = &entry.output {
                let unknown = (0..=u32::MAX).find(|tag| variants.iter().all(|variant| variant.tag != *tag)).expect("finite union has undeclared tags");
                let _ = writeln!(out, "expect('NON_CANONICAL', lambda: call_{index}.decode_output(bytes([1, 0x50]) + ({unknown}).to_bytes(4, 'big')))");
            }
            let unknown_failure = (0..=u32::MAX).find(|code| entry.failures.iter().all(|failure| failure.code != *code)).expect("finite failure table has undeclared codes");
            let _ = writeln!(out, "expect('UNKNOWN_FAILURE', lambda: call_{index}.decode_failure({unknown_failure}, b''))\n");
        }
        if self.entries.iter().any(|entry| !entry.failures.is_empty()) {
            out.push_str("print('BINDING_CASE typed_failure')\n");
        }
        out.push_str("print('BINDING_CASE stale_digest')\nprint('BINDING_CASE wrong_code_hash')\nprint('BINDING_CASE malformed_call')\n");
        out
    }

    #[must_use]
    pub fn generate_python_malformed_consumer(&self) -> String {
        let mut out = String::from("import bindings as b\n\n");
        for entry in &self.entries {
            let (stem, prefix) = names(entry);
            let _ = writeln!(out, "b.{prefix}Input('wrong-native-type')\nb.encode_{stem}(object(), b.CODE_HASH, b.INTERFACE_DIGEST)");
        }
        out
    }
}

fn sample(ty: &Type, context: &str) -> (String, String) {
    let scalar = match ty {
        Type::U8 => Some((127i128, 0x10, 1usize, false)),
        Type::U16 => Some((0x1234, 0x11, 2, false)),
        Type::U32 => Some((7, 0x12, 4, false)),
        Type::U64 => Some((9, 0x13, 8, false)),
        Type::U128 => Some((11, 0x14, 16, false)),
        Type::I8 => Some((-1, 0x18, 1, true)),
        Type::I16 => Some((-2, 0x19, 2, true)),
        Type::I32 => Some((-3, 0x1a, 4, true)),
        Type::I64 => Some((-4, 0x1b, 8, true)),
        Type::I128 => Some((-5, 0x1c, 16, true)),
        _ => None,
    };
    if let Some((value, tag, width, signed)) = scalar {
        let native = format!("{value}");
        let signed = if signed { "True" } else { "False" };
        return (native, format!("(bytes([{tag}]) + ({value}).to_bytes({width}, 'big', signed={signed}))"));
    }
    match ty {
        Type::U256 => ("bytes(31) + b'\\x0d'".into(), "(bytes([0x15]) + bytes(31) + b'\\x0d')".into()),
        Type::Bytes(bound) => {
            let count = (*bound).min(3);
            let value = format!("bytes(range(1, {}))", count + 1);
            (value.clone(), format!("(bytes([0x20]) + ({count}).to_bytes(4, 'big') + {value})"))
        }
        Type::Fixed(inner, bound) | Type::Variable(inner, bound) => {
            let (value, wire) = sample(inner, &format!("{context}Element"));
            let count = if matches!(ty, Type::Fixed(_, _)) { *bound } else { (*bound).min(2) };
            let tag = if matches!(ty, Type::Fixed(_, _)) { 0x30 } else { 0x31 };
            (format!("tuple({value} for _ in range({count}))"), format!("(bytes([{tag}]) + ({count}).to_bytes(4, 'big') + {wire} * {count})"))
        }
        Type::Option(inner) => {
            let (value, wire) = sample(inner, &format!("{context}Some"));
            (format!("b.Some({value})"), format!("(bytes([0x40, 1]) + {wire})"))
        }
        Type::Union(variants) => {
            let variant = &variants[0];
            let (value, wire) = sample(&variant.value, &format!("{context}Variant{}Value", variant.tag));
            (format!("{context}Variant{}({value})", variant.tag), format!("(bytes([0x50]) + ({}).to_bytes(4, 'big') + {wire})", variant.tag))
        }
        Type::EvmHead => ("bytes(31) + b'\\x01'".into(), "(bytes(31) + b'\\x01')".into()),
        _ => unreachable!(),
    }
}

fn invalid_samples(ty: &Type) -> Vec<String> {
    let integer = match ty {
        Type::U8 => Some((8, false)), Type::U16 => Some((16, false)),
        Type::U32 => Some((32, false)), Type::U64 => Some((64, false)),
        Type::U128 => Some((128, false)), Type::I8 => Some((8, true)),
        Type::I16 => Some((16, true)), Type::I32 => Some((32, true)),
        Type::I64 => Some((64, true)), Type::I128 => Some((128, true)), _ => None,
    };
    if let Some((bits, signed)) = integer {
        let mut values = alloc::vec![String::from("True"), String::from("1.0")];
        if signed {
            values.push(format!("(1 << {})", bits - 1));
            values.push(format!("(-(1 << {}) - 1)", bits - 1));
        } else {
            values.push(String::from("-1"));
            values.push(format!("(1 << {bits})"));
        }
        return values;
    }
    match ty {
        Type::U256 => alloc::vec!["bytes(31)".into(), "bytes(33)".into()],
        Type::Bytes(bound) => alloc::vec![format!("bytes({bound} + 1)"), "bytearray()".into()],
        Type::Fixed(_, _) => alloc::vec!["()".into(), "[]".into()],
        Type::Variable(_, bound) => alloc::vec![format!("tuple(None for _ in range({bound} + 1))"), "[]".into()],
        Type::Option(_) => alloc::vec!["True".into()],
        Type::Union(_) => alloc::vec!["object()".into()],
        Type::EvmHead => alloc::vec!["bytes(31)".into(), "bytes(b.MAX_CALLDATA_BYTES)".into()],
        _ => unreachable!(),
    }
}
