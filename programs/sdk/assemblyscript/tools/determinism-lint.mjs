#!/usr/bin/env node
/**
 * The determinism gate every LayerX program passes before deployment sees it.
 *
 * It walks the produced module section by section and refuses, by name, any
 * construct that could make two honest executions disagree: an import outside
 * the frozen layerx_v1 surface, a floating-point type or instruction, a vector
 * or atomic instruction, or an instruction this gate does not recognise. The
 * gate fails closed: an opcode it cannot decode is a refusal, never a pass.
 *
 * It carries the same rule names as the C gate in programs/sdk/c/tools, so a
 * refusal reads identically whichever language produced the module.
 */

import { readdirSync, readFileSync, statSync } from "node:fs";
import { extname, join } from "node:path";

const MAX_MODULE_BYTES = 1048576;
const MAX_FUNCTIONS = 4096;

const RULE_MALFORMED = "malformed-module";
const RULE_MODULE_TOO_LARGE = "module-too-large";
const RULE_TOO_MANY_FUNCTIONS = "too-many-functions";
const RULE_FORBIDDEN_IMPORT = "forbidden-import";
const RULE_FLOAT_TYPE = "forbidden-float-type";
const RULE_FLOAT_INSTRUCTION = "forbidden-float-instruction";
const RULE_VECTOR = "forbidden-vector-type";
const RULE_ATOMIC = "forbidden-atomic-instruction";
const RULE_UNKNOWN_INSTRUCTION = "unknown-instruction";
const RULE_MISSING_MEMORY = "missing-memory-export";
const RULE_IMPORT_DECLARATION = "forbidden-import-declaration";
const RULE_FORBIDDEN_SOURCE = "forbidden-source-construct";

const MEMORY_EXPORT = "memory";
const RULE_IMPORT_SIGNATURE = "wrong-import-signature";
let selectedAbiVersion = 1;
let abiFunctions = [];

function loadManifest(path) {
  const text = readFileSync(path, "utf8");
  if (!text.endsWith("\n")) refuse(RULE_MALFORMED, "unterminated ABI manifest");
  const seen = new Set();
  let previous = 1;
  const functions = text.slice(0, -1).split("\n").map((line) => {
    const fields = line.split("\t");
    if (fields.length !== 4 || !/^[1-4]$/.test(fields[0]) ||
        fields[1] !== `layerx_v${fields[0]}` || !/^[a-z][a-z0-9_]*$/.test(fields[2]) ||
        !/^\((?:(?:i32|i64)(?:,(?:i32|i64))*)?\)->(?:i32|i64)$/.test(fields[3])) {
      refuse(RULE_MALFORMED, "invalid ABI manifest row");
    }
    const introduced = Number(fields[0]);
    const key = `${fields[1]}::${fields[2]}`;
    if (introduced < previous || seen.has(key)) refuse(RULE_MALFORMED, "noncanonical ABI manifest");
    previous = introduced;
    seen.add(key);
    return { introduced, module: fields[1], name: fields[2], signature: fields[3] };
  });
  if (functions.length !== 28 || previous !== 4) refuse(RULE_MALFORMED, "incomplete ABI manifest");
  return functions;
}

class Refusal extends Error {
  constructor(rule, detail) {
    super(`${rule}: ${detail}`);
    this.rule = rule;
    this.detail = detail;
  }
}

function refuse(rule, detail) {
  throw new Refusal(rule, detail);
}

class Reader {
  constructor(bytes) {
    this.bytes = bytes;
    this.cursor = 0;
  }

  byte() {
    if (this.cursor >= this.bytes.length) refuse(RULE_MALFORMED, "truncated section");
    const value = this.bytes[this.cursor];
    this.cursor += 1;
    return value;
  }

  unsignedLeb() {
    let result = 0;
    for (let index = 0; index < 5; index += 1) {
      const byte = this.byte();
      if (index === 4 && (byte & 0xf0) !== 0) refuse(RULE_MALFORMED, "unsigned LEB128 overflow");
      result |= (byte & 0x7f) << (index * 7);
      if ((byte & 0x80) === 0) return result >>> 0;
    }
    refuse(RULE_MALFORMED, "unterminated unsigned LEB128");
    return 0;
  }

  skipUnsignedLeb() {
    this.unsignedLeb();
  }

