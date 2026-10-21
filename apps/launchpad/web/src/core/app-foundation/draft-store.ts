import { CORE_DEPLOYMENT } from '@/core/network/deployment';
export interface DraftPayload { name?: string; symbol?: string; description?: string; imageAssetId?: string; tags?: string[]; filters?: Record<string, string> }
export interface DraftEnvelope { version: 1; createdAt: number; expiresAt: number; deploymentVersion: string; chainId: 125; payload: DraftPayload }
export type DraftRecovery = { status: 'absent' | 'invalid' | 'expired' } | { status: 'restored' | 'deployment_changed'; draft: DraftEnvelope; requiresValidation: true };
const PREFIX = 'kindle:draft:v1:';
const DAY = 86400000;
const MAX_FLOW = 65536;
const MAX_TOTAL = 524288;
const memory = new Map<string, string>();
function flowKey(flow: string) { if (!/^[a-z][a-z0-9-]{0,63}$/.test(flow)) throw new TypeError('Invalid draft flow'); return PREFIX + flow; }
function checkedPayload(value: unknown): DraftPayload {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('Invalid draft payload');
  const record = value as Record<string, unknown>;
  if (Object.keys(record).some(key => !['name', 'symbol', 'description', 'imageAssetId', 'tags', 'filters'].includes(key))) throw new TypeError('Draft contains unsupported fields');
  for (const key of ['name', 'symbol', 'description', 'imageAssetId']) {
    const field = record[key]; if (field !== undefined && (typeof field !== 'string' || field.length > (key === 'description' ? 4096 : 256))) throw new TypeError('Invalid draft field');
  }
  if (record.tags !== undefined && (!Array.isArray(record.tags) || record.tags.length > 20 || record.tags.some(tag => typeof tag !== 'string' || tag.length > 64))) throw new TypeError('Invalid draft tags');
  if (record.filters !== undefined) {
    if (!record.filters || typeof record.filters !== 'object' || Array.isArray(record.filters)) throw new TypeError('Invalid draft filters');
    if (Object.entries(record.filters).some(([key, field]) => !['category', 'sort', 'timeframe', 'tab'].includes(key) || typeof field !== 'string' || field.length > 256)) throw new TypeError('Invalid draft filter');
  }
  return structuredClone(value) as DraftPayload;
}
export function saveDraft(flow: string, payload: DraftPayload, consent: boolean, now = Date.now(), deploymentVersion: string = CORE_DEPLOYMENT.manifestSha256): DraftEnvelope {
  if (!Number.isSafeInteger(now) || now < 0 || !Number.isSafeInteger(now + DAY) || typeof deploymentVersion !== 'string' || !/^[a-zA-Z0-9][a-zA-Z0-9._-]{0,255}$/.test(deploymentVersion)) throw new TypeError('Invalid draft context');
  const draft: DraftEnvelope = { version: 1, createdAt: now, expiresAt: now + DAY, deploymentVersion, chainId: 125, payload: checkedPayload(payload) };
  const key = flowKey(flow), text = JSON.stringify(draft), size = new TextEncoder().encode(text).length;
  if (size > MAX_FLOW) throw new RangeError('Draft exceeds 64 KiB');
  const pending = new Map(memory); pending.set(key, text);
  if ([...pending.values()].reduce((sum, item) => sum + new TextEncoder().encode(item).length, 0) > MAX_TOTAL) throw new RangeError('Drafts exceed 512 KiB');
  if (consent && typeof localStorage !== 'undefined') {
    try {
      let total = size;
      for (let index = 0; index < localStorage.length; index++) { const storedKey = localStorage.key(index); if (storedKey && storedKey !== key && storedKey.startsWith(PREFIX)) total += new TextEncoder().encode(localStorage.getItem(storedKey) || '').length; }
      if (total > MAX_TOTAL) throw new RangeError('Drafts exceed 512 KiB');
      localStorage.setItem(key, text);
    } catch (error) { if (error instanceof RangeError) throw error; }
  } else { try { localStorage.removeItem(key); } catch {} }
  memory.set(key, text);
  return draft;
}
export function restoreDraft(flow: string, consent: boolean, now = Date.now(), deploymentVersion: string = CORE_DEPLOYMENT.manifestSha256): DraftRecovery {
  const key = flowKey(flow);
  if (!Number.isSafeInteger(now) || now < 0 || typeof deploymentVersion !== 'string' || !/^[a-zA-Z0-9][a-zA-Z0-9._-]{0,255}$/.test(deploymentVersion)) throw new TypeError('Invalid draft context');
  let text = memory.get(key);
  if (consent) { try { text = localStorage.getItem(key) ?? text; } catch {} }
  if (!text) return { status: 'absent' };
  try {
    if (new TextEncoder().encode(text).length > MAX_FLOW) throw new TypeError('Oversized draft');
    const draft = JSON.parse(text) as DraftEnvelope;
    if (!draft || typeof draft !== 'object' || Array.isArray(draft)) throw new TypeError('Unsupported draft');
    if (Object.keys(draft).some(field => !['version', 'createdAt', 'expiresAt', 'deploymentVersion', 'chainId', 'payload'].includes(field)) || draft.version !== 1 || draft.chainId !== 125 || typeof draft.deploymentVersion !== 'string' || !/^[a-zA-Z0-9][a-zA-Z0-9._-]{0,255}$/.test(draft.deploymentVersion) || !Number.isSafeInteger(draft.createdAt) || draft.createdAt < 0 || !Number.isSafeInteger(draft.expiresAt) || draft.expiresAt - draft.createdAt !== DAY || draft.createdAt > now) throw new TypeError('Unsupported draft');
    draft.payload = checkedPayload(draft.payload);
    if (draft.expiresAt <= now) { removeDraft(flow); return { status: 'expired' }; }
    return { status: draft.deploymentVersion === deploymentVersion ? 'restored' : 'deployment_changed', draft, requiresValidation: true };
  } catch { removeDraft(flow); return { status: 'invalid' }; }
}
export function removeDraft(flow: string): void { const key = flowKey(flow); memory.delete(key); try { localStorage.removeItem(key); } catch {} }
