import { EXPLORER_BASE_URL } from '@/core/chain';

export const paxscanTxUrl = (hash: string) => `${ EXPLORER_BASE_URL }/tx/${ hash }`;
export const paxscanAddressUrl = (addr: string) => `${ EXPLORER_BASE_URL }/address/${ addr }`;
export const paxscanBlockUrl = (n: number | string) => `${ EXPLORER_BASE_URL }/block/${ n }`;
export const paxscanTokenUrl = (addr: string) => `${ EXPLORER_BASE_URL }/token/${ addr }`;
export const paxscanSearchUrl = (q: string) => `${ EXPLORER_BASE_URL }/search-results?q=${ encodeURIComponent(q) }`;
