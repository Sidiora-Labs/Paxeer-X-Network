import { afterEach, describe, expect, it } from 'vitest';
import {
  ANNOUNCE_PROVIDER_EVENT,
  PaxeerProvider,
  REQUEST_PROVIDER_EVENT,
  announceProvider,
  discoverProviders,
  install,
  validateProviderInfo,
  walletInterfaces,
  type Eip1193Provider,
  type Eip6963ProviderDetail,
  type RequestArguments,
} from '../src/index.js';

const ICON = 'data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAxIDEiLz4=';
const PAXEER_INFO = { name: 'Paxeer X Wallet', icon: ICON, rdns: 'invalid.example.paxeer' };

function embedded(): PaxeerProvider {
  return new PaxeerProvider({ gatewayUrl: 'http://127.0.0.1:9', rpcUrl: 'http://127.0.0.1:9/rpc', token: () => null });
}

class ChainIdProvider implements Eip1193Provider {
  async request(args: RequestArguments): Promise<unknown> {
    if (args.method === 'eth_chainId') return '0x7d';
    throw new Error(`unexpected ${args.method}`);
  }
  on(): this {
    return this;
  }
  removeListener(): this {
    return this;
  }
}

afterEach(() => {
  delete (globalThis as { window?: unknown }).window;
});

describe('EIP-6963', () => {
  it('eip6963_announceProvider_dispatches_a_frozen_detail_on_the_target', () => {
    const target = new EventTarget();
    const provider = embedded();
    const seen: Eip6963ProviderDetail[] = [];
    target.addEventListener(ANNOUNCE_PROVIDER_EVENT, (event) => seen.push((event as CustomEvent<Eip6963ProviderDetail>).detail));
    const stop = announceProvider({ info: PAXEER_INFO, provider }, target);
    expect(seen).toHaveLength(1);
    const detail = seen[0]!;
    expect(detail.provider).toBe(provider);
    expect(detail.info).toMatchObject(PAXEER_INFO);
    expect(detail.info.uuid).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    expect(Object.isFrozen(detail)).toBe(true);
    expect(Object.isFrozen(detail.info)).toBe(true);
    stop();
  });

  it('eip6963_announceProvider_answers_requestProvider_until_stopped', () => {
    const target = new EventTarget();
    let count = 0;
    target.addEventListener(ANNOUNCE_PROVIDER_EVENT, () => {
      count += 1;
    });
    const stop = announceProvider({ info: PAXEER_INFO, provider: embedded() }, target);
    target.dispatchEvent(new Event(REQUEST_PROVIDER_EVENT));
    target.dispatchEvent(new Event(REQUEST_PROVIDER_EVENT));
    expect(count).toBe(3);
    stop();
    target.dispatchEvent(new Event(REQUEST_PROVIDER_EVENT));
    expect(count).toBe(3);
  });

  it('eip6963_discoverProviders_collects_announcements_before_and_after_the_request', () => {
    const target = new EventTarget();
    const paxeer = embedded();
    const injected = new ChainIdProvider();
    const stopPaxeer = announceProvider({ info: PAXEER_INFO, provider: paxeer }, target);
    const heard: string[] = [];
    const discovery = discoverProviders((detail) => heard.push(detail.info.name), target);
    expect(discovery.providers.map((d) => d.provider)).toEqual([paxeer]);
    const stopInjected = announceProvider(
      { info: { name: 'Injected Wallet', icon: ICON, rdns: 'invalid.example.injected' }, provider: injected },
      target,
    );
    target.dispatchEvent(new Event(REQUEST_PROVIDER_EVENT));
    expect(discovery.providers.map((d) => d.info.name)).toEqual(['Paxeer X Wallet', 'Injected Wallet']);
    expect(heard).toEqual(['Paxeer X Wallet', 'Injected Wallet']);
    discovery.stop();
    announceProvider({ info: { name: 'Late Wallet', icon: ICON, rdns: 'invalid.example.late' }, provider: injected }, target);
    expect(discovery.providers).toHaveLength(2);
    stopPaxeer();
    stopInjected();
  });

  it('eip6963_discoverProviders_ignores_malformed_announcements', () => {
    const target = new EventTarget();
    const discovery = discoverProviders(undefined, target);
    target.dispatchEvent(new CustomEvent(ANNOUNCE_PROVIDER_EVENT, { detail: { info: { name: 'x' }, provider: {} } }));
    target.dispatchEvent(
      new CustomEvent(ANNOUNCE_PROVIDER_EVENT, {
        detail: { info: { uuid: 'not-a-uuid', name: 'x', icon: ICON, rdns: 'invalid.example.x' }, provider: new ChainIdProvider() },
      }),
    );
    expect(discovery.providers).toEqual([]);
    discovery.stop();
  });

  it('eip6963_validateProviderInfo_refuses_bad_fields', () => {
    const uuid = globalThis.crypto.randomUUID();
    expect(() => validateProviderInfo({ ...PAXEER_INFO, uuid: 'x' })).toThrow('UUIDv4');
    expect(() => validateProviderInfo({ ...PAXEER_INFO, uuid, name: ' ' })).toThrow('name');
    expect(() => validateProviderInfo({ ...PAXEER_INFO, uuid, icon: 'https://icon.invalid/a.png' })).toThrow('data URL');
    expect(() => validateProviderInfo({ ...PAXEER_INFO, uuid, rdns: 'paxeer' })).toThrow('reverse domain');
    expect(validateProviderInfo({ ...PAXEER_INFO, uuid }).uuid).toBe(uuid);
  });

  it('eip6963_requires_a_target_when_no_window_exists', () => {
    expect(() => announceProvider({ info: PAXEER_INFO, provider: embedded() })).toThrow('event target');
    expect(() => discoverProviders()).toThrow('event target');
  });

  it('eip6963_install_sets_window_paxeer_and_announces_on_the_window', () => {
    const win = new EventTarget();
    (globalThis as { window?: unknown }).window = win;
    const provider = embedded();
    const discovery = discoverProviders();
    const stop = install(provider, PAXEER_INFO);
    expect((win as unknown as { paxeer?: unknown }).paxeer).toBe(provider);
    expect(discovery.providers.map((d) => d.provider)).toEqual([provider]);
    stop();
    discovery.stop();
  });

  it('eip6963_walletInterfaces_present_the_embedded_and_every_injected_provider', async () => {
    const target = new EventTarget();
    const paxeer = embedded();
    const injected = new ChainIdProvider();
    announceProvider({ info: PAXEER_INFO, provider: paxeer }, target);
    announceProvider({ info: { name: 'Injected Wallet', icon: ICON, rdns: 'invalid.example.injected' }, provider: injected }, target);
    const discovery = discoverProviders(undefined, target);
    const own = discovery.providers.find((d) => d.provider === paxeer)!;
    const wallets = walletInterfaces(paxeer, own.info, discovery.providers);
    expect(wallets.map((w) => w.mode)).toEqual(['embedded', 'injected']);
    expect(wallets.map((w) => w.info?.name)).toEqual(['Paxeer X Wallet', 'Injected Wallet']);
    expect(await Promise.all(wallets.map((w) => w.chainId()))).toEqual([125, 125]);
    discovery.stop();
  });
});
