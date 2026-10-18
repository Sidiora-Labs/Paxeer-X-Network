export type WalletMeasurementKind = 'connector.initialize' | 'connector.connect' | 'wallet.sign' | 'wallet.send' | 'auth.challenge' | 'auth.login' | 'auth.refresh' | 'auth.read' | 'balance.refresh' | 'wallet.event' | 'wallet.cache_clear' | 'wallet.refresh_batch' | 'wallet.epoch_discard' | 'asset.metadata' | 'transfer.preview' | 'transfer.confirm';
export interface WalletMeasurementIdentity { runId: string; sourceSha256: string; deploymentManifestHash: string }
export interface WalletMeasurementSample { sequence: number; operationId: number; kind: WalletMeasurementKind; phase: 'start' | 'end' | 'event'; monotonicMs: number; wallTime: string; scope: string; outcome?: 'resolved' | 'rejected' }
export interface WalletMeasurementRaw { schemaVersion: 1; identity: Readonly<WalletMeasurementIdentity>; startedAt: string; endedAt: string; startedMonotonicMs: number; endedMonotonicMs: number; dropped: number; samples: readonly WalletMeasurementSample[] }
export class WalletMeasurementCapture {
  private samples: WalletMeasurementSample[] = [];
  private sequence = 0;
  private operation = 0;
  private dropped = 0;
  private closed = false;
  private readonly startedAt = new Date().toISOString();
  private readonly startedMonotonicMs = performance.now();
  readonly identity: Readonly<WalletMeasurementIdentity>;
  constructor(identity: WalletMeasurementIdentity) {
    if (!/^[a-zA-Z0-9_-]{1,80}$/.test(identity.runId) || !/^[0-9a-f]{64}$/.test(identity.sourceSha256) || !/^[0-9a-f]{64}$/.test(identity.deploymentManifestHash)) throw new Error('Current source and deployment measurement binding required');
    this.identity = Object.freeze({ ...identity });
  }
  private append(kind: WalletMeasurementKind, phase: WalletMeasurementSample['phase'], operationId: number, scope: string, outcome?: WalletMeasurementSample['outcome']) {
    if (this.closed) return;
    if (!/^[a-zA-Z0-9:._-]{1,160}$/.test(scope)) throw new Error('Invalid measurement scope');
    if (this.samples.length >= 50000) { this.dropped++; return; }
    this.samples.push(Object.freeze({ sequence: ++this.sequence, operationId, kind, phase, monotonicMs: performance.now(), wallTime: new Date().toISOString(), scope, ...(outcome === undefined ? {} : { outcome }) }));
  }
  event(kind: WalletMeasurementKind, scope: string) { this.append(kind, 'event', ++this.operation, scope); }
  measure<T>(kind: WalletMeasurementKind, scope: string, work: () => Promise<T>): Promise<T> {
    const id = ++this.operation;
    this.append(kind, 'start', id, scope);
    let result: Promise<T>;
    try { result = work(); } catch (error) { this.append(kind, 'end', id, scope, 'rejected'); return Promise.reject(error); }
    return result.then(value => { this.append(kind, 'end', id, scope, 'resolved'); return value; }, error => { this.append(kind, 'end', id, scope, 'rejected'); throw error; });
  }
  finish(): WalletMeasurementRaw {
    if (this.closed) throw new Error('Measurement capture already finished');
    this.closed = true;
    return Object.freeze({ schemaVersion: 1, identity: this.identity, startedAt: this.startedAt, endedAt: new Date().toISOString(), startedMonotonicMs: this.startedMonotonicMs, endedMonotonicMs: performance.now(), dropped: this.dropped, samples: Object.freeze([...this.samples]) });
  }
}
let active: WalletMeasurementCapture | undefined;
export function beginWalletMeasurement(identity: WalletMeasurementIdentity) {
  if (active) throw new Error('Wallet measurement already active');
  active = new WalletMeasurementCapture(identity);
}
export function finishWalletMeasurement() {
  if (!active) throw new Error('Wallet measurement was not started');
  const capture = active; active = undefined;
  return capture.finish();
}
export function measureWalletOperation<T>(kind: WalletMeasurementKind, scope: string, work: () => Promise<T>): Promise<T> { return active ? active.measure(kind, scope, work) : work(); }
export function recordWalletEvent(kind: WalletMeasurementKind, scope: string) { active?.event(kind, scope); }
export function installWalletMeasurementCapture(target: Window): () => void {
  const start = (event: Event) => { const input: unknown = (event as CustomEvent<unknown>).detail; if (typeof input !== 'object' || input === null || !('runId' in input) || !('sourceSha256' in input) || !('deploymentManifestHash' in input) || typeof input.runId !== 'string' || typeof input.sourceSha256 !== 'string' || typeof input.deploymentManifestHash !== 'string') throw new Error('Invalid wallet measurement binding'); beginWalletMeasurement({ runId: input.runId, sourceSha256: input.sourceSha256, deploymentManifestHash: input.deploymentManifestHash }); };
  const finish = () => { target.dispatchEvent(new CustomEvent('kindle:wallet-measurement-result', { detail: finishWalletMeasurement() })); };
  target.addEventListener('kindle:wallet-measurement-start', start);
  target.addEventListener('kindle:wallet-measurement-finish', finish);
  return () => { target.removeEventListener('kindle:wallet-measurement-start', start); target.removeEventListener('kindle:wallet-measurement-finish', finish); if (active) { active.finish(); active = undefined; } };
}
