# Calldata encoding golden vectors

This directory contains frozen golden test vectors for the canonical calldata encoding
of LayerX programs, implemented by `Calldata` in
[`crates/layerx-programs-runtime/src/abi/codec.rs`](../../../crates/layerx-programs-runtime/src/abi/codec.rs).

## Structure

- `valid/primitives.json` - canonical encodings that must decode successfully
- `invalid/malformed.json` - invalid or non-canonical encodings that must be rejected,
  plus the tag-only empty input that must pass
- `boundaries/depth_and_size.json` - nesting-depth and size boundary cases
- `evm/head_only_layout.json` - EVM head-only convention vectors

## Vector format

Each `.json` file is an array of vectors:

```json
{
  "description": "Human-readable description",
  "hex": "Hex-encoded bytes",
  "expected": "pass" | "reject",
  "error": "Expected error code (for reject cases)",
  "note": "Optional explanation"
}
```

A vector may carry `hex_generator` instead of `hex` when its bytes are too large to
write out; the test runner skips those vectors.

## Conventions

1. **Canonical requirement**: only one valid encoding exists per logical value
2. **Nesting limit**: maximum depth of 16 nested structures (`MAX_NESTING_DEPTH`)
3. **Size limits**: input ≤ 1 MiB (`MAX_CALLDATA_BYTES`), decoded ≤ 16 MiB
   (`DECODED_SIZE_LIMIT`)
4. **Byte order**: all multi-byte integers use big-endian encoding

## Coverage

- Primitive integer types (u8, u16, u32, u64, u128, u256, i8, i16, i32, i64, i128)
- Byte strings (empty, typical, a 4096-byte string)
- Fixed and variable arrays (empty, nested, maximum depth)
- Options (None, Some with nested values)
- Tagged unions (variant indices 0, 1 and 255)
- EVM head-only layout (32-byte aligned words)
- Rejection cases (truncated, non-canonical, too deeply nested, huge declared lengths, invalid tags)

## Running

The vectors are exercised by
[`crates/layerx-programs-runtime/tests/calldata_golden_vectors.rs`](../../../crates/layerx-programs-runtime/tests/calldata_golden_vectors.rs):

```sh
cd programs
cargo test --locked -p layerx-programs-runtime --test calldata_golden_vectors
```
