import React, { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, expect, test } from 'vitest';
import { VirtualList } from '../../web/src/ui/common/bounded-list';
import { listWindow } from '../../web/src/core/app-foundation/window';
import { isRouteActive, poolPath, routes, routeForPath, safeExternalDestination } from '../../web/src/core/app-foundation/navigation';
import { scopedQueryKey, cachePolicy } from '../../web/src/core/app-foundation/cache-scope';
import { useDialogFocus } from '../../web/src/core/app-foundation/use-dialog-focus';
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const roots: ReturnType<typeof createRoot>[] = [];
afterEach(() => { roots.forEach(root => act(() => root.unmount())); roots.length = 0; document.body.innerHTML = ''; });
function mount(node: React.ReactNode) { const el = document.createElement('div'); document.body.append(el); const root = createRoot(el); roots.push(root); act(() => root.render(node)); return { el, root }; }
test('real DOM windows 100000 rows and scrolls to last row without more than 200 mounts', () => {
  const items = Array.from({ length: 100000 }, (_, id) => ({ id }));
  const { el } = mount(<VirtualList items={items} estimateSize={26} maxHeight={400} renderItem={item => <button>{item.id}</button>} />);
  expect(el.querySelectorAll('[role=listitem]').length).toBeLessThanOrEqual(200);
  const list = el.querySelector('[role=list]') as HTMLElement;
  act(() => { list.scrollTop = 2600000; list.dispatchEvent(new Event('scroll', { bubbles: true })); });
  expect(el.textContent).toContain('99999');
  expect(el.querySelectorAll('[role=listitem]').length).toBeLessThanOrEqual(200);
});
test('stable address row identity survives reordered data', () => {
  const a = { address: 'a' }, b = { address: 'b' };
  const renderItem = (item: typeof a) => <input defaultValue={item.address} />;
  const { el, root } = mount(<VirtualList items={[a, b]} renderItem={renderItem} />);
  const first = el.querySelector('input')!; first.value = 'draft';
  act(() => root.render(<VirtualList items={[b, a]} renderItem={renderItem} />));
  expect(el.querySelectorAll('input')[1]).toBe(first);
  expect(first.value).toBe('draft');
});
test('geometry rejects hostile inputs and cannot exceed DOM budget', () => {
  for (const offset of [0, 250, 99999999]) expect(listWindow(100000, 26, 400, offset, 100000).end - listWindow(100000, 26, 400, offset, 100000).start).toBeLessThanOrEqual(200);
  expect(() => listWindow(10, 0, 400, 0)).toThrow();
  expect(() => listWindow(1000, 1, 400, 0)).toThrow();
  expect(() => listWindow(1000, 40, 400, NaN)).toThrow();
});
test('route boundaries and validated pool identity never conflate token and pool', () => {
  expect(new Set(routes.map(r => r.id)).size).toBe(routes.length);
  expect(isRouteActive('/profile-malicious', '/profile')).toBe(false);
  expect(isRouteActive('/chat/room', '/chat')).toBe(true);
  expect(poolPath(125, '0x' + 'Ab'.repeat(20))).toBe('/token/0x' + 'ab'.repeat(20));
  expect(() => poolPath(1, '0x' + 'ab'.repeat(20))).toThrow();
  expect(() => poolPath(125, 'javascript:alert(1)')).toThrow();
});
test('wallet, public, session and deployment caches cannot alias', () => {
  const key = (address: string) => scopedQueryKey('portfolio', 'v1', { kind: 'wallet', address });
  expect(key('0x' + 'aa'.repeat(20))).not.toEqual(key('0x' + 'bb'.repeat(20)));
  expect(key('0x' + 'aa'.repeat(20))).not.toEqual(scopedQueryKey('portfolio', 'v1', { kind: 'public' }));
  expect(scopedQueryKey('pool', 'v1', { kind: 'public' })).not.toEqual(scopedQueryKey('pool', 'v2', { kind: 'public' }));
  expect(cachePolicy.wallet.header).toContain('no-store');
});
test('dialog traps Tab and restores original opener on close', () => {
  const opener = document.createElement('button'); document.body.append(opener); opener.focus();
  function Dialog({ open }: { open: boolean }) { const ref = React.useRef<HTMLDivElement>(null); useDialogFocus(ref, open); return open ? <div ref={ref}><input /><button>Close</button></div> : null; }
  const { el, root } = mount(<Dialog open />);
  expect(document.activeElement).toBe(el.querySelector('input'));
  const close = el.querySelector('button')!; close.focus();
  act(() => close.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true })));
  expect(document.activeElement).toBe(el.querySelector('input'));
  act(() => root.render(<Dialog open={false} />)); expect(document.activeElement).toBe(opener);
});
import { QueryClient } from '@tanstack/react-query';
import { clearPrivateFoundationCache } from '../../web/src/core/app-foundation/private-cache';
test('actual query cache clears private values while preserving public snapshots', async () => {
  const client = new QueryClient();
  const publicKey = scopedQueryKey('pool', 'v1', { kind: 'public' });
  const privateKey = scopedQueryKey('portfolio', 'v1', { kind: 'wallet', address: '0x' + 'aa'.repeat(20) });
  client.setQueryData(publicKey, { name: 'Public' }); client.setQueryData(privateKey, { balance: 'secret' });
  await clearPrivateFoundationCache(client);
  expect(client.getQueryData(privateKey)).toBeUndefined(); expect(client.getQueryData(publicKey)).toEqual({ name: 'Public' });
  client.clear();
});
test('dialog route close preserves main focus while ordinary close restores opener', () => {
  const opener = document.createElement('button'), main = document.createElement('main'); main.tabIndex = -1; document.body.append(opener, main); opener.focus();
  const allowRestore = { current: true }; const restore = () => allowRestore.current;
  function Dialog({ open }: { open: boolean }) { const ref = React.useRef<HTMLDivElement>(null); useDialogFocus(ref, open, restore); return open ? <div ref={ref}><input /><button>Close</button></div> : null; }
  const { root } = mount(<Dialog open />);
  allowRestore.current = false; main.focus(); act(() => root.render(<Dialog open={false} />)); expect(document.activeElement).toBe(main);
  opener.focus(); allowRestore.current = true; act(() => root.render(<Dialog open />)); act(() => root.render(<Dialog open={false} />)); expect(document.activeElement).toBe(opener);
});
test('dialog skips hidden, inert, disabled and negative-tabindex controls; select/textarea remain reachable', () => {
  function Dialog() { const ref = React.useRef<HTMLDivElement>(null); useDialogFocus(ref, true); return <div ref={ref}><button hidden>Hidden</button><div style={{ display: 'none' }}><input /></div><button disabled>Disabled</button><input tabIndex={-1} /><div inert><input /></div><select><option>A</option></select><textarea /></div>; }
  const { el } = mount(<Dialog />); const select = el.querySelector('select')!, textarea = el.querySelector('textarea')!;
  expect(document.activeElement).toBe(select); textarea.focus(); act(() => textarea.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true }))); expect(document.activeElement).toBe(select);
  act(() => select.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true, cancelable: true }))); expect(document.activeElement).toBe(textarea);
});

