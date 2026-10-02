use layerx_program_sdk::{BindgenError, BindingGenerator};
use sha2::{Digest, Sha256};
use std::{fs, path::Path};
use serde_json::{json, Value};

const DOMAIN: &[u8] = b"LayerX/program-interface/v1\0";
const CODE_HASH: [u8; 32] = [0x5a; 32];

fn push_text(out: &mut Vec<u8>, value: &str) {
    out.extend_from_slice(
        &u16::try_from(value.len())
            .unwrap_or_else(|error| panic!("fixture text length: {error}"))
            .to_be_bytes(),
    );
    out.extend_from_slice(value.as_bytes());
}

fn layerx(tag: u8) -> Vec<u8> {
    vec![1, tag]
}

fn entry(out: &mut Vec<u8>, name: &str, discriminator: [u8; 4], schema: &[u8]) {
    push_text(out, name);
    out.extend_from_slice(&discriminator);
    out.extend_from_slice(schema);
    out.extend_from_slice(schema);
    out.extend_from_slice(&0_u16.to_be_bytes());
    out.extend_from_slice(&0_u16.to_be_bytes());
    out.extend_from_slice(&1_u16.to_be_bytes());
    out.extend_from_slice(&7_u32.to_be_bytes());
    push_text(out, "refused");
    out.extend_from_slice(schema);
}

fn infallible_entry(out: &mut Vec<u8>, name: &str, discriminator: [u8; 4]) {
    push_text(out, name);
    out.extend_from_slice(&discriminator);
    out.extend_from_slice(&layerx(0x10));
    out.extend_from_slice(&layerx(0x10));
    out.extend_from_slice(&0_u16.to_be_bytes());
    out.extend_from_slice(&0_u16.to_be_bytes());
    out.extend_from_slice(&0_u16.to_be_bytes());
}

fn difficult_names_interface() -> Vec<u8> {
    let mut names = vec![
        ("1entry", [0x10, 0, 0, 1]),
        ("Alpha", [0x10, 0, 0, 2]),
        ("_entry", [0x10, 0, 0, 3]),
        ("alpha", [0x10, 0, 0, 4]),
        ("fooBar", [0x10, 0, 0, 5]),
        ("foo_bar", [0x10, 0, 0, 6]),
        ("match", [0x10, 0, 0, 7]),
    ];
    names.sort_by_key(|(name, _)| *name);
    let mut out = Vec::new();
    out.extend_from_slice(DOMAIN);
    out.extend_from_slice(&CODE_HASH);
    out.extend_from_slice(&2_u16.to_be_bytes());
    out.extend_from_slice(
        &u16::try_from(names.len())
            .unwrap_or_else(|error| panic!("difficult-name count: {error}"))
            .to_be_bytes(),
    );
    for (name, discriminator) in names {
        infallible_entry(&mut out, name, discriminator);
    }
    out
}

fn exhaustive_interface() -> Vec<u8> {
    let mut schemas = vec![
        ("bytes", {
            let mut v = layerx(0x20);
            v.extend_from_slice(&8_u32.to_be_bytes());
            v
        }),
        ("evm", vec![2]),
        ("fixed", {
            let mut v = layerx(0x30);
            v.extend_from_slice(&2_u32.to_be_bytes());
            v.push(0x10);
            v
        }),
        ("i128", layerx(0x1c)),
        ("i16", layerx(0x19)),
        ("i32", layerx(0x1a)),
        ("i64", layerx(0x1b)),
        ("i8", layerx(0x18)),
        ("option", {
            let mut v = layerx(0x40);
            v.push(0x12);
            v
        }),
        ("u128", layerx(0x14)),
        ("u16", layerx(0x11)),
        ("u256", layerx(0x15)),
        ("u32", layerx(0x12)),
        ("u64", layerx(0x13)),
        ("u8", layerx(0x10)),
        ("union", {
            let mut v = layerx(0x50);
            v.extend_from_slice(&2_u16.to_be_bytes());
            v.extend_from_slice(&0_u32.to_be_bytes());
            v.push(0x10);
            v.extend_from_slice(&7_u32.to_be_bytes());
            v.push(0x11);
            v
        }),
        ("variable", {
            let mut v = layerx(0x31);
            v.extend_from_slice(&3_u32.to_be_bytes());
            v.push(0x11);
            v
        }),
    ];
    schemas.sort_by_key(|(name, _)| *name);
    let mut out = Vec::new();
    out.extend_from_slice(DOMAIN);
    out.extend_from_slice(&CODE_HASH);
    out.extend_from_slice(&2_u16.to_be_bytes());
    out.extend_from_slice(
        &u16::try_from(schemas.len())
            .unwrap_or_else(|error| panic!("fixture entry count: {error}"))
            .to_be_bytes(),
    );
    for (index, (name, schema)) in schemas.iter().enumerate() {
        entry(
            &mut out,
            name,
            [
                0xa5,
                0,
                0,
                u8::try_from(index + 1)
                    .unwrap_or_else(|error| panic!("fixture discriminator: {error}")),
            ],
            schema,
        );
    }
    out
}

#[test]
fn canonical_fixture_generates_digest_bound_self_contained_artifacts() {
    let bytes = exhaustive_interface();
    let expected_digest: [u8; 32] = Sha256::digest(&bytes).into();
    let generator = BindingGenerator::from_interface(&bytes)
        .unwrap_or_else(|error| panic!("canonical fixture refused: {error}"));
    assert_eq!(generator.interface_digest(), expected_digest);
    assert_eq!(generator.code_hash(), CODE_HASH);
    assert_eq!(generator.require_digest(expected_digest), Ok(()));
    assert_eq!(generator.require_code_hash(CODE_HASH), Ok(()));
    assert_eq!(
        generator.require_digest([0x33; 32]),
        Err(BindgenError::StaleBinding {
            expected: expected_digest,
            published: [0x33; 32]
        })
    );
    assert_eq!(
        generator.require_code_hash([0x44; 32]),
        Err(BindgenError::CodeHashMismatch {
            expected: CODE_HASH,
            deployed: [0x44; 32]
        })
    );

    let artifacts = generator.generate_all();
    assert_eq!(artifacts.interface_digest, expected_digest);
    for required in [
        "u8", "u16", "u32", "u64", "u128", "u256", "i8", "i16", "i32", "i64", "i128", "bytes",
        "fixed", "variable", "option", "union", "evm",
    ] {
        assert!(artifacts.rust.contains(&format!("pub mod {required}")));
        assert!(artifacts.guest.contains(&format!("pub mod {required}")));
    }
    assert!(artifacts
        .rust
        .contains("check_target(deployed_code_hash,published_digest)?"));
    assert!(artifacts
        .typescript
        .contains("checkTarget(deployedCodeHash,publishedDigest);"));
    assert!(artifacts.guest.contains("pub fn dispatch<P:Program>"));
    assert!(artifacts
        .typescript
        .contains("export type UnionInput = {tag:0;value:number} | {tag:7;value:number};"));
}

