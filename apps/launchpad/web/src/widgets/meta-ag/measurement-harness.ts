import { custom } from 'viem';
import { address, ADVANCED_WORKLOAD_LIMITS, orderPageSize, type Address } from './domain';

export type MeasurementKind = 'capabilities' | 'pagination' | 'eligibility' | 'quotes' | 'cancels' | 'approvals' | 'accountSwitches' | 'customRoutes' | 'positions';
export type MeasuredOutcome = 'verified' | 'rejected' | 'reverted' | 'unknown' | 'no-route' | 'unsupported';
export interface MeasurementIdentity { runId: string; manifestSha256: string; sourceManifestSha256: string; deploymentRevision: string; chainId: 125 }
export interface OperationObservation extends MeasurementIdentity {
  type: 'operation'; sequence: number; kind: MeasurementKind; account: Address; plannedAtMs: number;
  startedAtMs: number | null; finishedAtMs: number; outcome: MeasuredOutcome | 'dropped'; raw: unknown;
}
export interface WorkloadParameters {
  profile: 'peak' | 'burst';
  durationMs: number; offeredRatePerSecond: number; maxInFlight: number; maximumSchedulerLagMs: number;
  operationTimeoutMs: number; weights: Record<MeasurementKind, number>;
}
export type Observation = MeasurementIdentity & { type: string; [key: string]: unknown };
export type ObservationSink = (event: Observation | OperationObservation) => void;
export interface ActualOperation {
  account: Address; run: (kind: MeasurementKind) => Promise<{ outcome: MeasuredOutcome; raw: unknown }>;
}
const kinds: readonly MeasurementKind[] = ['capabilities','pagination','eligibility','quotes','cancels','approvals','accountSwitches','customRoutes','positions'];
const outcomes = ['verified','rejected','reverted','unknown','no-route','unsupported'] as const;

