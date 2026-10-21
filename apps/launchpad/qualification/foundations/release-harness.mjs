import assert from 'node:assert/strict';
import { readFile, writeFile, mkdir, realpath } from 'node:fs/promises';
import { resolve, relative, join, sep } from 'node:path';
import { createHash } from 'node:crypto';
import { gzipSync } from 'node:zlib';
import { performance } from 'node:perf_hooks';

const hash = bytes => createHash('sha256').update(bytes).digest('hex');
const digest = value => assert.match(value, /^[a-f0-9]{64}$/, 'A real SHA256 is required');
const integer = (value, min, max, label) => assert.ok(Number.isSafeInteger(value) && value >= min && value <= max, label);
const required = name => { const value = process.env[name]; assert.ok(value, `Required release input: ${name}`); return value; };
export function percentile(samples, quantile) {
  assert.ok(samples.length && samples.every(value => Number.isFinite(value) && value >= 0), 'Missing or invalid measured samples');
  assert.ok(quantile > 0 && quantile <= 1);
  return [...samples].sort((a, b) => a - b)[Math.ceil(samples.length * quantile) - 1];
}
function checkedOrigin(value, protocols) {
  const url = new URL(value);
  assert.ok(protocols.includes(url.protocol) && url.origin === value && !url.username && !url.password, 'Exact approved origin required');
  return url;
}
function checkedPath(value) {
  assert.ok(typeof value === 'string' && value.startsWith('/') && !value.startsWith('//') && !/[\\\u0000-\u0020]/.test(value), 'Canonical local route required');
  const url = new URL(value, 'https://route.invalid');
  assert.equal(url.origin, 'https://route.invalid');
  assert.equal(url.pathname + url.search, value);
  assert.equal(url.hash, '');
  return value;
}
async function boundFile(root, reference) {
  assert.ok(reference && typeof reference.path === 'string'); digest(reference.sha256);
  assert.ok(!/(?:^|\/)\.env(?:\.[^/]+)?(?:\/|$)|\.(?:pem|key|p12|pfx)$/i.test(reference.path), 'Secret-bearing files cannot enter release artifacts');
  const file = await realpath(resolve(root, reference.path));
  const rel = relative(await realpath(root), file);
  assert.ok(rel && !rel.startsWith('..' + sep) && rel !== '..' && !rel.startsWith(sep), 'Artifact escapes declared root');
  const bytes = await readFile(file);
  assert.equal(hash(bytes), reference.sha256, 'Artifact digest mismatch');
  return { bytes, path: file };
}
async function checkedObservedEvidence(root, reference, input, requirementId) {
  const { bytes } = await boundFile(root, reference);
  const record = JSON.parse(bytes);
  assert.equal(record.result, 'passed');
  if (requirementId) assert.ok(Array.isArray(record.requirements) && record.requirements.includes(requirementId), 'Observed evidence does not cover the cited criterion');
  assert.equal(record.sourceOnly, false, 'Source assertions cannot be release evidence');
  assert.equal(record.candidateRevision, input.candidateRevision);
  assert.equal(record.artifactDigest, input.artifactDigest);
  assert.equal(record.exitCode, 0);
  assert.ok(typeof record.command === 'string' && record.command.length > 0);
  assert.ok(Number.isFinite(Date.parse(record.startedAt)) && Date.parse(record.finishedAt) >= Date.parse(record.startedAt));
  assert.ok(Array.isArray(record.rawArtifacts) && record.rawArtifacts.length > 0, 'Observed raw artifacts required');
  for (const artifact of record.rawArtifacts) await boundFile(root, artifact);
  return { sha256: reference.sha256, command: record.command, exitCode: record.exitCode, startedAt: record.startedAt, finishedAt: record.finishedAt, rawArtifacts: record.rawArtifacts.map(value => ({ path: value.path, sha256: value.sha256 })) };
}
async function bundleMeasurement(root, input, output, retain) {
  const buildRoot = await realpath(resolve(root, input.buildRoot));
  assert.ok(relative(await realpath(root), buildRoot).startsWith('apps/launchpad/web/'), 'Build must belong to the candidate web source');
  const buildId = (await readFile(join(buildRoot, 'BUILD_ID'), 'utf8')).trim();
  assert.equal(buildId, input.buildId);
  const appBytes = await readFile(join(buildRoot, 'app-build-manifest.json'));
  const mainBytes = await readFile(join(buildRoot, 'build-manifest.json'));
  const app = JSON.parse(appBytes), main = JSON.parse(mainBytes);
  assert.ok(Array.isArray(app.pages?.['/layout']) && app.pages['/layout'].length, 'Actual optimized app layout manifest required');
  const shell = [...new Set([...app.pages['/layout'], ...(main.rootMainFiles ?? []), ...(main.polyfillFiles ?? [])])].filter(path => path.endsWith('.js'));
  const deferred = input.deferredChunks;
  assert.ok(Array.isArray(deferred) && deferred.length > 0);
  for (const family of ['chart', 'chat', 'video', 'wallet']) assert.ok(deferred.some(value => value.family === family), `Missing explicit ${family} vendor attribution`);
  for (const entry of deferred) { assert.ok(typeof entry.owner === 'string' && entry.owner && entry.interaction === true); assert.ok(!shell.includes(entry.path), 'Initial shell vendor code cannot be excluded as deferred'); }
  const paths = [...new Set([...shell, ...Object.values(app.pages).flat().filter(path => path.endsWith('.js')), ...deferred.map(value => value.path)])];
  const chunks = [];
  for (const path of paths) {
    assert.ok(path.startsWith('static/') && !path.includes('..') && !path.includes('\\'));
    const real = await realpath(join(buildRoot, path)); assert.ok(relative(buildRoot, real).startsWith('static/'));
    const bytes = await readFile(real);
    chunks.push({ path, sha256: hash(bytes), bytes: bytes.length, gzipBytes: gzipSync(bytes).length, initialShell: shell.includes(path), deferredFamily: deferred.find(value => value.path === path)?.family ?? null });
  }
  const compressedBytes = chunks.filter(value => value.initialShell).reduce((sum, value) => sum + value.gzipBytes, 0);
  const routes = Object.entries(app.pages).map(([route, entries]) => ({ route, gzipBytes: [...new Set(entries)].reduce((sum, path) => sum + (chunks.find(value => value.path === path)?.gzipBytes ?? 0), 0) }));
  const result = { buildId, appManifestSha256: hash(appBytes), mainManifestSha256: hash(mainBytes), compression: 'gzip default level6 per exact emitted JavaScript chunk', limitBytes: 180 * 1024, compressedBytes, shell, chunks, routes };
  await retain('bundle-budget.json', result);
  assert.ok(compressedBytes <= 180 * 1024, 'Shell exceeds180KiB compressed JavaScript');
  return result;
}
const driverElementKey = 'element-6066-11e4-a52e-4f735466cecf';
const keyCodes = { Tab: '\uE004', Escape: '\uE00C', Enter: '\uE007', ArrowDown: '\uE015', ArrowUp: '\uE013' };
const installationScript = `
const shell=document.querySelector('[data-shell-pathname]'); if(!shell)throw new Error('SHELL_MEASUREMENT_HOOK_REQUIRED');
const regions=arguments[0];if(!Array.isArray(regions)||!regions.length)throw new Error('REAL_MARKET_ROW_REGIONS_REQUIRED');
window.__l29Measurements={transitions:[],pending:null,rowMaximum:0}; const store=window.__l29Measurements;
const rows=()=>{const counts=[...Array.from(document.querySelectorAll('[data-foundation-bounded-list]')).map(list=>list.querySelectorAll('[role=listitem]').length),...regions.flatMap(region=>Array.from(document.querySelectorAll(region.selector)).map(list=>list.querySelectorAll(region.rowSelector).length))];store.rowMaximum=Math.max(store.rowMaximum,...counts);return counts;};
document.addEventListener('click',event=>{const a=event.target instanceof Element?event.target.closest('a[href]'):null;if(!a)return;const u=new URL(a.href);if(u.origin===location.origin&&u.pathname!==location.pathname)store.pending={startedAt:performance.now(),target:u.pathname};},true);
new MutationObserver(()=>{rows();if(store.pending&&shell.dataset.shellPathname===store.pending.target){const pending=store.pending;store.pending=null;requestAnimationFrame(()=>requestAnimationFrame(()=>{if(store.transitions.length>=512)throw new Error('MEASUREMENT_BUFFER_LIMIT');store.transitions.push({durationMs:performance.now()-pending.startedAt,rows:rows(),routeId:shell.dataset.shellRouteId});}));}}).observe(shell,{subtree:true,childList:true,attributes:true,attributeFilter:['data-shell-pathname']});
rows();return true;`;