  skipSignedLeb(maximumBytes) {
    for (let index = 0; index < maximumBytes; index += 1) {
      const byte = this.byte();
      if ((byte & 0x80) === 0) return;
    }
    refuse(RULE_MALFORMED, "unterminated signed LEB128");
  }

  blockTypeLeb() {
    let result = 0n;
    let shift = 0n;
    for (let index = 0; index < 5; index += 1) {
      const byte = this.byte();
      result |= BigInt(byte & 0x7f) << shift;
      shift += 7n;
      if ((byte & 0x80) === 0) {
        if (shift < 64n && (byte & 0x40) !== 0) result -= 1n << shift;
        return result;
      }
    }
    refuse(RULE_MALFORMED, "unterminated block type");
    return 0n;
  }

  name() {
    const length = this.unsignedLeb();
    if (this.cursor + length > this.bytes.length) {
      refuse(RULE_MALFORMED, "name exceeds the declared bound");
    }
    const value = Buffer.from(this.bytes.subarray(this.cursor, this.cursor + length)).toString(
      "utf8"
    );
    this.cursor += length;
    return value;
  }
}

function checkValueType(code) {
  if (code === 0x7f || code === 0x7e || code === 0x70 || code === 0x6f) return;
  if (code === 0x7d) refuse(RULE_FLOAT_TYPE, "f32 value type");
  if (code === 0x7c) refuse(RULE_FLOAT_TYPE, "f64 value type");
  if (code === 0x7b) refuse(RULE_VECTOR, "v128 value type");
  refuse(RULE_MALFORMED, "unknown value type");
}

function checkBlockType(reader) {
  const value = reader.blockTypeLeb();
  if (value >= 0n) return;
  if (value === -1n || value === -2n || value === -16n || value === -17n || value === -64n) return;
  if (value === -3n) refuse(RULE_FLOAT_TYPE, "f32 block type");
  if (value === -4n) refuse(RULE_FLOAT_TYPE, "f64 block type");
  if (value === -5n) refuse(RULE_VECTOR, "v128 block type");
  refuse(RULE_MALFORMED, "unknown block type");
}

function floatOpcode(opcode) {
  if (opcode === 0x2a || opcode === 0x2b) return true;
  if (opcode === 0x38 || opcode === 0x39) return true;
  if (opcode === 0x43 || opcode === 0x44) return true;
  if (opcode >= 0x5b && opcode <= 0x66) return true;
  if (opcode >= 0x8b && opcode <= 0xa6) return true;
  if (opcode >= 0xa8 && opcode <= 0xab) return true;
  if (opcode >= 0xae && opcode <= 0xbf) return true;
  return false;
}

function walkPrefixed(reader) {
  const operation = reader.unsignedLeb();
  if (operation <= 7) {
    refuse(RULE_FLOAT_INSTRUCTION, "saturating float to integer conversion");
  }
  switch (operation) {
    case 8:
      reader.skipUnsignedLeb();
      reader.byte();
      return;
    case 10:
      reader.byte();
      reader.byte();
      return;
    case 11:
      reader.byte();
      return;
    case 12:
    case 14:
      reader.skipUnsignedLeb();
      reader.skipUnsignedLeb();
      return;
    case 9:
    case 13:
    case 15:
    case 16:
    case 17:
      reader.skipUnsignedLeb();
      return;
    default:
      refuse(RULE_UNKNOWN_INSTRUCTION, "unrecognised 0xFC prefixed instruction");
  }
}

