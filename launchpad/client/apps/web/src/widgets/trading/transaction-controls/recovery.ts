import {
  attemptState,
  EvidenceIntegrityError,
  restorePending,
  serializePending,
  type PendingAttemptRecord,
  type ReceiptEvidence,
} from './domain';

export type PendingStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>;

export class PendingAttemptStore {
  readonly key: string;

  constructor(private readonly storage: PendingStorage, readonly account: string) {
    if (!/^0x[0-9a-fA-F]{40}$/.test(account)) throw new Error('invalid_account');
    this.key = `sidiora:pending:1:125:${account.toLowerCase()}`;
  }

  load(now = Date.now()): { records: PendingAttemptRecord[]; corrupt: boolean; } {
    let text: string | null;
    try { text = this.storage.getItem(this.key); } catch { return { records: [], corrupt: true }; }
    if (text === null) return { records: [], corrupt: false };
    if (text.length > 300000) return { records: [], corrupt: true };
    let values: unknown;
    try { values = JSON.parse(text); } catch { return { records: [], corrupt: true }; }
    if (!Array.isArray(values) || values.length > 200) return { records: [], corrupt: true };
    let corrupt = false;
    const records: PendingAttemptRecord[] = [];
    for (const value of values) {
      const restored = restorePending(JSON.stringify([value]), this.account, now);
      if (restored.corrupt || restored.records.some(record =>
        record.nonce !== undefined && (!Number.isSafeInteger(record.nonce) || record.nonce < 0)
      )) { corrupt = true; continue; }
      for (const record of restored.records) {
        if (records.some(existing => existing.txHash.toLowerCase() === record.txHash.toLowerCase())) { corrupt = true; continue; }
        records.push(record);
      }
    }
    return { records, corrupt };
  }

  save(record: PendingAttemptRecord, now = Date.now()): void {
    if (record.chainId !== 125 || record.from.toLowerCase() !== this.account.toLowerCase()) {
      throw new Error('pending_scope_mismatch');
    }
    if (record.version !== 1 || (record.nonce !== undefined &&
      (!Number.isSafeInteger(record.nonce) || record.nonce < 0))) throw new Error('invalid_pending_record');
    const validated = restorePending(serializePending([record], this.account, now), this.account, now);
    if (validated.corrupt || validated.records.length !== 1) throw new Error('invalid_pending_record');
    const loaded = this.load(now);
    if (loaded.corrupt) throw new Error('corrupt_pending_storage');
    const records = loaded.records.filter(value => value.txHash.toLowerCase() !== record.txHash.toLowerCase());
    this.storage.setItem(this.key, serializePending([...records, record], this.account, now));
  }

  remove(txHash: string, now = Date.now()): void {
    const loaded = this.load(now);
    if (loaded.corrupt) throw new Error('corrupt_pending_storage');
    this.storage.setItem(this.key, serializePending(
      loaded.records.filter(record => record.txHash.toLowerCase() !== txHash.toLowerCase()),
      this.account, now,
    ));
  }

  page(offset = 0, now = Date.now()): ReturnType<PendingAttemptStore['load']> {
    if (!Number.isSafeInteger(offset) || offset < 0) throw new Error('invalid_page');
    const loaded = this.load(now);
    return { ...loaded, records: loaded.records.slice(offset, offset + 25) };
  }

  clear(): void { this.storage.removeItem(this.key); }
}

export type RecoveryUpdate = {
  attempt: Readonly<PendingAttemptRecord>;
  state: ReturnType<typeof attemptState> | 'quarantined' | 'cancelled' | 'replaced' | 'changed_action';
  receipt?: ReceiptEvidence;
  error?: unknown;
};
export type RecoveryObservation = { kind: 'read_start' | 'read_end' | 'retry_scheduled' | 'wake'; txHash: string; at: number; monotonicMs: number; activeReaders: number; retries: number; delayMs?: number; state?: RecoveryUpdate['state'] };
type ReceiptReader = (attempt: Readonly<PendingAttemptRecord>, signal: AbortSignal) => Promise<ReceiptEvidence | undefined>;
type RecoveryJob = {
  attempt: Readonly<PendingAttemptRecord>;
  notify: (update: RecoveryUpdate) => void;
  due: number;
  retries: number;
  controller?: AbortController;
};

let activeReaders = 0;
const schedulers = new Set<ReceiptRecoveryScheduler>();

export class ReceiptRecoveryScheduler {
  private readonly jobs = new Map<string, RecoveryJob>();
  private timer?: ReturnType<typeof setTimeout>;
  private readonly quarantine = new Set<string>();
  private closed = false;

  constructor(private readonly read: ReceiptReader, private readonly observe?: (observation: RecoveryObservation) => void) { schedulers.add(this); }