async function measureBrowser(root, input, profile, bundle, retain) {
  integer(profile.device?.logicalProcessors, 1, 512, 'Declared reference CPU capacity required');
  integer(profile.device?.memoryMiB, 1, 1048576, 'Declared reference memory required');
  assert.ok(profile.id && profile.browserName && profile.browserVersion && profile.device?.hardware && profile.device?.os, 'Declared browser/device identity required');
  checkedOrigin(profile.driverOrigin, ['http:', 'https:']);
  assert.equal(profile.syntheticQualificationAccounts, true, 'Only declared qualification accounts may enter artifacts');
  await checkedObservedEvidence(root, profile.deviceEvidence, input);
  const probe = await boundFile(root, profile.runtimeProbe);
  const axe = await boundFile(root, input.axe);
  assert.ok(Array.isArray(profile.runtimeProbe.providerSources) && profile.runtimeProbe.providerSources.length > 0, 'Runtime probe must bind its real provider implementations');
  for (const source of profile.runtimeProbe.providerSources) await boundFile(root, source);
  assert.ok(input.axe.version && input.axe.license && input.axe.provenance, 'Pinned accessibility license/provenance required');
  const capture = { profileId: profile.id, device: profile.device, browserName: profile.browserName, browserVersion: profile.browserVersion, actualCapabilities: null, runtimeProbeSha256: profile.runtimeProbe.sha256, axeSha256: input.axe.sha256, routeVisits: [], samples: [], accessibility: [], transitions: [], passed: false };
  let session;
  async function request(path, method = 'GET', body) {
    const response = await fetch(new URL(path, profile.driverOrigin), { method, headers: { 'Content-Type': 'application/json' }, ...(body === undefined ? {} : { body: JSON.stringify(body) }), signal: AbortSignal.timeout(20000), redirect: 'error' });
    const record = await response.json();
    assert.ok(response.ok && !record.value?.error, `WebDriver request failed:${response.status}`);
    return record.value;
  }
  const command = (path, method, body) => request(`/session/${session}${path}`, method, body);
  const execute = (script, args = []) => command('/execute/sync', 'POST', { script, args });
  const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
  const element = async selector => { const matches = await command('/elements', 'POST', { using: 'css selector', value: selector }); for (const match of matches) { const id = match[driverElementKey]; if (await command(`/element/${id}/displayed`)) return id; } assert.fail('Visible real interaction element unavailable'); };
  async function waitFor(selector) { const started = performance.now(); for (;;) { if (await execute('return Array.from(document.querySelectorAll(arguments[0])).some(el=>el.getClientRects().length>0 && !el.closest("[hidden],[inert]") && getComputedStyle(el).visibility!=="hidden")', [selector])) return; assert.ok(performance.now() - started < 15000, 'Browser readiness timed out'); await pause(100); } }
  async function snapshot(label, requireHot = false) {
    const measured = await execute(probe.bytes.toString('utf8'));
    assert.equal(measured?.schemaVersion, 1); assert.equal(measured.source, 'actual-provider-registry');
    assert.ok(['browser-performance-memory', 'cdp-js-heap', 'platform-heap-profiler'].includes(measured.heapSource), 'Actual heap measurement source required');
    integer(measured.heapBytes, 1, Number.MAX_SAFE_INTEGER, 'Actual heap bytes required');
    integer(measured.activeConnections, 0, 100, 'Invalid active connection count'); integer(measured.subscriptions, 0, 10000, 'Invalid real subscription count');
    assert.ok(Array.isArray(measured.topics) && new Set(measured.topics.map(topic => topic.id)).size === measured.topics.length);
    assert.ok(measured.topics.length <= 20, 'Browser exceeds20topic limit');
    if (requireHot) { assert.equal(measured.state, 'live'); assert.equal(measured.activeConnections, 1); assert.deepEqual(measured.topics.map(topic => topic.id).sort(), [...input.hotTopics].sort()); }
    for (const topic of measured.topics) { assert.ok(typeof topic.id === 'string' && topic.id.startsWith('market.')); integer(topic.observedEvents, 0, Number.MAX_SAFE_INTEGER, 'Invalid observed event count'); integer(topic.appliedEvents, 0, Number.MAX_SAFE_INTEGER, 'Invalid applied event count'); assert.ok(topic.appliedEvents <= topic.observedEvents); }
    const dom = await execute('return {rows:[...Array.from(document.querySelectorAll("[data-foundation-bounded-list]")).map(list=>list.querySelectorAll("[role=listitem]").length),...arguments[0].flatMap(region=>Array.from(document.querySelectorAll(region.selector)).map(list=>list.querySelectorAll(region.rowSelector).length))],width:innerWidth,overflow:document.documentElement.scrollWidth,routeId:document.querySelector("[data-shell-route-id]")?.dataset.shellRouteId}', [input.rowRegions]);
    assert.ok(dom.rows.every(count => count <= 200), 'More than200rows rendered'); assert.ok(dom.overflow <= dom.width, 'Horizontal viewport overflow');
    const value = { label, at: new Date().toISOString(), heapBytes: measured.heapBytes, heapSource: measured.heapSource, activeConnections: measured.activeConnections, subscriptions: measured.subscriptions, state: measured.state, topics: measured.topics.map(topic => ({ identitySha256: hash(topic.id), observedEvents: topic.observedEvents, appliedEvents: topic.appliedEvents })), ...dom };
    capture.samples.push(value); return { value, measured };
  }
  async function accessibility(label) {
    await execute(axe.bytes.toString('utf8'));
    assert.equal(await execute('return window.axe?.version'), input.axe.version);
    const result = await command('/execute/async', 'POST', { script: 'const done=arguments[arguments.length-1]; window.axe.run(document,{runOnly:{type:"tag",values:["wcag2a","wcag2aa","wcag21aa","wcag22aa"]}}).then(value=>done({violations:value.violations.map(v=>({id:v.id,impact:v.impact,nodes:v.nodes.map(n=>({target:n.target,failureSummary:n.failureSummary}))})),incomplete:value.incomplete.map(v=>({id:v.id,nodes:v.nodes.length})),passes:value.passes.length})).catch(()=>done({error:"ACCESSIBILITY_ENGINE_FAILED"}));', args: [] });
    assert.ok(!result.error); capture.accessibility.push({ label, ...result }); assert.equal(result.violations.length, 0, 'WCAG2.2AA violation'); assert.equal(result.incomplete.length, 0, 'Accessibility incomplete checks require manual resolution');
  }
  async function steps(actions, label) {
    assert.ok(Array.isArray(actions) && actions.length > 0 && actions.length <= 200);
    for (const action of actions) {
      const started = performance.now();
      let requestedPathname;
      if (action.routeId) { assert.ok(['navigate', 'click'].includes(action.type), 'Route coverage requires actual navigation'); if (action.type === 'navigate') requestedPathname = new URL(input.origin + checkedPath(action.pathname)).pathname; else { const href = await command(`/element/${await element(action.selector)}/attribute/href`); requestedPathname = new URL(href, input.origin).pathname; } }
      if (action.type === 'navigate') { await command('/url', 'POST', { url: input.origin + checkedPath(action.pathname) }); await waitFor(action.readySelector); }
      else if (action.type === 'click') { await command(`/element/${await element(action.selector)}/click`, 'POST', {}); if (action.readySelector) await waitFor(action.readySelector); }
      else if (action.type === 'type') { await command(`/element/${await element(action.selector)}/clear`, 'POST', {}); await command(`/element/${await element(action.selector)}/value`, 'POST', { text: action.value }); }
      else if (action.type === 'key') { assert.ok(keyCodes[action.key]); await command(`/element/${await element(action.selector)}/value`, 'POST', { text: keyCodes[action.key] }); }
      else if (action.type === 'back' || action.type === 'forward') { await command('/' + action.type, 'POST', {}); await waitFor(action.readySelector); }
      else if (action.type === 'wait') { await waitFor(action.selector); }
      else if (action.type === 'assert') {
        const observed = await execute('const el=document.querySelector(arguments[0]);return {exists:!!el,text:el?.textContent||"",attribute:el?.getAttribute(arguments[1]),focused:el===document.activeElement,path:location.pathname+location.search,count:document.querySelectorAll(arguments[0]).length}', [action.selector, action.attribute ?? 'aria-current']);
        if (action.exists !== undefined) assert.equal(observed.exists, action.exists);
        if (action.textIncludes !== undefined) assert.ok(observed.text.includes(action.textIncludes));
        if (action.attributeEquals !== undefined) assert.equal(observed.attribute, action.attributeEquals);
        if (action.focused !== undefined) assert.equal(observed.focused, action.focused);
        if (action.pathname !== undefined) assert.equal(observed.path, checkedPath(action.pathname));
        if (action.maxCount !== undefined) assert.ok(observed.count <= action.maxCount);
      } else assert.fail('Unsupported real browser action');
      if (action.routeId) { const route = input.observedRouteContract.routes.find(route => route.id === action.routeId); assert.ok(route && (requestedPathname === route.pathname || route.pathname !== '/' && requestedPathname.startsWith(route.pathname + '/')), 'Requested route disagrees with frozen identity'); const renderedRoute = action.renderedRouteId || action.routeId; assert.ok(input.observedRouteContract.routes.some(route => route.id === renderedRoute)); await waitFor('[data-shell-route-id="' + renderedRoute + '"]'); capture.routeVisits.push(action.routeId); }
      capture.samples.push({ label, action: action.type, durationMs: performance.now() - started, observed: true });
    }
  }
  try {
    const created = await request('/session', 'POST', { capabilities: { alwaysMatch: profile.capabilities } }); session = created.sessionId;
    assert.ok(session); capture.actualCapabilities = { browserName: created.capabilities.browserName, browserVersion: created.capabilities.browserVersion, platformName: created.capabilities.platformName };
    const hardware = await execute('return {logicalProcessors:navigator.hardwareConcurrency,reportedMemoryGiB:navigator.deviceMemory??null,userAgent:navigator.userAgent}'); assert.equal(hardware.logicalProcessors, profile.device.logicalProcessors); capture.browserHardware = { logicalProcessors: hardware.logicalProcessors, reportedMemoryGiB: hardware.reportedMemoryGiB, userAgentSha256: hash(hardware.userAgent) };
    assert.equal(created.capabilities.browserName, profile.browserName); assert.equal(created.capabilities.browserVersion, profile.browserVersion);
    await command('/timeouts', 'POST', { script: 20000, pageLoad: 20000 });
    await command('/url', 'POST', { url: input.origin + checkedPath(input.publicRoute.pathname) }); await waitFor(input.publicRoute.readySelector);
    await execute(installationScript, [input.rowRegions]);
    const resources = await execute('return performance.getEntriesByType("resource").filter(entry=>entry.initiatorType==="script").map(entry=>{const url=new URL(entry.name);return {path:url.pathname,origin:url.origin};})');
    assert.ok(resources.length > 0, 'Actual initial script resource trace required');
    for (const deferred of input.deferredChunks) assert.ok(!resources.some(value => value.path === '/_next/' + deferred.path), 'Deferred chart/chat/video/wallet chunk loaded before interaction');
    assert.ok(resources.every(value => input.assetOrigins.includes(value.origin) && bundle.chunks.some(chunk => '/_next/' + chunk.path === value.path)), 'Initial JavaScript asset lacks declared origin or bundle attribution');
    capture.samples.push({ label: 'independent-public-shell', initialScripts: resources.map(value => ({ path: value.path, origin: value.origin, declared: bundle.chunks.some(chunk => '/_next/' + chunk.path === value.path) })) });
    await accessibility('public-shell');
    assert.equal(typeof profile.setWindowRect, 'boolean', 'Declared device resize capability required'); if (typeof created.capabilities.setWindowRect === 'boolean') assert.equal(created.capabilities.setWindowRect, profile.setWindowRect);
    for (const viewport of profile.setWindowRect ? [320, 375, 390, 768, 1024, 1440] : [profile.viewport.width]) { if (profile.setWindowRect) { await command('/window/rect', 'POST', { width: viewport, height: 900 }); } const geometry = await execute('return {width:innerWidth,overflow:document.documentElement.scrollWidth,zoom:devicePixelRatio,main:!!document.querySelector("main")}'); assert.ok(geometry.main && geometry.overflow <= geometry.width); assert.equal(geometry.width, viewport); capture.samples.push({ label: 'viewport', ...geometry }); }
    if (profile.setWindowRect) await command('/window/rect', 'POST', { width: profile.viewport.width, height: profile.viewport.height });
    for (const group of ['navigation', 'search', 'back', 'error', 'cache-isolation', 'keyboard']) { assert.ok(input.corpus[group]); await steps(input.corpus[group], group); await accessibility(group); }
    assert.ok(input.search && typeof input.search.queryA === 'string' && typeof input.search.queryB === 'string' && input.search.queryA !== input.search.queryB);
    assert.match(input.search.expectedBAddress, /^0x[0-9a-f]{40}$/);
    await command('/url', 'POST', { url: input.origin + checkedPath(input.publicRoute.pathname) }); await waitFor(input.publicRoute.readySelector);
    const mobileSearch = await execute('return innerWidth<640');
    if (mobileSearch) await command(`/element/${await element('button[aria-label="Search tokens"]')}/click`, 'POST', {});
    await waitFor('input[role=combobox]');
    const searchElement = await element('input[role=combobox]');
    await command(`/element/${searchElement}/value`, 'POST', { text: input.search.queryA }); await pause(350);
    await command(`/element/${searchElement}/clear`, 'POST', {}); await command(`/element/${searchElement}/value`, 'POST', { text: input.search.queryB });
    await waitFor('[data-entity-identity$="' + input.search.expectedBAddress + '"]');
    const latestIdentities = await execute('return Array.from(document.querySelectorAll("[role=option]")).map(el=>el.getAttribute("data-entity-identity"))');
    assert.ok(latestIdentities.length > 0 && latestIdentities.length <= 20); assert.ok(latestIdentities.every(identity => /^125:(?:token|pool):0x[0-9a-f]{40}$/.test(identity)));
    await pause(2500); assert.deepEqual(await execute('return Array.from(document.querySelectorAll("[role=option]")).map(el=>el.getAttribute("data-entity-identity"))'), latestIdentities);
    await command(`/element/${searchElement}/value`, 'POST', { text: keyCodes.ArrowDown });
    const keyboardSelection = await execute('const input=document.activeElement;const selected=document.getElementById(input.getAttribute("aria-activedescendant"));return {present:!!selected,identity:selected?.getAttribute("data-entity-identity"),selected:selected?.getAttribute("aria-selected")};');
    assert.ok(keyboardSelection.present); assert.equal(keyboardSelection.selected, 'true'); assert.ok(latestIdentities.includes(keyboardSelection.identity));
    await command(`/element/${searchElement}/value`, 'POST', { text: keyCodes.Escape });
    if (mobileSearch) { const returned = await execute('return {dialog:Array.from(document.querySelectorAll("[role=dialog]")).some(el=>el.getAttribute("aria-label")=="Search tokens"),opener:document.activeElement?.getAttribute("aria-label")}'); assert.equal(returned.dialog, false); assert.equal(returned.opener, 'Search tokens'); } else { const searchClosed = await command(`/element/${searchElement}/attribute/aria-expanded`); const searchFocused = await execute('return document.activeElement?.getAttribute("role")=="combobox"'); assert.equal(searchClosed, 'false'); assert.equal(searchFocused, true); }
    capture.samples.push({ label: 'superseded-search-and-keyboard', identities: latestIdentities.map(identity => hash(identity)), keyboardSelectionIdentitySha256: hash(keyboardSelection.identity), closedAndFocusRestored: true });
    for (const route of input.observedRouteContract.routes) assert.ok(capture.routeVisits.includes(route.id), 'Navigation corpus omitted a frozen destination');
    assert.ok([...input.routeCycle.mount, ...input.routeCycle.unmount].every(action => action.type !== 'navigate'), 'Route cycles must use actual client navigation without reloading the document');
    await command('/url', 'POST', { url: input.origin + checkedPath(input.publicRoute.pathname) }); await waitFor(input.publicRoute.readySelector); await execute(installationScript, [input.rowRegions]);
    await steps(input.routeCycle.mount, 'warmup-mount'); await steps(input.routeCycle.unmount, 'warmup-unmount');
    const baseline = (await snapshot('route-baseline')).value;
    for (let cycle = 0; cycle < 20; cycle++) { await steps(input.routeCycle.mount, `mount-${cycle}`); await snapshot(`mounted-${cycle}`); await steps(input.routeCycle.unmount, `unmount-${cycle}`); const observed = (await snapshot(`retained-${cycle}`)).value; assert.equal(observed.activeConnections, baseline.activeConnections, 'Retained connection leak'); assert.equal(observed.subscriptions, baseline.subscriptions, 'Retained subscription leak'); assert.deepEqual(observed.topics.map(value => value.identitySha256).sort(), baseline.topics.map(value => value.identitySha256).sort(), 'Retained topic leak'); }
    const last = capture.samples.findLast(value => value.label === 'retained-19'); assert.ok(last.heapBytes <= baseline.heapBytes * 1.1, 'Retained heap grows beyond10percent after20cycles');
    const transitionState = await execute('return window.__l29Measurements'); assert.ok(transitionState.transitions.length >= 20, 'Warm client-route transition samples missing'); capture.transitions = transitionState.transitions;
    const navigationP75Ms = percentile(capture.transitions.map(value => value.durationMs), 0.75); assert.ok(navigationP75Ms <= 500, 'Warm navigation p75 exceeds500ms'); assert.ok(transitionState.rowMaximum > 0 && transitionState.rowMaximum <= 200, 'Actual market row observations required');
    await steps(input.hotRoute.actions, 'five-hot-topics');
    const hotBefore = await snapshot('hot-before', true); const hotStarted = performance.now();
    while (performance.now() - hotStarted < input.profileDurationMs) { await pause(1000); await snapshot('hot-sample', true); }
    const hotAfter = await snapshot('hot-after', true);
    for (const topic of hotAfter.measured.topics) { const before = hotBefore.measured.topics.find(value => value.id === topic.id); assert.ok(topic.observedEvents > before.observedEvents, 'Required hot topic produced no real events'); if (topic.id.startsWith('market.price.')) assert.ok(topic.appliedEvents - before.appliedEvents <= Math.ceil((performance.now() - hotStarted) / 1000) * 2 + 1, 'Price application exceeds2Hz/topic'); }
    await accessibility('five-hot-topics'); capture.summary = { navigationP75Ms, routeCycles: 20, rowMaximum: transitionState.rowMaximum, retainedHeapRatio: last.heapBytes / baseline.heapBytes, observedHotTopicCount: hotAfter.measured.topics.length, profileDurationMs: performance.now() - hotStarted }; capture.passed = true;
  } finally { await retain(`browser-${profile.id}.json`, capture); if (session) await command('', 'DELETE'); }
  return capture.summary;
}

