import { issue, type BoundaryParser, type ParseResult } from '../shared';

export const SHELL_ROUTE_NAMES = [
  'portfolio',
  'send',
  'receive',
  'transactions',
  'swap',
  'discover',
  'dex',
  'colosseum',
  'dao',
  'paxfun',
  'wormhole',
  'points',
  'paxscan',
  'pns',
  'sidiora-fun',
  'browser',
  'settings',
  'contacts',
  'ramp',
  'token-detail',
  'tx-detail',
] as const;

export type ShellRouteName = (typeof SHELL_ROUTE_NAMES)[number];

type StaticRouteName = Exclude<
  ShellRouteName,
  'browser' | 'paxfun' | 'paxscan' | 'send' | 'token-detail' | 'tx-detail'
>;

export type ShellRoute =
  | { readonly name: StaticRouteName }
  | { readonly name: 'send'; readonly token?: string; readonly to?: string }
  | {
      readonly name: 'token-detail';
      readonly token: string;
      readonly symbol?: string;
    }
  | { readonly name: 'tx-detail'; readonly hash: string }
  | {
      readonly name: 'paxfun';
      readonly pool: string;
      readonly symbol?: string;
    }
  | { readonly name: 'paxscan'; readonly path?: string }
  | { readonly name: 'browser'; readonly url: string };

export type CustodyMode = 'embedded' | 'funded' | 'self-custody';
export type RouteDataState =
  | 'loading'
  | 'empty'
  | 'stale'
  | 'partial'
  | 'offline'
  | 'permission-denied'
  | 'recoverable-error'
  | 'terminal-error'
  | 'ready';

export interface RoutePolicy {
  readonly custody: readonly CustodyMode[];
  readonly requiresUnlocked: boolean;
  readonly requiresAccount: boolean;
  readonly feature?: string;
  readonly recovery: ShellRouteName;
  readonly states: Readonly<Record<RouteDataState, 'render' | 'not-applicable'>>;
  readonly draftLifetime: 'route' | 'session-observation' | 'none';
}

const ALL_STATES: Readonly<Record<RouteDataState, 'render'>> = {
  loading: 'render',
  empty: 'render',
  stale: 'render',
  partial: 'render',
  offline: 'render',
  'permission-denied': 'render',
  'recoverable-error': 'render',
  'terminal-error': 'render',
  ready: 'render',
};

const ALL_CUSTODY: readonly CustodyMode[] = [
  'embedded',
  'funded',
  'self-custody',
];
const SELF_AND_EMBEDDED: readonly CustodyMode[] = [
  'embedded',
  'self-custody',
];
const SELF_ONLY: readonly CustodyMode[] = ['self-custody'];

function policy(
  custody: readonly CustodyMode[],
  options: Partial<RoutePolicy> = {},
): RoutePolicy {
  return {
    custody,
    requiresUnlocked: true,
    requiresAccount: true,
    recovery: 'portfolio',
    states: ALL_STATES,
    draftLifetime: 'none',
    ...options,
  };
}

export const ROUTE_POLICIES: Readonly<Record<ShellRouteName, RoutePolicy>> = {
  portfolio: policy(ALL_CUSTODY),
  send: policy(SELF_AND_EMBEDDED, { draftLifetime: 'route' }),
  receive: policy(SELF_AND_EMBEDDED),
  transactions: policy(ALL_CUSTODY),
  swap: policy(ALL_CUSTODY, { draftLifetime: 'route' }),
  discover: policy(ALL_CUSTODY),
  dex: policy(SELF_ONLY, { recovery: 'discover', feature: 'dapp-browser' }),
  colosseum: policy(SELF_ONLY, { recovery: 'discover', feature: 'dapp-browser' }),
  dao: policy(SELF_ONLY, { recovery: 'discover', feature: 'dapp-browser' }),
  paxfun: policy(SELF_ONLY, { recovery: 'discover', feature: 'dapp-browser' }),
  wormhole: policy(SELF_ONLY, { recovery: 'discover', feature: 'dapp-browser' }),
  points: policy(SELF_ONLY, { recovery: 'discover', feature: 'dapp-browser' }),
  paxscan: policy(SELF_ONLY, { recovery: 'discover', feature: 'dapp-browser' }),
  pns: policy(SELF_ONLY, { recovery: 'discover' }),
  'sidiora-fun': policy(SELF_ONLY, {
    recovery: 'discover',
    feature: 'dapp-browser',
  }),
  browser: policy(SELF_ONLY, { recovery: 'discover', feature: 'dapp-browser' }),
  settings: policy(ALL_CUSTODY),
  contacts: policy(SELF_AND_EMBEDDED, { recovery: 'settings' }),
  ramp: policy(SELF_AND_EMBEDDED),
  'token-detail': policy(ALL_CUSTODY),
  'tx-detail': policy(ALL_CUSTODY, {
    recovery: 'transactions',
    draftLifetime: 'session-observation',
  }),
};

