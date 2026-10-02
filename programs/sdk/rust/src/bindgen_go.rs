use alloc::format;
use alloc::string::String;
use core::fmt::Write;

use crate::bindgen::{BindingGenerator, Entry, Type};

impl BindingGenerator {
    #[must_use]
    pub fn generate_go(&self) -> String {
        let mut out = String::from(GO_PRELUDE);
        let _ = writeln!(out, "func InterfaceDigest() [32]byte {{ return [32]byte{{{}}} }}", go_bytes(&self.digest));
        let _ = writeln!(out, "func CodeHash() [32]byte {{ return [32]byte{{{}}} }}", go_bytes(&self.code_hash));
        for entry in &self.entries {
            go_entry(&mut out, entry);
        }
        out
    }
}

fn go_bytes(bytes: &[u8]) -> String {
    let mut out = String::new();
    for byte in bytes {
        let _ = write!(out, "{byte},");
    }
    out
}

fn go_name(entry: &Entry) -> String {
    let mut name = format!("Lx{}D", entry.name);
    for byte in entry.discriminator {
        let _ = write!(name, "{byte:02x}");
    }
    name
}

fn go_convention(ty: &Type) -> u8 {
    if matches!(ty, Type::EvmHead) { 2 } else { 1 }
}

fn go_integer(ty: &Type) -> Option<(&'static str, u8, u8)> {
    Some(match ty {
        Type::U8 => ("uint8", 0x10, 1),
        Type::U16 => ("uint16", 0x11, 2),
        Type::U32 => ("uint32", 0x12, 4),
        Type::U64 => ("uint64", 0x13, 8),
        Type::I8 => ("int8", 0x18, 1),
        Type::I16 => ("int16", 0x19, 2),
        Type::I32 => ("int32", 0x1a, 4),
        Type::I64 => ("int64", 0x1b, 8),
        _ => return None,
    })
}

