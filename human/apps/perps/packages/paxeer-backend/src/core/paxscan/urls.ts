import { PAXSCAN_BASE_URL } from './config';

export const paxscanTxUrl = (hash: string) => `${ PAXSCAN_BASE_URL }/tx/${ hash }`;
export const paxscanAddressUrl = (addr: string) => `${ PAXSCAN_BASE_URL }/address/${ addr }`;
export const paxscanBlockUrl = (n: number | string) => `${ PAXSCAN_BASE_URL }/block/${ n }`;
export const paxscanTokenUrl = (addr: string) => `${ PAXSCAN_BASE_URL }/token/${ addr }`;
export const paxscanSearchUrl = (q: string) => `${ PAXSCAN_BASE_URL }/search-results?q=${ encodeURIComponent(q) }`;