  watch(attempt: PendingAttemptRecord, notify: (update: RecoveryUpdate) => void): () => void {
    if (this.closed) throw new Error('recovery_closed');
    const normalized = restorePending(serializePending([attempt], attempt.from, Date.now()), attempt.from, Date.now());
    if (normalized.corrupt || normalized.records.length !== 1 || (attempt.nonce !== undefined &&
      (!Number.isSafeInteger(attempt.nonce) || attempt.nonce < 0))) throw new Error('invalid_pending_attempt');
    const key = attempt.txHash.toLowerCase();
    if (this.quarantine.has(key)) throw new EvidenceIntegrityError('attempt_quarantined');
    if (this.jobs.has(key)) throw new Error('attempt_already_watched');
    if (this.jobs.size >= 200) throw new Error('recovery_queue_full');
    const job: RecoveryJob = { attempt: Object.freeze({ ...normalized.records[0] }), notify, due: Date.now(), retries: 0 };
    this.jobs.set(key, job);
    this.pump();
    return () => {
      if (this.jobs.get(key) !== job) return;
      this.jobs.delete(key);
      job.controller?.abort();
      this.pump();
    };
  }

  wake(txHash: string): void {
    const job = this.jobs.get(txHash.toLowerCase());
    if (!job || job.controller) return;
    job.due = Date.now();
    this.observe?.({ kind: 'wake', txHash: job.attempt.txHash, at: Date.now(), monotonicMs: performance.now(), activeReaders, retries: job.retries });
    this.pump();
  }

  close(): void {
    this.closed = true;
    clearTimeout(this.timer);
    for (const job of this.jobs.values()) job.controller?.abort();
    this.jobs.clear();
    this.quarantine.clear();
    schedulers.delete(this);
  }

  private pump(): void {
    clearTimeout(this.timer);
    if (this.closed) return;
    const now = Date.now();
    for (const [key, job] of this.jobs) {
      if (activeReaders >= 4) return;
      if (job.controller || job.due > now) continue;
      job.controller = new AbortController();
      activeReaders++;
      this.observe?.({ kind: 'read_start', txHash: job.attempt.txHash, at: Date.now(), monotonicMs: performance.now(), activeReaders, retries: job.retries });
      void this.check(key, job);
    }
    const due = [...this.jobs.values()].filter(job => !job.controller).map(job => job.due);
    if (due.length) this.timer = setTimeout(() => this.pump(), Math.max(1, Math.min(...due) - Date.now()));
  }

  private async check(key: string, job: RecoveryJob): Promise<void> {
    const controller = job.controller!;
    let receipt: ReceiptEvidence | undefined;
    let error: unknown;
    let state: RecoveryUpdate['state'] = attemptState(job.attempt, Date.now());
    let deadline: ReturnType<typeof setTimeout> | undefined;
    try {
      deadline = setTimeout(() => {
        controller.abort();
        if (!this.closed && this.jobs.get(key) === job) job.notify({ attempt: job.attempt, state: attemptState(job.attempt, Date.now()), error: new Error('Receipt lookup timed out; outcome remains unknown') });
      }, 10000);
      receipt = await this.read(job.attempt, controller.signal);
      try { state = attemptState(job.attempt, Date.now(), receipt); } catch { throw new EvidenceIntegrityError('invalid_receipt_evidence'); }
    } catch (caught) { receipt = undefined; error = caught; state = caught instanceof EvidenceIntegrityError ? 'quarantined' : attemptState(job.attempt, Date.now()); }
    finally { clearTimeout(deadline); activeReaders--; job.controller = undefined; this.observe?.({ kind: 'read_end', txHash: job.attempt.txHash, at: Date.now(), monotonicMs: performance.now(), activeReaders, retries: job.retries, state }); }
    const admitted = !this.closed && this.jobs.get(key) === job;
    if (admitted) {
      if (state === 'quarantined') { this.jobs.delete(key); this.quarantine.add(key); }
      else if (state === 'confirmed_success' || state === 'confirmed_reverted') this.jobs.delete(key);
      else {
        const base = Math.min(10000, 500 * 2 ** Math.min(job.retries++, 5));
        const delayMs = Math.max(500, Math.min(10000, Math.floor(base * (0.8 + Math.random() * 0.4))));
        job.due = Date.now() + delayMs;
        this.observe?.({ kind: 'retry_scheduled', txHash: job.attempt.txHash, at: Date.now(), monotonicMs: performance.now(), activeReaders, retries: job.retries, delayMs, state });
        this.jobs.delete(key);
        this.jobs.set(key, job);
      }
    }
    for (const scheduler of schedulers) scheduler.pump();
    if (admitted && !this.closed) job.notify({ attempt: job.attempt, state, receipt, error });
  }
}