fn go_type(out: &mut String, ty: &Type, name: &str) {
    if let Some((primitive, tag, width)) = go_integer(ty) {
        let _ = writeln!(out, "type {name} {primitive}\nfunc encode{name}(w *writer, v {name}) error {{ return w.number({tag},{width},uint64(v)) }}\nfunc read{name}(r *reader) ({name},error) {{ n,e:=r.number({tag},{width});return {name}(n),e }}");
        return;
    }
    match ty {
        Type::U128 | Type::U256 | Type::I128 => {
            let (width, tag) = match ty { Type::U256 => (32, 0x15), Type::I128 => (16, 0x1c), _ => (16, 0x14) };
            let _ = writeln!(out, "type {name} [{width}]byte\nfunc encode{name}(w *writer,v {name}) error {{ if e:=w.byte({tag});e!=nil {{return e}};return w.raw(v[:]) }}\nfunc read{name}(r *reader) ({name},error) {{ var v {name};if e:=r.tag({tag});e!=nil {{return v,e}};b,e:=r.take({width});if e!=nil {{return v,e}};copy(v[:],b);return v,nil }}");
        }
        Type::Bytes(_) | Type::EvmHead => {
            let max = match ty { Type::Bytes(max) => *max, _ => 1_048_575 };
            let condition = if matches!(ty, Type::EvmHead) { " || len(v)%32!=0" } else { "" };
            let _ = writeln!(out, "type {name} struct {{ value []byte }}\nfunc New{name}(v []byte) ({name},error) {{ if len(v)>MaxCalldataBytes || uint64(len(v))>{max}{condition} {{return {name}{{}},ErrInvalidValue}};x:={name}{{value:append([]byte(nil),v...)}};if e:=encode{name}(&writer{{}},x);e!=nil {{return {name}{{}},e}};return x,nil }}\nfunc (v {name}) Value() []byte {{return append([]byte(nil),v.value...)}}\nfunc encode{name}(w *writer,v {name}) error {{ if uint64(len(v.value))>{max}{} {{return ErrInvalidValue}};", if matches!(ty, Type::EvmHead) { " || len(v.value)%32!=0" } else { "" });
            if !matches!(ty, Type::EvmHead) {
                out.push_str("if e:=w.byte(0x20);e!=nil {return e};if e:=w.count(uint32(len(v.value)));e!=nil {return e};\n");
            }
            let _ = writeln!(out, "return w.raw(v.value) }}\nfunc read{name}(r *reader) ({name},error) {{");
            if matches!(ty, Type::EvmHead) {
                out.push_str("n:=len(r.data)-r.at;if n%32!=0 {return ");
                let _ = writeln!(out, "{name}{{}},ErrNonCanonical}};");
            } else {
                let _ = writeln!(out, "if e:=r.tag(0x20);e!=nil {{return {name}{{}},e}};count,e:=r.count();if e!=nil {{return {name}{{}},e}};if count>{max} || uint64(count)>uint64(len(r.data)-r.at) {{return {name}{{}},ErrInvalidValue}};n:=int(count);");
            }
            let _ = writeln!(out, "if e:=r.reserve(uint64(n),1);e!=nil {{return {name}{{}},e}};b,e:=r.take(n);if e!=nil {{return {name}{{}},e}};return {name}{{value:append([]byte(nil),b...)}},nil }}");
        }
        Type::Fixed(child, max) | Type::Variable(child, max) => {
            let child_name = format!("{name}Element");
            go_type(out, child, &child_name);
            let compare = if matches!(ty, Type::Fixed(..)) { "!=" } else { ">" };
            let tag = if matches!(ty, Type::Fixed(..)) { 0x30 } else { 0x31 };
            let _ = writeln!(out, "type {name} struct {{ value []{child_name} }}\nfunc New{name}(v []{child_name}) ({name},error) {{if uint64(len(v)){compare}{max} || uint64(len(v))>DecodedSizeLimit/uint64(reflect.TypeOf((*{child_name})(nil)).Elem().Size()) {{return {name}{{}},ErrInvalidValue}};x:={name}{{value:append([]{child_name}(nil),v...)}};if e:=encode{name}(&writer{{}},x);e!=nil {{return {name}{{}},e}};return x,nil}}\nfunc (v {name}) Value() []{child_name} {{return append([]{child_name}(nil),v.value...)}}\nfunc encode{name}(w *writer,v {name}) error {{if uint64(len(v.value)){compare}{max} {{return ErrInvalidValue}};if e:=w.byte({tag});e!=nil {{return e}};if e:=w.count(uint32(len(v.value)));e!=nil {{return e}};for _,item:=range v.value {{if e:=encode{child_name}(w,item);e!=nil {{return e}}}};return nil}}\nfunc read{name}(r *reader) ({name},error) {{if e:=r.tag({tag});e!=nil {{return {name}{{}},e}};n,e:=r.count();if e!=nil {{return {name}{{}},e}};if n{compare}{max} || uint64(n)>uint64(len(r.data)-r.at) {{return {name}{{}},ErrInvalidValue}};if e:=r.reserve(uint64(n),uint64(reflect.TypeOf((*{child_name})(nil)).Elem().Size()));e!=nil {{return {name}{{}},e}};v:=make([]{child_name},int(n));for i:=range v {{item,e:=read{child_name}(r);if e!=nil {{return {name}{{}},e}};v[i]=item}};return {name}{{value:v}},nil}}");
        }
        Type::Option(child) => {
            let child_name = format!("{name}SomeValue");
            go_type(out, child, &child_name);
            let _ = writeln!(out, "type {name} struct {{present bool;value {child_name}}}\nfunc None{name}() {name} {{return {name}{{}}}}\nfunc Some{name}(v {child_name}) ({name},error) {{x:={name}{{present:true,value:v}};if e:=encode{name}(&writer{{}},x);e!=nil {{return {name}{{}},e}};return x,nil}}\nfunc(v {name}) Value() ({child_name},bool) {{return v.value,v.present}}\nfunc encode{name}(w *writer,v {name}) error {{if e:=w.byte(0x40);e!=nil {{return e}};if !v.present {{return w.byte(0)}};if e:=w.byte(1);e!=nil {{return e}};return encode{child_name}(w,v.value)}}\nfunc read{name}(r *reader) ({name},error) {{if e:=r.tag(0x40);e!=nil {{return {name}{{}},e}};present,e:=r.byte();if e!=nil {{return {name}{{}},e}};if present==0 {{return {name}{{}},nil}};if present!=1 {{return {name}{{}},ErrNonCanonical}};v,e:=read{child_name}(r);if e!=nil {{return {name}{{}},e}};return {name}{{present:true,value:v}},nil}}");
        }
        Type::Union(variants) => {
            for variant in variants {
                go_type(out, &variant.value, &format!("{name}Variant{}Value", variant.tag));
            }
            let _ = write!(out, "type {name} struct {{valid bool;tag uint32;");
            for variant in variants {
                let _ = write!(out, "v{} {name}Variant{}Value;", variant.tag, variant.tag);
            }
            out.push_str("}\n");
            for variant in variants {
                let tag = variant.tag;
                let _ = writeln!(out, "func New{name}Variant{tag}(v {name}Variant{tag}Value) ({name},error) {{x:={name}{{valid:true,tag:{tag},v{tag}:v}};if e:=encode{name}(&writer{{}},x);e!=nil {{return {name}{{}},e}};return x,nil}}\nfunc(v {name}) AsVariant{tag}() ({name}Variant{tag}Value,bool) {{return v.v{tag},v.valid&&v.tag=={tag}}}");
            }
            let _ = writeln!(out, "func encode{name}(w *writer,v {name}) error {{if !v.valid {{return ErrInvalidValue}};if e:=w.byte(0x50);e!=nil {{return e}};if e:=w.count(v.tag);e!=nil {{return e}};switch v.tag {{");
            for variant in variants {
                let tag = variant.tag;
                let _ = writeln!(out, "case {tag}:return encode{name}Variant{tag}Value(w,v.v{tag})");
            }
            let _ = writeln!(out, "default:return ErrNonCanonical}} }}\nfunc read{name}(r *reader) ({name},error) {{if e:=r.tag(0x50);e!=nil {{return {name}{{}},e}};tag,e:=r.count();if e!=nil {{return {name}{{}},e}};switch tag {{");
            for variant in variants {
                let tag = variant.tag;
                let _ = writeln!(out, "case {tag}:v,e:=read{name}Variant{tag}Value(r);if e!=nil {{return {name}{{}},e}};return {name}{{valid:true,tag:{tag},v{tag}:v}},nil");
            }
            let _ = writeln!(out, "default:return {name}{{}},ErrNonCanonical}} }}");
        }
        Type::U8 | Type::U16 | Type::U32 | Type::U64 | Type::I8 | Type::I16 | Type::I32 | Type::I64 => unreachable!(),
    }
}

