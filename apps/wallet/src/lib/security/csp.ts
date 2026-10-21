import {
  PWA_NETWORK_ENV,
  configuredOrigins,
  processPwaNetworkEnv,
  type ConfiguredOrigin,
  type PwaNetworkEnv,
} from '@/pwa/config';

function imageOrigins(...values: Array<string | undefined>): string[] {
  return values
    .filter((value): value is string => typeof value === 'string' && value.length > 0)
    .map((value) => new URL(value).origin);
}

const IMAGE_ORIGINS: readonly string[] = [
  ...imageOrigins(process.env.NEXT_PUBLIC_MEDIA_STORAGE_ORIGIN),
];

export function connectOrigins(env: PwaNetworkEnv = processPwaNetworkEnv()): ConfiguredOrigin[] {
  const origins = configuredOrigins(env);
  const realtime = origins
    .filter((entry) => entry.variable === PWA_NETWORK_ENV.identity)
    .map((entry) => ({ variable: entry.variable, origin: entry.origin.replace(/^http/, 'ws') }));
  const seen = new Set<string>();
  return [...origins, ...realtime].filter((entry) => {
    if (seen.has(entry.origin)) return false;
    seen.add(entry.origin);
    return true;
  });
}

function directive(policy: string, name: string): string[] | null {
  for (const part of policy.split(';')) {
    const tokens = part.trim().split(/\s+/).filter((token) => token.length > 0);
    if (tokens[0] === name) return tokens.slice(1);
  }
  return null;
}

export function assertConnectSrcCoversConfigured(
  policy: string,
  env: PwaNetworkEnv = processPwaNetworkEnv(),
): void {
  const sources = directive(policy, 'connect-src');
  if (sources === null) throw new Error('the policy has no connect-src directive');
  if (!sources.includes("'self'")) throw new Error("connect-src does not allow 'self'");
  const expected = connectOrigins(env);
  for (const entry of expected) {
    if (!sources.includes(entry.origin)) {
      throw new Error(`connect-src lacks the origin of ${entry.variable}`);
    }
  }
  const allowed = new Set(expected.map((entry) => entry.origin));
  for (const source of sources) {
    if (source.includes('://') && !allowed.has(source)) {
      throw new Error(`connect-src carries ${source}, which no configuration name yields`);
    }
  }
}

export function createCspNonce(): string {
  return crypto.randomUUID().replaceAll('-', '');
}

export function buildContentSecurityPolicy(
  nonce: string,
  env: PwaNetworkEnv = processPwaNetworkEnv(),
): string {
  const connect = connectOrigins(env).map((entry) => entry.origin);
  const directives = [
    "default-src 'self'",
    `script-src 'self' 'nonce-${nonce}' 'strict-dynamic' 'wasm-unsafe-eval' 'unsafe-eval'`,
    "style-src 'self' 'unsafe-inline'",
    `img-src 'self' data: blob: ${IMAGE_ORIGINS.join(' ')}`,
    "font-src 'self'",
    ['connect-src', "'self'", 'data:', 'blob:', ...connect].join(' '),
    "frame-src 'self' blob:",
    "worker-src 'self' blob:",
    "manifest-src 'self'",
    "media-src 'self' blob:",
    "object-src 'none'",
    "base-uri 'self'",
    "form-action 'self'",
    "frame-ancestors 'none'",
    'upgrade-insecure-requests',
  ];
  return directives.join('; ');
}
