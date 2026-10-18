import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient } from '@tanstack/react-query';
import { afterEach, expect, test } from 'vitest';
import { saveDraft, restoreDraft, removeDraft } from '../../apps/web/src/core/app-foundation/draft-store';
import { captureNavigationContext, installNavigationHistory, readNavigationContext } from '../../apps/web/src/core/app-foundation/history-context';
import { scopedQueryKey } from '../../apps/web/src/core/app-foundation/cache-scope';
import { changeFoundationPrincipal } from '../../apps/web/src/core/app-foundation/private-cache';
import { recoveryError, retryDelay, requestDraftHandoff } from '../../apps/web/src/core/app-foundation/recovery';
import { ErrorBoundary } from '../../apps/web/src/ui/common/error-boundary';
import { SearchResultsList, SearchInput } from '../../apps/web/src/shell/global-search-parts';
import type { SearchEntity } from '../../apps/web/src/core/app-foundation/search-client';
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const roots: ReturnType<typeof createRoot>[] = [];
afterEach(() => { for (const root of roots) act(() => root.unmount()); roots.length = 0; document.body.innerHTML = ''; localStorage.clear(); });
function mount(node: React.ReactNode) { const element = document.createElement('div'); document.body.append(element); const root = createRoot(element); roots.push(root); act(() => root.render(node)); return element; }
test('real draft store expires, distinguishes deployment changes and refuses authorization payloads', () => {
  const draft = saveDraft('launch', { name: 'Example', symbol: 'EX', description: 'Public metadata' }, true, 1000, 'release-a');
  expect(draft.chainId).toBe(125);
  expect(restoreDraft('launch', true, 2000, 'release-b')).toMatchObject({ status: 'deployment_changed', requiresValidation: true });
  expect(restoreDraft('launch', true, 86401000, 'release-b')).toEqual({ status: 'expired' });
  expect(() => saveDraft('launch', { signature: '0xsecret' } as unknown as Parameters<typeof saveDraft>[1], true)).toThrow('unsupported fields');
  expect(() => saveDraft('launch', { filters: { access_token: 'secret' } }, true)).toThrow('Invalid draft filter');
});
test('memory-only drafts operate without persistence and corrupt storage safely resets', () => {
  saveDraft('private-flow', { symbol: 'MEM' }, false, 1000, 'release-a');
  expect(localStorage.getItem('kindle:draft:v1:private-flow')).toBeNull();
  expect(restoreDraft('private-flow', false, 2000, 'release-a')).toMatchObject({ status: 'restored' });
  localStorage.setItem('kindle:draft:v1:broken', '{');
  expect(restoreDraft('broken', true)).toEqual({ status: 'invalid' });
  expect(localStorage.getItem('kindle:draft:v1:broken')).toBeNull();
  removeDraft('private-flow');
});
test('actual query cache removes A before publishing B while public snapshot survives', async () => {
  const client = new QueryClient();
  const a = scopedQueryKey('portfolio', 'release-a', { kind: 'wallet', address: '0x' + 'aa'.repeat(20) });
  const publicKey = scopedQueryKey('pool', 'release-a', { kind: 'public' });
  client.setQueryData(a, { amount: 'private-A' }); client.setQueryData(publicKey, { name: 'Public pool' });
  let published = false;
  await changeFoundationPrincipal(client, () => { expect(client.getQueryData(a)).toBeUndefined(); published = true; });
  expect(published).toBe(true); expect(client.getQueryData(publicKey)).toEqual({ name: 'Public pool' }); client.clear();
});
test('actual browser history captures allowlisted filters and retains Next state', () => {
  window.history.replaceState({ __NA: true, tree: ['preserved'] }, '', '/?category=gainers&sort=volume&access_token=excluded');
  const cleanup = installNavigationHistory();
  expect(window.history.state.tree).toEqual(['preserved']);
  expect(window.history.state.kindleFoundationV1.returnContext.filters).toEqual({ category: 'gainers', sort: 'volume' });
  expect(JSON.stringify(window.history.state.kindleFoundationV1)).not.toContain('excluded');
  cleanup();
  window.history.replaceState(null, '', '/token/invalid');
  expect(captureNavigationContext(window.location, 0)).toBeNull();
});
test('local boundary contains actual render exception and leaves sibling draft mounted', () => {
  function Broken(): React.ReactNode { throw new Error('Bearer secret-private-body'); }
  const element = mount(<><input defaultValue="unsent-public-draft" /><ErrorBoundary area="market-widget"><Broken /></ErrorBoundary><a href="/">Discovery</a></>);
  expect(element.querySelector('input')?.value).toBe('unsent-public-draft');
  expect(element.querySelector('a')?.textContent).toBe('Discovery');
  expect(element.querySelector('[role=alert]')).not.toBeNull();
  expect(element.textContent).not.toContain('secret-private-body');
});
test('chunk and authorization recovery expose redacted classification and bounded retries', () => {
  const failure = new Error('Loading chunk old-version failed: bearer secret'); failure.name = 'ChunkLoadError';
  const chunk = recoveryError(failure);
  expect(chunk.code).toBe('chunk_load'); expect(JSON.stringify(chunk)).not.toContain('secret');
  expect(recoveryError({ status: 403 })).toMatchObject({ code: 'unauthorized', retriable: false });
  expect(retryDelay(1)).toBeGreaterThanOrEqual(500); expect(retryDelay(3)).toBeLessThan(2250); expect(() => retryDelay(4)).toThrow();
});
test('real search controls expose exact entity address and accessible selection identity', () => {
  const row: SearchEntity = { identity: '125:pool:0x' + 'ab'.repeat(20), chainId: 125, kind: 'pool', pool_address: '0x' + 'ab'.repeat(20), token_address: '0x' + 'cd'.repeat(20), name: 'Pool', symbol: 'DUP', images: { logo: null } };
  const element = mount(<><SearchInput value="DUP" onChange={() => {}} onFocus={() => {}} inputRef={null} onKeyDown={() => {}} expanded listId="results" activeIndex={0} /><SearchResultsList results={[row]} loading={false} error={null} onRetry={() => {}} onSelect={() => {}} activeIndex={0} listId="results" /></>);
  expect(element.querySelector('[role=combobox]')?.getAttribute('aria-activedescendant')).toBe('results-0');
  expect(element.querySelector('[role=option]')?.getAttribute('aria-selected')).toBe('true');
  expect(element.textContent).toContain('Paxeer 125'); expect(element.textContent).toContain('Pool');
});