fn go_entry(out: &mut String, entry: &Entry) {
    let name = go_name(entry);
    let input = format!("{name}Input");
    let output = format!("{name}Output");
    go_type(out, &entry.input, &input);
    go_type(out, &entry.output, &output);
    let _ = writeln!(out, "type {name}Failure interface {{error;Code() uint32;Name() string;sealed{name}Failure()}}");
    for failure in &entry.failures {
        let fail_name = format!("{name}FailureCode{}", failure.code);
        go_type(out, &failure.detail, &format!("{fail_name}Detail"));
        let _ = writeln!(out, "type {fail_name} struct {{Detail {fail_name}Detail}}\nfunc(v {fail_name}) Error() string {{return \"{}\"}}\nfunc(v {fail_name}) Name() string {{return \"{}\"}}\nfunc(v {fail_name}) Code() uint32 {{return {}}}\nfunc(v {fail_name}) sealed{name}Failure() {{}}", failure.name, failure.name, failure.code);
    }
    let convention = go_convention(&entry.input);
    let _ = writeln!(out, "func Encode{name}(input {input},deployedCodeHash,publishedDigest [32]byte) (*call[{output},{name}Failure],error) {{if e:=checkTarget(deployedCodeHash,publishedDigest);e!=nil {{return nil,e}};w:=writer{{}};if e:=w.byte({convention});e!=nil {{return nil,e}};if e:=encode{input}(&w,input);e!=nil {{return nil,e}};b:=append([]byte{{{}}},w.data...);return &call[{output},{name}Failure]{{data:b,output:Decode{name}Output,failure:Decode{name}Failure}},nil}}", go_bytes(&entry.discriminator));
    let _ = writeln!(out, "func Decode{name}Output(data []byte) ({output},error) {{var zero {output};r,e:=messageReader(data,{});if e!=nil {{return zero,e}};v,e:=read{output}(r);if e!=nil {{return zero,e}};if e:=r.done();e!=nil {{return zero,e}};return v,nil}}",go_convention(&entry.output));
    let _ = writeln!(out, "func Decode{name}Failure(code uint32,data []byte) ({name}Failure,error) {{switch code {{");
    for failure in &entry.failures {
        let fail_name = format!("{name}FailureCode{}", failure.code);
        let _ = writeln!(out,"case {}:r,e:=messageReader(data,{});if e!=nil {{return nil,e}};v,e:=read{fail_name}Detail(r);if e!=nil {{return nil,e}};if e:=r.done();e!=nil {{return nil,e}};return {fail_name}{{Detail:v}},nil",failure.code,go_convention(&failure.detail));
    }
    out.push_str("default:return nil,ErrUnknownFailure}}\n");
}

