import { CORE_DEPLOYMENT } from '@/core/network/deployment';
export interface RecoveryError { code: 'chunk_load' | 'unauthorized' | 'unavailable' | 'render_failed'; retriable: boolean; correlationId: string; deploymentVersion: string }
export function recoveryError(error: unknown): RecoveryError {
  let code: RecoveryError['code'] = 'render_failed';
  try {
    const status = error && typeof error === 'object' && 'status' in error ? error.status : undefined;
    const name = error instanceof Error ? error.name : '';
    const message = error instanceof Error ? error.message : '';
    code = status === 401 || status === 403 ? 'unauthorized' : name === 'ChunkLoadError' || typeof message === 'string' && /loading chunk|dynamically imported module/i.test(message) ? 'chunk_load' : typeof status === 'number' ? 'unavailable' : 'render_failed';
  } catch {}
  return { code, retriable: code !== 'unauthorized', correlationId: crypto.randomUUID(), deploymentVersion: CORE_DEPLOYMENT.manifestSha256 };
}
export function retryDelay(attempt: number): number { if (!Number.isSafeInteger(attempt) || attempt < 1 || attempt > 3) throw new RangeError('Retry limit reached'); return Math.min(30000, 500 * 2 ** (attempt - 1)) + Math.floor(Math.random() * 250); }
export async function requestDraftHandoff(): Promise<void> {
  const pending: Promise<void>[] = [];
  let accepting = true;
  window.dispatchEvent(new CustomEvent('kindle:preserve-nonsecret-drafts', { detail: { waitUntil(work: Promise<void>) { if (!accepting) throw new Error('Draft handoff ended'); pending.push(work); } } }));
  accepting = false;
  await Promise.all(pending);
}