function walkExpression(reader, limit) {
  let depth = 0;
  while (reader.cursor < limit) {
    const opcode = reader.byte();
    if (floatOpcode(opcode)) refuse(RULE_FLOAT_INSTRUCTION, "floating-point instruction");
    if (opcode >= 0x28 && opcode <= 0x3e) {
      reader.skipUnsignedLeb();
      reader.skipUnsignedLeb();
      continue;
    }
    if (opcode >= 0x45 && opcode <= 0xc4) continue;
    switch (opcode) {
      case 0x00:
      case 0x01:
      case 0x05:
      case 0x0f:
      case 0x1a:
      case 0x1b:
      case 0xd1:
        break;
      case 0x02:
      case 0x03:
      case 0x04:
        checkBlockType(reader);
        depth += 1;
        break;
      case 0x0b:
        if (depth === 0) return;
        depth -= 1;
        break;
      case 0x0c:
      case 0x0d:
      case 0x10:
      case 0x20:
      case 0x21:
      case 0x22:
      case 0x23:
      case 0x24:
      case 0x25:
      case 0x26:
      case 0x3f:
      case 0x40:
      case 0xd2:
        reader.skipUnsignedLeb();
        break;
      case 0x0e: {
        const targets = reader.unsignedLeb();
        for (let index = 0; index < targets; index += 1) reader.skipUnsignedLeb();
        reader.skipUnsignedLeb();
        break;
      }
      case 0x11:
        reader.skipUnsignedLeb();
        reader.skipUnsignedLeb();
        break;
      case 0x1c: {
        const types = reader.unsignedLeb();
        for (let index = 0; index < types; index += 1) checkValueType(reader.byte());
        break;
      }
      case 0x41:
        reader.skipSignedLeb(5);
        break;
      case 0x42:
        reader.skipSignedLeb(10);
        break;
      case 0xd0:
        reader.byte();
        break;
      case 0xfc:
        walkPrefixed(reader);
        break;
      case 0xfd:
        refuse(RULE_VECTOR, "v128 vector instruction");
        break;
      case 0xfe:
        refuse(RULE_ATOMIC, "shared-memory atomic instruction");
        break;
      default:
        refuse(RULE_UNKNOWN_INSTRUCTION, "unrecognised instruction opcode");
    }
  }
  refuse(RULE_MALFORMED, "expression ran past its section");
}

function permittedImport(module, name) {
  return abiFunctions.find((entry) => entry.introduced <= selectedAbiVersion &&
    entry.module === module && entry.name === name);
}

function valueTypeName(code) {
  if (code === 0x7f) return "i32";
  if (code === 0x7e) return "i64";
  if (code === 0x70) return "funcref";
  if (code === 0x6f) return "externref";
  refuse(RULE_MALFORMED, "unknown value type");
}

function checkTypeSection(reader, limit) {
  const count = reader.unsignedLeb();
  for (let index = 0; index < count; index += 1) {
    if (reader.cursor > limit) refuse(RULE_MALFORMED, "type section overran");
    if (reader.byte() !== 0x60) refuse(RULE_MALFORMED, "unknown type form");
    const parameters = reader.unsignedLeb();
    const parameterTypes = [];
    for (let position = 0; position < parameters; position += 1) {
      const code = reader.byte();
      checkValueType(code);
      parameterTypes.push(valueTypeName(code));
    }
    const results = reader.unsignedLeb();
    const resultTypes = [];
    for (let position = 0; position < results; position += 1) {
      const code = reader.byte();
      checkValueType(code);
      resultTypes.push(valueTypeName(code));
    }
    reader.types.push(`(${parameterTypes.join(",")})->${resultTypes.join(",")}`);
  }
}

function checkImportSection(reader, limit) {
  const count = reader.unsignedLeb();
  for (let index = 0; index < count; index += 1) {
    if (reader.cursor > limit) refuse(RULE_MALFORMED, "import section overran");
    const module = reader.name();
    const name = reader.name();
    const kind = reader.byte();
    const functionDeclaration = permittedImport(module, name);
    if (kind !== 0x00 || !functionDeclaration) {
      refuse(RULE_FORBIDDEN_IMPORT, `${module}::${name}`);
    }
    const typeIndex = reader.unsignedLeb();
    if (reader.types[typeIndex] !== functionDeclaration.signature) {
      refuse(RULE_IMPORT_SIGNATURE, `${module}::${name}`);
    }
    const key = `${module}::${name}`;
    if (reader.imported.has(key)) refuse(RULE_FORBIDDEN_IMPORT, `duplicate ${key}`);
    reader.imported.add(key);
  }
}

function checkFunctionSection(reader) {
  const count = reader.unsignedLeb();
  if (count > MAX_FUNCTIONS) {
    refuse(RULE_TOO_MANY_FUNCTIONS, "module declares more functions than the declared limit");
  }
  for (let index = 0; index < count; index += 1) reader.skipUnsignedLeb();
}

