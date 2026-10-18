import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import assert from 'node:assert/strict';
import { fileURLToPath } from 'node:url';
import { createRequire } from 'node:module';
const root = fileURLToPath(new URL('../../../../', import.meta.url));
const sdk = path.join(root, 'client/packages/sdk');
const location = process.env.SDK_PHASE4_BUILD_CONFIG;
if (!location) throw new Error('ACTUAL_PHASE4_BUILD_INPUTS_REQUIRED');
const config = JSON.parse(fs.readFileSync(location, 'utf8'));
const hash = value => crypto.createHash('sha256').update(value).digest('hex');
const sourceBytes = fs.readFileSync(config.candidateManifest.path);
assert.equal(hash(sourceBytes), config.candidateManifest.sha256);
const candidate = JSON.parse(sourceBytes);
assert.equal(candidate.sourceSha256, config.sourceSha256);
for (const [name, expected] of Object.entries(candidate.files)) {
  const input = path.resolve(root, name);
  assert.ok(input.startsWith(root) && fs.statSync(input).isFile());
  assert.equal(hash(fs.readFileSync(input)), expected);
}
const allowed = path.join(root, 'evidence/production-system/L24/phase4');
const output = path.resolve(config.bundleOutput), manifestOutput = path.resolve(config.bundleManifestOutput), sdkManifestOutput = path.resolve(config.sdkManifestOutput);
assert.ok(output.startsWith(allowed + path.sep) && manifestOutput.startsWith(allowed + path.sep));
assert.ok(sdkManifestOutput.startsWith(allowed + path.sep)); assert.equal(fs.existsSync(sdkManifestOutput), false);
assert.equal(fs.existsSync(output), false); assert.equal(fs.existsSync(manifestOutput), false);
const webRequire = createRequire(path.join(root, 'client/apps/web/package.json'));
const esbuild = webRequire('esbuild');
const typescript = webRequire('typescript');
assert.equal(typescript.version, JSON.parse(fs.readFileSync(path.join(sdk, 'package.json'), 'utf8')).devDependencies.typescript);
const tsConfig = typescript.readConfigFile(path.join(sdk, 'tsconfig.json'), typescript.sys.readFile);
if (tsConfig.error) throw new Error('SDK_BUILD_CONFIG_FAILED');
const parsed = typescript.parseJsonConfigFileContent(tsConfig.config, typescript.sys, sdk);
if (parsed.errors.length) throw new Error('SDK_BUILD_CONFIG_FAILED');
const program = typescript.createProgram(parsed.fileNames, { ...parsed.options, noEmitOnError: true });
const diagnostics = typescript.getPreEmitDiagnostics(program);
if (diagnostics.length) { process.stderr.write(typescript.formatDiagnosticsWithColorAndContext(diagnostics, { getCurrentDirectory: () => root, getCanonicalFileName: name => name, getNewLine: () => '\n' })); throw new Error('SDK_COMPILE_FAILED'); }
const sdkOutputs = new Map();
const emitted = program.emit(undefined, (name, text) => sdkOutputs.set(path.resolve(name), text));
if (emitted.emitSkipped || emitted.diagnostics.length) throw new Error('SDK_EMIT_FAILED');
const sdkOutputDigests = {};
for (const [name, text] of sdkOutputs) {
  assert.ok(name.startsWith(path.join(sdk, 'dist') + path.sep));
  fs.mkdirSync(path.dirname(name), { recursive: true }); fs.writeFileSync(name, text);
  sdkOutputDigests[path.relative(root, name)] = hash(text);
}
assert.ok(Object.hasOwn(sdkOutputDigests, 'client/packages/sdk/dist/index.js'));