import { renderToStaticMarkup } from 'react-dom/server';
import { redactedRecoveryPayload } from '../../apps/web/src/core/app-foundation/redacted-report';
import { checkedSearchMetadata } from '../../apps/web/src/core/app-foundation/search-client';
import { loadRecent, saveRecent, clearRecentStorage, setSearchPersistenceConsent } from '../../apps/web/src/shell/use-global-search';

test('tampered draft envelopes reset every invalid version, authority and authorization field', () => {
  const envelope = saveDraft('tampered', { name: 'Unsent metadata', filters: { tab: 'details' } }, true, 1000, 'release-a');
  const variants = [null, [], { ...envelope, version: 2 }, { ...envelope, chainId: 1 }, { ...envelope, createdAt: -1, expiresAt: 86400000 - 1 }, { ...envelope, deploymentVersion: 'Bearer signed-canary' }, { ...envelope, payload: { ...envelope.payload, signedMessage: 'signed-canary' } }, { ...envelope, payload: { filters: { tab: 'details', signature: 'signed-canary' } } }];
  for (const value of variants) {
    localStorage.setItem('kindle:draft:v1:tampered', JSON.stringify(value));
    expect(restoreDraft('tampered', true, 2000, 'release-a')).toEqual({ status: 'invalid' });
    expect(localStorage.getItem('kindle:draft:v1:tampered')).toBeNull();
    expect(restoreDraft('tampered', false, 2000, 'release-a')).toEqual({ status: 'absent' });
  }
  saveDraft('tampered', envelope.payload, true, 1000, 'release-a');
  expect(restoreDraft('tampered', true, 2000, 'release-b')).toMatchObject({ status: 'deployment_changed', requiresValidation: true, draft: { payload: envelope.payload } });
  removeDraft('tampered');
});
test('recent storage rejects credentials, malformed rows and duplicates and discards memory aliases', () => {
  const item = { address: '0x' + 'aa'.repeat(20), name: 'Public', symbol: 'PUB', logo: null, marketCap: '0', ts: Date.now() };
  setSearchPersistenceConsent(true);
  for (const value of [{ token: 'credential-canary' }, [{ ...item, bearer: 'credential-canary' }], [{ ...item, address: 'https://attacker.invalid' }], [item, item], [{ ...item, ts: -1 }]]) {
    saveRecent([item]); localStorage.setItem('sidiora_recent_viewed', JSON.stringify(value));
    expect(loadRecent()).toEqual([]); expect(localStorage.getItem('sidiora_recent_viewed')).toBeNull();
  }
  saveRecent([item]); const loaded = loadRecent(); loaded[0].name = 'mutation'; expect(loadRecent()[0].name).toBe('Public');
  setSearchPersistenceConsent(true, true); expect(loadRecent()).toEqual([]); expect(localStorage.getItem('sidiora_recent_viewed')).toBeNull();
  clearRecentStorage(); setSearchPersistenceConsent(false);
});
test('redacted telemetry cannot serialize forged codes, deployment versions, traces or signed messages', () => {
  const canary = 'Bearer credential-canary signed-message-canary';
  const error = { code: canary, retriable: true, correlationId: canary, deploymentVersion: canary, stack: canary, signedMessage: canary } as unknown as Parameters<typeof redactedRecoveryPayload>[0];
  const payload = redactedRecoveryPayload(error, canary);
  const encoded = JSON.stringify(payload);
  expect(encoded).not.toContain('credential-canary'); expect(encoded).not.toContain('signed-message-canary'); expect(encoded).not.toContain('stack');
  expect(payload.context.code).toBe('render_failed'); expect(payload.context.area).toBe('widget');
  expect(payload.context.correlationId).toMatch(/^[0-9a-f-]{36}$/);
  expect(redactedRecoveryPayload(recoveryError({ status: 401 }), 'global-search').context.retriable).toBe(false);
});
test('chunk404 render failure retains route sibling and only offers explicit controlled reload', () => {
  function FailedChunk(): React.ReactNode { const error = new Error('Loading chunk404 failed: Bearer credential-canary signed-message-canary'); error.name = 'ChunkLoadError'; throw error; }
  const element = mount(<><input defaultValue="nonsecret draft" /><ErrorBoundary area="market-widget"><FailedChunk /></ErrorBoundary><a href="/">Discovery</a></>);
  expect(element.querySelector('input')?.value).toBe('nonsecret draft'); expect(element.querySelector('a')?.textContent).toBe('Discovery');
  expect(element.textContent).toContain('Preserve draft and reload'); expect(element.textContent).not.toContain('credential-canary'); expect(element.textContent).not.toContain('signed-message-canary');
  expect(element.textContent).not.toContain('Retry section');
});
test('reload handoff waits for real persistence completion and propagates storage failure', async () => {
  let release!: () => void; const pending = new Promise<void>(resolve => { release = resolve; });
  const preserve = (event: Event) => (event as CustomEvent<{ waitUntil: (work: Promise<void>) => void }>).detail.waitUntil(pending);
  window.addEventListener('kindle:preserve-nonsecret-drafts', preserve);
  try { let finished = false; const handoff = requestDraftHandoff().then(() => { finished = true; }); await Promise.resolve(); expect(finished).toBe(false); release(); await handoff; expect(finished).toBe(true); }
  finally { window.removeEventListener('kindle:preserve-nonsecret-drafts', preserve); }
  const denied = (event: Event) => (event as CustomEvent<{ waitUntil: (work: Promise<void>) => void }>).detail.waitUntil(Promise.reject(new Error('Storage unavailable')));
  window.addEventListener('kindle:preserve-nonsecret-drafts', denied);
  try { await expect(requestDraftHandoff()).rejects.toThrow('Storage unavailable'); } finally { window.removeEventListener('kindle:preserve-nonsecret-drafts', denied); }
});
test('broken metadata cannot override route identity or insert markup into public rendering', () => {
  const token = '0x' + 'aa'.repeat(20), pool = '0x' + 'bb'.repeat(20);
  expect(() => checkedSearchMetadata({ [token]: { token_address: pool } }, [token])).toThrow('identity mismatch');
  expect(() => checkedSearchMetadata({ [token]: { token_address: token, pool_address: 'https://attacker.invalid' } }, [token])).toThrow('Invalid address');
  expect(() => checkedSearchMetadata({ [pool]: null }, [token])).toThrow('Unexpected');
  expect(() => checkedSearchMetadata({ [token]: { token_address: token, name: [] } }, [token])).toThrow('display field');
  const metadata = checkedSearchMetadata({ [token]: { token_address: token, pool_address: pool, name: '<img src=x onerror=alert(1)>', symbol: '<script>evil</script>' } }, [token])[token]!;
  const row: SearchEntity = { identity: '125:pool:' + pool, chainId: 125, kind: 'pool', pool_address: pool, token_address: token, name: metadata.name!, symbol: metadata.symbol!, images: { logo: null } };
  const html = renderToStaticMarkup(<SearchResultsList results={[row]} loading={false} error={null} onRetry={() => {}} onSelect={() => {}} activeIndex={0} listId="safe-results" />);
  expect(html).not.toContain('<img'); expect(html).not.toContain('<script>'); expect(html).toContain('&lt;img');
});
test('tampered history cannot restore another pool, credentials, arrays or an unknown route', () => {
  const pool = '0x' + 'aa'.repeat(20), other = '0x' + 'bb'.repeat(20);
  window.history.replaceState({}, '', '/token/' + pool + '?tab=details');
  const context = captureNavigationContext(window.location, 10)!;
  expect(readNavigationContext({ ...context, token: 'credential-canary' })).toBeNull();
  expect(readNavigationContext({ ...context, routeId: 'unknown' })).toBeNull();
  expect(readNavigationContext({ ...context, returnContext: { filters: [], scrollY: 1 } })).toBeNull();
  const cleanup = installNavigationHistory(); let restored = 0;
  const observe = () => { restored++; }; window.addEventListener('kindle:restore-navigation', observe);
  try { window.dispatchEvent(new PopStateEvent('popstate', { state: { kindleFoundationV1: { ...context, poolAddress: other } } })); expect(restored).toBe(0); }
  finally { cleanup(); window.removeEventListener('kindle:restore-navigation', observe); window.history.replaceState(null, '', '/'); }
});
test('sign-out clears real account and explicit private query families before history can revisit them', async () => {
  const client = new QueryClient(); const publicKey = scopedQueryKey('pool', 'release-a', { kind: 'public' });
  const privateKeys = [['private-account', 125, 'owner-a'], ['wallet', 'owner-a'], scopedQueryKey('messages', 'release-a', { kind: 'session', principalId: 'owner-a', epoch: 1 })];
  client.setQueryData(publicKey, { name: 'Public' }); for (const key of privateKeys) client.setQueryData(key, { body: 'private-canary' });
  let clears = 0; const observe = () => { clears++; }; window.addEventListener('kindle:private-state-cleared', observe);
  try { await changeFoundationPrincipal(client, () => { for (const key of privateKeys) expect(client.getQueryData(key)).toBeUndefined(); }); expect(clears).toBe(1); expect(client.getQueryData(publicKey)).toEqual({ name: 'Public' }); }
  finally { window.removeEventListener('kindle:private-state-cleared', observe); client.clear(); }
});
