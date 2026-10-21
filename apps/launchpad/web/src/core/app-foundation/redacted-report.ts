import { CORE_DEPLOYMENT } from '@/core/network/deployment';
import type { RecoveryError } from './recovery';
export function redactedRecoveryPayload(error: RecoveryError, area: string) {
  const code = ['chunk_load', 'unauthorized', 'unavailable', 'render_failed'].includes(error.code) ? error.code : 'render_failed';
  const safe = { code, retriable: code !== 'unauthorized', correlationId: typeof error.correlationId === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(error.correlationId) ? error.correlationId : crypto.randomUUID(), deploymentVersion: CORE_DEPLOYMENT.manifestSha256, area: ['widget', 'global-search', 'platform-metrics', 'market-widget'].includes(area) ? area : 'widget' };
  return { message: safe.code, context: safe, timestamp: Date.now() };
}
export function reportRecovery(error: RecoveryError, area: string): void {
  if (typeof navigator === 'undefined') return;
  try { navigator.sendBeacon?.('/api/errors', new Blob([JSON.stringify(redactedRecoveryPayload(error, area))], { type: 'application/json' })); } catch {}
}