export function validateMeasurementIdentity(value: MeasurementIdentity) {
  if (!/^[0-9a-f-]{36}$/i.test(value.runId) || !/^[0-9a-f]{64}$/.test(value.manifestSha256) || !/^[0-9a-f]{64}$/.test(value.sourceManifestSha256) || value.chainId !== 125 || !value.deploymentRevision || value.deploymentRevision.length > 128) throw new Error('Measurement source/deployment/run identity unavailable');
}
export function validateWorkload(value: WorkloadParameters) {
  if (!['peak','burst'].includes(value.profile)) throw new Error('Explicit admitted global peak/burst workload profile required');
  for (const name of ['durationMs','maxInFlight','maximumSchedulerLagMs','operationTimeoutMs'] as const) if (!Number.isSafeInteger(value[name]) || value[name] <= 0) throw new Error(`Invalid actual workload ${name}`);
  if (value.durationMs > 600000 || value.maxInFlight > 512 || value.operationTimeoutMs > 60000 || !Number.isFinite(value.offeredRatePerSecond) || value.offeredRatePerSecond <= 0 || value.offeredRatePerSecond > (value.profile === 'peak' ? 10000 : 20000) || Math.ceil(value.durationMs * value.offeredRatePerSecond / 1000) > 1000000) throw new Error('Lane workload exceeds bounded execution/capture budget');
  if (!value.weights || Object.keys(value.weights).length !== kinds.length || Object.keys(value.weights).some(key => !kinds.includes(key as MeasurementKind))) throw new Error('Complete mixed workload required');
  if (Math.ceil(value.durationMs * value.offeredRatePerSecond / 1000) < Object.values(value.weights).reduce((sum,value) => sum + value,0)) throw new Error('Duration/rate cannot offer the complete declared operation mix');
  for (const kind of kinds) if (!Number.isSafeInteger(value.weights[kind]) || value.weights[kind] < 1 || value.weights[kind] > 1000) throw new Error(`Missing actual workload ${kind}`);
}
export function percentile(values: readonly number[], quantile: number): number | null {
  if (!Number.isFinite(quantile) || quantile <= 0 || quantile > 1 || values.some(value => !Number.isFinite(value) || value < 0)) throw new Error('Malformed measured latency/quantile');
  if (values.length === 0) return null;
  const sorted = [...values].sort((a,b) => a - b);
  return sorted[Math.ceil(quantile * sorted.length) - 1]!;
}
export function summarizeOperations(records: readonly OperationObservation[], identity: MeasurementIdentity, durationMs: number) {
  validateMeasurementIdentity(identity);
  if (!Number.isFinite(durationMs) || durationMs <= 0 || records.length === 0) throw new Error('Missing real elapsed workload');
  const sequences = new Set<number>();
  const grouped = Object.fromEntries(kinds.map(kind => [kind, { offered: 0, admitted: 0, verified: 0, rejected: 0, reverted: 0, unknown: 0, noRoute: 0, unsupported: 0, dropped: 0, latencyMs: [] as number[], serviceMs: [] as number[] }])) as Record<MeasurementKind, { offered: number; admitted: number; verified: number; rejected: number; reverted: number; unknown: number; noRoute: number; unsupported: number; dropped: number; latencyMs: number[]; serviceMs: number[] }>;
  for (const record of records) {
    if (record.runId !== identity.runId || record.manifestSha256 !== identity.manifestSha256 || record.sourceManifestSha256 !== identity.sourceManifestSha256 || record.deploymentRevision !== identity.deploymentRevision || record.chainId !== 125 || !kinds.includes(record.kind) || !Number.isSafeInteger(record.sequence) || record.sequence < 0 || sequences.has(record.sequence)) throw new Error('Duplicate or foreign workload observation');
    sequences.add(record.sequence); address(record.account);
    if (![record.plannedAtMs,record.finishedAtMs].every(Number.isFinite) || record.finishedAtMs < record.plannedAtMs || record.startedAtMs !== null && (!Number.isFinite(record.startedAtMs) || record.startedAtMs < record.plannedAtMs || record.startedAtMs > record.finishedAtMs)) throw new Error('Invalid measured timing');
    const group = grouped[record.kind]; group.offered++;
    if (record.outcome === 'dropped') { if (record.startedAtMs !== null) throw new Error('Dropped request cannot be admitted'); group.dropped++; continue; }
    if (record.startedAtMs === null || !outcomes.includes(record.outcome)) throw new Error('Unclassified admitted outcome');
    group.admitted++; group.latencyMs.push(record.finishedAtMs - record.plannedAtMs); group.serviceMs.push(record.finishedAtMs - record.startedAtMs);
    if (record.outcome === 'no-route') group.noRoute++; else group[record.outcome]++;
  }
  return { durationMs, operations: Object.fromEntries(kinds.map(kind => { const value = grouped[kind]; return [kind, { ...value, offeredPerSecond: value.offered * 1000 / durationMs, admittedPerSecond: value.admitted * 1000 / durationMs, verifiedPerSecond: value.verified * 1000 / durationMs, p95Ms: percentile(value.latencyMs,.95), p99Ms: percentile(value.latencyMs,.99), maximumMs: value.latencyMs.length ? value.latencyMs.reduce((maximum, value) => Math.max(maximum,value),0) : null }]; })), globalCapacityAccepted: false, finalAcceptanceOwner: 'serial9.1' };
}

