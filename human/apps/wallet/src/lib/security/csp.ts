function configuredOrigins(...values: Array<string | undefined>): string[] {
  return values
    .filter((value): value is string => typeof value === 'string' && value.length > 0)
    .map((value) => new URL(value).origin);
}

const CONNECT_ORIGINS: readonly string[] = [
  'https://api.openai.com',
  'https://api.hyperpax.xyz',
  'https://connect.paxportwallet.com',
  'https://data-api.crossverse.app',
  'https://eu-east-1.public.node.hyperpaxeer.com',
  'https://open.er-api.com',
  'https://public-mainnet.rpcpaxeer.online',
  'https://sidiora.fun',
  'https://supabase.paxeer.app',
  'wss://supabase.paxeer.app',
  ...configuredOrigins(process.env.NEXT_PUBLIC_PNS_API_BASE),
];

const IMAGE_ORIGINS: readonly string[] = [
  ...configuredOrigins(process.env.NEXT_PUBLIC_MEDIA_STORAGE_ORIGIN),
];

const FRAME_ORIGINS = [
  'https://app.hyperpax.xyz',
  'https://app.webpoints.app',
  'https://colosseum.hyperpaxeer.com',
  'https://crossverse.app',
  'https://dao.hyperpaxeer.com',
  'https://kindlelaunch.com',
  'https://paxscan.io',
  'https://www.kindlelaunch.com',
] as const;

export function createCspNonce(): string {
  return crypto.randomUUID().replaceAll('-', '');
}

export function buildContentSecurityPolicy(nonce: string): string {
  const directives = [
    "default-src 'self'",
    `script-src 'self' 'nonce-${nonce}' 'strict-dynamic' 'wasm-unsafe-eval' 'unsafe-eval'`,
    "style-src 'self' 'unsafe-inline'",
    `img-src 'self' data: blob: ${IMAGE_ORIGINS.join(' ')}`,
    "font-src 'self'",
    `connect-src 'self' data: blob: ${CONNECT_ORIGINS.join(' ')}`,
    `frame-src 'self' blob: ${FRAME_ORIGINS.join(' ')}`,
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
