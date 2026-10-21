export type ControlledCapability =
  | 'pool_creation'
  | 'quote'
  | 'socket'
  | 'paid_features';

export interface CapabilityWarning {
  capability: ControlledCapability;
  allowed: boolean;
  stale: boolean;
  reason: string;
  revision: string;
  observedAt: string;
}

export class CapabilityAdmissionError extends Error {
  readonly warning: CapabilityWarning;

  constructor(warning: CapabilityWarning) {
    const name = warning.capability === 'pool_creation'
      ? 'Pool creation'
      : warning.capability.replaceAll('_', ' ');
    super(warning.stale
      ? `${name} is unavailable because its capability status is stale or unavailable.`
      : `${name} is currently disabled.`);
    this.name = 'CapabilityAdmissionError';
    this.warning = warning;
  }
}

function unavailable(capability: ControlledCapability): CapabilityWarning {
  return {
    capability,
    allowed: false,
    stale: true,
    reason: 'cap_unavailable',
    revision: '0',
    observedAt: new Date().toISOString(),
  };
}

function parseWarning(value: unknown, capability: ControlledCapability): CapabilityWarning {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new CapabilityAdmissionError(unavailable(capability));
  }
  const envelope = value as Record<string, unknown>;
  const data = envelope.data;
  if (typeof data !== 'object' || data === null || Array.isArray(data)) {
    throw new CapabilityAdmissionError(unavailable(capability));
  }
  const warning = data as Record<string, unknown>;
  if (
    warning.capability !== capability ||
    typeof warning.allowed !== 'boolean' ||
    typeof warning.stale !== 'boolean' ||
    typeof warning.reason !== 'string' ||
    warning.reason.length === 0 ||
    warning.reason.length > 128 ||
    typeof warning.revision !== 'string' ||
    !/^(0|[1-9][0-9]{0,77})$/.test(warning.revision) ||
    typeof warning.observedAt !== 'string' ||
    !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/.test(warning.observedAt)
  ) {
    throw new CapabilityAdmissionError(unavailable(capability));
  }
  const observedAt = Date.parse(warning.observedAt);
  const age = Date.now() - observedAt;
  if (!Number.isFinite(observedAt) || age < -1_000 || age > 60_000) {
    throw new CapabilityAdmissionError({
      capability,
      allowed: false,
      stale: true,
      reason: 'cap_stale',
      revision: warning.revision,
      observedAt: warning.observedAt,
    });
  }
  if (warning.allowed && (warning.stale || warning.revision === '0' || warning.reason !== 'available')) {
    throw new CapabilityAdmissionError(unavailable(capability));
  }
  return {
    capability,
    allowed: warning.allowed,
    stale: warning.stale,
    reason: warning.reason,
    revision: warning.revision,
    observedAt: warning.observedAt,
  };
}

export async function readFreshCapability(
  capability: ControlledCapability,
  signal?: AbortSignal,
): Promise<CapabilityWarning> {
  const controller = new AbortController();
  const abort = () => controller.abort(signal?.reason);
  if (signal?.aborted) abort();
  else signal?.addEventListener('abort', abort, { once: true });
  const timer = setTimeout(() => controller.abort(), 5_000);
  try {
    const response = await fetch(`/v2/control/caps/${capability}`, {
      method: 'GET',
      credentials: 'same-origin',
      cache: 'no-store',
      redirect: 'error',
      headers: { Accept: 'application/json' },
      signal: controller.signal,
    });
    if (!response.ok) throw new CapabilityAdmissionError(unavailable(capability));
    const declaredLength = response.headers.get('Content-Length');
    if (declaredLength !== null && (!/^\d+$/.test(declaredLength) || Number(declaredLength) > 65_536)) {
      throw new CapabilityAdmissionError(unavailable(capability));
    }
    if (!response.body) throw new CapabilityAdmissionError(unavailable(capability));
    const reader = response.body.getReader();
    const decoder = new TextDecoder('utf-8', { fatal: true });
    let size = 0;
    let text = '';
    try {
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        size += value.byteLength;
        if (size > 65_536) throw new CapabilityAdmissionError(unavailable(capability));
        text += decoder.decode(value, { stream: true });
      }
      text += decoder.decode();
    } finally {
      await reader.cancel();
      reader.releaseLock();
    }
    return parseWarning(JSON.parse(text) as unknown, capability);
  } catch (error: unknown) {
    if (error instanceof CapabilityAdmissionError) throw error;
    throw new CapabilityAdmissionError(unavailable(capability));
  } finally {
    clearTimeout(timer);
    signal?.removeEventListener('abort', abort);
  }
}

export async function admitBeforeWallet<T>(
  invokeWallet: () => Promise<T>,
  showCapability: (warning: CapabilityWarning) => void,
  signal?: AbortSignal,
): Promise<T> {
  let warning: CapabilityWarning;
  try {
    warning = await readFreshCapability('pool_creation', signal);
  } catch (error: unknown) {
    if (error instanceof CapabilityAdmissionError) showCapability(error.warning);
    throw error;
  }
  showCapability(warning);
  const age = Date.now() - Date.parse(warning.observedAt);
  if (!warning.allowed || warning.stale || signal?.aborted || !Number.isFinite(age) || age < -1_000 || age > 60_000) {
    const expired = !Number.isFinite(age) || age < -1_000 || age > 60_000;
    const denied = { ...warning, allowed: false, stale: warning.stale || expired, reason: expired ? 'cap_stale' : warning.reason };
    if (expired) showCapability(denied);
    throw new CapabilityAdmissionError(denied);
  }
  return invokeWallet();
}
