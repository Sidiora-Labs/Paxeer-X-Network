export const PAXSCAN_API_URL = process.env.NEXT_PUBLIC_PAXSCAN_API_URL ?? '';
export const PAXSCAN_WS_URL = process.env.NEXT_PUBLIC_PAXSCAN_WS_URL ?? '';
export const PAXSCAN_BASE_URL = process.env.NEXT_PUBLIC_PAXSCAN_BASE_URL ?? '';

if (process.env.NODE_ENV !== 'production') {
  if (!PAXSCAN_API_URL) console.warn('[paxscan] NEXT_PUBLIC_PAXSCAN_API_URL is empty');
  if (!PAXSCAN_WS_URL) console.warn('[paxscan] NEXT_PUBLIC_PAXSCAN_WS_URL is empty');
  if (!PAXSCAN_BASE_URL) console.warn('[paxscan] NEXT_PUBLIC_PAXSCAN_BASE_URL is empty');
}