assert.equal(esbuild.version, config.esbuildVersion);
assert.ok(typeof config.esbuildVersion === 'string' && /^\d+\.\d+\.\d+$/.test(config.esbuildVersion));
assert.ok(typeof config.browserTarget === 'string' && /^chrome\d+$/.test(config.browserTarget));
assert.ok(config.publicEnvironment && Object.entries(config.publicEnvironment).every(([name, value]) => name.startsWith('NEXT_PUBLIC_') && typeof value === 'string'));
const definitions = { 'process.env.NODE_ENV': JSON.stringify('production') };
for (const [name, value] of Object.entries(config.publicEnvironment)) definitions['process.env.' + name] = JSON.stringify(value);
const publicNames = [...fs.readFileSync(path.join(root, 'client/apps/web/src/core/sdk-config.ts'), 'utf8').matchAll(/process\.env\.(NEXT_PUBLIC_[A-Z0-9_]+)/g), ...fs.readFileSync(path.join(root, 'client/apps/web/src/core/report-error.ts'), 'utf8').matchAll(/process\.env\.(NEXT_PUBLIC_[A-Z0-9_]+)/g)].map(match => match[1]);
assert.ok(publicNames.every(name => Object.hasOwn(config.publicEnvironment, name)), 'ACTUAL_PUBLIC_ENVIRONMENT_REQUIRED');
const result = await esbuild.build({
  entryPoints: [path.join(sdk, 'test/browser-measurement.ts')],
  absWorkingDir: root,
  platform: 'browser', format: 'iife', bundle: true, write: false, metafile: true,
  sourcemap: false, outfile: output, target: config.browserTarget,
  define: definitions,
  alias: {
    '@': path.join(root, 'client/apps/web/src'),
    '@kindle/sdk': path.join(sdk, 'src/index.ts'),
    'react': webRequire.resolve('react'),
    'react/jsx-runtime': webRequire.resolve('react/jsx-runtime'),
    'react-dom/client': webRequire.resolve('react-dom/client'),
  },
});
assert.equal(result.outputFiles.length, 1);
const inputs = {}, compiledInputs = {};
for (const name of Object.keys(result.metafile.inputs)) {
  const input = path.resolve(root, name);
  assert.ok(input.startsWith(root) && fs.statSync(input).isFile(), 'BUILD_INPUT_OUTSIDE_CANDIDATE');
  inputs[path.relative(root, input)] = hash(fs.readFileSync(input)); compiledInputs[path.relative(root, input)] = inputs[path.relative(root, input)];
}
for (const file of fs.readdirSync(path.join(sdk, 'src'))) {
  const name = 'client/packages/sdk/src/' + file;
  if (fs.statSync(path.join(root, name)).isFile()) inputs[name] = hash(fs.readFileSync(path.join(root, name)));
}
for (const file of fs.readdirSync(path.join(root, 'client/apps/web/src/core/realtime'))) {
  const name = 'client/apps/web/src/core/realtime/' + file;
  if (file.endsWith('.ts') && fs.statSync(path.join(root, name)).isFile()) inputs[name] = hash(fs.readFileSync(path.join(root, name)));
}
inputs['client/packages/sdk/test/browser-measurement.ts'] = hash(fs.readFileSync(path.join(sdk, 'test/browser-measurement.ts')));
for (const [name, expected] of Object.entries(candidate.files)) assert.equal(hash(fs.readFileSync(path.resolve(root, name))), expected, 'SOURCE_CHANGED_DURING_BUILD');
fs.mkdirSync(path.dirname(output), { recursive: true, mode: 0o700 });
fs.mkdirSync(path.dirname(manifestOutput), { recursive: true, mode: 0o700 });
fs.writeFileSync(output, result.outputFiles[0].contents, { mode: 0o600, flag: 'wx' });
const manifest = { sourceSha256: config.sourceSha256, candidateManifestSha256: config.candidateManifest.sha256, esbuildVersion: esbuild.version, browserTarget: config.browserTarget, bundleSha256: hash(result.outputFiles[0].contents), compiledInputs, publicEnvironmentSha256: hash(JSON.stringify(config.publicEnvironment)), inputs: Object.fromEntries(Object.entries(inputs).sort(([a], [b]) => a.localeCompare(b))) };
fs.mkdirSync(path.dirname(sdkManifestOutput), { recursive: true, mode: 0o700 });
const sdkInputs = Object.fromEntries(Object.entries(inputs).filter(([name]) => name.startsWith('client/packages/sdk/src/')));
const sdkManifest = { sourceSha256: config.sourceSha256, typescriptVersion: typescript.version, inputs: sdkInputs, outputs: sdkOutputDigests };
fs.writeFileSync(sdkManifestOutput, JSON.stringify(sdkManifest, null, 2) + '\n', { mode: 0o600, flag: 'wx' });
fs.writeFileSync(manifestOutput, JSON.stringify(manifest, null, 2) + '\n', { mode: 0o600, flag: 'wx' });
process.stdout.write(JSON.stringify({ bundlePath: output, bundleSha256: manifest.bundleSha256, manifestPath: manifestOutput, manifestSha256: hash(fs.readFileSync(manifestOutput)), sdkManifestPath: sdkManifestOutput, sdkManifestSha256: hash(fs.readFileSync(sdkManifestOutput)) }) + '\n');
