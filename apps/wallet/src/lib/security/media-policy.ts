import { parseHttpsUrl } from '@/domains/shared';

export const ALLOWED_MEDIA_ORIGINS: ReadonlySet<string> = new Set([
  'https://cdn.redixusercontent.ocfstudio.com',
  'https://img.logo.dev',
  'https://raw.githubusercontent.com',
  'https://sidiora.fun',
]);

const ALLOWED_MEDIA_TYPES: ReadonlySet<string> = new Set([
  'image/gif',
  'image/jpeg',
  'image/png',
  'image/webp',
]);

export function isAllowedMediaContentType(contentType: string | null): boolean {
  const normalized = contentType?.split(';')[0]?.trim().toLowerCase();
  return normalized !== undefined && ALLOWED_MEDIA_TYPES.has(normalized);
}

export function safeMediaPath(source: string | null | undefined): string {
  if (!source) return '/wallet/default_icon.webp';
  if (source.startsWith('/') && !source.startsWith('//')) {
    return source === '/wallet' || source.startsWith('/wallet/') ? source : `/wallet${source}`;
  }
  const parsed = parseHttpsUrl(source, {
    allowedOrigins: ALLOWED_MEDIA_ORIGINS,
    maxLength: 2048,
  });
  if (!parsed.ok) return '/wallet/default_icon.webp';
  return `/wallet/api/media?url=${encodeURIComponent(parsed.value.toString())}`;
}
