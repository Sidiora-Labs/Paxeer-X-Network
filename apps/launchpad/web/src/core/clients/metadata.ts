/**
 * Metadata service client (media/metadata, NEXT_PUBLIC_METADATA_API_URL).
 *
 * Provides a single network-aware fetcher for token metadata. Uses the
 * batch endpoint (`GET /metadata/batch?addresses=...`) when available,
 * falls back to per-token requests when the backend is older than the
 * batch endpoint deploy.
 *
 * - Caps requests at 100 addresses per call (matches backend MAX_BATCH).
 * - Deduplicates and lowercases input.
 * - Coalesces concurrent fetches for the same set of misses through an
 *   in-memory inflight map so a burst of consumers triggers ONE request.
 * - Returns a map keyed by lowercased address. Missing/404 entries are
 *   stored as `null`.
 */

import { metadataApiUrl } from '@/core/sdk-config';
import { reportError } from '@/core/report-error';
import type { TokenMetadata } from '@/widgets/home/types';

// Backend cap. If we're asked for more, chunk client-side.
export const METADATA_BATCH_LIMIT = 100;

export class MetadataUnavailable extends Error {constructor(){super("Metadata unavailable");this.name="MetadataUnavailable";}}
type MaybeMeta = TokenMetadata | null;

function validMeta(v:unknown,address:string):v is TokenMetadata {
 if(!v||typeof v!=='object'||Array.isArray(v))return false;const m=v as Record<string,unknown>;
 if(typeof m.token_address!=='string'||m.token_address.toLowerCase()!==address)return false;
 for(const k of ['name','symbol','description'])if(m[k]!=null&&(typeof m[k]!=='string'||(m[k] as string).length>8192))return false;
 if(m.decimals!==undefined&&(!Number.isInteger(m.decimals)||Number(m.decimals)<0||Number(m.decimals)>255))return false;
 if(m.tags!==undefined&&(!Array.isArray(m.tags)||m.tags.length>20||m.tags.some(x=>typeof x!=='string'||x.length>512)))return false;
 return true;
}
async function boundedJSON(res:Response):Promise<unknown>{if(!res.body)throw new MetadataUnavailable();const reader=res.body.getReader();const parts:Uint8Array[]=[];let size=0;try{for(;;){const {done,value}=await reader.read();if(done)break;size+=value.length;if(size>1024*1024)throw new MetadataUnavailable();parts.push(value)}}finally{await reader.cancel()};const bytes=new Uint8Array(size);let i=0;for(const p of parts){bytes.set(p,i);i+=p.length};try{return JSON.parse(new TextDecoder('utf-8',{fatal:true}).decode(bytes))}catch{throw new MetadataUnavailable()}}

// ── Internal: inflight de-duplication ─────────────────────────────

const inflight = new Map<string, Promise<MaybeMeta>>();

/**
 * Normalize, dedup, lowercase. Drops anything that doesn't look like an
 * EVM address. Order preserved.
 */
function normalizeAddresses(addresses: string[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const raw of addresses) {
    if (typeof raw !== 'string') continue;
    const a = raw.toLowerCase();
    if (!/^0x[a-f0-9]{40}$/.test(a)) continue;
    if (seen.has(a)) continue;
    seen.add(a);
    out.push(a);
  }
  return out;
}

function chunk<T>(arr: T[], size: number): T[][] {
  if (arr.length <= size) return [arr];
  const out: T[][] = [];
  for (let i = 0; i < arr.length; i += size) {
    out.push(arr.slice(i, i + size));
  }
  return out;
}

// ── Single fetch (kept for fallback + standalone usage) ───────────

export async function fetchTokenMetadata(
  tokenAddress: string,
  signal?: AbortSignal,
): Promise<MaybeMeta> {
  const addr = tokenAddress.toLowerCase();
  if (!/^0x[a-f0-9]{40}$/.test(addr)) return null;

  const existing = inflight.get(addr);
  if (existing) return existing;

  const promise = (async () => {
    try {
      const res = await fetch(metadataApiUrl(`/metadata/${addr}`), { signal });
      if (res.status===404) return null;
      if (!res.ok) throw new MetadataUnavailable();
      const value=await boundedJSON(res);if(!validMeta(value,addr))throw new MetadataUnavailable();return value;
    } catch (err) {
      if ((err as { name?: string })?.name !== 'AbortError') {
        reportError(err, { area: 'metadata-client', addr });
      }
      throw err;
    } finally {
      inflight.delete(addr);
    }
  })();

  inflight.set(addr, promise);
  return promise;
}

// ── Batch fetch ────────────────────────────────────────────────────

/**
 * Whether the metadata service supports `/metadata/batch`. Set to false
 * after a 404 so subsequent calls go straight to per-token fetches.
 * Re-checked once per session.
 */
let batchEndpointSupported: boolean | null = null;