export async function runReleaseHarness() {
  const root = await realpath(process.cwd());
  const inputPath = required('L29_RELEASE_INPUT'); const expected = required('L29_RELEASE_INPUT_SHA256'); digest(expected);
  const inputBytes = await readFile(inputPath); assert.equal(hash(inputBytes), expected); const input = JSON.parse(inputBytes);
  const output = resolve(root, 'launchpad/evidence/production-system/L29/phase4'); await mkdir(output, { recursive: true });
  const manifest = { schemaVersion: 1, task: '4.29', inputSha256: expected, candidateRevision: input.candidateRevision, artifactDigest: input.artifactDigest, startedAt: new Date().toISOString(), artifacts: [], checks: [], laneMeasurementsPassed: false, finalSharedAcceptance: 'deferred-to-serial9.1' };
  async function retain(name, data) { assert.match(name, /^[a-zA-Z0-9._-]+\.json$/); const bytes = Buffer.from(JSON.stringify(data, null, 2) + '\n'); await writeFile(join(output, name), bytes); const entry = { path: `launchpad/evidence/production-system/L29/phase4/${name}`, sha256: hash(bytes), bytes: bytes.length }; manifest.artifacts = manifest.artifacts.filter(value => value.path !== entry.path); manifest.artifacts.push(entry); }
  try {
    assert.equal(input.schemaVersion, 1); assert.match(input.candidateRevision, /^[a-f0-9]{40,64}$/); digest(input.artifactDigest); checkedOrigin(input.origin, ['https:']);
    assert.ok(Array.isArray(input.assetOrigins) && input.assetOrigins.length > 0); for (const assetOrigin of input.assetOrigins) checkedOrigin(assetOrigin, ['https:']);
    assert.ok(input.referenceRegion && input.backendRevision && input.operatorAuthority, 'Approved reference region/backend/operator authority required');
    assert.ok(Array.isArray(input.hotTopics) && input.hotTopics.length === 5 && new Set(input.hotTopics).size === 5 && input.hotTopics.every(value => /^market\.[a-z-]+\./.test(value)));
    assert.ok(Array.isArray(input.rowRegions) && input.rowRegions.length > 0 && input.rowRegions.every(region => typeof region.selector === 'string' && region.selector && typeof region.rowSelector === 'string' && region.rowSelector), 'Declared actual market list selectors required');
    integer(input.profileDurationMs, 60000, 300000, 'Real hot-topic profile interval required');
    const sourceManifest = await boundFile(root, input.sourceManifest); const source = JSON.parse(sourceManifest.bytes); assert.equal(source.candidateRevision, input.candidateRevision); assert.ok(source.files.length > 0); for (const file of source.files) await boundFile(root, file);
    const artifacts = await boundFile(root, input.artifactManifest); assert.equal(hash(artifacts.bytes), input.artifactDigest); const artifactRecord = JSON.parse(artifacts.bytes); assert.equal(artifactRecord.candidateRevision, input.candidateRevision); assert.equal(artifactRecord.buildMode, 'production'); assert.equal(artifactRecord.sourceManifestSha256, input.sourceManifest.sha256); assert.ok(artifactRecord.files.length); for (const file of artifactRecord.files) await boundFile(root, file);
    const routeReference = await boundFile(root, input.routeContract); input.observedRouteContract = JSON.parse(routeReference.bytes); assert.equal(input.observedRouteContract.schemaVersion, 1); assert.ok(input.observedRouteContract.routes.length > 0); await boundFile(root, { path: input.observedRouteContract.sourcePath, sha256: input.observedRouteContract.sourceSha256 });
    manifest.inputs = ['sourceManifest', 'artifactManifest', 'routeContract', 'axe'].map(name => ({ kind: name, path: input[name].path, sha256: input[name].sha256 }));
    for (const kind of ['dependencyInstall', 'optimizedBuild', 'manualAccessibility', 'cacheIsolation', 'errorControls']) manifest.checks.push({ kind, observed: await checkedObservedEvidence(root, input[kind], input) });
    const clauses = Array.from({ length: 9 }, (_, index) => Array.from({ length: 5 }, (_, clause) => `D10_${String(index + 1).padStart(3, '0')}.${clause + 1}`)).flat();
    assert.ok(Array.isArray(input.requirementEvidence) && input.requirementEvidence.length === clauses.length);
    assert.equal(new Set(input.requirementEvidence.map(value => value.requirementId)).size, clauses.length);
    for (const clause of clauses) { const evidence = input.requirementEvidence.find(value => value.requirementId === clause); assert.ok(evidence, `Missing observed requirement:${clause}`); manifest.checks.push({ requirementId: clause, observed: await checkedObservedEvidence(root, evidence, input, clause) }); }
    const bundle = await bundleMeasurement(root, input, output, retain); manifest.checks.push({ kind: 'shell-budget', compressedBytes: bundle.compressedBytes });
    assert.ok(Array.isArray(input.browsers) && input.browsers.length >= 5);
    for (const id of ['chrome', 'firefox', 'safari', 'android-chrome', 'ios-safari']) assert.ok(input.browsers.some(profile => profile.id === id), `Declared browser matrix missing:${id}`);
    assert.equal(new Set(input.browsers.map(profile => profile.id)).size, input.browsers.length); assert.ok(input.browsers.some(profile => profile.device.referenceMidrange === true));
    for (const profile of input.browsers) { assert.match(profile.id, /^[a-z0-9-]+$/); integer(profile.viewport.width, 320, 4096, 'Declared viewport required'); integer(profile.viewport.height, 320, 4096, 'Declared viewport required'); manifest.checks.push({ kind: 'browser-measurement', browser: profile.id, result: await measureBrowser(root, input, profile, bundle, retain) }); }
    for (let sample = 0; sample < 20; sample++) { const started = performance.now(); const response = await fetch(input.origin + checkedPath(input.publicRoute.pathname), { redirect: 'error', credentials: 'omit', signal: AbortSignal.timeout(15000) }); const ttfbMs = performance.now() - started; assert.equal(response.status, 200); const html = await response.text(); const cacheControl = response.headers.get('cache-control'); assert.ok(cacheControl && /(?:public|s-maxage)/i.test(cacheControl) && !/(?:private|no-store)/i.test(cacheControl), 'Anonymous HTML cache isolation is not established'); assert.equal(response.headers.get('set-cookie'), null, 'Public HTML response sets a private cookie'); assert.ok(!/Bearer\s+[A-Za-z0-9._-]+|(?:access_token|signedMessage|wallet_signature)\s*[:=]/.test(html)); manifest.checks.push({ kind: 'anonymous-ttfb', ttfbMs, cacheControl: response.headers.get('cache-control'), htmlSha256: hash(html) }); }
    assert.ok(percentile(manifest.checks.filter(value => value.kind === 'anonymous-ttfb').map(value => value.ttfbMs), 0.95) <= 500, 'Public HTML TTFB p95 exceeds500ms');
    manifest.laneMeasurementsPassed = true;
  } catch (error) { manifest.failure = { code: 'RELEASE_MEASUREMENT_FAILED', type: error instanceof Error ? error.name : 'UnknownError', messageSha256: hash(error instanceof Error ? error.message : String(error)) }; throw error; }
  finally { manifest.finishedAt = new Date().toISOString(); await writeFile(join(output, 'release-evidence-manifest.json'), JSON.stringify(manifest, null, 2) + '\n'); }
}
