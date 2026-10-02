use alloc::format;
use alloc::string::String;
use core::fmt::Write;

use crate::bindgen::{BindingGenerator, Entry, Type};

impl BindingGenerator {
    #[must_use]
    pub fn generate_java(&self) -> String {
        let mut out = String::from(JAVA_RUNTIME);
        let _ = writeln!(out, "public static final String INTERFACE_DIGEST=\"{}\";\npublic static final String CODE_HASH=\"{}\";", hex(&self.digest), hex(&self.code_hash));
        for entry in &self.entries {
            java_entry(&mut out, entry);
        }
        out.push_str("}\n");
        out
    }

    #[must_use]
    pub fn generate_kotlin(&self) -> String {
        let mut out = String::from(KOTLIN_RUNTIME);
        let _ = writeln!(out, "const val INTERFACE_DIGEST: String = \"{}\"\nconst val CODE_HASH: String = \"{}\"", hex(&self.digest), hex(&self.code_hash));
        out.push_str("class Call<O,F> private constructor(bytes: ByteArray) {private val encoded=bytes.copyOf();val bytes: ByteArray get()=encoded.copyOf();companion object {\n");
        for entry in &self.entries {
            let suffix=hex(&entry.discriminator);
            let name=format!("Entry{suffix}");
            let disc=entry.discriminator.iter().map(|b|format!("{b}.toByte()")).collect::<alloc::vec::Vec<_>>().join(",");
            let _=writeln!(out,"internal fun entry{suffix}(input: {name}.Input,code: String,digest: String): Call<{name}.Output,{name}.Failure> {{checkTarget(code,digest);return Call(callBytes(byteArrayOf({disc}),{},input.canonicalBytes()))}}",convention(&entry.input));
        }
        out.push_str("}}\n");
        for entry in &self.entries {
            kotlin_entry(&mut out, entry);
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

fn integer(t: &Type) -> Option<(u8, u32, bool)> {
    Some(match t {
        Type::U8 => (0x10, 1, false),
        Type::U16 => (0x11, 2, false),
        Type::U32 => (0x12, 4, false),
        Type::U64 => (0x13, 8, false),
        Type::U128 => (0x14, 16, false),
        Type::U256 => (0x15, 32, false),
        Type::I8 => (0x18, 1, true),
        Type::I16 => (0x19, 2, true),
        Type::I32 => (0x1a, 4, true),
        Type::I64 => (0x1b, 8, true),
        Type::I128 => (0x1c, 16, true),
        _ => return None,
    })
}

fn convention(t: &Type) -> u8 {
    if matches!(t, Type::EvmHead) { 2 } else { 1 }
}

fn java_node(out: &mut String, t: &Type, name: &str) {
    if let Some((tag, width, signed)) = integer(t) {
        let _ = writeln!(out, "public static final class {name} {{ public final BigInteger value; private final byte[] wire; public {name}(BigInteger value) {{ this.value=integer(value,{width},{signed}); this.wire=number({tag},{width},this.value); }} public byte[] canonicalBytes(){{return wire.clone();}} private static {name} read(Reader r){{r.tag({tag});return new {name}(r.number({width},{signed}));}} }}");
        return;
    }
    match t {
        Type::Bytes(_) | Type::EvmHead => {
            let (check, enc, dec) = if matches!(t, Type::EvmHead) {
                (String::from("if(value.length%32!=0||value.length>MAX_BYTES-1)throw refusal(\"INVALID_VALUE\");"),
                 String::from("w.put(this.value);"),
                 String::from("return new NAME(r.take(r.remaining()));"))
            } else if let Type::Bytes(max) = t {
                (format!("if((long)value.length>{max}L||value.length>MAX_BYTES-6)throw refusal(\"INVALID_VALUE\");"),
                 String::from("w.b(32);w.u32(this.value.length);w.put(this.value);"),
                 format!("r.tag(32);long n=r.u32();if(n>{max}L)throw refusal(\"NON_CANONICAL\");return new NAME(r.take(r.length(n)));"))
            } else { unreachable!() };
            let dec = dec.replace("NAME", name);
            let _ = writeln!(out,"public static final class {name} {{ private final byte[] value; private final byte[] wire; public {name}(byte[] value){{nonNull(value);{check}this.value=value.clone();Writer w=new Writer();{enc}wire=w.bytes();}} public byte[] value(){{return value.clone();}} public byte[] canonicalBytes(){{return wire.clone();}} private static {name} read(Reader r){{{dec}}} }}");
        }
        Type::Fixed(child, n) | Type::Variable(child, n) => {
            let child_name = format!("{name}Element");
            java_node(out, child, &child_name);
            let (tag, cmp) = if matches!(t, Type::Fixed(_, _)) { (48, "!=") } else { (49, ">") };
            let _ = writeln!(out,"public static final class {name} {{ public final List<{child_name}> value; private final byte[] wire; public {name}(List<{child_name}> value){{nonNull(value);if((long)value.size(){cmp}{n}L)throw refusal(\"INVALID_VALUE\");ArrayList<{child_name}> copy=new ArrayList<>();Writer w=new Writer();w.b({tag});w.u32(value.size());for({child_name} item:value){{nonNull(item);copy.add(item);w.put(item.canonicalBytes());}}this.value=Collections.unmodifiableList(copy);wire=w.bytes();}} public byte[] canonicalBytes(){{return wire.clone();}} private static {name} read(Reader r){{r.tag({tag});long count=r.u32();if(count{cmp}{n}L||count>r.remaining())throw refusal(\"NON_CANONICAL\");r.charge(count*8L);int n=r.length(count);ArrayList<{child_name}> values=new ArrayList<>(n);for(int i=0;i<n;i++)values.add({child_name}.read(r));return new {name}(values);}} }}");
        }
        Type::Option(child) => {
            let child_name = format!("{name}Some");
            java_node(out, child, &child_name);
            let _ = writeln!(out,"public static final class {name} {{ private final {child_name} value; private final byte[] wire; private {name}({child_name} value){{this.value=value;Writer w=new Writer();w.b(64);w.b(value==null?0:1);if(value!=null)w.put(value.canonicalBytes());wire=w.bytes();}} public static {name} none(){{return new {name}(null);}} public static {name} some({child_name} value){{nonNull(value);return new {name}(value);}} public boolean isPresent(){{return value!=null;}} public {child_name} value(){{if(value==null)throw refusal(\"ABSENT_OPTION\");return value;}} public byte[] canonicalBytes(){{return wire.clone();}} private static {name} read(Reader r){{r.tag(64);int tag=r.byteValue();if(tag==0)return none();if(tag==1)return some({child_name}.read(r));throw refusal(\"NON_CANONICAL\");}} }}");
        }
        Type::Union(variants) => {
            for v in variants {
                java_node(out, &v.value, &format!("{name}Variant{}Value", v.tag));
            }
            let _ = writeln!(out, "public abstract static class {name} {{ private final byte[] wire; private {name}(long tag,byte[] value){{Writer w=new Writer();w.b(80);w.u32(tag);w.put(value);wire=w.bytes();}} public final byte[] canonicalBytes(){{return wire.clone();}} public abstract long tag(); private static {name} read(Reader r){{r.tag(80);long tag=r.u32();");
            for v in variants {
                let _ = writeln!(out, "if(tag=={}L)return new {name}Variant{}({name}Variant{}Value.read(r));",v.tag,v.tag,v.tag);
            }
            out.push_str("throw refusal(\"NON_CANONICAL\");}}\n");
            for v in variants {
                let tag = v.tag;
                let _ = writeln!(out, "public static final class {name}Variant{tag} extends {name}{{public final {name}Variant{tag}Value value;public {name}Variant{tag}({name}Variant{tag}Value value){{super({tag}L,nonNull(value).canonicalBytes());this.value=value;}}public long tag(){{return {tag}L;}}}}");
            }
        }
        _ => unreachable!(),
    }
}

fn java_entry(out: &mut String, entry: &Entry) {
    let name = format!("Entry{}", hex(&entry.discriminator));
    let _ = writeln!(out,"public static final class {name} {{ private {name}(){{}} public static final String NAME=\"{}\";",entry.name);
    java_node(out, &entry.input, "Input");
    java_node(out, &entry.output, "Output");
    out.push_str("public abstract static class Failure {private Failure(){}public abstract long code();public abstract String name();}\n");
    for failure in &entry.failures {
        let name = format!("FailureCode{}",failure.code);
        let detail = format!("{name}Detail");
        java_node(out, &failure.detail, &detail);
        let _ = writeln!(out,"public static final class {name} extends Failure {{public final {detail} detail;public {name}({detail} detail){{this.detail=nonNull(detail);}}public long code(){{return {}L;}}public String name(){{return \"{}\";}}}}",failure.code,failure.name);
    }
    let disc = entry.discriminator.iter().map(|b| format!("(byte){b}")).collect::<alloc::vec::Vec<_>>().join(",");
    let _ = writeln!(out,"public static Call<Output,Failure> call(Input input,String deployedCodeHash,String publishedDigest){{checkTarget(deployedCodeHash,publishedDigest);nonNull(input);return new Call<>(callBytes(new byte[]{{{disc}}},{},input.canonicalBytes()));}}",convention(&entry.input));
    let _ = writeln!(out,"public static Output decodeOutput(byte[] bytes){{Reader r=new Reader(bytes,{});Output value=Output.read(r);r.done();return value;}}",convention(&entry.output));
    out.push_str("public static Failure decodeFailure(long code,byte[] bytes){\n");
    for failure in &entry.failures {
        let _ = writeln!(out,"if(code=={}L){{Reader r=new Reader(bytes,{});Failure value=new FailureCode{}(FailureCode{}Detail.read(r));r.done();return value;}}",failure.code,convention(&failure.detail),failure.code,failure.code);
    }
    out.push_str("throw refusal(\"UNKNOWN_FAILURE\");}\n}\n");
}

fn kotlin_node(out: &mut String, t: &Type, name: &str) {
    if let Some((tag, width, signed)) = integer(t) {
        let _ = writeln!(out,"class {name}(value: BigInteger) {{ val value: BigInteger = integer(value,{width},{signed}); private val wire: ByteArray = number({tag},{width},this.value); fun canonicalBytes(): ByteArray = wire.copyOf(); companion object {{ internal fun read(r: Reader): {name} {{r.tag({tag});return {name}(r.number({width},{signed}))}} }} }}");
        return;
    }
    match t {
        Type::Bytes(_) | Type::EvmHead => {
            let (check, enc, dec) = if matches!(t, Type::EvmHead) {
                (String::from("if(value.size%32!=0||value.size>MAX_BYTES-1)throw refusal(\"INVALID_VALUE\")"),
                 String::from("w.put(storage)"),
                 String::from("return NAME(r.take(r.remaining()))"))
            } else if let Type::Bytes(max) = t {
                (format!("if(value.size.toLong()>{max}L||value.size>MAX_BYTES-6)throw refusal(\"INVALID_VALUE\")"),
                 String::from("w.b(32);w.u32(storage.size.toLong());w.put(storage)"),
                 format!("r.tag(32);val n=r.u32();if(n>{max}L)throw refusal(\"NON_CANONICAL\");return NAME(r.take(r.length(n)))"))
            } else { unreachable!() };
            let dec = dec.replace("NAME",name);
            let _ = writeln!(out,"class {name}(value: ByteArray) {{private val storage: ByteArray;private val wire: ByteArray;init {{{check};storage=value.copyOf();val w=Writer();{enc};wire=w.bytes()}} val value: ByteArray get()=storage.copyOf();fun canonicalBytes(): ByteArray=wire.copyOf();companion object {{internal fun read(r: Reader): {name} {{{dec}}}}} }}");
        }
        Type::Fixed(child,n) | Type::Variable(child,n) => {
            let child_name=format!("{name}Element");
            kotlin_node(out,child,&child_name);
            let (tag,cmp)=if matches!(t,Type::Fixed(_, _)){(48,"!=")}else{(49,">")};
            let _=writeln!(out,"class {name}(value: List<{child_name}>) {{val value: List<{child_name}>;private val wire: ByteArray;init {{if(value.size.toLong(){cmp}{n}L)throw refusal(\"INVALID_VALUE\");val copy=ArrayList<{child_name}>();val w=Writer();w.b({tag});w.u32(value.size.toLong());for(item in value){{copy.add(item);w.put(item.canonicalBytes())}};this.value=java.util.Collections.unmodifiableList(copy);wire=w.bytes()}} fun canonicalBytes(): ByteArray=wire.copyOf();companion object {{internal fun read(r: Reader): {name} {{r.tag({tag});val count=r.u32();if(count{cmp}{n}L||count>r.remaining().toLong())throw refusal(\"NON_CANONICAL\");r.charge(count*8L);val n=r.length(count);val values=ArrayList<{child_name}>(n);repeat(n){{values.add({child_name}.read(r))}};return {name}(values)}}}} }}");
        }
        Type::Option(child) => {
            let child_name=format!("{name}Some");
            kotlin_node(out,child,&child_name);
            let _=writeln!(out,"class {name} private constructor(private val stored: {child_name}?) {{private val wire: ByteArray;init {{val w=Writer();w.b(64);w.b(if(stored==null)0 else 1);if(stored!=null)w.put(stored.canonicalBytes());wire=w.bytes()}} val isPresent: Boolean get()=stored!=null;val value: {child_name} get()=stored?:throw refusal(\"ABSENT_OPTION\");fun canonicalBytes(): ByteArray=wire.copyOf();companion object {{fun none(): {name}={name}(null);fun some(value: {child_name}): {name}={name}(value);internal fun read(r: Reader): {name} {{r.tag(64);return when(r.byteValue()){{0->none();1->some({child_name}.read(r));else->throw refusal(\"NON_CANONICAL\")}}}}}} }}");
        }
        Type::Union(variants) => {
            for v in variants {
                kotlin_node(out,&v.value,&format!("{name}Variant{}Value",v.tag));
            }
            let _=writeln!(out,"sealed class {name} private constructor(val tag: Long,value: ByteArray) {{private val wire: ByteArray;init {{val w=Writer();w.b(80);w.u32(tag);w.put(value);wire=w.bytes()}} fun canonicalBytes(): ByteArray=wire.copyOf();companion object {{internal fun read(r: Reader): {name} {{r.tag(80);return when(r.u32()){{");
            for v in variants {
                let _=writeln!(out,"{}L->{name}Variant{}({name}Variant{}Value.read(r))",v.tag,v.tag,v.tag);
            }
            out.push_str("else->throw refusal(\"NON_CANONICAL\")}}}\n");
            for v in variants {
                let tag=v.tag;
                let _=writeln!(out,"class {name}Variant{tag}(val value: {name}Variant{tag}Value): {name}({tag}L,value.canonicalBytes())");
            }
            out.push_str("}\n");
        }
        _=>unreachable!(),
    }
}

fn kotlin_entry(out: &mut String, entry: &Entry) {
    let name=format!("Entry{}",hex(&entry.discriminator));
    let _=writeln!(out,"object {name} {{const val NAME: String=\"{}\"",entry.name);
    kotlin_node(out,&entry.input,"Input");
    kotlin_node(out,&entry.output,"Output");
    out.push_str("sealed class Failure {abstract val code: Long;abstract val name: String\n");
    for failure in &entry.failures {
        let name=format!("FailureCode{}",failure.code);
        let detail=format!("{name}Detail");
        kotlin_node(out,&failure.detail,&detail);
        let _=writeln!(out,"class {name}(val detail: {detail}): Failure() {{override val code: Long={}L;override val name: String=\"{}\"}}",failure.code,failure.name);
    }
    out.push_str("}\n");
    let suffix=hex(&entry.discriminator);
    let _=writeln!(out,"fun call(input: Input,deployedCodeHash: String,publishedDigest: String): Call<Output,Failure> = Call.entry{suffix}(input,deployedCodeHash,publishedDigest)");
    let _=writeln!(out,"fun decodeOutput(bytes: ByteArray): Output {{val r=Reader(bytes,{});val value=Output.read(r);r.done();return value}}",convention(&entry.output));
    out.push_str("fun decodeFailure(code: Long,bytes: ByteArray): Failure {return when(code){\n");
    for failure in &entry.failures {
        let _=writeln!(out,"{}L->{{val r=Reader(bytes,{});val value=Failure.FailureCode{}(Failure.FailureCode{}Detail.read(r));r.done();value}}",failure.code,convention(&failure.detail),failure.code,failure.code);
    }
    out.push_str("else->throw refusal(\"UNKNOWN_FAILURE\")}}\n}\n");
}

const JAVA_RUNTIME: &str = r#"import java.math.BigInteger;
import java.io.ByteArrayOutputStream;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import java.util.Locale;

public final class ProgramBindings {
private ProgramBindings(){}
private static final int MAX_BYTES=1048576;
private static final long MAX_DECODED=16777216L;
public static final class BindingRefusal extends IllegalArgumentException {
public final String code;
private BindingRefusal(String code){super(code);this.code=code;}
}
private static BindingRefusal refusal(String code){return new BindingRefusal(code);}
private static <T> T nonNull(T value){if(value==null)throw refusal("INVALID_VALUE");return value;}
private static String hash(String value){
nonNull(value);if(value.startsWith("0x")||value.startsWith("0X"))value=value.substring(2);
if(!value.matches("[0-9a-fA-F]{64}"))throw refusal("INVALID_VALUE");
return value.toLowerCase(Locale.ROOT);
}
private static void checkTarget(String code,String digest){
if(!hash(code).equals(CODE_HASH))throw refusal("CODE_HASH_MISMATCH");
if(!hash(digest).equals(INTERFACE_DIGEST))throw refusal("STALE_INTERFACE");
}
public static final class Call<O,F>{
private final byte[] encoded;
private Call(byte[] bytes){encoded=bytes.clone();}
public byte[] bytes(){return encoded.clone();}
}
private static BigInteger integer(BigInteger value,int width,boolean signed){
nonNull(value);int bits=width*8;BigInteger min=signed?BigInteger.ONE.shiftLeft(bits-1).negate():BigInteger.ZERO;
BigInteger max=BigInteger.ONE.shiftLeft(signed?bits-1:bits);
if(value.compareTo(min)<0||value.compareTo(max)>=0)throw refusal("INVALID_VALUE");return value;
}
private static byte[] number(int tag,int width,BigInteger value){
Writer w=new Writer();w.b(tag);BigInteger v=value.signum()<0?value.add(BigInteger.ONE.shiftLeft(width*8)):value;
for(int i=width-1;i>=0;i--)w.b(v.shiftRight(i*8).intValue()&255);return w.bytes();
}
private static final class Writer {
private final ByteArrayOutputStream bytes=new ByteArrayOutputStream();
private void room(int n){if(n<0||n>MAX_BYTES-1-bytes.size())throw refusal("INVALID_VALUE");}
void b(int value){room(1);bytes.write(value);}
void u32(long value){if(value<0||value>0xffffffffL)throw refusal("INVALID_VALUE");for(int i=3;i>=0;i--)b((int)(value>>>(8*i))&255);}
void put(byte[] value){room(value.length);bytes.write(value,0,value.length);}
byte[] bytes(){return bytes.toByteArray();}
}
private static final class Reader {
private final byte[] bytes;private int at;private long decoded;
Reader(byte[] value,int convention){nonNull(value);if(value.length<1)throw refusal("TRUNCATED");if(value.length>MAX_BYTES)throw refusal("NON_CANONICAL");bytes=value.clone();if(byteValue()!=convention)throw refusal("NON_CANONICAL");}
int remaining(){return bytes.length-at;}
void charge(long n){if(n<0||n>MAX_DECODED-decoded)throw refusal("NON_CANONICAL");decoded+=n;}
int length(long n){if(n<0||n>Integer.MAX_VALUE||n>remaining())throw refusal("TRUNCATED");return (int)n;}
byte[] take(int n){if(n<0||n>remaining())throw refusal("TRUNCATED");charge(n);byte[] out=Arrays.copyOfRange(bytes,at,at+n);at+=n;return out;}
int byteValue(){return take(1)[0]&255;}
void tag(int tag){if(byteValue()!=tag)throw refusal("NON_CANONICAL");}
long u32(){long n=0;for(int i=0;i<4;i++)n=(n<<8)|byteValue();return n;}
BigInteger number(int n,boolean signed){byte[] value=take(n);return signed?new BigInteger(value):new BigInteger(1,value);}
void done(){if(at!=bytes.length)throw refusal("TRAILING_BYTES");}
}
private static byte[] callBytes(byte[] discriminator,int convention,byte[] payload){
if(payload.length>MAX_BYTES-1)throw refusal("INVALID_VALUE");
byte[] result=new byte[5+payload.length];System.arraycopy(discriminator,0,result,0,4);result[4]=(byte)convention;System.arraycopy(payload,0,result,5,payload.length);return result;
}
"#;

const KOTLIN_RUNTIME: &str = r#"import java.math.BigInteger
import java.io.ByteArrayOutputStream
import java.util.Locale

object ProgramBindings {
private const val MAX_BYTES: Int=1048576
private const val MAX_DECODED: Long=16777216L
class BindingRefusal internal constructor(val code: String): IllegalArgumentException(code)
private fun refusal(code: String): BindingRefusal=BindingRefusal(code)
private fun hash(value: String): String {
val raw=if(value.startsWith("0x")||value.startsWith("0X"))value.substring(2)else value
if(!raw.matches(Regex("[0-9a-fA-F]{64}")))throw refusal("INVALID_VALUE")
return raw.lowercase(Locale.ROOT)
}
private fun checkTarget(code: String,digest: String){
if(hash(code)!=CODE_HASH)throw refusal("CODE_HASH_MISMATCH")
if(hash(digest)!=INTERFACE_DIGEST)throw refusal("STALE_INTERFACE")
}
private fun integer(value: BigInteger,width: Int,signed: Boolean): BigInteger {
val bits=width*8
val min=if(signed)BigInteger.ONE.shiftLeft(bits-1).negate()else BigInteger.ZERO
val max=BigInteger.ONE.shiftLeft(if(signed)bits-1 else bits)
if(value<min||value>=max)throw refusal("INVALID_VALUE")
return value
}
private fun number(tag: Int,width: Int,value: BigInteger): ByteArray {
val w=Writer();w.b(tag)
val v=if(value.signum()<0)value.add(BigInteger.ONE.shiftLeft(width*8))else value
for(i in width-1 downTo 0)w.b(v.shiftRight(i*8).toInt() and 255)
return w.bytes()
}
internal class Writer {
private val output=ByteArrayOutputStream()
private fun room(n: Int){if(n<0||n>MAX_BYTES-1-output.size())throw refusal("INVALID_VALUE")}
fun b(value: Int){room(1);output.write(value)}
fun u32(value: Long){if(value<0||value>0xffffffffL)throw refusal("INVALID_VALUE");for(i in 3 downTo 0)b(((value ushr (8*i)) and 255L).toInt())}
fun put(value: ByteArray){room(value.size);output.write(value,0,value.size)}
fun bytes(): ByteArray=output.toByteArray()
}
internal class Reader(value: ByteArray,convention: Int) {
private val bytes: ByteArray
private var at=0
private var decoded=0L
init {if(value.isEmpty())throw refusal("TRUNCATED");if(value.size>MAX_BYTES)throw refusal("NON_CANONICAL");bytes=value.copyOf();if(byteValue()!=convention)throw refusal("NON_CANONICAL")}
fun remaining(): Int=bytes.size-at
fun charge(n: Long){if(n<0||n>MAX_DECODED-decoded)throw refusal("NON_CANONICAL");decoded+=n}
fun length(n: Long): Int {if(n<0||n>Int.MAX_VALUE||n>remaining())throw refusal("TRUNCATED");return n.toInt()}
fun take(n: Int): ByteArray {if(n<0||n>remaining())throw refusal("TRUNCATED");charge(n.toLong());val out=bytes.copyOfRange(at,at+n);at+=n;return out}
fun byteValue(): Int=take(1)[0].toInt() and 255
fun tag(tag: Int){if(byteValue()!=tag)throw refusal("NON_CANONICAL")}
fun u32(): Long {var n=0L;repeat(4){n=(n shl 8) or byteValue().toLong()};return n}
fun number(n: Int,signed: Boolean): BigInteger {val value=take(n);return if(signed)BigInteger(value)else BigInteger(1,value)}
fun done(){if(at!=bytes.size)throw refusal("TRAILING_BYTES")}
}
private fun callBytes(discriminator: ByteArray,convention: Int,payload: ByteArray): ByteArray {
if(payload.size>MAX_BYTES-1)throw refusal("INVALID_VALUE")
val result=ByteArray(5+payload.size);discriminator.copyInto(result,0);result[4]=convention.toByte();payload.copyInto(result,5);return result
}
"#;

impl BindingGenerator {
    #[must_use]
    pub fn generate_java_consumer(&self) -> String {
        let mut out=String::from("import java.math.BigInteger;\nimport java.util.Arrays;\nimport java.util.Collections;\npublic final class BindingConsumer {\nprivate static void equal(byte[] a,byte[] b){if(!Arrays.equals(a,b))throw new AssertionError(\"canonical bytes differ\");}\nprivate static void refused(String code,Runnable action){try{action.run();}catch(ProgramBindings.BindingRefusal e){if(!e.code.equals(code))throw new AssertionError(e.code);return;}throw new AssertionError(\"missing refusal: \"+code);}\npublic static void main(String[] args){\n");
        for entry in &self.entries {
            let prefix=format!("ProgramBindings.Entry{}",hex(&entry.discriminator));
            let (sample,bytes)=sample_value(&entry.input,&prefix,"Input",false,false);
            let expected=message_bytes(&bytes,convention(&entry.input),Some(entry.discriminator));
            let _=writeln!(out,"{{{prefix}.Input input={sample};equal({prefix}.call(input,ProgramBindings.CODE_HASH,ProgramBindings.INTERFACE_DIGEST).bytes(),{});",java_bytes(&expected));
            let (_,output)=sample_value(&entry.output,&prefix,"Output",false,false);
            let message=message_bytes(&output,convention(&entry.output),None);
            let _=writeln!(out,"equal({prefix}.decodeOutput({}).canonicalBytes(),{});",java_bytes(&message),java_bytes(&output));
            let _=writeln!(out,"refused(\"CODE_HASH_MISMATCH\",()->{prefix}.call(input,\"{}\",ProgramBindings.INTERFACE_DIGEST));refused(\"STALE_INTERFACE\",()->{prefix}.call(input,ProgramBindings.CODE_HASH,\"{}\"));",wrong_hash(self.code_hash),wrong_hash(self.digest));
            emit_java_decode_negatives(&mut out,&prefix,&message);
            emit_java_schema_refusal(&mut out,&prefix,&entry.output);
            if matches!(entry.output,Type::Option(_)|Type::Union(_)) {
                let (_,alternate)=sample_value(&entry.output,&prefix,"Output",false,true);
                let framed=message_bytes(&alternate,convention(&entry.output),None);
                let _=writeln!(out,"equal({prefix}.decodeOutput({}).canonicalBytes(),{});",java_bytes(&framed),java_bytes(&alternate));
            }
            for failure in &entry.failures {
                let detail_name=format!("FailureCode{}Detail",failure.code);
                let (_,detail)=sample_value(&failure.detail,&prefix,&detail_name,false,false);
                let framed=message_bytes(&detail,convention(&failure.detail),None);
                let _=writeln!(out,"{{{prefix}.Failure failure={prefix}.decodeFailure({}L,{});if(!(failure instanceof {prefix}.FailureCode{})||failure.code()!={}L||!failure.name().equals(\"{}\"))throw new AssertionError(\"typed failure\");equal((({prefix}.FailureCode{})failure).detail.canonicalBytes(),{});}}",failure.code,java_bytes(&framed),failure.code,failure.code,failure.name,failure.code,java_bytes(&detail));
            }
            let unknown=unknown_failure(entry);
            let _=writeln!(out,"refused(\"UNKNOWN_FAILURE\",()->{prefix}.decodeFailure({unknown}L,new byte[]{{}}));");
            if matches!(entry.input,Type::Option(_)|Type::Union(_)) {
                let (alternate,bytes)=sample_value(&entry.input,&prefix,"Input",false,true);
                let expected=message_bytes(&bytes,convention(&entry.input),Some(entry.discriminator));
                let _=writeln!(out,"equal({prefix}.call({alternate},ProgramBindings.CODE_HASH,ProgramBindings.INTERFACE_DIGEST).bytes(),{});",java_bytes(&expected));
            }
            let _=writeln!(out,"System.out.println(\"BINDING_CASE roundtrip_{}\");}}",entry.name);
        }
        emit_java_construction_refusal(&mut out,self);
        out.push_str("System.out.println(\"BINDING_CASE typed_failure\");System.out.println(\"BINDING_CASE stale_digest\");System.out.println(\"BINDING_CASE wrong_code_hash\");System.out.println(\"BINDING_CASE malformed_call\");\n}}\n");
        out
    }

    #[must_use]
    pub fn generate_java_malformed_consumer(&self) -> String {
        let mut out=String::from("public final class BindingMalformed {public static void main(String[] args){\n");
        if let Some(entry)=self.entries.first() {
            let _=writeln!(out,"ProgramBindings.Entry{}.call(\"not a typed input\",ProgramBindings.CODE_HASH,ProgramBindings.INTERFACE_DIGEST);",hex(&entry.discriminator));
        }
        out.push_str("}}\n");
        out
    }

    #[must_use]
    pub fn generate_kotlin_consumer(&self) -> String {
        let mut out=String::from("import java.math.BigInteger\nprivate fun equal(a: ByteArray,b: ByteArray){check(a.contentEquals(b)){\"canonical bytes differ\"}}\nprivate fun refused(code: String,action: ()->Unit){try{action()}catch(e: ProgramBindings.BindingRefusal){check(e.code==code){e.code};return};error(\"missing refusal: $code\")}\nfun main(){\n");
        for entry in &self.entries {
            let prefix=format!("ProgramBindings.Entry{}",hex(&entry.discriminator));
            let (sample,bytes)=sample_value(&entry.input,&prefix,"Input",true,false);
            let expected=message_bytes(&bytes,convention(&entry.input),Some(entry.discriminator));
            let _=writeln!(out,"run{{val input={sample};equal({prefix}.call(input,ProgramBindings.CODE_HASH,ProgramBindings.INTERFACE_DIGEST).bytes,{})",kotlin_bytes(&expected));
            let (_,output)=sample_value(&entry.output,&prefix,"Output",true,false);
            let message=message_bytes(&output,convention(&entry.output),None);
            let _=writeln!(out,"equal({prefix}.decodeOutput({}).canonicalBytes(),{})",kotlin_bytes(&message),kotlin_bytes(&output));
            let _=writeln!(out,"refused(\"CODE_HASH_MISMATCH\"){{{prefix}.call(input,\"{}\",ProgramBindings.INTERFACE_DIGEST)}};refused(\"STALE_INTERFACE\"){{{prefix}.call(input,ProgramBindings.CODE_HASH,\"{}\")}}",wrong_hash(self.code_hash),wrong_hash(self.digest));
            emit_kotlin_decode_negatives(&mut out,&prefix,&message);
            emit_kotlin_schema_refusal(&mut out,&prefix,&entry.output);
            if matches!(entry.output,Type::Option(_)|Type::Union(_)) {
                let (_,alternate)=sample_value(&entry.output,&prefix,"Output",true,true);
                let framed=message_bytes(&alternate,convention(&entry.output),None);
                let _=writeln!(out,"equal({prefix}.decodeOutput({}).canonicalBytes(),{})",kotlin_bytes(&framed),kotlin_bytes(&alternate));
            }
            for failure in &entry.failures {
                let detail_name=format!("FailureCode{}Detail",failure.code);
                let (_,detail)=sample_value(&failure.detail,&format!("{prefix}.Failure"),&detail_name,true,false);
                let framed=message_bytes(&detail,convention(&failure.detail),None);
                let _=writeln!(out,"run{{val failure={prefix}.decodeFailure({}L,{});check(failure is {prefix}.Failure.FailureCode{});check(failure.code=={}L&&failure.name==\"{}\");equal(failure.detail.canonicalBytes(),{})}}",failure.code,kotlin_bytes(&framed),failure.code,failure.code,failure.name,kotlin_bytes(&detail));
            }
            let unknown=unknown_failure(entry);
            let _=writeln!(out,"refused(\"UNKNOWN_FAILURE\"){{{prefix}.decodeFailure({unknown}L,byteArrayOf())}}");
            if matches!(entry.input,Type::Option(_)|Type::Union(_)) {
                let (alternate,bytes)=sample_value(&entry.input,&prefix,"Input",true,true);
                let expected=message_bytes(&bytes,convention(&entry.input),Some(entry.discriminator));
                let _=writeln!(out,"equal({prefix}.call({alternate},ProgramBindings.CODE_HASH,ProgramBindings.INTERFACE_DIGEST).bytes,{})",kotlin_bytes(&expected));
            }
            let _=writeln!(out,"println(\"BINDING_CASE roundtrip_{}\")}}",entry.name);
        }
        emit_kotlin_construction_refusal(&mut out,self);
        out.push_str("println(\"BINDING_CASE typed_failure\");println(\"BINDING_CASE stale_digest\");println(\"BINDING_CASE wrong_code_hash\");println(\"BINDING_CASE malformed_call\")\n}\n");
        out
    }

    #[must_use]
    pub fn generate_kotlin_malformed_consumer(&self) -> String {
        let mut out=String::from("fun main(){\n");
        if let Some(entry)=self.entries.first() {
            let _=writeln!(out,"ProgramBindings.Entry{}.call(\"not a typed input\",ProgramBindings.CODE_HASH,ProgramBindings.INTERFACE_DIGEST)",hex(&entry.discriminator));
        }
        out.push_str("}\n");
        out
    }
}

fn wrong_hash(mut value: [u8;32]) -> String {
    value[0]^=1;
    hex(&value)
}

fn unknown_failure(entry: &Entry) -> u32 {
    let mut candidate=0u32;
    while entry.failures.iter().any(|f|f.code==candidate) {candidate+=1;}
    candidate
}

fn message_bytes(payload: &[u8], convention: u8, discriminator: Option<[u8;4]>) -> alloc::vec::Vec<u8> {
    let mut bytes=alloc::vec::Vec::new();
    if let Some(discriminator)=discriminator {bytes.extend_from_slice(&discriminator);}
    bytes.push(convention);
    bytes.extend_from_slice(payload);
    bytes
}

fn java_bytes(bytes: &[u8]) -> String {
    format!("new byte[]{{{}}}",bytes.iter().map(|b|format!("(byte){b}")).collect::<alloc::vec::Vec<_>>().join(","))
}

fn kotlin_bytes(bytes: &[u8]) -> String {
    format!("byteArrayOf({})",bytes.iter().map(|b|format!("{b}.toByte()")).collect::<alloc::vec::Vec<_>>().join(","))
}

fn sample_value(t: &Type,prefix: &str,name: &str,kotlin: bool,alternate: bool) -> (String,alloc::vec::Vec<u8>) {
    let ctor=if kotlin{String::new()}else{String::from("new ")};
    let ty=format!("{prefix}.{name}");
    if let Some((tag,width,signed))=integer(t) {
        let value=if alternate{"0"}else if signed{"-1"}else{"1"};
        let mut bytes=alloc::vec![if signed&&!alternate{255}else{0};width as usize+1];
        bytes[0]=tag;
        if !signed&&!alternate {bytes[width as usize]=1;}
        return (format!("{ctor}{ty}({ctor}BigInteger(\"{value}\"))"),bytes);
    }
    match t {
        Type::Bytes(_) => {
            let bytes=alloc::vec![0x20,0,0,0,1,42];
            let raw=if kotlin{"byteArrayOf(42)"}else{"new byte[]{42}"};
            (format!("{ctor}{ty}({raw})"),bytes)
        }
        Type::EvmHead => {
            let mut bytes=alloc::vec![0;32];bytes[31]=1;
            let literal=if kotlin{kotlin_bytes(&bytes)}else{java_bytes(&bytes)};
            (format!("{ctor}{ty}({literal})"),bytes)
        }
        Type::Fixed(child,count)|Type::Variable(child,count) => {
            let n=if matches!(t,Type::Fixed(_, _)){*count}else{core::cmp::min(*count,2)};
            let (child_expr,child_bytes)=sample_value(child,prefix,&format!("{name}Element"),kotlin,alternate);
            let mut bytes=alloc::vec![if matches!(t,Type::Fixed(_, _)){48}else{49}];
            bytes.extend_from_slice(&n.to_be_bytes());
            for _ in 0..n {bytes.extend_from_slice(&child_bytes);}
            let list=if kotlin{format!("List({n}){{{child_expr}}}")}else{format!("Collections.nCopies({n},{child_expr})")};
            (format!("{ctor}{ty}({list})"),bytes)
        }
        Type::Option(child) => {
            if alternate {(format!("{ty}.none()"),alloc::vec![64,0])}else{
                let (expr,child_bytes)=sample_value(child,prefix,&format!("{name}Some"),kotlin,false);
                let mut bytes=alloc::vec![64,1];bytes.extend_from_slice(&child_bytes);
                (format!("{ty}.some({expr})"),bytes)
            }
        }
        Type::Union(variants) => {
            let v=if alternate{variants.last()}else{variants.first()}.expect("validated nonempty union");
            let (expr,child_bytes)=sample_value(&v.value,prefix,&format!("{name}Variant{}Value",v.tag),kotlin,alternate);
            let mut bytes=alloc::vec![80];bytes.extend_from_slice(&v.tag.to_be_bytes());bytes.extend_from_slice(&child_bytes);
            let variant=if kotlin{format!("{ty}.{name}Variant{}",v.tag)}else{format!("{prefix}.{name}Variant{}",v.tag)};
            (format!("{ctor}{variant}({expr})"),bytes)
        }
        _=>unreachable!(),
    }
}

fn emit_java_decode_negatives(out: &mut String,prefix: &str,message: &[u8]) {
    let mut wrong=message.to_vec();wrong[0]=0;
    let _=writeln!(out,"refused(\"NON_CANONICAL\",()->{prefix}.decodeOutput({}));refused(\"TRUNCATED\",()->{prefix}.decodeOutput(new byte[]{{}}));",java_bytes(&wrong));
    if message.first()==Some(&1) {
        let mut trailing=message.to_vec();trailing.push(0);
        let _=writeln!(out,"refused(\"TRAILING_BYTES\",()->{prefix}.decodeOutput({}));",java_bytes(&trailing));
        let mut tag=message.to_vec();tag[1]=0;
        let _=writeln!(out,"refused(\"NON_CANONICAL\",()->{prefix}.decodeOutput({}));",java_bytes(&tag));
    }else{
        let _=writeln!(out,"refused(\"INVALID_VALUE\",()->{prefix}.decodeOutput(new byte[]{{2,0}}));");
    }
}

fn emit_kotlin_decode_negatives(out: &mut String,prefix: &str,message: &[u8]) {
    let mut wrong=message.to_vec();wrong[0]=0;
    let _=writeln!(out,"refused(\"NON_CANONICAL\"){{{prefix}.decodeOutput({})}};refused(\"TRUNCATED\"){{{prefix}.decodeOutput(byteArrayOf())}}",kotlin_bytes(&wrong));
    if message.first()==Some(&1) {
        let mut trailing=message.to_vec();trailing.push(0);
        let _=writeln!(out,"refused(\"TRAILING_BYTES\"){{{prefix}.decodeOutput({})}}",kotlin_bytes(&trailing));
        let mut tag=message.to_vec();tag[1]=0;
        let _=writeln!(out,"refused(\"NON_CANONICAL\"){{{prefix}.decodeOutput({})}}",kotlin_bytes(&tag));
    }else{
        let _=writeln!(out,"refused(\"INVALID_VALUE\"){{{prefix}.decodeOutput(byteArrayOf(2,0))}}");
    }
}

fn emit_java_construction_refusal(out: &mut String,generator: &BindingGenerator) {
    for entry in &generator.entries {
        if let Some((_,width,signed))=integer(&entry.input) {
            let prefix=format!("ProgramBindings.Entry{}",hex(&entry.discriminator));
            let bits=width*8-if signed { 1 } else { 0 };
            let _=writeln!(out,"refused(\"INVALID_VALUE\",()->new {prefix}.Input(BigInteger.ONE.shiftLeft({bits})));");
            if !signed {
                let _=writeln!(out,"refused(\"INVALID_VALUE\",()->new {prefix}.Input(BigInteger.valueOf(-1)));");
            }else{
                let _=writeln!(out,"refused(\"INVALID_VALUE\",()->new {prefix}.Input(BigInteger.ONE.shiftLeft({bits}).negate().subtract(BigInteger.ONE)));");
            }
        }
    }
}

fn emit_kotlin_construction_refusal(out: &mut String,generator: &BindingGenerator) {
    for entry in &generator.entries {
        if let Some((_,width,signed))=integer(&entry.input) {
            let prefix=format!("ProgramBindings.Entry{}",hex(&entry.discriminator));
            let bits=width*8-if signed { 1 } else { 0 };
            let _=writeln!(out,"refused(\"INVALID_VALUE\"){{{prefix}.Input(BigInteger.ONE.shiftLeft({bits}))}}");
            if !signed {
                let _=writeln!(out,"refused(\"INVALID_VALUE\"){{{prefix}.Input(BigInteger.valueOf(-1))}}");
            }else{
                let _=writeln!(out,"refused(\"INVALID_VALUE\"){{{prefix}.Input(BigInteger.ONE.shiftLeft({bits}).negate().subtract(BigInteger.ONE))}}");
            }
        }
    }
}

fn schema_refusal(t: &Type) -> Option<alloc::vec::Vec<u8>> {
    let mut value=alloc::vec![1];
    match t {
        Type::Bytes(n)|Type::Fixed(_,n)|Type::Variable(_,n) if *n<u32::MAX => {
            value.push(match t {Type::Bytes(_)=>32,Type::Fixed(_,_)=>48,_=>49});
            value.extend_from_slice(&(n+1).to_be_bytes());
        }
        Type::Option(_) => {value.extend_from_slice(&[64,2]);}
        Type::Union(variants) => {
            let mut tag=0u32;
            while variants.iter().any(|variant|variant.tag==tag){tag+=1;}
            value.push(80);value.extend_from_slice(&tag.to_be_bytes());
        }
        _=>return None,
    }
    Some(value)
}

fn emit_java_schema_refusal(out: &mut String,prefix: &str,t: &Type) {
    if let Some(bytes)=schema_refusal(t) {
        let _=writeln!(out,"refused(\"NON_CANONICAL\",()->{prefix}.decodeOutput({}));",java_bytes(&bytes));
    }
}

fn emit_kotlin_schema_refusal(out: &mut String,prefix: &str,t: &Type) {
    if let Some(bytes)=schema_refusal(t) {
        let _=writeln!(out,"refused(\"NON_CANONICAL\"){{{prefix}.decodeOutput({})}}",kotlin_bytes(&bytes));
    }
}