function checkGlobalSection(reader, limit) {
  const count = reader.unsignedLeb();
  for (let index = 0; index < count; index += 1) {
    if (reader.cursor > limit) refuse(RULE_MALFORMED, "global section overran");
    checkValueType(reader.byte());
    reader.byte();
    walkExpression(reader, limit);
  }
}

function checkExportSection(reader, limit) {
  const count = reader.unsignedLeb();
  let memoryExported = false;
  for (let index = 0; index < count; index += 1) {
    if (reader.cursor > limit) refuse(RULE_MALFORMED, "export section overran");
    const name = reader.name();
    const kind = reader.byte();
    reader.skipUnsignedLeb();
    if (kind === 0x02 && name === MEMORY_EXPORT) memoryExported = true;
  }
  return memoryExported;
}

function checkCodeSection(reader, limit) {
  const count = reader.unsignedLeb();
  for (let index = 0; index < count; index += 1) {
    const bodySize = reader.unsignedLeb();
    const bodyEnd = reader.cursor + bodySize;
    if (bodyEnd > limit) refuse(RULE_MALFORMED, "function body overran");
    const declarations = reader.unsignedLeb();
    for (let declaration = 0; declaration < declarations; declaration += 1) {
      reader.skipUnsignedLeb();
      checkValueType(reader.byte());
    }
    walkExpression(reader, bodyEnd);
    if (reader.cursor !== bodyEnd) refuse(RULE_MALFORMED, "function body has trailing bytes");
  }
}

function checkModule(bytes) {
  if (bytes.length > MAX_MODULE_BYTES) {
    refuse(RULE_MODULE_TOO_LARGE, "module exceeds the declared byte-size limit");
  }
  const preamble = [0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00];
  if (bytes.length < preamble.length) refuse(RULE_MALFORMED, "missing WebAssembly preamble");
  for (let index = 0; index < preamble.length; index += 1) {
    if (bytes[index] !== preamble[index]) {
      refuse(RULE_MALFORMED, "missing WebAssembly preamble");
    }
  }
  const reader = new Reader(bytes);
  reader.types = [];
  reader.imported = new Set();
  reader.cursor = preamble.length;
  let memoryExported = false;
  while (reader.cursor < bytes.length) {
    const id = reader.byte();
    const size = reader.unsignedLeb();
    const sectionEnd = reader.cursor + size;
    if (sectionEnd > bytes.length) refuse(RULE_MALFORMED, "section overran the module");
    switch (id) {
      case 1:
        checkTypeSection(reader, sectionEnd);
        break;
      case 2:
        checkImportSection(reader, sectionEnd);
        break;
      case 3:
        checkFunctionSection(reader);
        break;
      case 6:
        checkGlobalSection(reader, sectionEnd);
        break;
      case 7:
        if (checkExportSection(reader, sectionEnd)) memoryExported = true;
        break;
      case 10:
        checkCodeSection(reader, sectionEnd);
        break;
      default:
        break;
    }
    if ([1, 2, 3, 6, 7, 10].includes(id) && reader.cursor !== sectionEnd) {
      refuse(RULE_MALFORMED, "section has trailing or overrun bytes");
    }
    reader.cursor = sectionEnd;
  }
  if (!memoryExported) {
    refuse(
      RULE_MISSING_MEMORY,
      'the host reads and writes guest memory through the "memory" export'
    );
  }
}

const FORBIDDEN_SOURCE_CONSTRUCTS = [
  { pattern: /\bf32\b/, detail: "f32 is a floating-point type" },
  { pattern: /\bf64\b/, detail: "f64 is a floating-point type" },
  { pattern: /\bv128\b/, detail: "v128 is a vector type" },
  { pattern: /\bnumber\b/, detail: "number widens to a floating-point type" },
  { pattern: /\bDate\b/, detail: "Date reads a clock" },
  { pattern: /\bMath\s*\./, detail: "Math reaches floating-point and entropy" },
  { pattern: /\bNativeMath\b/, detail: "NativeMath reaches floating-point and entropy" },
  { pattern: /\bperformance\b/, detail: "performance reads a clock" },
  { pattern: /\bprocess\b/, detail: "process is ambient host authority" },
  { pattern: /\bfetch\b/, detail: "fetch reaches the network" },
  { pattern: /\batomic\./, detail: "atomics require shared memory" },
  { pattern: /\bthreads\b/, detail: "threads are non-deterministic" },
  { pattern: /\bparseFloat\b/, detail: "parseFloat produces a floating-point value" }
];

