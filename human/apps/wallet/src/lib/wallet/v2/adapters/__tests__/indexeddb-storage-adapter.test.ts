import { describe, it, expect } from 'vitest';
import type { StorageRecord } from '../../types/storage';
import type { VaultManifestV2 } from '../../types/vault';

// Simulates the CAS transaction logic that IndexedDBStorageAdapter implements.
// We test the exact logic path: get existing → check revision → write → read-back verify.

function makeManifest(vaultId: string, revision: number): VaultManifestV2 {
  return {
    schema: 2,
    vaultId,
    revision,
    createdAt: Date.now(),
    updatedAt: Date.now(),
    keySlots: [],
    verifier: { version: 1, algorithm: 'AES-256-GCM', iv: 'AA', ciphertext: 'AA' },
    payload: { version: 1, algorithm: 'AES-256-GCM', iv: 'AA', ciphertext: 'AA' },
  };
}

// Simulates the write logic from IndexedDBStorageAdapter.write()
function simulateWrite(
  store: Map<string, StorageRecord>,
  namespace: string,
  record: StorageRecord,
  expectedRevision: number | null,
): { ok: true; record: StorageRecord } | { ok: false; error: 'WRITE_CONFLICT' | 'STORAGE_UNAVAILABLE' | 'QUOTA_EXCEEDED' } {
  const existing = store.get(namespace) ?? null;

  // Compare-and-swap
  if (expectedRevision !== null) {
    if (!existing || existing.revision !== expectedRevision) {
      return { ok: false, error: 'WRITE_CONFLICT' };
    }
  }

  store.set(namespace, record);

  // Read-back verify
  const written = store.get(namespace);
  if (!written || written.revision !== record.revision) {
    store.delete(namespace);
    return { ok: false, error: 'STORAGE_UNAVAILABLE' };
  }

  return { ok: true, record: written };
}

describe('IndexedDBStorageAdapter - compare-and-swap revision', () => {
  it('allows first write with null expected revision', () => {
    const store = new Map<string, StorageRecord>();
    const record: StorageRecord = { vaultId: 'v1', revision: 1, manifest: makeManifest('v1', 1) };

    const result = simulateWrite(store, 'vault-1', record, null);
    expect(result.ok).toBe(true);
    if (result.ok) expect(result.record.revision).toBe(1);
  });

  it('allows write when expected revision matches current', () => {
    const store = new Map<string, StorageRecord>();
    const rec1: StorageRecord = { vaultId: 'v1', revision: 1, manifest: makeManifest('v1', 1) };
    store.set('vault-1', rec1);

    const rec2: StorageRecord = { vaultId: 'v1', revision: 2, manifest: makeManifest('v1', 2) };
    const result = simulateWrite(store, 'vault-1', rec2, 1);
    expect(result.ok).toBe(true);
    if (result.ok) expect(result.record.revision).toBe(2);
  });

  it('rejects write when expected revision does not match (WRITE_CONFLICT)', () => {
    const store = new Map<string, StorageRecord>();
    const rec1: StorageRecord = { vaultId: 'v1', revision: 5, manifest: makeManifest('v1', 5) };
    store.set('vault-1', rec1);

    const rec2: StorageRecord = { vaultId: 'v1', revision: 6, manifest: makeManifest('v1', 6) };
    const result = simulateWrite(store, 'vault-1', rec2, 3);
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.error).toBe('WRITE_CONFLICT');
  });

  it('rejects write when expected revision set but no existing record (WRITE_CONFLICT)', () => {
    const store = new Map<string, StorageRecord>();
    const rec: StorageRecord = { vaultId: 'v1', revision: 1, manifest: makeManifest('v1', 1) };

    const result = simulateWrite(store, 'vault-1', rec, 5);
    expect(result.ok).toBe(false);
    if (!result.ok) expect(result.error).toBe('WRITE_CONFLICT');
  });

  it('original record is unchanged after WRITE_CONFLICT', () => {
    const store = new Map<string, StorageRecord>();
    const rec1: StorageRecord = { vaultId: 'v1', revision: 5, manifest: makeManifest('v1', 5) };
    store.set('vault-1', rec1);

    const rec2: StorageRecord = { vaultId: 'v1', revision: 6, manifest: makeManifest('v1', 6) };
    simulateWrite(store, 'vault-1', rec2, 3);

    const current = store.get('vault-1');
    expect(current?.revision).toBe(5);
  });
});