const GO_PRELUDE: &str = r#"package bindings

import (
    "encoding/binary"
    "reflect"
)

const MaxCalldataBytes = 1048576
const DecodedSizeLimit = 16777216

type BindingRefusal string
func(e BindingRefusal) Error() string {return string(e)}
const (
    ErrCodeHashMismatch BindingRefusal = "CODE_HASH_MISMATCH"
    ErrStaleInterface BindingRefusal = "STALE_INTERFACE"
    ErrInvalidValue BindingRefusal = "INVALID_VALUE"
    ErrNonCanonical BindingRefusal = "NON_CANONICAL"
    ErrTruncated BindingRefusal = "TRUNCATED"
    ErrTrailingBytes BindingRefusal = "TRAILING_BYTES"
    ErrUnknownFailure BindingRefusal = "UNKNOWN_FAILURE"
)

func checkTarget(code,digest [32]byte) error {
    if code!=CodeHash() {return ErrCodeHashMismatch}
    if digest!=InterfaceDigest() {return ErrStaleInterface}
    return nil
}

type call[Output any,Failure error] struct {
    data []byte
    output func([]byte)(Output,error)
    failure func(uint32,[]byte)(Failure,error)
}
func(c *call[Output,Failure]) Bytes() ([]byte,error) {
    if c==nil || len(c.data)<5 || c.output==nil || c.failure==nil {return nil,ErrInvalidValue}
    return append([]byte(nil),c.data...),nil
}
func(c *call[Output,Failure]) DecodeOutput(data []byte) (Output,error) {
    if c==nil || c.output==nil {var zero Output;return zero,ErrInvalidValue}
    return c.output(data)
}
func(c *call[Output,Failure]) DecodeFailure(code uint32,data []byte) (Failure,error) {
    if c==nil || c.failure==nil {var zero Failure;return zero,ErrInvalidValue}
    return c.failure(code,data)
}

type writer struct {data []byte}
func(w *writer) raw(data []byte) error {
    if len(data)>MaxCalldataBytes-len(w.data) {return ErrInvalidValue}
    w.data=append(w.data,data...);return nil
}
func(w *writer) byte(v byte) error {return w.raw([]byte{v})}
func(w *writer) count(v uint32) error {var b [4]byte;binary.BigEndian.PutUint32(b[:],v);return w.raw(b[:])}
func(w *writer) number(tag byte,width int,v uint64) error {
    if e:=w.byte(tag);e!=nil {return e}
    var b [8]byte;binary.BigEndian.PutUint64(b[:],v);return w.raw(b[8-width:])
}

type reader struct {data []byte;at int;decoded uint64}
func(r *reader) reserve(count,size uint64) error {
    if size!=0 && count>(DecodedSizeLimit-r.decoded)/size {return ErrInvalidValue}
    r.decoded+=count*size;return nil
}
func(r *reader) take(n int) ([]byte,error) {
    if n<0 || n>len(r.data)-r.at {return nil,ErrTruncated}
    if e:=r.reserve(uint64(n),1);e!=nil {return nil,e}
    b:=r.data[r.at:r.at+n];r.at+=n;return b,nil
}
func(r *reader) byte() (byte,error) {b,e:=r.take(1);if e!=nil {return 0,e};return b[0],nil}
func(r *reader) tag(tag byte) error {v,e:=r.byte();if e!=nil {return e};if v!=tag {return ErrNonCanonical};return nil}
func(r *reader) count() (uint32,error) {b,e:=r.take(4);if e!=nil {return 0,e};return binary.BigEndian.Uint32(b),nil}
func(r *reader) number(tag byte,width int) (uint64,error) {
    if e:=r.tag(tag);e!=nil {return 0,e};b,e:=r.take(width);if e!=nil {return 0,e}
    var value uint64;for _,v:=range b {value=(value<<8)|uint64(v)};return value,nil
}
func(r *reader) done() error {if r.at!=len(r.data) {return ErrTrailingBytes};return nil}
func messageReader(data []byte,convention byte) (*reader,error) {
    if len(data)==0 {return nil,ErrTruncated};if len(data)>MaxCalldataBytes {return nil,ErrInvalidValue}
    if data[0]!=convention {return nil,ErrNonCanonical};return &reader{data:data[1:]},nil
}
var _ = reflect.TypeOf