export async function runActualWorkload(identity: MeasurementIdentity, parameters: WorkloadParameters, workers: readonly ActualOperation[], sink: ObservationSink, signal?: AbortSignal) {
  validateMeasurementIdentity(identity); validateWorkload(parameters);
  if (workers.length < 2 || workers.length > 512 || new Set(workers.map(worker => address(worker.account))).size < 2) throw new Error('At least two actual funded owner identities required');
  const schedule = kinds.flatMap(kind => Array<MeasurementKind>(parameters.weights[kind]).fill(kind));
  const records: OperationObservation[] = [], active = new Set<Promise<void>>();
  const occupied = new Set<number>();
  let captureFailure: unknown;
  const started = performance.now(), interval = 1000 / parameters.offeredRatePerSecond;
  const count = Math.ceil(parameters.durationMs / interval);
  for (let sequence = 0; sequence < count; sequence++) {
    if (signal?.aborted) throw new Error('Actual workload interrupted; partial evidence retained');
    const plannedAtMs = started + sequence * interval;
    const wait = plannedAtMs - performance.now();
    if (wait > 0) await new Promise<void>(resolve => setTimeout(resolve,wait));
    const now = performance.now(), kind = schedule[sequence % schedule.length];
    const available = workers.findIndex((_,index) => !occupied.has(index));
    if (available < 0 || active.size >= parameters.maxInFlight || now - plannedAtMs > parameters.maximumSchedulerLagMs) {
      const record: OperationObservation = { ...identity, type: 'operation', sequence, kind, account: workers[sequence % workers.length].account, plannedAtMs, startedAtMs: null, finishedAtMs: now, outcome: 'dropped', raw: { reason: available < 0 ? 'actual_owner_busy' : active.size >= parameters.maxInFlight ? 'inflight_budget' : 'scheduler_lag' } };
      sink(record); records.push({ ...record,raw: null }); continue;
    }
    occupied.add(available);
    const worker = workers[available];
    const job: Promise<void> = (async () => {
      let outcome: MeasuredOutcome = 'unknown', raw: unknown;
      const timeout = setTimeout(() => sink({ ...identity, type: 'operation_deadline', sequence, kind, observedAtMs: performance.now(), reason: 'Provider work retains owner admission until actual completion' }), parameters.operationTimeoutMs);
      try { const result = await worker.run(kind); if (!outcomes.includes(result.outcome)) throw new Error('Provider returned unclassified outcome'); outcome = result.outcome; raw = result.raw; }
      catch (error) { raw = { error: error instanceof Error ? error.message : String(error) }; }
      finally {
        clearTimeout(timeout);
        const record: OperationObservation = { ...identity, type: 'operation', sequence, kind, account: worker.account, plannedAtMs, startedAtMs: now, finishedAtMs: performance.now(), outcome, raw };
        sink(record); records.push({ ...record,raw: null }); occupied.delete(available);
      }
    })();
    active.add(job);
    void job.then(() => active.delete(job), error => { captureFailure = error; active.delete(job); });
  }
  await Promise.all(active);
  if (captureFailure) throw captureFailure;
  return summarizeOperations(records,identity,performance.now() - started);
}