const ADDRESS = /^0x[0-9a-fA-F]{40}$/;
const HASH = /^0x[0-9a-fA-F]{64}$/;
const SYMBOL = /^[A-Za-z0-9._+-]{1,20}$/;
const PAXSCAN_PATH = /^\/[A-Za-z0-9._~:@%+/-]{0,512}$/;

function invalid(message: string): ParseResult<never> {
  return issue('$route', 'invalid_format', message);
}

function isStaticRoute(name: string): name is StaticRouteName {
  return (
    (SHELL_ROUTE_NAMES as readonly string[]).includes(name) &&
    !['browser', 'paxfun', 'paxscan', 'send', 'token-detail', 'tx-detail'].includes(
      name,
    )
  );
}

export function parseRouteUrl(input: string | URL): ParseResult<ShellRoute> {
  let url: URL;
  try {
    url = input instanceof URL ? input : new URL(input, 'https://wallet.local');
  } catch {
    return invalid('Route URL is invalid');
  }
  if (url.pathname !== '/') return invalid('Route path is invalid');
  const allowed = new Set(['screen', 'token', 'symbol', 'hash', 'pool', 'path', 'url', 'to']);
  const keys = [...url.searchParams.keys()];
  if (
    keys.some((key) => !allowed.has(key)) ||
    new Set(keys).size !== keys.length
  ) {
    return invalid('Route query contains unknown fields');
  }
  const hasOnly = (expected: readonly string[]) =>
    keys.every((key) => expected.includes(key));
  const name = url.searchParams.get('screen') ?? 'portfolio';
  if (isStaticRoute(name)) {
    return hasOnly(['screen'])
      ? { ok: true, value: { name } }
      : invalid('Static route contains unexpected parameters');
  }
  if (name === 'send') {
    if (!hasOnly(['screen', 'token', 'to'])) return invalid('Send route is invalid');
    const token = url.searchParams.get('token');
    const to = url.searchParams.get('to');
    if (token !== null && token !== 'pax' && !ADDRESS.test(token)) {
      return invalid('Send token is invalid');
    }
    if (to !== null && !ADDRESS.test(to)) {
      return invalid('Send destination is invalid');
    }
    return {
      ok: true,
      value: {
        name: 'send',
        ...(token ? { token: token.toLowerCase() } : {}),
        ...(to ? { to: to.toLowerCase() } : {}),
      },
    };
  }
  if (name === 'token-detail') {
    if (!hasOnly(['screen', 'token', 'symbol'])) {
      return invalid('Token route is invalid');
    }
    const token = url.searchParams.get('token');
    const symbol = url.searchParams.get('symbol');
    if (!token || (token !== 'pax' && !ADDRESS.test(token))) {
      return invalid('Token route is invalid');
    }
    if (symbol !== null && !SYMBOL.test(symbol)) {
      return invalid('Token symbol is invalid');
    }
    return {
      ok: true,
      value: {
        name,
        token: token.toLowerCase(),
        ...(symbol ? { symbol } : {}),
      },
    };
  }
  if (name === 'tx-detail') {
    if (!hasOnly(['screen', 'hash'])) {
      return invalid('Transaction route is invalid');
    }
    const hash = url.searchParams.get('hash');
    return hash && HASH.test(hash)
      ? { ok: true, value: { name, hash: hash.toLowerCase() } }
      : invalid('Transaction route is invalid');
  }
  if (name === 'paxfun') {
    if (!hasOnly(['screen', 'pool', 'symbol'])) {
      return invalid('Trade route is invalid');
    }
    const pool = url.searchParams.get('pool');
    const symbol = url.searchParams.get('symbol');
    if (!pool || !ADDRESS.test(pool) || (symbol !== null && !SYMBOL.test(symbol))) {
      return invalid('Trade route is invalid');
    }
    return {
      ok: true,
      value: {
        name,
        pool: pool.toLowerCase(),
        ...(symbol ? { symbol } : {}),
      },
    };
  }
  if (name === 'paxscan') {
    if (!hasOnly(['screen', 'path'])) {
      return invalid('Explorer route is invalid');
    }
    const path = url.searchParams.get('path');
    if (path !== null && !PAXSCAN_PATH.test(path)) {
      return invalid('Explorer route is invalid');
    }
    return {
      ok: true,
      value: { name, ...(path ? { path } : {}) },
    };
  }
  if (name === 'browser') {
    if (!hasOnly(['screen', 'url'])) {
      return invalid('Browser route is invalid');
    }
    const raw = url.searchParams.get('url');
    if (!raw || raw.length > 2_048) return invalid('Browser route is invalid');
    try {
      const destination = new URL(raw);
      if (
        destination.protocol !== 'https:' ||
        destination.username ||
        destination.password
      ) {
        return invalid('Browser route is invalid');
      }
      return { ok: true, value: { name, url: destination.toString() } };
    } catch {
      return invalid('Browser route is invalid');
    }
  }
  return issue('$route.name', 'unsupported_value', 'Route is unsupported');
}