"#;

impl BindingGenerator {
    #[must_use]
    pub fn generate_go_consumer(&self) -> String {
        let mut out = String::from("package main\nimport(\"bytes\";\"fmt\";\"reflect\";b \"conformance/bindings\")\nfunc require(ok bool,message string){if !ok {panic(message)}}\nfunc main(){\n");
        let mut typed_failures = false;
        let mut malformed_calls = false;
        for entry in &self.entries {
            let name = go_name(entry);
            out.push_str("{\n");
            let input = go_sample(&mut out, &entry.input, &format!("{name}Input"), "input", None);
            let _ = writeln!(out, "call,e:=b.Encode{name}(input,b.CodeHash(),b.InterfaceDigest());require(e==nil,\"valid call\");encoded,e:=call.Bytes();require(e==nil,\"call bytes\");require(bytes.Equal(encoded,[]byte{{{}{}{}}}),\"canonical input\");", go_bytes(&entry.discriminator), go_convention(&entry.input), go_tail(&input));
            out.push_str("encoded[0]^=255;again,e:=call.Bytes();require(e==nil && encoded[0]!=again[0],\"immutable call bytes\");\n");
            let output_bytes = go_sample(&mut out, &entry.output, &format!("{name}Output"), "expected", None);
            let _ = writeln!(out, "outputBytes:=[]byte{{{}{}}};output,e:=call.DecodeOutput(outputBytes);require(e==nil && reflect.DeepEqual(output,expected),\"typed output\");", go_convention(&entry.output), go_tail(&output_bytes));
            out.push_str("_,e=call.DecodeOutput(append(append([]byte(nil),outputBytes...),0));require(e!=nil,\"trailing output\");badConvention:=append([]byte(nil),outputBytes...);badConvention[0]^=255;_,e=call.DecodeOutput(badConvention);require(e!=nil,\"wrong convention\");\n");
            if matches!(entry.output, Type::EvmHead) {
                out.push_str("if len(outputBytes)>1 {_,e=call.DecodeOutput(outputBytes[:len(outputBytes)-1]);require(e!=nil,\"truncated EVM head\")}\n");
            } else {
                out.push_str("for end:=0;end<len(outputBytes);end++ {_,e=call.DecodeOutput(outputBytes[:end]);require(e!=nil,\"truncated output\")};badTag:=append([]byte(nil),outputBytes...);badTag[1]^=255;_,e=call.DecodeOutput(badTag);require(e!=nil,\"wrong type tag\");\n");
            }
            let _ = writeln!(out, "badHash:=b.CodeHash();badHash[0]^=1;_,e=b.Encode{name}(input,badHash,b.InterfaceDigest());require(e==b.ErrCodeHashMismatch,\"wrong deployed hash\");badDigest:=b.InterfaceDigest();badDigest[0]^=1;_,e=b.Encode{name}(input,b.CodeHash(),badDigest);require(e==b.ErrStaleInterface,\"stale digest\");");
            for failure in &entry.failures {
                typed_failures = true;
                let failure_name = format!("{name}FailureCode{}", failure.code);
                out.push_str("{\n");
                let detail = go_sample(&mut out, &failure.detail, &format!("{failure_name}Detail"), "detail", None);
                let _ = writeln!(out,"failureBytes:=[]byte{{{}{}}};failure,e:=call.DecodeFailure({},failureBytes);require(e==nil && failure.Code()=={} && failure.Name()==\"{}\",\"typed failure metadata\");typed,ok:=failure.(b.{failure_name});require(ok && reflect.DeepEqual(typed.Detail,detail),\"typed failure detail\");_,e=call.DecodeFailure({},append(append([]byte(nil),failureBytes...),0));require(e!=nil,\"trailing failure detail\");",go_convention(&failure.detail),go_tail(&detail),failure.code,failure.code,failure.name,failure.code);
                out.push_str("}\n");
            }
            let mut unknown = 0_u32;
            while entry.failures.iter().any(|failure| failure.code == unknown) { unknown += 1; }
            let _ = writeln!(out,"_,e=call.DecodeFailure({unknown},nil);require(e==b.ErrUnknownFailure,\"unknown failure\");");
            malformed_calls |= go_invalid_input(&mut out, entry, &name);
            if let Type::Option(_) = &entry.input {
                let _ = writeln!(out,"none:=b.None{name}Input();noneCall,e:=b.Encode{name}(none,b.CodeHash(),b.InterfaceDigest());require(e==nil,\"none call\");noneBytes,e:=noneCall.Bytes();require(e==nil && bytes.Equal(noneBytes,[]byte{{{}1,64,0}}),\"canonical none\");",go_bytes(&entry.discriminator));
            }
            if let Type::Union(variants) = &entry.input {
                for variant in variants {
                    out.push_str("{\n");
                    let bytes = go_sample(&mut out,&variant.value,&format!("{name}InputVariant{}Value",variant.tag),"variant",Some(10));
                    let mut golden = alloc::vec![1,0x50];
                    golden.extend_from_slice(&variant.tag.to_be_bytes());
                    golden.extend_from_slice(&bytes);
                    let _ = writeln!(out,"value,e:=b.New{name}InputVariant{}(variant);require(e==nil,\"union constructor\");unionCall,e:=b.Encode{name}(value,b.CodeHash(),b.InterfaceDigest());require(e==nil,\"union call\");unionBytes,e:=unionCall.Bytes();require(e==nil && bytes.Equal(unionBytes,[]byte{{{}{}}}),\"union canonical variant\");",variant.tag,go_bytes(&entry.discriminator),go_bytes(&golden));
                    out.push_str("}\n");
                }
            }
            let _ = writeln!(out,"fmt.Println(\"BINDING_CASE roundtrip_{}\")\n}}",entry.name);
        }
        if !self.entries.is_empty() {
            out.push_str("fmt.Println(\"BINDING_CASE stale_digest\");fmt.Println(\"BINDING_CASE wrong_code_hash\");\n");
        }
        if typed_failures { out.push_str("fmt.Println(\"BINDING_CASE typed_failure\");\n"); }
        if malformed_calls { out.push_str("fmt.Println(\"BINDING_CASE malformed_call\");\n"); }
        out.push_str("}\nvar _ = bytes.Equal\nvar _ = reflect.DeepEqual\nvar _ = b.CodeHash\n");
        out
    }

