import assert from 'node:assert/strict';
import { mkdir, readFile, stat, writeFile } from 'node:fs/promises';
import { resolve, join } from 'node:path';
import { gzipSync } from 'node:zlib';
import { createHash } from 'node:crypto';
const required = name => { const value = process.env[name]; if (!value) throw new Error(`Required real staging input: ${name}`); return value; };
if (process.env.L29_QUALIFICATION_PHASE === 'phase4') {
  const { runReleaseHarness } = await import('./release-harness.mjs');
  await runReleaseHarness();
} else {
const origin = new URL(required('L29_STAGING_ORIGIN'));
assert.equal(origin.protocol, 'https:'); assert.equal(origin.pathname, '/'); assert.equal(origin.search, ''); assert.equal(origin.username, '');
const driver = new URL(required('L29_WEBDRIVER_ORIGIN'));
assert.ok(['http:', 'https:'].includes(driver.protocol));
const queryA = required('L29_QUERY_A'), queryB = required('L29_QUERY_B'), expectedAddress = required('L29_EXPECTED_B_ADDRESS').toLowerCase();
assert.match(expectedAddress, /^0x[0-9a-f]{40}$/);
const phase = process.env.L29_QUALIFICATION_PHASE || 'phase2'; assert.ok(['phase2', 'phase3'].includes(phase));
const output = resolve('launchpad/evidence/production-system/L29/' + phase); await mkdir(output, { recursive: true });
const observations = { schemaVersion: 1, origin: origin.origin, startedAt: new Date().toISOString(), checks: [], focusedChecksPassed: false };
let session;
async function request(path, method = 'GET', body) {
  const response = await fetch(new URL(path, driver), { method, headers: { 'Content-Type': 'application/json' }, ...(body === undefined ? {} : { body: JSON.stringify(body) }), signal: AbortSignal.timeout(15000) });
  const result = await response.json(); if (!response.ok || result.value?.error) throw new Error(`WebDriver failed ${response.status}: ${result.value?.error || 'invalid response'}`); return result.value;
}
const command = (path, method, body) => request(`/session/${session}${path}`, method, body);
const execute = script => command('/execute/sync', 'POST', { script, args: [] });
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
try {
  const capabilities = { browserName: 'chrome', 'goog:chromeOptions': { args: ['--headless=new', '--window-size=1440,900'] } };
  const created = await request('/session', 'POST', { capabilities: { alwaysMatch: capabilities } }); session = created.sessionId;
  await command('/url', 'POST', { url: origin.href });
  const anonymous = await fetch(origin, { redirect: 'error', signal: AbortSignal.timeout(15000) }); assert.equal(anonymous.status, 200);
  const html = await anonymous.text(); assert.ok(html.includes('main-content')); assert.ok(!/Bearer\s+[A-Za-z0-9._-]+/.test(html));
  observations.checks.push({ name: 'anonymous-public-html', status: anonymous.status, cacheControl: anonymous.headers.get('cache-control') });
  const search = await command('/element', 'POST', { using: 'css selector', value: 'input[role=combobox]' });
  const elementId = search['element-6066-11e4-a52e-4f735466cecf'];
  await command(`/element/${elementId}/value`, 'POST', { text: queryA }); await pause(350);
  await command(`/element/${elementId}/clear`, 'POST', {}); await command(`/element/${elementId}/value`, 'POST', { text: queryB });
  let text = '';
  for (let attempt = 0; attempt < 60; attempt++) { text = await execute('return document.querySelector("[role=listbox]")?.textContent || ""'); if (text.includes(expectedAddress.slice(2, 8))) break; await pause(250); }
  const identity = await execute('return Array.from(document.querySelectorAll("[role=option]")).map(el => ({identity:el.getAttribute("data-entity-identity"),text:el.textContent}))');
  assert.ok(identity.some(row => row.identity?.endsWith(expectedAddress)), 'Expected query B entity missing');
  await pause(2500);
  assert.deepEqual(await execute('return Array.from(document.querySelectorAll("[role=option]")).map(el => el.getAttribute("data-entity-identity"))'), identity.map(row => row.identity));
  observations.checks.push({ name: 'superseded-search-final-identity', identities: identity.map(row => row.identity) });
  for (const width of [320, 768, 1440]) {
    await command('/window/rect', 'POST', { width, height: 900 });
    const geometry = await execute('return {width:innerWidth,body:document.documentElement.scrollWidth,main:!!document.querySelector("main")}');
    assert.ok(geometry.main); assert.ok(geometry.body <= geometry.width, `Horizontal overflow at ${width}`);
    await writeFile(join(output, `shell-${width}.png`), Buffer.from(await command('/screenshot'), 'base64'));
    observations.checks.push({ name: `shell-width-${width}`, ...geometry });
  }
  if (phase === 'phase3') {
    const canary = 'qualification-credential-canary-' + createHash('sha256').update(origin.origin + observations.startedAt).digest('hex').slice(0, 16);
    const unknown = new URL('/unknown-shell-route-' + canary, origin);
    const unknownResponse = await fetch(unknown, { redirect: 'error', signal: AbortSignal.timeout(15000) });
    assert.equal(unknownResponse.status, 404, 'Unknown route must retain real404 semantics');
    await command('/url', 'POST', { url: unknown.href });
    let recoverable = false;
    for (let attempt = 0; attempt < 40; attempt++) { recoverable = await execute('return Array.from(document.querySelectorAll("main a")).some(link => link.getAttribute("href") === "/")'); if (recoverable) break; await pause(250); }
    assert.ok(recoverable, 'Unknown route requires a usable discovery action');
    observations.checks.push({ name: 'unknown-route-recovery', status: unknownResponse.status });
    await command('/url', 'POST', { url: origin.href });
    await command('/execute/sync', 'POST', { script: 'localStorage.setItem("sidiora_recent_viewed", JSON.stringify([{address:"https://attacker.invalid",name:arguments[0],symbol:"BAD",logo:null,marketCap:"0",ts:Date.now(),access_token:arguments[0]}])); localStorage.setItem("kindle:draft:v1:launch",JSON.stringify({version:999,createdAt:Date.now(),expiresAt:Date.now()+86400000,deploymentVersion:arguments[0],chainId:1,payload:{signature:arguments[0]}})); return true;', args: [canary] });
    await command('/refresh', 'POST', {}); await pause(1000);
    const recentState = await execute('return {removed:localStorage.getItem("sidiora_recent_viewed")===null,text:document.querySelector("main")?.textContent || "",html:document.documentElement.outerHTML}');
    assert.ok(recentState.removed, 'Malformed recent persistence must be removed'); assert.ok(!recentState.html.includes(canary), 'Persisted credential canary entered public rendering');
    const anonymousReload = await fetch(origin, { redirect: 'error', signal: AbortSignal.timeout(15000) }); const publicHTML = await anonymousReload.text(); assert.ok(!publicHTML.includes(canary));
    observations.checks.push({ name: 'persisted-canary-excluded-from-public-html', canarySha256: createHash('sha256').update(canary).digest('hex'), recentRemoved: recentState.removed });
    await command('/execute/sync', 'POST', { script: 'localStorage.removeItem("kindle:draft:v1:launch"); return true;', args: [] });
  }
  const buildRoot = resolve('apps/launchpad/web/.next');
  const manifest = JSON.parse(await readFile(join(buildRoot, 'app-build-manifest.json'), 'utf8'));
  const paths = [...new Set(manifest.pages['/layout'] || [])]; assert.ok(paths.length, 'Actual shell build manifest required');
  const chunks = await Promise.all(paths.filter(path => path.endsWith('.js')).map(async path => ({ path, compressedBytes: gzipSync(await readFile(join(buildRoot, path))).length, bytes: (await stat(join(buildRoot, path))).size })));
  const compressedBytes = chunks.reduce((sum, chunk) => sum + chunk.compressedBytes, 0);
  await writeFile(join(output, 'bundle-budget.json'), JSON.stringify({ schemaVersion: 1, source: 'actual optimized Next build layout entry', compressedBytes, limitBytes: 180 * 1024, chunks }, null, 2));
  assert.ok(compressedBytes <= 180 * 1024, 'Shell exceeds 180 KiB compressed budget');
  observations.checks.push({ name: 'shell-bundle-budget', compressedBytes });
  observations.focusedChecksPassed = true;
} finally {
  observations.finishedAt = new Date().toISOString();
  await writeFile(join(output, 'observed-journeys.json'), JSON.stringify(observations, null, 2));
  if (session) await command('', 'DELETE');
}

}
