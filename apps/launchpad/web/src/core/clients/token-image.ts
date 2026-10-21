const FALLBACK_TOKEN_ICON = '/icons/token-fallback.svg';

const TRUSTED_IMAGE_HOSTS = new Set([
  'metadata-production-ae57.up.railway.app',
  'ipfs.io',
  'gateway.pinata.cloud',
  'cloudflare-ipfs.com',
  'arweave.net',
  'nftstorage.link',
]);

function isTrustedHost(url: string): boolean {
  try {
    const host = new URL(url).hostname;
    return TRUSTED_IMAGE_HOSTS.has(host);
  } catch {
    return false;
  }
}

function isSvgUrl(url: string): boolean {
  try {
    const pathname = new URL(url).pathname.toLowerCase();
    return pathname.endsWith('.svg');
  } catch {
    return url.toLowerCase().endsWith('.svg');
  }
}

function normalizeIpfs(url: string): string {
  if (url.startsWith('ipfs://')) {
    return `https://ipfs.io/ipfs/${url.slice(7)}`;
  }
  return url;
}

export function getSafeTokenImageUrl(rawUrl: string | null | undefined): string {
  if (!rawUrl || typeof rawUrl !== 'string' || rawUrl.trim() === '') {
    return FALLBACK_TOKEN_ICON;
  }

  const normalized = normalizeIpfs(rawUrl.trim());

  // Reject SVG from untrusted sources (XSS risk)
  if (isSvgUrl(normalized) && !isTrustedHost(normalized)) {
    return FALLBACK_TOKEN_ICON;
  }

  // Reject non-http(s) schemes
  try {
    const scheme = new URL(normalized).protocol;
    if (scheme !== 'https:' && scheme !== 'http:') {
      return FALLBACK_TOKEN_ICON;
    }
  } catch {
    return FALLBACK_TOKEN_ICON;
  }

  // For data: URIs, reject SVG data
  if (normalized.startsWith('data:image/svg')) {
    return FALLBACK_TOKEN_ICON;
  }

  return normalized;
}

export function getTokenImageProps(rawUrl: string | null | undefined, size: number = 32) {
  const src = getSafeTokenImageUrl(rawUrl);
  return {
    src,
    width: size,
    height: size,
    alt: '',
    unoptimized: !isTrustedHost(src),
  };
}