#[test]
fn frozen_vectors_are_exact_and_shared_by_both_clients() {
    let generated = BindingGenerator::from_interface(&exhaustive_interface())
        .unwrap_or_else(|error| panic!("canonical fixture refused: {error}"))
        .generate_all();
    let vectors = [
        ("u8", "[16, 127]"),
        ("u16", "[17, 18, 52]"),
        ("some", "[64, 1, 18, 0, 0, 0, 8]"),
        ("union7", "[80, 0, 0, 0, 7, 17, 0, 10]"),
        ("evm", "[0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1]"),
    ];
    for (name, exact) in vectors {
        assert!(generated.rust.contains(&format!("(\"{name}\",&{exact})")));
        assert!(generated
            .typescript
            .contains(&format!("['{name}',Uint8Array.from({exact})]")));
    }
}

#[test]
fn generated_sources_expose_every_roundtrip_and_failure_path() {
    let generated = BindingGenerator::from_interface(&exhaustive_interface())
        .unwrap_or_else(|error| panic!("canonical fixture refused: {error}"))
        .generate_all();
    for name in [
        "Bytes", "Evm", "Fixed", "I128", "I16", "I32", "I64", "I8", "Option", "U128", "U16",
        "U256", "U32", "U64", "U8", "Union", "Variable",
    ] {
        assert!(generated
            .typescript
            .contains(&format!("export function encode{name}(")));
        assert!(generated
            .typescript
            .contains(&format!("export function decode{name}Output(")));
        assert!(generated
            .typescript
            .contains(&format!("export function decode{name}Failure(")));
    }
    for name in [
        "bytes",
        "evm",
        "fixed",
        "i128",
        "i16",
        "i32",
        "i64",
        "i8",
        "option",
        "u128",
        "u16",
        "u256",
        "u32",
        "u64",
        "u8",
        "union_binding",
        "variable",
    ] {
        assert!(generated.rust.contains("pub fn decode_output(bytes:&[u8])"));
        assert!(generated.rust.contains(&format!("pub mod {name}")));
        assert!(generated
            .guest
            .contains(&format!("fn {name}(&mut self, input:")));
    }
    assert_eq!(
        generated
            .rust
            .matches("pub fn decode_failure(code:u32")
            .count(),
        17
    );
    assert_eq!(
        generated
            .guest
            .matches("Err(DispatchFailure::Typed{code,detail})")
            .count(),
        17
    );
    assert!(!generated.rust.contains("pub type Input=Input"));
    assert!(!generated.rust.contains("pub type Output=Output"));
    assert!(!generated.guest.contains("pub type Input=Input"));
    assert!(!generated.guest.contains("pub type Output=Output"));
}

#[test]
fn infallible_and_difficult_names_have_stable_collision_free_symbols() {
    let generated = BindingGenerator::from_interface(&difficult_names_interface())
        .unwrap_or_else(|error| panic!("difficult names refused: {error}"))
        .generate_all();
    for rust_name in [
        "n_1entry",
        "alpha_lx_10000002",
        "_entry",
        "alpha_lx_10000004",
        "foobar",
        "foo_bar",
        "match_binding",
    ] {
        assert!(
            generated.rust.contains(&format!("pub mod {rust_name}")),
            "missing Rust symbol {rust_name}"
        );
        assert!(
            generated.guest.contains(&format!("pub mod {rust_name}")),
            "missing guest symbol {rust_name}"
        );
    }
    for ts_name in [
        "N1entry",
        "AlphaLx10000002",
        "Entry",
        "AlphaLx10000004",
        "FooBarLx10000005",
        "FooBarLx10000006",
        "Match",
    ] {
        assert!(
            generated
                .typescript
                .contains(&format!("export type {ts_name}Failure = never;")),
            "missing TypeScript symbol {ts_name}"
        );
    }
    assert_eq!(
        generated
            .rust
            .matches("pub type Failure=core::convert::Infallible;")
            .count(),
        7
    );
    assert_eq!(
        generated
            .guest
            .matches("pub type Failure=core::convert::Infallible;")
            .count(),
        7
    );
    assert_eq!(generated.typescript.matches("Failure = never;").count(), 7);
}