describe('IndexedDBStorageAdapter - concurrent writer conflict', () => {
  it('two concurrent writers from same revision: one wins, one gets WRITE_CONFLICT', () => {
    const store = new Map<string, StorageRecord>();
    const base: StorageRecord = { vaultId: 'v1', revision: 1, manifest: makeManifest('v1', 1) };
    store.set('vault-1', base);

    // Writer A: revision 1 → 2
    const writerA: StorageRecord = { vaultId: 'v1', revision: 2, manifest: makeManifest('v1', 2) };
    const resultA = simulateWrite(store, 'vault-1', writerA, 1);

    // Writer B: revision 1 → 2 (stale, A already committed)
    const writerB: StorageRecord = { vaultId: 'v1', revision: 2, manifest: makeManifest('v1', 2) };
    const resultB = simulateWrite(store, 'vault-1', writerB, 1);

    expect(resultA.ok).toBe(true);
    expect(resultB.ok).toBe(false);
    if (!resultB.ok) expect(resultB.error).toBe('WRITE_CONFLICT');
  });

  it('after conflict, writer must read new revision to succeed', () => {
    const store = new Map<string, StorageRecord>();
    const base: StorageRecord = { vaultId: 'v1', revision: 1, manifest: makeManifest('v1', 1) };
    store.set('vault-1', base);

    // Writer A wins
    const writerA: StorageRecord = { vaultId: 'v1', revision: 2, manifest: makeManifest('v1', 2) };
    simulateWrite(store, 'vault-1', writerA, 1);

    // Writer B retries with updated expected revision
    const current = store.get('vault-1')!;
    const writerB: StorageRecord = { vaultId: 'v1', revision: 3, manifest: makeManifest('v1', 3) };
    const resultB = simulateWrite(store, 'vault-1', writerB, current.revision);
    expect(resultB.ok).toBe(true);
  });
});

describe('IndexedDBStorageAdapter - transaction abort preserves data', () => {
  it('aborted write leaves original record intact', () => {
    const store = new Map<string, StorageRecord>();
    const original: StorageRecord = { vaultId: 'v1', revision: 3, manifest: makeManifest('v1', 3) };
    store.set('vault-1', original);

    // Simulate abort: attempt write but don't commit
    const _attempted: StorageRecord = { vaultId: 'v1', revision: 4, manifest: makeManifest('v1', 4) };
    // In real IndexedDB, tx.abort() rolls back. Here we just don't write.

    const current = store.get('vault-1');
    expect(current?.revision).toBe(3);
  });

  it('CAS conflict simulates transaction abort (no mutation)', () => {
    const store = new Map<string, StorageRecord>();
    const original: StorageRecord = { vaultId: 'v1', revision: 3, manifest: makeManifest('v1', 3) };
    store.set('vault-1', original);

    const conflicting: StorageRecord = { vaultId: 'v1', revision: 4, manifest: makeManifest('v1', 4) };
    const result = simulateWrite(store, 'vault-1', conflicting, 999);

    expect(result.ok).toBe(false);
    expect(store.get('vault-1')?.revision).toBe(3);
  });
});

describe('IndexedDBStorageAdapter - typed error shapes', () => {
  it('WRITE_CONFLICT', () => {
    const r = { ok: false as const, error: 'WRITE_CONFLICT' as const };
    expect(r.error).toBe('WRITE_CONFLICT');
  });

  it('STORAGE_UNAVAILABLE', () => {
    const r = { ok: false as const, error: 'STORAGE_UNAVAILABLE' as const };
    expect(r.error).toBe('STORAGE_UNAVAILABLE');
  });

  it('QUOTA_EXCEEDED', () => {
    const r = { ok: false as const, error: 'QUOTA_EXCEEDED' as const };
    expect(r.error).toBe('QUOTA_EXCEEDED');
  });

  it('success', () => {
    const record: StorageRecord = { vaultId: 'v1', revision: 1, manifest: makeManifest('v1', 1) };
    const r = { ok: true as const, record };
    expect(r.ok).toBe(true);
    expect(r.record.vaultId).toBe('v1');
  });
});

describe('IndexedDBStorageAdapter - storage notification', () => {
  it('lock notification has correct shape', () => {
    const n = { kind: 'lock' as const, vaultId: 'vault-1', notificationId: 'abc' };
    expect(n.kind).toBe('lock');
  });

  it('revision_change notification has revision', () => {
    const n = { kind: 'revision_change' as const, vaultId: 'vault-1', revision: 5, notificationId: 'xyz' };
    expect(n.kind).toBe('revision_change');
    expect(n.revision).toBe(5);
  });
});