async function fetchBatchOnce(
  addresses: string[],
  signal?: AbortSignal,
): Promise<Record<string, MaybeMeta>> {
  if (addresses.length === 0) return {};

  // ── Try the batch endpoint first ──
  if (batchEndpointSupported !== false) {
    try {
      const url = metadataApiUrl(
        `/metadata/batch?addresses=${encodeURIComponent(addresses.join(','))}`,
      );
      const res = await fetch(url, { signal });
      if (res.ok) {
        batchEndpointSupported = true;
        const value=await boundedJSON(res);if(!value||typeof value!=='object'||Array.isArray(value))throw new MetadataUnavailable();const data=value as Record<string,MaybeMeta>;
        // Ensure every requested address has a key, even if backend
        // dropped it (shouldn't happen, but defensive).
        const out: Record<string, MaybeMeta> = {};
        for(const a of addresses){if(!Object.hasOwn(data,a)||(data[a]!==null&&!validMeta(data[a],a)))throw new MetadataUnavailable();out[a]=data[a]}
        return out;
      }
      if (res.status === 404) {
        batchEndpointSupported = false; // older backend → fall through
      } else {
        // Preserve transient failure; do not fan out into per-token reads.
        throw new MetadataUnavailable();
      }
    } catch (err) {
      if ((err as { name?: string })?.name === 'AbortError') {
        throw err;
      }
      // Preserve network error without changing capability or cached data.
      throw err;
    }
  }

  return fallbackPerToken(addresses, signal);
}

async function fallbackPerToken(
  addresses: string[],
  signal?: AbortSignal,
): Promise<Record<string, MaybeMeta>> {
  const results = await Promise.all(
    addresses.map((a) => fetchTokenMetadata(a, signal)),
  );
  const out: Record<string, MaybeMeta> = {};
  addresses.forEach((a, i) => {
    out[a] = results[i];
  });
  return out;
}

/**
 * Fetch metadata for many tokens. Returns a map keyed by lowercased
 * address; missing tokens map to `null`.
 *
 * - Order-independent.
 * - Deduplicates input.
 * - Splits requests over METADATA_BATCH_LIMIT.
 * - Returns `{}` for empty / all-invalid input.
 *
 * Use `useTokenMetadataBatch` from `hooks/market/use-token-metadata.ts`
 * inside React components — it wraps this and seeds the per-address
 * React Query cache so individual `useTokenMetadata(addr)` calls in
 * children don't refetch.
 */
export async function fetchTokenMetadataBatch(
  addresses: string[],
  signal?: AbortSignal,
): Promise<Record<string, MaybeMeta>> {
  const normalized = normalizeAddresses(addresses);
  if (normalized.length === 0) return {};

  const groups = chunk(normalized, METADATA_BATCH_LIMIT);
  const merged: Record<string, MaybeMeta> = {};

  for (const group of groups) {
    const part = await fetchBatchOnce(group, signal);
    Object.assign(merged, part);
  }

  return merged;
}

export type MetadataV2Data = import('@kindle/sdk').V2ContractTypes['MetadataGetData'];
export type MetadataV2Patch = import('@kindle/sdk').V2ContractTypes['MetadataPatchBody'];
export type MetadataV2Operation = {wallet:string;operationId:string;nonce:string;expiresAt:string;audience:string;path:string;body:string;bodyHash:string;statement:string};
async function metadataV2Decode(response:Response):Promise<MetadataV2Data>{
 const {decodeV2Contract}=await import('@kindle/sdk');
 const envelope=decodeV2Contract('MetadataGetResponse',await boundedJSON(response));
 if(envelope.data.token.sourcePosition?.chainId!=='125')throw new MetadataUnavailable();
 return envelope.data;
}
export async function fetchMetadataV2(tokenAddress:string,signal?:AbortSignal):Promise<MetadataV2Data|null>{
 const token=tokenAddress.toLowerCase();if(!/^0x[a-f0-9]{40}$/.test(token))throw new TypeError('Invalid token address');
 const response=await fetch(metadataApiUrl(`/v2/tokens/${token}/metadata`),{signal,cache:'no-store'});
 if(response.status===404)return null;if(!response.ok)throw new MetadataUnavailable();const data=await metadataV2Decode(response);if(data.token.address.toLowerCase()!==token)throw new MetadataUnavailable();return data;
}
export async function prepareMetadataV2Operation(tokenAddress:string,wallet:string,audience:string,patch:MetadataV2Patch):Promise<MetadataV2Operation>{
 const {decodeV2Contract}=await import('@kindle/sdk');decodeV2Contract('MetadataPatchBody',patch);
 const token=tokenAddress.toLowerCase();wallet=wallet.toLowerCase();
 if(!/^0x[a-f0-9]{40}$/.test(token)||!/^0x[a-f0-9]{40}$/.test(wallet)||!audience||/[\r\n]/.test(audience))throw new TypeError('Invalid metadata action scope');
 const body=JSON.stringify(patch),bytes=new TextEncoder().encode(body);if(bytes.length>32768)throw new TypeError('Metadata exceeds 32 KiB');
 const hex=(value:Uint8Array)=>Array.from(value,n=>n.toString(16).padStart(2,'0')).join('');
 const bodyHash=hex(new Uint8Array(await crypto.subtle.digest('SHA-256',bytes)));const nonce=hex(crypto.getRandomValues(new Uint8Array(32)));
 const operationId=crypto.randomUUID(),expiresAt=String(Math.floor(Date.now()/1000)+240),path=`/v2/tokens/${token}/metadata`;
 const statement=['KindleLaunch metadata action v2',audience,'125',wallet,operationId,'PATCH',path,bodyHash,nonce,expiresAt].join('\n');
 return {wallet,operationId,nonce,expiresAt,audience,path,body,bodyHash,statement};
}
export async function sendMetadataV2Operation(operation:MetadataV2Operation,signature:string,signal?:AbortSignal,authenticationHeaders:Record<string,string>={}):Promise<MetadataV2Data>{
 if(!/^0x[0-9a-fA-F]+$/.test(signature))throw new TypeError('Invalid wallet signature');
 const response=await fetch(metadataApiUrl(operation.path),{method:'PATCH',signal,credentials:'include',redirect:'error',body:operation.body,headers:{...authenticationHeaders,'Content-Type':'application/json','X-Action-Wallet':operation.wallet,'Idempotency-Key':operation.operationId,'X-Action-Signature':signature,'X-Action-Nonce':operation.nonce,'X-Action-Expires':operation.expiresAt,'X-Action-Audience':operation.audience}});
 if(response.status===403)throw new Error('Wallet authorization was rejected.');
 if(response.status===409)throw new Error('Metadata revision changed. Reload the page before editing again.');
 if(response.status===422)throw new Error('Check the metadata field lengths and links.');
 if(!response.ok)throw new MetadataUnavailable();const data=await metadataV2Decode(response);if(operation.path!==`/v2/tokens/${data.token.address.toLowerCase()}/metadata`)throw new MetadataUnavailable();return data;
}