/**
 * Source gate. A guest may only name the frozen layerx_v1 module in an import
 * declaration, and may not name a construct that reaches a clock, the network,
 * a thread, entropy or a floating-point value. Both are refused before the
 * compiler runs.
 */
function checkSource(text) {
  const externals = text.matchAll(/@external\s*\(\s*"([^"]*)"\s*,\s*"([^"]*)"\s*\)/g);
  const bindings = [...externals];
  for (const match of bindings) {
    if (!permittedImport(match[1], match[2])) {
      refuse(RULE_IMPORT_DECLARATION, `${match[1]}::${match[2]} outside selected ABI`);
    }
  }
  if ([...text.matchAll(/@external\s*\(/g)].length !== bindings.length) {
    refuse(RULE_IMPORT_DECLARATION, "external binding lacks exact module and function name");
  }
  const declared = text.matchAll(/\bdeclare\s+function\s+([A-Za-z_$][\w$]*)/g);
  const externalCount = [...text.matchAll(/@external\s*\(/g)].length;
  const declaredNames = [...declared];
  if (declaredNames.length > externalCount) {
    refuse(
      RULE_IMPORT_DECLARATION,
      "an ambient function declaration carries no @external module binding"
    );
  }
  const stripped = text.replace(/\/\*[\s\S]*?\*\//g, "").replace(/\/\/[^\n]*/g, "");
  for (const construct of FORBIDDEN_SOURCE_CONSTRUCTS) {
    if (construct.pattern.test(stripped)) {
      refuse(RULE_FORBIDDEN_SOURCE, construct.detail);
    }
  }
}

function collectPaths(target) {
  const stats = statSync(target);
  if (!stats.isDirectory()) return [target];
  const collected = [];
  for (const entry of readdirSync(target)) {
    const path = join(target, entry);
    if (statSync(path).isDirectory()) {
      collected.push(...collectPaths(path));
      continue;
    }
    const extension = extname(path);
    if (extension === ".ts" || extension === ".wasm") collected.push(path);
  }
  return collected.sort();
}

function lintPath(path) {
  let bytes;
  try {
    bytes = readFileSync(path);
  } catch {
    process.stderr.write(`determinism-lint: ${path}: unreadable\n`);
    return 1;
  }
  try {
    if (extname(path) === ".wasm") {
      checkModule(new Uint8Array(bytes));
    } else {
      checkSource(bytes.toString("utf8"));
    }
  } catch (error) {
    if (error instanceof Refusal) {
      process.stderr.write(`determinism-lint: ${path}: ${error.rule}: ${error.detail}\n`);
      return 1;
    }
    process.stderr.write(`determinism-lint: ${path}: ${RULE_MALFORMED}: ${error.message}\n`);
    return 1;
  }
  process.stdout.write(`determinism-lint: ${path}: passed\n`);
  return 0;
}

function main(argv) {
  let manifest = new URL("../../abi-manifest.tsv", import.meta.url);
  let index = 0;
  while (index < argv.length && argv[index].startsWith("--")) {
    if (argv[index] === "--abi-version" && index + 1 < argv.length) {
      if (!/^[1-4]$/.test(argv[index + 1])) return 2;
      selectedAbiVersion = Number(argv[index + 1]);
    } else if (argv[index] === "--abi-manifest" && index + 1 < argv.length) {
      manifest = argv[index + 1];
    } else return 2;
    index += 2;
  }
  argv = argv.slice(index);
  try { abiFunctions = loadManifest(manifest); }
  catch (error) {
    process.stderr.write(`determinism-lint: ABI manifest: ${error.message}\n`);
    return 2;
  }
  if (argv.length === 0) {
    process.stderr.write("usage: determinism-lint <module.wasm|source.ts|directory>...\n");
    return 2;
  }
  let failures = 0;
  for (const target of argv) {
    let paths;
    try {
      paths = collectPaths(target);
    } catch {
      process.stderr.write(`determinism-lint: ${target}: unreadable\n`);
      failures += 1;
      continue;
    }
    for (const path of paths) failures += lintPath(path);
  }
  return failures === 0 ? 0 : 1;
}

process.exitCode = main(process.argv.slice(2));