export const parseRouteQuery: BoundaryParser<ShellRoute> = (input) => {
  if (typeof input !== 'object' || input === null || Array.isArray(input)) {
    return invalid('Route query is invalid');
  }
  const source = input as Record<string, unknown>;
  const name = source.name;
  if (typeof name !== 'string') return invalid('Route query is invalid');
  const url = new URL('https://wallet.local');
  for (const [key, value] of Object.entries(source)) {
    if (typeof value !== 'string') return invalid('Route query is invalid');
    let target = key === 'name' ? 'screen' : key;
    if (key === 'value') {
      target =
        name === 'tx-detail'
          ? 'hash'
          : name === 'token-detail' || name === 'send'
            ? 'token'
            : name === 'browser'
              ? 'url'
              : name === 'paxfun'
                ? 'pool'
                : name === 'paxscan'
                  ? 'path'
                  : 'value';
    }
    url.searchParams.set(target, value);
  }
  return parseRouteUrl(url);
};

export function serializeRoute(route: ShellRoute): string {
  const url = new URL('/', 'https://wallet.local');
  if (route.name !== 'portfolio') url.searchParams.set('screen', route.name);
  switch (route.name) {
    case 'send':
      if (route.token) url.searchParams.set('token', route.token);
      break;
    case 'token-detail':
      url.searchParams.set('token', route.token);
      if (route.symbol) url.searchParams.set('symbol', route.symbol);
      break;
    case 'tx-detail':
      url.searchParams.set('hash', route.hash);
      break;
    case 'paxfun':
      url.searchParams.set('pool', route.pool);
      if (route.symbol) url.searchParams.set('symbol', route.symbol);
      break;
    case 'paxscan':
      if (route.path) url.searchParams.set('path', route.path);
      break;
    case 'browser':
      url.searchParams.set('url', route.url);
      break;
  }
  return `${url.pathname}${url.search}`;
}

export function routeGuard(
  route: ShellRoute,
  context: {
    custody: CustodyMode;
    unlocked: boolean;
    hasAccount: boolean;
    features?: ReadonlySet<string>;
  },
): { allowed: true } | { allowed: false; recovery: ShellRoute } {
  const policy = ROUTE_POLICIES[route.name];
  const denied =
    !policy.custody.includes(context.custody) ||
    (policy.requiresUnlocked && !context.unlocked) ||
    (policy.requiresAccount && !context.hasAccount) ||
    (policy.feature !== undefined && !context.features?.has(policy.feature));
  return denied
    ? { allowed: false, recovery: { name: policy.recovery } as ShellRoute }
    : { allowed: true };
}