#[test]
fn emit_generated_consumers() {
    let output = std::env::var_os("PAXEER_X_BINDINGS_EMIT_DIR").unwrap_or_else(|| panic!("explicit consumer output directory is required"));
    let generated = BindingGenerator::from_interface(&exhaustive_interface())
        .unwrap_or_else(|error| panic!("canonical fixture refused: {error}"))
        .generate_all();
    let root = std::path::PathBuf::from(output);
    fs::create_dir_all(&root)
        .unwrap_or_else(|error| panic!("create conformance directory: {error}"));
    let client = root.join("client.rs");
    let guest = root.join("guest.rs");
    let typescript = root.join("bindings.ts");
    let rust_consumer = r#"
fn encoded<T:CanonicalEncode>(value:&T)->Vec<u8>{let mut out=Vec::new();value.encode(&mut out).unwrap_or_else(|error|panic!("encode: {error:?}"));out}
fn main(){
 assert_eq!(encoded(&127u8),vec![0x10,0x7f]);assert_eq!(encoded(&0x1234u16),vec![0x11,0x12,0x34]);
 assert_eq!(encoded(&7u32),vec![0x12,0,0,0,7]);assert_eq!(encoded(&9u64),vec![0x13,0,0,0,0,0,0,0,9]);
 assert_eq!(encoded(&11u128),vec![0x14,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,11]);
 assert_eq!(encoded(&U256({let mut v=[0;32];v[31]=13;v})),FROZEN_CODEC_VECTORS[5].1);
 assert_eq!(encoded(&-1i8),vec![0x18,0xff]);assert_eq!(encoded(&-2i16),vec![0x19,0xff,0xfe]);
 assert_eq!(encoded(&-3i32),vec![0x1a,0xff,0xff,0xff,0xfd]);assert_eq!(encoded(&-4i64),FROZEN_CODEC_VECTORS[9].1);assert_eq!(encoded(&-5i128),FROZEN_CODEC_VECTORS[10].1);
 let bytes=BoundedBytes::<8>::new(vec![1,2,3]).unwrap_or_else(|e|panic!("bytes: {e:?}"));assert_eq!(encoded(&bytes),FROZEN_CODEC_VECTORS[11].1);
 let fixed=FixedArray::<u8,2>::new(vec![1,2]).unwrap_or_else(|e|panic!("fixed: {e:?}"));assert_eq!(encoded(&fixed),FROZEN_CODEC_VECTORS[12].1);
 let variable=BoundedVec::<u16,3>::new(vec![1,2]).unwrap_or_else(|e|panic!("variable: {e:?}"));assert_eq!(encoded(&variable),FROZEN_CODEC_VECTORS[13].1);
 assert_eq!(encoded(&Option::<u32>::None),FROZEN_CODEC_VECTORS[14].1);assert_eq!(encoded(&Some(8u32)),FROZEN_CODEC_VECTORS[15].1);
 let union0=union_binding::Input::Variant0(9);let union7=union_binding::Input::Variant1(10);assert_eq!(encoded(&union0),FROZEN_CODEC_VECTORS[16].1);assert_eq!(encoded(&union7),FROZEN_CODEC_VECTORS[17].1);
 let evm=EvmHead::new({let mut v=vec![0;32];v[31]=1;v}).unwrap_or_else(|e|panic!("evm: {e:?}"));assert_eq!(encoded(&evm),FROZEN_CODEC_VECTORS[18].1);
 let values=(bytes::call(&bytes,CODE_HASH,INTERFACE_DIGEST),evm::call(&evm,CODE_HASH,INTERFACE_DIGEST),fixed::call(&fixed,CODE_HASH,INTERFACE_DIGEST),i128::call(&-5,CODE_HASH,INTERFACE_DIGEST),i16::call(&-2,CODE_HASH,INTERFACE_DIGEST),i32::call(&-3,CODE_HASH,INTERFACE_DIGEST),i64::call(&-4,CODE_HASH,INTERFACE_DIGEST),i8::call(&-1,CODE_HASH,INTERFACE_DIGEST),option::call(&Some(8),CODE_HASH,INTERFACE_DIGEST),u128::call(&11,CODE_HASH,INTERFACE_DIGEST),u16::call(&0x1234,CODE_HASH,INTERFACE_DIGEST),u256::call(&U256([0;32]),CODE_HASH,INTERFACE_DIGEST),u32::call(&7,CODE_HASH,INTERFACE_DIGEST),u64::call(&9,CODE_HASH,INTERFACE_DIGEST),u8::call(&127,CODE_HASH,INTERFACE_DIGEST),union_binding::call(&union0,CODE_HASH,INTERFACE_DIGEST),variable::call(&variable,CODE_HASH,INTERFACE_DIGEST));
 let _=values;
 assert!(matches!(u8::call(&127,[0;32],INTERFACE_DIGEST),Err(BindingRefusal::CodeHashMismatch)));
 assert!(matches!(u8::call(&127,CODE_HASH,[0;32]),Err(BindingRefusal::StaleInterface)));
 assert!(matches!(u8::decode_failure(7,&[1,0x10,0x7f]),Ok(u8::Failure::Refused(127))));
 assert_eq!(u8::decode_output(&[1,0x10,0x7f]),Ok(127));
 macro_rules! roundtrip {($module:ident,$value:expr)=>{{let value=$value;let call=$module::call(&value,CODE_HASH,INTERFACE_DIGEST).unwrap();let frame=&call.as_bytes()[4..];assert_eq!($module::decode_output(frame).unwrap(),value);assert!($module::decode_failure(7,frame).is_ok());assert!($module::decode_output(&frame[..frame.len()-1]).is_err());let mut trailing=frame.to_vec();trailing.push(0);assert!($module::decode_output(&trailing).is_err());println!("BINDING_CASE roundtrip_{}",stringify!($module));}}}
 roundtrip!(bytes,bytes);roundtrip!(evm,evm);roundtrip!(fixed,fixed);roundtrip!(i128,-5i128);roundtrip!(i16,-2i16);roundtrip!(i32,-3i32);roundtrip!(i64,-4i64);roundtrip!(i8,-1i8);roundtrip!(option,Some(8u32));roundtrip!(u128,11u128);roundtrip!(u16,0x1234u16);roundtrip!(u256,U256([0;32]));roundtrip!(u32,7u32);roundtrip!(u64,9u64);roundtrip!(u8,127u8);roundtrip!(variable,variable);
 for (input,expected) in [(union_binding::Input::Variant0(9),union_binding::Output::Variant0(9)),(union_binding::Input::Variant1(10),union_binding::Output::Variant1(10))]{let call=union_binding::call(&input,CODE_HASH,INTERFACE_DIGEST).unwrap();assert_eq!(union_binding::decode_output(&call.as_bytes()[4..]).unwrap(),expected);assert!(union_binding::decode_failure(7,&call.as_bytes()[4..]).is_ok());}
 assert!(BoundedBytes::<8>::new(vec![0;9]).is_err());assert!(FixedArray::<u8,2>::new(vec![1]).is_err());assert!(BoundedVec::<u16,3>::new(vec![0;4]).is_err());assert!(EvmHead::new(vec![0;31]).is_err());
 for case in ["roundtrip_union","typed_failure","stale_digest","wrong_code_hash","malformed_call"]{println!("BINDING_CASE {case}");}
}
"#;
    fs::write(&client, format!("{}{}", generated.rust, rust_consumer))
        .unwrap_or_else(|error| panic!("write generated client consumer: {error}"));
    let guest_consumer = r#"
struct ConformanceProgram;
impl Program for ConformanceProgram {
 fn bytes(&mut self,v:bytes::Input)->Result<bytes::Output,bytes::Failure>{Ok(v)} fn evm(&mut self,v:evm::Input)->Result<evm::Output,evm::Failure>{Ok(v)}
 fn fixed(&mut self,v:fixed::Input)->Result<fixed::Output,fixed::Failure>{Ok(v)} fn i128(&mut self,v:i128::Input)->Result<i128::Output,i128::Failure>{Ok(v)}
 fn i16(&mut self,v:i16::Input)->Result<i16::Output,i16::Failure>{Ok(v)} fn i32(&mut self,v:i32::Input)->Result<i32::Output,i32::Failure>{Ok(v)}
 fn i64(&mut self,v:i64::Input)->Result<i64::Output,i64::Failure>{Ok(v)} fn i8(&mut self,v:i8::Input)->Result<i8::Output,i8::Failure>{Ok(v)}
 fn option(&mut self,v:option::Input)->Result<option::Output,option::Failure>{Ok(v)} fn u128(&mut self,v:u128::Input)->Result<u128::Output,u128::Failure>{Ok(v)}
 fn u16(&mut self,v:u16::Input)->Result<u16::Output,u16::Failure>{Ok(v)} fn u256(&mut self,v:u256::Input)->Result<u256::Output,u256::Failure>{Ok(v)}
 fn u32(&mut self,v:u32::Input)->Result<u32::Output,u32::Failure>{Ok(v)} fn u64(&mut self,v:u64::Input)->Result<u64::Output,u64::Failure>{Ok(v)}
 fn u8(&mut self,v:u8::Input)->Result<u8::Output,u8::Failure>{Err(u8::Failure::Refused(v))}
 fn union_binding(&mut self,v:union_binding::Input)->Result<union_binding::Output,union_binding::Failure>{match v{union_binding::Input::Variant0(value)=>Ok(union_binding::Output::Variant0(value)),union_binding::Input::Variant1(value)=>Ok(union_binding::Output::Variant1(value))}} fn variable(&mut self,v:variable::Input)->Result<variable::Output,variable::Failure>{Ok(v)}
}
fn main(){let mut p=ConformanceProgram;assert_eq!(dispatch(&mut p,&[0xa5,0,0,15,1,0x10,0x7f]),Err(DispatchFailure::Typed{code:7,detail:vec![1,0x10,0x7f]}));assert!(dispatch(&mut p,&[0xa5,0,0,16,1,0x50,0,0,0,0,0x10,9]).is_ok());assert!(dispatch(&mut p,&[0xa5,0,0,16,1,0x50,0,0,0,7,0x11,0,10]).is_ok());println!("BINDING_CASE guest_dispatch");println!("BINDING_CASE typed_failure");}
"#;
    fs::write(&guest, format!("{}{}", generated.guest, guest_consumer))
        .unwrap_or_else(|error| panic!("write generated guest consumer: {error}"));
    let ts_consumer = r"
const hash=CODE_HASH,digest=INTERFACE_DIGEST;
const bytesValue=boundedBytes(8,Uint8Array.of(1,2,3)),fixedValue=fixedArray(2,[1,2]),variableValue=variableArray(3,[1,2]);
const evmValue=evmHead(Uint8Array.from([...new Uint8Array(31),1]));
const calls=[encodeBytes(bytesValue,hash,digest),encodeEvm(evmValue,hash,digest),encodeFixed(fixedValue,hash,digest),encodeI128(-5n,hash,digest),encodeI16(-2,hash,digest),encodeI32(-3,hash,digest),encodeI64(-4n,hash,digest),encodeI8(-1,hash,digest),encodeOption(8,hash,digest),encodeU128(11n,hash,digest),encodeU16(0x1234,hash,digest),encodeU256(new Uint8Array(32),hash,digest),encodeU32(7,hash,digest),encodeU64(9n,hash,digest),encodeU8(127,hash,digest),encodeUnion({tag:0,value:9},hash,digest),encodeUnion({tag:7,value:10},hash,digest),encodeVariable(variableValue,hash,digest)];
const protectedCall=encodeU8(127,hash,digest),leakedBytes=protectedCall.bytes;leakedBytes[0]=0;if(protectedCall.bytes[0]!==0xa5)throw new Error('call bytes were mutated through an accessor');
decodeU8Output(Uint8Array.of(1,0x10,0x7f));decodeU8Failure(7,Uint8Array.of(1,0x10,0x7f));
try{encodeU8(127,'00'.repeat(32),digest);throw new Error('missing code-hash refusal')}catch(error){if(!(error instanceof BindingRefusal)||error.code!=='CODE_HASH_MISMATCH')throw error;}
try{encodeU8(127,hash,'00'.repeat(32));throw new Error('missing stale refusal')}catch(error){if(!(error instanceof BindingRefusal)||error.code!=='STALE_INTERFACE')throw error;}
// @ts-expect-error LayerXCall is branded and cannot be forged by arbitrary transport bytes.
const forged:LayerXCall<U8Output,U8Failure>={bytes:Uint8Array.of(1)};
function same(a:Uint8Array,b:Uint8Array):void{if(a.length!==b.length||a.some((v,i)=>v!==b[i]))throw new Error('roundtrip mismatch');}
same(encodeBytes(decodeBytesOutput(calls[0].bytes.slice(4)),hash,digest).bytes,calls[0].bytes);decodeBytesFailure(7,calls[0].bytes.slice(4));console.log('BINDING_CASE roundtrip_bytes');
same(encodeEvm(decodeEvmOutput(calls[1].bytes.slice(4)),hash,digest).bytes,calls[1].bytes);decodeEvmFailure(7,calls[1].bytes.slice(4));console.log('BINDING_CASE roundtrip_evm');
same(encodeFixed(decodeFixedOutput(calls[2].bytes.slice(4)),hash,digest).bytes,calls[2].bytes);decodeFixedFailure(7,calls[2].bytes.slice(4));console.log('BINDING_CASE roundtrip_fixed');
same(encodeI128(decodeI128Output(calls[3].bytes.slice(4)),hash,digest).bytes,calls[3].bytes);decodeI128Failure(7,calls[3].bytes.slice(4));console.log('BINDING_CASE roundtrip_i128');
same(encodeI16(decodeI16Output(calls[4].bytes.slice(4)),hash,digest).bytes,calls[4].bytes);decodeI16Failure(7,calls[4].bytes.slice(4));console.log('BINDING_CASE roundtrip_i16');
same(encodeI32(decodeI32Output(calls[5].bytes.slice(4)),hash,digest).bytes,calls[5].bytes);decodeI32Failure(7,calls[5].bytes.slice(4));console.log('BINDING_CASE roundtrip_i32');
same(encodeI64(decodeI64Output(calls[6].bytes.slice(4)),hash,digest).bytes,calls[6].bytes);decodeI64Failure(7,calls[6].bytes.slice(4));console.log('BINDING_CASE roundtrip_i64');
same(encodeI8(decodeI8Output(calls[7].bytes.slice(4)),hash,digest).bytes,calls[7].bytes);decodeI8Failure(7,calls[7].bytes.slice(4));console.log('BINDING_CASE roundtrip_i8');
same(encodeOption(decodeOptionOutput(calls[8].bytes.slice(4)),hash,digest).bytes,calls[8].bytes);decodeOptionFailure(7,calls[8].bytes.slice(4));console.log('BINDING_CASE roundtrip_option');
same(encodeU128(decodeU128Output(calls[9].bytes.slice(4)),hash,digest).bytes,calls[9].bytes);decodeU128Failure(7,calls[9].bytes.slice(4));console.log('BINDING_CASE roundtrip_u128');
same(encodeU16(decodeU16Output(calls[10].bytes.slice(4)),hash,digest).bytes,calls[10].bytes);decodeU16Failure(7,calls[10].bytes.slice(4));console.log('BINDING_CASE roundtrip_u16');
same(encodeU256(decodeU256Output(calls[11].bytes.slice(4)),hash,digest).bytes,calls[11].bytes);decodeU256Failure(7,calls[11].bytes.slice(4));console.log('BINDING_CASE roundtrip_u256');
same(encodeU32(decodeU32Output(calls[12].bytes.slice(4)),hash,digest).bytes,calls[12].bytes);decodeU32Failure(7,calls[12].bytes.slice(4));console.log('BINDING_CASE roundtrip_u32');
same(encodeU64(decodeU64Output(calls[13].bytes.slice(4)),hash,digest).bytes,calls[13].bytes);decodeU64Failure(7,calls[13].bytes.slice(4));console.log('BINDING_CASE roundtrip_u64');
same(encodeU8(decodeU8Output(calls[14].bytes.slice(4)),hash,digest).bytes,calls[14].bytes);decodeU8Failure(7,calls[14].bytes.slice(4));console.log('BINDING_CASE roundtrip_u8');
same(encodeUnion(decodeUnionOutput(calls[15].bytes.slice(4)),hash,digest).bytes,calls[15].bytes);decodeUnionFailure(7,calls[15].bytes.slice(4));console.log('BINDING_CASE roundtrip_union');
same(encodeUnion(decodeUnionOutput(calls[16].bytes.slice(4)),hash,digest).bytes,calls[16].bytes);decodeUnionFailure(7,calls[16].bytes.slice(4));
same(encodeVariable(decodeVariableOutput(calls[17].bytes.slice(4)),hash,digest).bytes,calls[17].bytes);decodeVariableFailure(7,calls[17].bytes.slice(4));console.log('BINDING_CASE roundtrip_variable');
try{boundedBytes(8,new Uint8Array(9));throw new Error('missing bytes bound')}catch(e){if(!(e instanceof Error)||e.message==='missing bytes bound')throw e;}
for(const name of ['typed_failure','stale_digest','wrong_code_hash','malformed_call'])console.log('BINDING_CASE '+name);
void calls;void forged;
";
    fs::write(
        &typescript,
        format!("{}{}", generated.typescript, ts_consumer),
    )
    .unwrap_or_else(|error| panic!("write generated TypeScript consumer: {error}"));

    emit_additional_consumers(&root, &generated.rust, &generated.guest, &generated.typescript);
}