    #[must_use]
    pub fn generate_go_malformed_consumer(&self) -> String {
        let mut out = String::from("package main\nimport b \"conformance/bindings\"\nfunc main(){\n");
        if let Some(entry) = self.entries.first() {
            let _ = writeln!(out,"_,_=b.Encode{}(\"malformed raw bytes\",b.CodeHash(),b.InterfaceDigest())",go_name(entry));
        } else {
            out.push_str("_ = b.call[uint8,error]{}\n");
        }
        out.push_str("}\n");
        out
    }
}

fn go_tail(bytes: &[u8]) -> String {
    if bytes.is_empty() { String::new() } else { format!(",{}",go_bytes(bytes)) }
}

fn go_sample(out: &mut String, ty: &Type, name: &str, variable: &str, seed: Option<u64>) -> alloc::vec::Vec<u8> {
    let mut bytes = alloc::vec::Vec::new();
    if let Some((_,tag,width)) = go_integer(ty) {
        let signed = matches!(ty,Type::I8|Type::I16|Type::I32|Type::I64);
        let value = if signed { match ty { Type::I8=>-1_i64, Type::I16=>-2,Type::I32=>-3,_=>-4 } } else { i64::try_from(seed.unwrap_or(match ty {Type::U8=>127,Type::U16=>0x1234,Type::U32=>7,_=>9})).expect("conformance seed fits i64") };
        let _ = writeln!(out,"{variable}:=b.{name}({value})");
        bytes.push(tag);
        bytes.extend_from_slice(&(value as u64).to_be_bytes()[8-usize::from(width)..]);
        return bytes;
    }
    match ty {
        Type::U128|Type::U256|Type::I128 => {
            let (width,tag) = match ty {Type::U256=>(32,0x15),Type::I128=>(16,0x1c),_=>(16,0x14)};
            let mut data = alloc::vec![0; width];
            if matches!(ty,Type::I128) { data.fill(255);data[width-1]=251; } else {data[width-1]=if width==32 {13} else {11};}
            let _ = writeln!(out,"{variable}:=b.{name}{{{}}}",go_bytes(&data));
            bytes.push(tag);bytes.extend(data);
        }
        Type::Bytes(max) => {
            let count = (*max).min(3);
            let data: alloc::vec::Vec<u8> = (1..=count).map(|v|v as u8).collect();
            let _ = writeln!(out,"{variable},{variable}Err:=b.New{name}([]byte{{{}}});require({variable}Err==nil,\"byte constructor\");",go_bytes(&data));
            bytes.push(0x20);bytes.extend_from_slice(&count.to_be_bytes());bytes.extend(data);
        }
        Type::EvmHead => {
            let mut data = [0_u8;32];data[31]=1;
            let _ = writeln!(out,"{variable},{variable}Err:=b.New{name}([]byte{{{}}});require({variable}Err==nil,\"EVM constructor\");",go_bytes(&data));
            bytes.extend_from_slice(&data);
        }
        Type::Fixed(child,max)|Type::Variable(child,max) => {
            let count = if matches!(ty,Type::Fixed(..)) {*max} else {(*max).min(2)};
            if count>1024 {
                let _ = writeln!(out,"var {variable} b.{name};panic(\"conformance sample exceeds bounded vector size\");");
                return bytes;
            }
            bytes.push(if matches!(ty,Type::Fixed(..)) {0x30} else {0x31});bytes.extend_from_slice(&count.to_be_bytes());
            let mut names = String::new();
            for index in 0..count {
                let child_var = format!("{variable}Item{index}");
                bytes.extend(go_sample(out,child,&format!("{name}Element"),&child_var,Some(u64::from(index)+1)));
                let _ = write!(names,"{child_var},");
            }
            let _ = writeln!(out,"{variable},{variable}Err:=b.New{name}([]b.{name}Element{{{names}}});require({variable}Err==nil,\"array constructor\");");
        }
        Type::Option(child) => {
            let child_bytes = go_sample(out,child,&format!("{name}SomeValue"),&format!("{variable}Some"),Some(8));
            let _ = writeln!(out,"{variable},{variable}Err:=b.Some{name}({variable}Some);require({variable}Err==nil,\"option constructor\");");
            bytes.extend_from_slice(&[0x40,1]);bytes.extend(child_bytes);
        }
        Type::Union(variants) => {
            if let Some(variant) = variants.first() {
                let child_bytes = go_sample(out,&variant.value,&format!("{name}Variant{}Value",variant.tag),&format!("{variable}Variant"),Some(9));
                let _ = writeln!(out,"{variable},{variable}Err:=b.New{name}Variant{}({variable}Variant);require({variable}Err==nil,\"union constructor\");",variant.tag);
                bytes.push(0x50);bytes.extend_from_slice(&variant.tag.to_be_bytes());bytes.extend(child_bytes);
            }
        }
        Type::U8|Type::U16|Type::U32|Type::U64|Type::I8|Type::I16|Type::I32|Type::I64 => unreachable!(),
    }
    bytes
}

fn go_invalid_input(out: &mut String, entry: &Entry, name: &str) -> bool {
    match &entry.input {
        Type::Bytes(max) => {
            let count = u64::from(*max).min(1_048_576)+1;
            let _ = writeln!(out,"_,e=b.New{name}Input(make([]byte,{count}));require(e!=nil,\"bounded byte refusal\");");
        }
        Type::EvmHead => {
            let _ = writeln!(out,"_,e=b.New{name}Input([]byte{{1}});require(e!=nil,\"unaligned EVM refusal\");");
        }
        Type::Fixed(..)|Type::Union(..) => {
            let _ = writeln!(out,"var invalid b.{name}Input;_,e=b.Encode{name}(invalid,b.CodeHash(),b.InterfaceDigest());require(e!=nil,\"zero malformed value refusal\");");
        }
        Type::Variable(_,max) => {
            let count = u64::from(*max).min(1_048_576)+1;
            let _ = writeln!(out,"_,e=b.New{name}Input(make([]b.{name}InputElement,{count}));require(e!=nil,\"array bound refusal\");");
        }
        _ => return false,
    }
    true
}
