import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { Ajv2020 } from 'ajv/dist/2020.js';
import standaloneCode from 'ajv/dist/standalone/index.js';
import { _ } from 'ajv/dist/compile/codegen/index.js';
import { fullFormats } from 'ajv-formats/dist/formats.js';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const read = path => JSON.parse(readFileSync(resolve(root, path), 'utf8'));
const v1Document = read('src/schemas.gen.json');
const v2Document = read('../sdk-v2/src/schemas.gen.json');
const operationsSource = readFileSync(resolve(root, 'src/operations.gen.ts'), 'utf8');
const operationsMatch = /export const operationDefinitions = (\{[\s\S]*\}) as const;\s*$/.exec(operationsSource);
if (!operationsMatch) throw new Error('operations.gen.ts does not contain a JSON operation definition literal');
const operations = JSON.parse(operationsMatch[1]);
const identifier = /^[A-Za-z_$][A-Za-z0-9_$]*$/;

function instance() {
  const ajv = new Ajv2020({ strict: true, allErrors: true, validateFormats: true, allowUnionTypes: true, inlineRefs: false, code: { source: true, esm: true, formats: _`fullFormats` } });
  for (const [name, format] of Object.entries(fullFormats)) ajv.addFormat(name, format);
  return ajv;
}

function exportName(name) {
  const safe = name.replace(/[^A-Za-z0-9_$]/g, '_');
  if (!identifier.test(safe)) throw new Error(`Cannot derive an export name from ${name}`);
  return safe;
}

function moduleCode(ajv, exports, indexes) {
  let code = standaloneCode(ajv, exports);
  const imports = new Map([['fullFormats', 'import { fullFormats } from "ajv-formats/dist/formats.js";']]);
  // Ajv runtime helpers are CommonJS modules exporting `exports.default`. Node's native ESM
  // hands a default import the whole `module.exports` object, while bundlers that honour
  // `__esModule` (Turbopack, webpack) hand it `exports.default` directly. Resolve both shapes.
  code = code.replace(/require\("([^"]+)"\)(\.default)?/g, (_match, specifier, member) => {
    const alias = `runtime_${specifier.replace(/[^A-Za-z0-9]/g, '_')}`;
    const target = /\.[cm]?js$/.test(specifier) ? specifier : `${specifier}.js`;
    imports.set(alias, `import ${alias} from "${target}";`);
    return member ? `(typeof ${alias} === "function" ? ${alias} : ${alias}.default)` : alias;
  });
  if (/\brequire\s*\(/.test(code)) throw new Error('Generated validator code still contains require calls');
  const indexCode = Object.entries(indexes).map(([name, entries]) => `export const ${name} = Object.freeze({${entries.map(([key, value]) => `${JSON.stringify(key)}: ${value}`).join(', ')}});`).join('\n');
  return `${[...imports.values()].join('\n')}\n${code}\n${indexCode}\n`;
}

function declaration(indexes) {
  return `import type { ValidateFunction } from 'ajv/dist/2020.js';\n${Object.keys(indexes).map(name => `export declare const ${name}: Readonly<Record<string, ValidateFunction>>;`).join('\n')}\n`;
}

function emit(base, code, indexes) {
  mkdirSync(resolve(root, 'dist'), { recursive: true });
  writeFileSync(resolve(root, 'dist', `${base}.js`), code);
  writeFileSync(resolve(root, 'dist', `${base}.d.ts`), declaration(indexes));
  writeFileSync(resolve(root, 'src', `${base}.d.ts`), declaration(indexes));
  return Buffer.byteLength(code);
}

function contractModule(document, key, base, indexName) {
  const ajv = instance();
  ajv.addSchema(document, key);
  const exports = {};
  const entries = [];
  for (const name of Object.keys(document.$defs)) {
    const alias = exportName(name);
    exports[alias] = `${key}#/$defs/${name}`;
    entries.push([name, alias]);
  }
  const indexes = { [indexName]: entries };
  return emit(base, moduleCode(ajv, exports, indexes), indexes);
}

function rebase(schema, key) {
  if (Array.isArray(schema)) return schema.map(item => rebase(item, key));
  if (schema && typeof schema === 'object') {
    return Object.fromEntries(Object.entries(schema).map(([field, value]) => [field, field === '$ref' && typeof value === 'string' && value.startsWith('#/') ? `${key}${value}` : rebase(value, key)]));
  }
  return schema;
}

function operationModule() {
  const key = 'urn:kindle:operation-v2';
  const ajv = instance();
  ajv.addSchema(v2Document, key);
  const exports = {};
  const entries = [];
  for (const [name, definition] of Object.entries(operations)) {
    for (const location of ['path', 'query']) {
      const parameters = definition.parameters.filter(parameter => parameter.in === location);
      const schema = rebase({ type: 'object', additionalProperties: false, properties: Object.fromEntries(parameters.map(parameter => [parameter.name, parameter.schema])), required: parameters.filter(parameter => parameter.required).map(parameter => parameter.name) }, key);
      const alias = exportName(`op_${name}_${location}`);
      ajv.addSchema(schema, `urn:kindle:operation:${name}:${location}`);
      exports[alias] = `urn:kindle:operation:${name}:${location}`;
      entries.push([`${name}:${location}`, alias]);
    }
    for (const parameter of definition.parameters.filter(parameter => parameter.in === 'header')) {
      const alias = exportName(`op_${name}_header_${parameter.name}`);
      ajv.addSchema(rebase(parameter.schema, key), `urn:kindle:operation:${name}:header:${parameter.name}`);
      exports[alias] = `urn:kindle:operation:${name}:header:${parameter.name}`;
      entries.push([`${name}:header:${parameter.name}`, alias]);
    }
  }
  const indexes = { operationValidators: entries };
  return emit('validators-operations.gen', moduleCode(ajv, exports, indexes), indexes);
}

const sizes = {
  v1: contractModule(v1Document, 'kindle-v1', 'validators-v1.gen', 'contractValidators'),
  v2: contractModule(v2Document, 'kindle-v2', 'validators-v2.gen', 'v2ContractValidators'),
  operations: operationModule(),
};
process.stdout.write(`${JSON.stringify(sizes)}\n`);