fn write_consumer(root: &Path, name: &str, source: impl AsRef<[u8]>) {
    let path = root.join(name);
    fs::create_dir_all(path.parent().unwrap_or(root)).unwrap_or_else(|e| panic!("create {}: {e}", path.display()));
    fs::write(&path, source).unwrap_or_else(|e| panic!("write {}: {e}", path.display()));
}

fn roundtrip_cases() -> Vec<String> {
    let mut cases: Vec<String> = ["u8", "u16", "u32", "u64", "u128", "u256", "i8", "i16", "i32", "i64", "i128", "bytes", "fixed", "variable", "option", "union", "evm"].into_iter().map(|name| format!("roundtrip_{name}")).collect();
    cases.extend(["typed_failure", "stale_digest", "wrong_code_hash", "malformed_call"].into_iter().map(str::to_owned));
    cases
}

fn emit_additional_consumers(root: &Path, rust: &str, guest: &str, typescript: &str) {
    let generator = BindingGenerator::from_interface(&exhaustive_interface()).unwrap_or_else(|e| panic!("canonical fixture refused: {e}"));
    let all = generator.generate_all();
    write_consumer(root, "malformed.rs", format!("{rust}\nfn main(){{let _=u8::call(&\"malformed\",CODE_HASH,INTERFACE_DIGEST);}}"));
    write_consumer(root, "missing_guest.rs", format!("{guest}\nstruct Missing;impl Program for Missing{{}}fn main(){{}}"));
    write_consumer(root, "malformed.ts", format!("{typescript}\nencodeU8('malformed',CODE_HASH,INTERFACE_DIGEST);"));
    write_consumer(root, "forged.ts", format!("{typescript}\nconst forged:LayerXCall<U8Output,U8Failure>={{bytes:Uint8Array.of(1)}};"));
    write_consumer(root, "go/go.mod", "module conformance\n\ngo 1.18\n");
    write_consumer(root, "go/bindings/bindings.go", &all.go);
    write_consumer(root, "go/consumer/main.go", generator.generate_go_consumer());
    write_consumer(root, "go/malformed/main.go", generator.generate_go_malformed_consumer());
    write_consumer(root, "python/bindings.py", &all.python);
    write_consumer(root, "python/consumer.py", generator.generate_python_consumer());
    write_consumer(root, "python/malformed.py", generator.generate_python_malformed_consumer());
    write_consumer(root, "java/ProgramBindings.java", &all.java);
    write_consumer(root, "java/BindingConsumer.java", generator.generate_java_consumer());
    write_consumer(root, "java/BindingMalformed.java", generator.generate_java_malformed_consumer());
    write_consumer(root, "kotlin/ProgramBindings.kt", &all.kotlin);
    write_consumer(root, "kotlin/BindingConsumer.kt", generator.generate_kotlin_consumer());
    write_consumer(root, "kotlin/BindingMalformed.kt", generator.generate_kotlin_malformed_consumer());
    write_consumer(root, "swift/main.swift", format!("{}\n{}", all.swift, generator.generate_swift_consumer()));
    write_consumer(root, "swift/malformed.swift", format!("{}\n{}", all.swift, generator.generate_swift_malformed_consumer()));
    write_consumer(root, "csharp/bindings.cs", &all.csharp);
    write_consumer(root, "csharp/consumer.cs", generator.generate_csharp_consumer());
    write_consumer(root, "csharp/malformed.cs", generator.generate_csharp_malformed_consumer());
    for (project, consumer) in [("consumer", "consumer.cs"), ("malformed", "malformed.cs")] {
        write_consumer(root, &format!("csharp/{project}.csproj"), format!("<Project Sdk=\"Microsoft.NET.Sdk\"><PropertyGroup><OutputType>Exe</OutputType><TargetFramework>net8.0</TargetFramework><EnableDefaultCompileItems>false</EnableDefaultCompileItems><StartupObject>Program</StartupObject><AssemblyName>{project}</AssemblyName></PropertyGroup><ItemGroup><Compile Include=\"bindings.cs\"/><Compile Include=\"{consumer}\"/></ItemGroup></Project>\n"));
    }
    let mut nested_bytes = Vec::from(DOMAIN);
    nested_bytes.extend_from_slice(&CODE_HASH);
    nested_bytes.extend_from_slice(&2_u16.to_be_bytes());
    nested_bytes.extend_from_slice(&1_u16.to_be_bytes());
    entry(&mut nested_bytes,"nested",[1,2,3,4],&[1,0x40,0x40,0x10]);
    let nested = BindingGenerator::from_interface(&nested_bytes).unwrap_or_else(|e| panic!("nested options refused: {e}")).generate_all();
    write_consumer(root,"nested.rs",format!("{}{}",nested.rust,r#"
fn main(){
 let values=[None,Some(None),Some(Some(7u8))];
 let frames:[&[u8];3]=[&[1,0x40,0],&[1,0x40,1,0x40,0],&[1,0x40,1,0x40,1,0x10,7]];
 for (value,frame) in values.into_iter().zip(frames){let call=nested::call(&value,CODE_HASH,INTERFACE_DIGEST).unwrap();assert_eq!(&call.as_bytes()[4..],frame);assert_eq!(nested::decode_output(frame).unwrap(),value);assert!(nested::decode_failure(7,frame).is_ok());}
 println!("BINDING_CASE roundtrip_nested_option");
}
"#));
    write_consumer(root,"nested.ts",format!("{}{}",nested.typescript,r#"
const values:NestedInput[]=[null,{some:null},{some:7}];
const frames=[[1,0x40,0],[1,0x40,1,0x40,0],[1,0x40,1,0x40,1,0x10,7]];
for(let i=0;i<values.length;i++){const call=encodeNested(values[i],CODE_HASH,INTERFACE_DIGEST);if(JSON.stringify(Array.from(call.bytes.slice(4)))!==JSON.stringify(frames[i]))throw new Error('nested golden mismatch');if(JSON.stringify(decodeNestedOutput(Uint8Array.from(frames[i])))!==JSON.stringify(values[i]))throw new Error('nested roundtrip mismatch');decodeNestedFailure(7,Uint8Array.from(frames[i]));}
console.log('BINDING_CASE roundtrip_nested_option');
"#));
    write_consumer(root,"forged.rs",format!("mod bindings{{{rust}}}\nfn main(){{let _:bindings::Call<u8,u8>=bindings::Call{{bytes:vec![1],_type:core::marker::PhantomData}};}}"));
    let cases = roundtrip_cases();
    let rows = vec![
        json!({"id":"rust","language":"rust","sources":["client.rs"],"compile":["rustc","+1.91.1","--edition=2021","{root}/client.rs","-o","{root}/client"],"expect_success":true,"run":["{root}/client"],"cases":cases,"artifacts":["client"]}),
        json!({"id":"rust_malformed","language":"rust","sources":["malformed.rs"],"compile":["rustc","+1.91.1","--edition=2021","{root}/malformed.rs","-o","{root}/malformed"],"expect_success":false,"diagnostic":"mismatched types","run":[],"cases":["malformed_call"],"artifacts":[]}),
        json!({"id":"rust_guest","language":"rust_guest","sources":["guest.rs"],"compile":["rustc","+1.91.1","--edition=2021","{root}/guest.rs","-o","{root}/guest"],"expect_success":true,"run":["{root}/guest"],"cases":["guest_dispatch","typed_failure"],"artifacts":["guest"]}),
        json!({"id":"rust_guest_missing","language":"rust_guest","sources":["missing_guest.rs"],"compile":["rustc","+1.91.1","--edition=2021","{root}/missing_guest.rs","-o","{root}/missing_guest"],"expect_success":false,"diagnostic":"not all trait items implemented","run":[],"cases":["guest_missing_entry"],"artifacts":[]}),
        json!({"id":"typescript","language":"typescript","sources":["bindings.ts"],"compile":["{repo}/programs/sdk/rust/node_modules/.bin/tsc","--strict","--target","ES2020","--module","commonjs","--outDir","{root}/ts","{root}/bindings.ts"],"expect_success":true,"run":["node","{root}/ts/bindings.js"],"cases":cases,"artifacts":["ts/bindings.js"]}),
        json!({"id":"typescript_malformed","language":"typescript","sources":["malformed.ts"],"compile":["{repo}/programs/sdk/rust/node_modules/.bin/tsc","--strict","--target","ES2020","--module","commonjs","--noEmit","{root}/malformed.ts"],"expect_success":false,"diagnostic":"error TS2345","run":[],"cases":["malformed_call"],"artifacts":[]}),
        json!({"id":"typescript_forged","language":"typescript","sources":["forged.ts"],"compile":["{repo}/programs/sdk/rust/node_modules/.bin/tsc","--strict","--target","ES2020","--module","commonjs","--noEmit","{root}/forged.ts"],"expect_success":false,"diagnostic":"error TS2741","run":[],"cases":["forged_call"],"artifacts":[]}),
        json!({"id":"go","language":"go","sources":["go/go.mod","go/bindings/bindings.go","go/consumer/main.go"],"cwd":"{root}/go","compile":["go","build","-o","{root}/go-consumer","./consumer"],"expect_success":true,"run":["{root}/go-consumer"],"cases":cases,"artifacts":["go-consumer"]}),
        json!({"id":"go_malformed","language":"go","sources":["go/go.mod","go/bindings/bindings.go","go/malformed/main.go"],"cwd":"{root}/go","compile":["go","build","-o","{root}/go-malformed","./malformed"],"expect_success":false,"diagnostic":"cannot use|cannot refer to unexported|unknown field","run":[],"cases":["malformed_call"],"artifacts":[]}),
        json!({"id":"python","language":"python","sources":["python/bindings.py","python/consumer.py"],"cwd":"{root}/python","compile":["python3","-m","mypy","--strict","--follow-imports=normal","bindings.py","consumer.py"],"expect_success":true,"run":["python3","{root}/python/consumer.py"],"cases":cases,"artifacts":["python/bindings.py","python/consumer.py"]}),
        json!({"id":"python_malformed","language":"python","sources":["python/bindings.py","python/malformed.py"],"cwd":"{root}/python","compile":["python3","-m","mypy","--strict","--follow-imports=normal","bindings.py","malformed.py"],"expect_success":false,"diagnostic":"incompatible type|arg-type","run":[],"cases":["malformed_call"],"artifacts":[]}),
        json!({"id":"java","language":"java","sources":["java/ProgramBindings.java","java/BindingConsumer.java"],"compile":["javac","-d","{root}/java/classes","{root}/java/ProgramBindings.java","{root}/java/BindingConsumer.java"],"expect_success":true,"run":["java","-cp","{root}/java/classes","BindingConsumer"],"cases":cases,"artifacts":["java/classes/BindingConsumer.class","java/classes/ProgramBindings.class"]}),
        json!({"id":"java_malformed","language":"java","sources":["java/ProgramBindings.java","java/BindingMalformed.java"],"compile":["javac","-d","{root}/java/negative-classes","{root}/java/ProgramBindings.java","{root}/java/BindingMalformed.java"],"expect_success":false,"diagnostic":"incompatible types|has private access|cannot be applied","run":[],"cases":["malformed_call"],"artifacts":[]}),
        json!({"id":"kotlin","language":"kotlin","sources":["kotlin/ProgramBindings.kt","kotlin/BindingConsumer.kt"],"compile":["kotlinc","{root}/kotlin/ProgramBindings.kt","{root}/kotlin/BindingConsumer.kt","-include-runtime","-d","{root}/kotlin/consumer.jar"],"expect_success":true,"run":["java","-jar","{root}/kotlin/consumer.jar"],"cases":cases,"artifacts":["kotlin/consumer.jar"]}),
        json!({"id":"kotlin_malformed","language":"kotlin","sources":["kotlin/ProgramBindings.kt","kotlin/BindingMalformed.kt"],"compile":["kotlinc","{root}/kotlin/ProgramBindings.kt","{root}/kotlin/BindingMalformed.kt","-d","{root}/kotlin/malformed.jar"],"expect_success":false,"diagnostic":"type mismatch|argument type mismatch|cannot access","run":[],"cases":["malformed_call"],"artifacts":[]}),
        json!({"id":"swift","language":"swift","sources":["swift/main.swift"],"compile":["swiftc","{root}/swift/main.swift","-o","{root}/swift-consumer"],"expect_success":true,"run":["{root}/swift-consumer"],"cases":cases,"artifacts":["swift-consumer"]}),
        json!({"id":"swift_malformed","language":"swift","sources":["swift/malformed.swift"],"compile":["swiftc","{root}/swift/malformed.swift","-o","{root}/swift-malformed"],"expect_success":false,"diagnostic":"cannot convert value of type|cannot convert value","run":[],"cases":["malformed_call"],"artifacts":[]}),
        json!({"id":"csharp","language":"csharp","sources":["csharp/bindings.cs","csharp/consumer.cs","csharp/consumer.csproj"],"compile":["dotnet","build","{root}/csharp/consumer.csproj","--configuration","Release","--output","{root}/csharp/out","--nologo","-p:RestoreSources={root}/offline-nuget","-p:NuGetAudit=false"],"expect_success":true,"run":["dotnet","{root}/csharp/out/consumer.dll"],"cases":cases,"artifacts":["csharp/out/consumer.dll","csharp/out/consumer.deps.json","csharp/out/consumer.runtimeconfig.json"]}),
        json!({"id":"csharp_malformed","language":"csharp","sources":["csharp/bindings.cs","csharp/malformed.cs","csharp/malformed.csproj"],"compile":["dotnet","build","{root}/csharp/malformed.csproj","--configuration","Release","--output","{root}/csharp/negative","--nologo","-p:RestoreSources={root}/offline-nuget","-p:NuGetAudit=false"],"expect_success":false,"diagnostic":"error CS0122|error CS1503|error CS1729","run":[],"cases":["malformed_call"],"artifacts":[]}),
        json!({"id":"rust_nested_option","language":"rust","sources":["nested.rs"],"compile":["rustc","+1.91.1","--edition=2021","{root}/nested.rs","-o","{root}/nested"],"expect_success":true,"run":["{root}/nested"],"cases":["roundtrip_nested_option"],"artifacts":["nested"]}),
        json!({"id":"typescript_nested_option","language":"typescript","sources":["nested.ts"],"compile":["{repo}/programs/sdk/rust/node_modules/.bin/tsc","--strict","--target","ES2020","--module","commonjs","--outDir","{root}/ts-nested","{root}/nested.ts"],"expect_success":true,"run":["node","{root}/ts-nested/nested.js"],"cases":["roundtrip_nested_option"],"artifacts":["ts-nested/nested.js"]}),
        json!({"id":"rust_forged","language":"rust","sources":["forged.rs"],"compile":["rustc","+1.91.1","--edition=2021","{root}/forged.rs","-o","{root}/forged"],"expect_success":false,"diagnostic":"private","run":[],"cases":["forged_call"],"artifacts":[]}),
    ];
    write_consumer(root, "consumer-plan.json", serde_json::to_vec_pretty(&json!({"schema":1,"consumers":rows})).unwrap_or_else(|e| panic!("serialize consumers: {e}")));
}

fn read_json(path: &Path) -> Value {
    serde_json::from_slice(&fs::read(path).unwrap_or_else(|e| panic!("read {}: {e}",path.display()))).unwrap_or_else(|e| panic!("parse {}: {e}",path.display()))
}

#[test]
fn generated_client_guest_and_typescript_are_compiler_inputs() {
    let root = std::path::PathBuf::from(std::env::var_os("PAXEER_X_BINDINGS_ARTIFACTS").unwrap_or_else(|| panic!("prebuilt bindings artifacts are required; run the explicit producer first")));
    let plan = read_json(&root.join("consumer-plan.json"));
    let recorded = read_json(&root.join("compile-results.json"));
    assert_eq!(plan["schema"],1);
    assert_eq!(recorded["schema"],1);
    let planned = plan["consumers"].as_array().unwrap_or_else(|| panic!("missing consumer plan"));
    let results = recorded["consumers"].as_array().unwrap_or_else(|| panic!("missing compiler results"));
    assert_eq!(planned.len(), results.len());
    assert_eq!(planned.len(),22);
    for expected in planned {
        let matches: Vec<_> = results.iter().filter(|r| r["id"] == expected["id"]).collect();
        assert_eq!(matches.len(),1,"missing or duplicated compiler result {}",expected["id"]);
        let result = matches[0];
        for key in ["language","sources","compile","expect_success","cases","artifacts"] {assert_eq!(result[key],expected[key],"compiler plan changed: {} {key}",expected["id"]);}
        let code = result["exit_code"].as_i64().unwrap_or_else(|| panic!("compiler result has no exit code"));
        assert_eq!(code == 0,expected["expect_success"].as_bool().unwrap_or(false),"compiler outcome {}",expected["id"]);
        let log = &result["log"];
        verify_artifact_hash(&root,log);
        for key in ["source_artifacts","built_artifacts"] {
            for artifact in result[key].as_array().unwrap_or_else(|| panic!("missing {key}")) {verify_artifact_hash(&root,artifact);}
        }
    }
}

fn verify_artifact_hash(root: &Path, artifact: &Value) {
    let relative = artifact["path"].as_str().unwrap_or_else(|| panic!("missing artifact path"));
    let path = Path::new(relative);
    let path = if path.is_absolute() {path.to_path_buf()} else {root.join(path)};
    let actual = path.canonicalize().unwrap_or_else(|e| panic!("resolve artifact {relative}: {e}"));
    let artifact_root = root.parent().unwrap_or(root).canonicalize().unwrap_or_else(|e| panic!("resolve artifact root: {e}"));
    assert!(actual.starts_with(artifact_root),"artifact escapes producer root");
    let bytes = fs::read(actual).unwrap_or_else(|e| panic!("read artifact {relative}: {e}"));
    assert_eq!(artifact["bytes"].as_u64(),Some(bytes.len() as u64));
    let hash: String = Sha256::digest(&bytes).iter().map(|b| format!("{b:02x}")).collect();
    assert_eq!(artifact["sha256"].as_str(),Some(hash.as_str()),"artifact changed: {relative}");
}

fn versioned_interface(abi: u16, version: u8, capability: Option<&[u8]>) -> Vec<u8> {
    let mut bytes = format!("LayerX/program-interface/v{version}\0").into_bytes();
    bytes.extend_from_slice(&CODE_HASH);
    bytes.extend_from_slice(&abi.to_be_bytes());
    bytes.extend_from_slice(&1_u16.to_be_bytes());
    push_text(&mut bytes,"call");
    bytes.extend_from_slice(&[1,2,3,4]);
    bytes.extend_from_slice(&layerx(0x10));
    bytes.extend_from_slice(&layerx(0x10));
    bytes.extend_from_slice(&(if capability.is_some() {1_u16} else {0_u16}).to_be_bytes());
    if let Some(value) = capability {bytes.extend_from_slice(value);}
    bytes.extend_from_slice(&0_u16.to_be_bytes());
    bytes.extend_from_slice(&0_u16.to_be_bytes());
    bytes
}

fn dynamic_spend(asset: [u8;32], maximum: u128, recipient: u32, amount: u32) -> Vec<u8> {
    let mut cap = vec![10];
    cap.extend_from_slice(&asset);
    cap.extend_from_slice(&maximum.to_be_bytes());
    cap.extend_from_slice(&recipient.to_be_bytes());
    cap.extend_from_slice(&amount.to_be_bytes());
    cap
}

#[test]
fn canonical_domain_abi_and_dynamic_capability_matrix_is_exact() {
    let dynamic = dynamic_spend([1;32],1,0,32);
    for abi in 1_u16..=4 {
        for version in 1_u8..=4 {
            for capability in [None,Some(dynamic.as_slice())] {
                let expected_version = match abi {1=>1,2=>if capability.is_some(){2}else{1},3=>3,4=>4,_=>unreachable!()};
                let expected = version == expected_version && (abi != 1 || capability.is_none());
                assert_eq!(BindingGenerator::from_interface(&versioned_interface(abi,version,capability)).is_ok(),expected,"ABI {abi}, domain {version}, dynamic {}",capability.is_some());
            }
        }
    }
    for bad in [dynamic_spend([0;32],1,0,32),dynamic_spend([1;32],0,0,32),dynamic_spend([1;32],1,u32::MAX,32),dynamic_spend([1;32],1,0,u32::MAX)] {
        assert!(BindingGenerator::from_interface(&versioned_interface(2,2,Some(&bad))).is_err());
    }
    for abi in 1_u16..=4 {
        let version = match abi {1|2=>1,3=>3,4=>4,_=>unreachable!()};
        assert_eq!(BindingGenerator::from_interface(&versioned_interface(abi,version,Some(&[11]))).is_ok(),abi>=3);
        assert_eq!(BindingGenerator::from_interface(&versioned_interface(abi,version,Some(&[12]))).is_ok(),abi==4);
    }
    assert!(BindingGenerator::from_interface(&versioned_interface(0,1,None)).is_err());
    assert!(BindingGenerator::from_interface(&versioned_interface(5,4,None)).is_err());
    let valid = versioned_interface(4,4,None);
    for end in 0..valid.len() {assert!(BindingGenerator::from_interface(&valid[..end]).is_err());}
    let mut trailing = valid;
    trailing.push(0);
    assert!(BindingGenerator::from_interface(&trailing).is_err());
}

#[test]
fn immutable_published_interfaces_generate_all_language_families() {
    let root = std::path::PathBuf::from(std::env::var_os("PAXEER_X_BINDINGS_FIXTURES").unwrap_or_else(|| panic!("immutable published fixtures are required")));
    for name in ["abi1","abi2","abi2-dynamic","abi3","abi3-dynamic","abi4","abi4-dynamic"] {
        let bytes = fs::read(root.join(name).join("interface.bin")).unwrap_or_else(|e| panic!("read {name} interface: {e}"));
        let generator = BindingGenerator::from_interface(&bytes).unwrap_or_else(|e| panic!("published {name} refused: {e}"));
        let digest: [u8;32] = Sha256::digest(&bytes).into();
        assert_eq!(generator.interface_digest(),digest);
        assert_eq!(generator.require_digest(digest),Ok(()));
        let mut stale = digest; stale[0] ^= 1;
        assert!(matches!(generator.require_digest(stale),Err(BindgenError::StaleBinding{..})));
        let mut wrong = generator.code_hash(); wrong[0] ^= 1;
        assert!(matches!(generator.require_code_hash(wrong),Err(BindgenError::CodeHashMismatch{..})));
        let generated = generator.generate_all();
        assert_eq!(generated.interface_digest,digest);
        for source in [&generated.rust,&generated.typescript,&generated.guest,&generated.go,&generated.java,&generated.kotlin,&generated.python,&generated.swift,&generated.csharp] {assert!(!source.is_empty(),"{name} emitted empty source");}
    }
}
