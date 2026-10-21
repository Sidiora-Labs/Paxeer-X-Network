const fs = require('fs'), path = require('path');
const ts = require('../../web/node_modules/typescript');
const crypto = require('node:crypto');
const root = path.resolve(__dirname, '../../../../launchpad');
const client = path.join(root, 'client');
const packages = new Map();
for (const name of fs.readdirSync(path.join(client, 'packages'))) {
  const dir = path.join(client, 'packages', name), file = path.join(dir, 'package.json');
  if (fs.existsSync(file)) { const p = JSON.parse(fs.readFileSync(file)); packages.set(p.name, { ...p, dir, owner: 'L24 (SDK); serial integrator for manifest' }); }
}
const config = ts.readConfigFile(path.join(client, 'apps/web/tsconfig.json'), ts.sys.readFile);
const parsed = ts.parseJsonConfigFileContent(config.config, ts.sys, path.join(client, 'apps/web'));
function files(dir) { return fs.readdirSync(dir, { withFileTypes: true }).flatMap(e => ['node_modules', '.next', 'dist', '.cache'].includes(e.name) ? [] : e.isDirectory() ? files(path.join(dir, e.name)) : /\.[cm]?[jt]sx?$/.test(e.name) ? [path.join(dir, e.name)] : []); }
const allFiles = files(client);
const program = ts.createProgram(allFiles, parsed.options), checker = program.getTypeChecker();
const entries = [];
for (const source of program.getSourceFiles()) {
  if (source.fileName.includes('node_modules') || !source.fileName.startsWith(client)) continue;
  ts.forEachChild(source, node => {
    if (!ts.isImportDeclaration(node) || !ts.isStringLiteral(node.moduleSpecifier)) return;
    const module = node.moduleSpecifier.text;
    const pkg = packages.get(module.split('/').slice(0, module.startsWith('@') ? 2 : 1).join('/'));
    if (!pkg && !module.includes('wallet-sdk') && module !== '@tanstack/react-virtual' && !['paxifyui', 'sidiora-ui-tokens'].includes(module)) return;
    const symbol = checker.getSymbolAtLocation(node.moduleSpecifier);
    const exports = symbol ? checker.getExportsOfModule(symbol).map(s => s.name) : [];
    const clause = node.importClause; const requested = [];
    if (clause?.name) requested.push('default');
    if (clause?.namedBindings) { if (ts.isNamespaceImport(clause.namedBindings)) requested.push('*'); else for (const spec of clause.namedBindings.elements) requested.push((spec.propertyName || spec.name).text); }
    entries.push({ importer: path.relative(root, source.fileName), module, requested, exports, resolved: !!symbol, missing: requested.filter(n => n !== '*' && !exports.includes(n)), owner: pkg?.owner || (module.includes('wallet-sdk') ? 'L25 (external wallet)' : 'L29'), packageContract: pkg ? path.relative(root, path.join(pkg.dir, 'package.json')) : 'client/apps/web/src/core/wallet-sdk/index.tsx', versionPolicy: pkg ? 'workspace:*' : 'local source', exportEntry: pkg?.exports || pkg?.main || null });
  });
}
const result = { schemaVersion: 1, kind: 'source-export-audit', entries, missing: entries.filter(e => !e.resolved || e.missing.length), historical: { virtualizer: 'Removed undeclared @tanstack/react-virtual import in owned real bounded-list equivalent.', wallet: 'External Web3 SDK restored at baseline; actual exported symbols checked, no historical missing-SDK assumption.', paxifyui: 'No current web imports', sidioraUiTokens: 'No current web imports' }, packages: [...packages.values()].map(({ dir, ...p }) => ({ ...p, path: path.relative(root, dir) })) };
const lockfile = path.join(client, 'pnpm-lock.yaml');
result.provenance = { lockfile: 'client/pnpm-lock.yaml', sha256: crypto.createHash('sha256').update(fs.readFileSync(lockfile)).digest('hex'), packageManifests: [...packages.values()].map(pkg => ({ name: pkg.name, owner: pkg.owner, path: path.relative(root, path.join(pkg.dir, 'package.json')), sha256: crypto.createHash('sha256').update(fs.readFileSync(path.join(pkg.dir, 'package.json'))).digest('hex'), license: pkg.license ?? null, repository: pkg.repository ?? null })) };
const tokens = JSON.parse(fs.readFileSync(path.join(root, 'client/apps/web/src/core/app-foundation/design-tokens.json'), 'utf8'));
function luminance(hex) { const channels = hex.slice(1).match(/../g).map(channel => { const value = parseInt(channel, 16) / 255; return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4; }); return channels[0] * 0.2126 + channels[1] * 0.7152 + channels[2] * 0.0722; }
function ratio(a, b) { const left = luminance(a), right = luminance(b); return (Math.max(left, right) + 0.05) / (Math.min(left, right) + 0.05); }
const measurements = ['canvas', 'surface', 'raised'].flatMap(background => ['text', 'muted', 'focus'].map(foreground => ({ background, foreground, ratio: ratio(tokens.color[background], tokens.color[foreground]), required: foreground === 'focus' ? 3 : 4.5 })));
const phase2 = path.join(root, 'evidence/production-system/L29/phase2'); fs.mkdirSync(phase2, { recursive: true });
fs.writeFileSync(path.join(phase2, 'dependency-closure.json'), JSON.stringify(result, null, 2) + '\n');
fs.writeFileSync(path.join(phase2, 'contrast.json'), JSON.stringify({ schemaVersion: 1, tokenVersion: tokens.version, measurements }, null, 2) + '\n');
if (measurements.some(measurement => measurement.ratio < measurement.required)) throw new Error('Shell contrast bound failed');
const output = path.join(root, 'evidence/production-system/L29/dependency-closure.json');
fs.mkdirSync(path.dirname(output), { recursive: true });
fs.writeFileSync(output, JSON.stringify(result, null, 2) + '\n');
console.log(JSON.stringify({ imports: entries.length, missing: result.missing.length }));
if (result.missing.length) process.exitCode = 1;

