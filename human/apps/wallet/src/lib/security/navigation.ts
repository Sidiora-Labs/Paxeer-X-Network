import { parseHttpsUrl } from '@/domains/shared';

export const EXTERNAL_NAVIGATION_ORIGINS: ReadonlySet<string> = new Set([
  'https://app.hyperpax.xyz',
  'https://app.webpoints.app',
  'https://colosseum.hyperpaxeer.com',
  'https://crossverse.app',
  'https://dao.hyperpaxeer.com',
  'https://kindlelaunch.com',
  'https://paxscan.io',
  'https://t.me',
  'https://www.kindlelaunch.com',
  'https://x.com',
]);

export function validatedExternalUrl(input: string): URL | null {
  const parsed = parseHttpsUrl(input, {
    allowedOrigins: EXTERNAL_NAVIGATION_ORIGINS,
    maxLength: 2048,
  });
  return parsed.ok ? parsed.value : null;
}

export function openExternalUrl(input: string): boolean {
  if (typeof window === 'undefined') return false;
  const url = validatedExternalUrl(input);
  if (!url) return false;
  const opened = window.open(url.toString(), '_blank', 'noopener,noreferrer');
  if (opened) opened.opener = null;
  return opened !== null;
}
