import type { Eip1193Provider } from './types.js';

export const ANNOUNCE_PROVIDER_EVENT = 'eip6963:announceProvider';
export const REQUEST_PROVIDER_EVENT = 'eip6963:requestProvider';

export interface Eip6963ProviderInfo {
  readonly uuid: string;
  readonly name: string;
  readonly icon: string;
  readonly rdns: string;
}

export interface Eip6963ProviderDetail {
  readonly info: Eip6963ProviderInfo;
  readonly provider: Eip1193Provider;
}

export interface ProviderDiscovery {
  readonly providers: readonly Eip6963ProviderDetail[];
  stop(): void;
}

const UUID_V4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
const RDNS = /^[a-z0-9-]+(\.[a-z0-9-]+)+$/i;
const ICON = /^data:image\/[a-z0-9.+-]+(;[a-z0-9=.+-]+)*(;base64)?,.+$/i;

function defaultTarget(): EventTarget | undefined {
  return (globalThis as { window?: EventTarget }).window;
}

function requireTarget(target: EventTarget | undefined): EventTarget {
  if (!target) throw new Error('EIP-6963 requires an event target; no window exists in this environment');
  return target;
}

export function validateProviderInfo(info: Eip6963ProviderInfo): Eip6963ProviderInfo {
  if (!UUID_V4.test(info.uuid)) throw new Error('EIP-6963 info.uuid must be a UUIDv4');
  if (typeof info.name !== 'string' || info.name.trim().length === 0) throw new Error('EIP-6963 info.name is required');
  if (!ICON.test(info.icon)) throw new Error('EIP-6963 info.icon must be an image data URL');
  if (!RDNS.test(info.rdns)) throw new Error('EIP-6963 info.rdns must be a reverse domain name');
  return Object.freeze({ uuid: info.uuid, name: info.name, icon: info.icon, rdns: info.rdns });
}

export function announceProvider(
  detail: { info: Omit<Eip6963ProviderInfo, 'uuid'> & { uuid?: string }; provider: Eip1193Provider },
  target: EventTarget | undefined = defaultTarget(),
): () => void {
  const eventTarget = requireTarget(target);
  const info = validateProviderInfo({ ...detail.info, uuid: detail.info.uuid ?? globalThis.crypto.randomUUID() });
  const frozen: Eip6963ProviderDetail = Object.freeze({ info, provider: detail.provider });
  const announce = (): void => {
    eventTarget.dispatchEvent(new CustomEvent(ANNOUNCE_PROVIDER_EVENT, { detail: frozen }));
  };
  eventTarget.addEventListener(REQUEST_PROVIDER_EVENT, announce);
  announce();
  return () => eventTarget.removeEventListener(REQUEST_PROVIDER_EVENT, announce);
}

export function discoverProviders(
  onProvider?: (detail: Eip6963ProviderDetail) => void,
  target: EventTarget | undefined = defaultTarget(),
): ProviderDiscovery {
  const eventTarget = requireTarget(target);
  const providers: Eip6963ProviderDetail[] = [];
  const listener = (event: Event): void => {
    const detail = (event as CustomEvent<unknown>).detail;
    if (!isDetail(detail)) return;
    try {
      validateProviderInfo(detail.info);
    } catch {
      return;
    }
    if (providers.some((known) => known.info.uuid === detail.info.uuid)) return;
    providers.push(detail);
    onProvider?.(detail);
  };
  eventTarget.addEventListener(ANNOUNCE_PROVIDER_EVENT, listener);
  eventTarget.dispatchEvent(new Event(REQUEST_PROVIDER_EVENT));
  return {
    providers,
    stop: () => eventTarget.removeEventListener(ANNOUNCE_PROVIDER_EVENT, listener),
  };
}

export function install(
  provider: Eip1193Provider,
  info: Omit<Eip6963ProviderInfo, 'uuid'> & { uuid?: string },
  target: EventTarget | undefined = defaultTarget(),
): () => void {
  const win = defaultTarget();
  if (win) (win as unknown as { paxeer?: Eip1193Provider }).paxeer = provider;
  return announceProvider({ info, provider }, target);
}

function isDetail(value: unknown): value is Eip6963ProviderDetail {
  if (typeof value !== 'object' || value === null) return false;
  const record = value as Record<string, unknown>;
  const info = record.info as Record<string, unknown> | undefined;
  const provider = record.provider as Record<string, unknown> | undefined;
  return (
    typeof info === 'object' &&
    info !== null &&
    typeof info.uuid === 'string' &&
    typeof info.name === 'string' &&
    typeof info.icon === 'string' &&
    typeof info.rdns === 'string' &&
    typeof provider === 'object' &&
    provider !== null &&
    typeof provider.request === 'function'
  );
}