const navigationPath = 'client/apps/web/src/core/app-foundation/navigation.ts';
const navigationBytes = fs.readFileSync(path.join(root, navigationPath));
const navigationSource = ts.createSourceFile(navigationPath, navigationBytes.toString('utf8'), ts.ScriptTarget.Latest, true);
let routeArray;
ts.forEachChild(navigationSource, node => {
  if (!ts.isVariableStatement(node)) return;
  for (const declaration of node.declarationList.declarations) {
    if (declaration.name.getText(navigationSource) !== 'routes') continue;
    let value = declaration.initializer;
    while (value && ts.isAsExpression(value)) value = value.expression;
    if (value && ts.isArrayLiteralExpression(value)) routeArray = value;
  }
});
if (!routeArray || !routeArray.elements.length) throw new Error('Actual navigation route array required');
const routeContract = routeArray.elements.map(element => {
  if (!ts.isObjectLiteralExpression(element)) throw new Error('Navigation contract must contain real route objects');
  const route = {};
  for (const property of element.properties) {
    if (!ts.isPropertyAssignment(property) || !ts.isStringLiteral(property.initializer)) throw new Error('Navigation route field is not a fixed string');
    route[property.name.getText(navigationSource)] = property.initializer.text;
  }
  for (const field of ['id', 'pathname', 'label', 'permission']) if (!route[field]) throw new Error('Navigation identity field missing');
  return route;
});
if (new Set(routeContract.map(route => route.id)).size !== routeContract.length) throw new Error('Duplicate navigation identity');
const selectedPhase = process.env.L29_QUALIFICATION_PHASE || 'phase2';
if (!['phase2', 'phase3', 'phase4'].includes(selectedPhase)) throw new Error('Unknown foundation qualification phase');
const phaseOutput = path.join(root, 'evidence/production-system/L29', selectedPhase);
fs.mkdirSync(phaseOutput, { recursive: true });
fs.writeFileSync(path.join(phaseOutput, 'route-contract.json'), JSON.stringify({ schemaVersion: 1, kind: 'source-route-contract', sourcePath: navigationPath, sourceSha256: crypto.createHash('sha256').update(navigationBytes).digest('hex'), routes: routeContract }, null, 2) + '\n');