test('modal isolates each background branch and restores prior accessibility state', () => {
  const prior = document.createElement('section'); prior.setAttribute('inert', ''); prior.setAttribute('aria-hidden', 'false'); document.body.append(prior);
  function Dialog({ open }: { open: boolean }) { const ref = React.useRef<HTMLDivElement>(null); useDialogFocus(ref, open); return <><header><button>Search opener</button></header><section>{open ? <div role="dialog" ref={ref}><input /><button>Close</button></div> : null}<main><button>Private action</button></main></section><nav><button>Navigate</button></nav></>; }
  const { el, root } = mount(<Dialog open />);
  for (const branch of ['header', 'main', 'nav']) { expect(el.querySelector(branch)?.hasAttribute('inert')).toBe(true); expect(el.querySelector(branch)?.getAttribute('aria-hidden')).toBe('true'); }
  expect(el.querySelector('[role=dialog]')?.closest('[inert]')).toBeNull();
  expect(prior.getAttribute('aria-hidden')).toBe('true');
  act(() => root.render(<Dialog open={false} />));
  for (const branch of ['header', 'main', 'nav']) { expect(el.querySelector(branch)?.hasAttribute('inert')).toBe(false); expect(el.querySelector(branch)?.hasAttribute('aria-hidden')).toBe(false); }
  expect(prior.hasAttribute('inert')).toBe(true); expect(prior.getAttribute('aria-hidden')).toBe('false');
});

test('route parser rejects unknown, encoded, malformed and origin-relative escape paths', () => {
  for (const path of ['/new/unknown', '/token/not-an-address', '/profile/invalid', '//attacker.invalid', '/chat/%2f%2fattacker.invalid', '/chat/a/b', '/docs/unknown', '/create?redirect=https://attacker.invalid']) expect(routeForPath(path)).toBeUndefined();
  expect(routeForPath('/token/0x' + 'aa'.repeat(20))?.id).toBe('token');
  expect(routeForPath('/tokens/0x' + 'aa'.repeat(20) + '/metadata')?.id).toBe('token-metadata');
});
test('outbound destinations use fixed HTTPS authority rather than suffix or metadata claims', () => {
  for (const value of ['javascript:alert(1)', '//t.me/Sidiorafunsupport', 'https://t.me.attacker.invalid/Sidiorafunsupport', 'https://attacker.invalid@t.me/Sidiorafunsupport', 'https://t.me/attacker', 'https://t.me/Sidiorafunsupport?next=https://attacker.invalid', 'https://t.me/Sidiorafunsupport#token', 'https://t.me:8443/Sidiorafunsupport', 'http://t.me/Sidiorafunsupport']) expect(safeExternalDestination(value)).toBeNull();
  expect(safeExternalDestination('https://t.me/Sidiorafunsupport')).toBe('https://t.me/Sidiorafunsupport');
  expect(safeExternalDestination('https://paxscan.paxeer.app/tx/0x' + 'ab'.repeat(32))).not.toBeNull();
});
test('modal contains programmatic focus escapes and focuses itself if controls are absent', () => {
  const outside = document.createElement('button'); document.body.append(outside);
  function Dialog() { const ref = React.useRef<HTMLDivElement>(null); useDialogFocus(ref, true); return <div ref={ref} role="dialog">Recovery is unavailable.</div>; }
  const { el } = mount(<Dialog />); const dialog = el.querySelector('[role=dialog]')!;
  expect(document.activeElement).toBe(dialog); outside.focus(); expect(document.activeElement).toBe(dialog);
});

test('malformed cache scopes cannot masquerade as a session or include credential payloads', () => {
  expect(() => scopedQueryKey('portfolio', 'release-a', { kind: 'unknown' } as unknown as Parameters<typeof scopedQueryKey>[2])).toThrow('Invalid cache identity');
  expect(() => scopedQueryKey('portfolio', 'release-a', { kind: 'session', principalId: 'Bearer credential-canary', epoch: 1 })).toThrow('Invalid session scope');
  expect(() => scopedQueryKey('portfolio', 'Bearer credential-canary', { kind: 'public' })).toThrow('Invalid cache identity');
});