export function createCapturedRpcTransport(url: string, identity: MeasurementIdentity, sink: ObservationSink) {
  validateMeasurementIdentity(identity);
  const target = new URL(url);
  if (!['http:','https:'].includes(target.protocol) || !['localhost','127.0.0.1','[::1]'].includes(target.hostname) || target.username || target.password) throw new Error('Captured write transport requires explicit isolated loopback topology');
  let sequence = 0, active = 0, maximumConcurrent = 0;
  const request = async ({ method, params }: { method: string; params?: unknown }) => {
    const requestId = ++sequence, startedAtMs = performance.now(), startedAtUnixMs = Date.now();
    active++; maximumConcurrent = Math.max(maximumConcurrent,active);
    const body = JSON.stringify({ jsonrpc: '2.0', id: requestId, method, params: params ?? [] });
    let responseBody: string | null = null, status: number | null = null;
    try {
      const response = await fetch(target, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body, signal: AbortSignal.timeout(60000), redirect: 'error' });
      status = response.status;
      const reader = response.body?.getReader(); if (!reader) throw new Error('Missing actual JSON-RPC body');
      const chunks: Uint8Array[] = []; let size = 0;
      for (;;) { const next = await reader.read(); if (next.done) break; size += next.value.byteLength; if (size > 4194304) { await reader.cancel(); throw new Error('Actual RPC response exceeds capture bound'); } chunks.push(next.value); }
      const bytes = new Uint8Array(size); let offset = 0; for (const chunk of chunks) { bytes.set(chunk,offset); offset += chunk.length; }
      responseBody = new TextDecoder('utf-8',{ fatal: true }).decode(bytes);
      if (!response.ok) throw new Error(`Actual RPC HTTP${response.status}`);
      const result: unknown = JSON.parse(responseBody);
      if (!result || typeof result !== 'object') throw new Error('Malformed actual RPC result');
      const record = result as Record<string,unknown>;
      if (record.jsonrpc !== '2.0' || record.id !== requestId || (!Object.hasOwn(record,'result') && !Object.hasOwn(record,'error')) || Object.hasOwn(record,'result') && Object.hasOwn(record,'error')) throw new Error('Actual RPC identity/result conflict');
      if (record.error) { const error = record.error as { code?: number; message?: string; data?: unknown }; throw Object.assign(new Error(error.message ?? 'Actual RPC error'),{ code: error.code, data: error.data }); }
      return record.result;
    } finally {
      active--;
      sink({ ...identity, type: 'rpc', sequence: requestId, method, startedAtMs, finishedAtMs: performance.now(), startedAtUnixMs, finishedAtUnixMs: Date.now(), httpStatus: status, requestBody: body, responseBody, maximumConcurrent });
    }
  };
  return { transport: custom({ request }, { retryCount: 0 }), snapshot: () => ({ calls: sequence, active, maximumConcurrent }) };
}

export function assertActualPage(page: { items: readonly { owner: Address }[] }, owner: Address, size: number) {
  orderPageSize(size);
  if (page.items.length > size || page.items.some(item => address(item.owner) !== address(owner))) throw new Error('Actual pagination exceeds bound or crosses ownership');
}

export function inspectAdvancedAccessibility(root: HTMLElement) {
  const controls = [...root.querySelectorAll<HTMLElement>('button,input,select,textarea,a[href]')];
  const findings: string[] = [];
  for (const control of controls) {
    const input = control as HTMLInputElement;
    const labels = 'labels' in input ? [...(input.labels ?? [])].map(label => label.textContent ?? '').join(' ') : '';
    const labelledBy = control.getAttribute('aria-labelledby')?.split(/\s+/).map(id => root.ownerDocument.getElementById(id)?.textContent ?? '').join(' ');
    const name = control.getAttribute('aria-label') ?? labelledBy ?? (labels || control.textContent || '');
    if (!name.trim()) findings.push(`unnamed_control:${control.tagName}:${control.getAttribute('type') ?? ''}`);
    if (control.getAttribute('tabindex') && Number(control.getAttribute('tabindex')) > 0) findings.push('positive_tabindex');
  }
  const rect = root.getBoundingClientRect(), viewportWidth = root.ownerDocument.documentElement.clientWidth;
  if (viewportWidth <= 0 || rect.width <= 0 || rect.height <= 0) findings.push('missing_actual_rendered_browser_surface');
  if (rect.width > viewportWidth + 1 || root.scrollWidth > root.clientWidth + 1) findings.push('responsive_horizontal_overflow');
  if (!root.querySelector('[role="status"],[role="alert"],[aria-live]')) findings.push('missing_announced_status');
  const keyboard = controls.filter(control => !('disabled' in control && (control as HTMLInputElement).disabled));
  const previous = root.ownerDocument.activeElement;
  for (const control of keyboard) { control.focus(); if (root.ownerDocument.activeElement !== control) findings.push(`unfocusable_control:${control.tagName}`); }
  if (previous instanceof HTMLElement) previous.focus();
  return { keyboardControlsChecked: keyboard.length, checkedAtUnixMs: Date.now(), controls: controls.length, viewportWidth, viewportHeight: root.ownerDocument.documentElement.clientHeight, findings, bounds: { x: rect.x, y: rect.y, width: rect.width, height: rect.height }, limits: ADVANCED_WORKLOAD_LIMITS };
}