export type MetadataMediaImage={assetId:string;sha256:string;mime:'image/png'|'image/jpeg'|'image/webp';width:number;height:number;variants:Record<'256'|'512'|'1024',string>};
export type MetadataMediaProjection={metadata:{chainId:125;poolAddress:string;tokenAddress:string;revision:string;updatedAt:string|null;source:string;moderationStatus:'allowed'|'blocked';logoAssetId:string|null;display:{name:string|null;symbol:string|null;description:string;tags:string[];socials:Array<{label:string;url:string}>}};image:MetadataMediaImage|null};
export async function fetchMetadataMedia(tokenAddress:string,signal?:AbortSignal):Promise<MetadataMediaProjection>{
 const token=tokenAddress.toLowerCase();if(!/^0x[a-f0-9]{40}$/.test(token))throw new TypeError('Invalid token address');
 const response=await fetch(metadataApiUrl(`/v2/tokens/${token}/media`),{signal,cache:'no-store',redirect:'error'});if(!response.ok)throw new MetadataUnavailable();
 const raw=await boundedJSON(response);if(!raw||typeof raw!=='object')throw new MetadataUnavailable();const outer=raw as Record<string,unknown>;
 const body=outer.data;if(!body||typeof body!=='object')throw new MetadataUnavailable();const data=body as MetadataMediaProjection;
 const m=data.metadata;
 if(!m||m.chainId!==125||m.tokenAddress!==token||!/^0x[a-f0-9]{40}$/.test(m.poolAddress)||!revisionValidMedia(m.revision)||!['allowed','blocked'].includes(m.moderationStatus))throw new MetadataUnavailable();
 if(!m.display||typeof m.display.description!=='string'||Array.from(m.display.description).length>4096||!Array.isArray(m.display.tags)||m.display.tags.length>20||!Array.isArray(m.display.socials)||m.display.socials.length>5)throw new MetadataUnavailable();
 for(const link of m.display.socials){if(!link||typeof link.label!=='string'||typeof link.url!=='string')throw new MetadataUnavailable();try{const u=new URL(link.url);if(u.protocol!=='https:'||u.username||u.password)throw new Error()}catch{throw new MetadataUnavailable()}}
 const image=data.image;
 if(m.moderationStatus==='blocked'&&(image!==null||m.logoAssetId!==null))throw new MetadataUnavailable();
 if(image!==null){if(!image||image.assetId!==m.logoAssetId||!/^0x[a-f0-9]{64}$/.test(image.sha256)||!['image/png','image/jpeg','image/webp'].includes(image.mime)||!Number.isInteger(image.width)||!Number.isInteger(image.height)||image.width<1||image.height<1||image.width>8192||image.height>8192||image.width*image.height>20000000)throw new MetadataUnavailable();for(const size of ['256','512','1024'] as const){try{const u=new URL(image.variants[size]);if(u.protocol!=='https:'||u.username||u.password||u.search||u.hash||!new RegExp(`^/approved/${image.sha256.slice(2)}/${size}/[a-f0-9]{64}\\.png$`).test(u.pathname))throw new Error()}catch{throw new MetadataUnavailable()}}}
 return data;
}
function revisionValidMedia(s:unknown):s is string{return typeof s==='string'&&/^(0|[1-9][0-9]{0,77})$/.test(s)&&BigInt(s)<=((1n<<256n)-1n)}
