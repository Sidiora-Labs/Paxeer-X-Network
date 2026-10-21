const BASE_URL_ENV = 'NEXT_PUBLIC_PAXSCAN_BASE_URL';

function parseBaseUrl(raw: string | undefined): string | null {
  if (!raw) return null;
  let url: URL;
  try {
    url = new URL(raw);
  } catch {
    throw new Error(`${BASE_URL_ENV} is not a valid URL: ${raw}`);
  }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') {
    throw new Error(`${BASE_URL_ENV} must use http or https: ${raw}`);
  }
  if (url.username || url.password || url.pathname !== '/' || url.search || url.hash) {
    throw new Error(`${BASE_URL_ENV} must be a bare origin: ${raw}`);
  }
  return url.origin;
}

export const PAXSCAN_API_URL = process.env.NEXT_PUBLIC_PAXSCAN_API_URL ?? '';
export const PAXSCAN_WS_URL = process.env.NEXT_PUBLIC_PAXSCAN_WS_URL ?? '';
export const PAXSCAN_BASE_URL = parseBaseUrl(process.env.NEXT_PUBLIC_PAXSCAN_BASE_URL);

if (process.env.NODE_ENV !== 'production') {
  if (!PAXSCAN_API_URL) console.warn('[paxscan] NEXT_PUBLIC_PAXSCAN_API_URL is empty');
  if (!PAXSCAN_WS_URL) console.warn('[paxscan] NEXT_PUBLIC_PAXSCAN_WS_URL is empty');
}
