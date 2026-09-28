import type { StoragePort } from '../ports/storage-port';
import type {
  AuthThrottleRecord,
  MetadataWriteResult,
  PersistedSessionRecord,
  StorageCapabilities,
  StorageDeleteResult,
  StorageFailure,
  StorageNotification,
  StorageNotificationKind,
  StorageRecord,
  StorageWriteResult,
} from '../types/storage';
import { WalletError } from '../types/errors';
import { validateManifest } from '../core/vault-validators';

const DB_NAME = 'paxport-wallet-v2';
const DB_VERSION = 2;
const STORE_NAME = 'vaults';
const CHANNEL_NAME = 'paxport-wallet-sync';
const FALLBACK_NOTIFICATION_KEY = `paxport-notify-${CHANNEL_NAME}`;
const LEGACY_V2_NAMESPACE = 'paxport-wallet-v2';

interface PersistedVaultRecord {
  namespace: string;
  kind: 'vault';
  vaultId: string;
  revision: number;
  manifest: unknown;
}

interface PersistedThrottleRecord {
  namespace: string;
  kind: 'auth-throttle';
  record: unknown;
  revision: number;
}

interface PersistedSessionStorageRecord extends PersistedSessionRecord {
  namespace: string;
  kind: 'session';
  revision: number;
}

type PersistedRecord =
  | PersistedVaultRecord
  | PersistedThrottleRecord
  | PersistedSessionStorageRecord;

function hasIndexedDb(): boolean {
  return typeof globalThis.indexedDB !== 'undefined';
}

function isQuotaError(error: DOMException | null): boolean {
  return error?.name === 'QuotaExceededError';
}

function mapStorageFailure(error: DOMException | null): StorageFailure {
  return isQuotaError(error) ? 'QUOTA_EXCEEDED' : 'STORAGE_UNAVAILABLE';
}

function openDatabase(): Promise<IDBDatabase> {
  if (!hasIndexedDb()) {
    throw WalletError.storageUnavailable(new Error('IndexedDB is not available'));
  }

  return new Promise((resolve, reject) => {
    const request = globalThis.indexedDB.open(DB_NAME, DB_VERSION);
    let settled = false;

    request.onupgradeneeded = (event) => {
      const db = request.result;
      const transaction = request.transaction;
      if (!transaction) {
        throw WalletError.storageUnavailable(
          new Error('IndexedDB upgrade transaction is unavailable'),
        );
      }

      if (event.oldVersion < 1) {
        const store = db.createObjectStore(STORE_NAME, { keyPath: 'namespace' });
        store.createIndex('revision', 'revision', { unique: false });
        return;
      }

      if (event.oldVersion < 2) {
        const legacyStore = transaction.objectStore(STORE_NAME);
        const readLegacy = legacyStore.getAll();
        readLegacy.onsuccess = () => {
          const legacyRows = readLegacy.result as Array<Record<string, unknown>>;
          db.deleteObjectStore(STORE_NAME);
          const currentStore = db.createObjectStore(STORE_NAME, { keyPath: 'namespace' });
          currentStore.createIndex('revision', 'revision', { unique: false });

          for (const row of legacyRows) {
            if (
              typeof row.vaultId !== 'string'
              || typeof row.revision !== 'number'
              || row.manifest === undefined
            ) {
              continue;
            }
            currentStore.put({
              namespace:
                typeof row.namespace === 'string' ? row.namespace : LEGACY_V2_NAMESPACE,
              kind: 'vault',
              vaultId: row.vaultId,
              revision: row.revision,
              manifest: row.manifest,
            } satisfies PersistedVaultRecord);
          }
        };
        readLegacy.onerror = () => transaction.abort();
      }
    };

    request.onsuccess = () => {
      if (settled) {
        request.result.close();
        return;
      }
      settled = true;
      resolve(request.result);
    };
    request.onerror = () => {
      if (settled) return;
      settled = true;
      reject(WalletError.storageUnavailable(request.error ?? undefined));
    };
    request.onblocked = () => {
      if (settled) return;
      settled = true;
      reject(WalletError.storageUnavailable(new Error('IndexedDB upgrade blocked')));
    };
  });
}

export class IndexedDBStorageAdapter implements StoragePort {
  private db: IDBDatabase | null = null;
  private initialization: Promise<void> | null = null;
  private channel: BroadcastChannel | null = null;
  private listeners = new Set<(notification: StorageNotification) => void>();
  private storageListener: ((event: StorageEvent) => void) | null = null;

  static assertReady(): void {
    if (!hasIndexedDb()) {
      throw WalletError.storageUnavailable(new Error('IndexedDB is not available'));
    }
  }

  capabilities(): StorageCapabilities {
    return {
      indexedDb: hasIndexedDb(),
      atomicTransactions: true,
      revisionChecks: true,
      crossContextNotifications:
        typeof globalThis.BroadcastChannel === 'function'
        || typeof globalThis.addEventListener === 'function',
    };
  }

  async initialize(): Promise<void> {
    if (this.db) return;
    if (!this.initialization) {
      this.initialization = (async () => {
        const db = await openDatabase();
        this.db = db;
        db.onversionchange = () => {
          db.close();
          if (this.db === db) this.db = null;
          this.initialization = null;
        };

        if (typeof globalThis.BroadcastChannel === 'function') {
          this.channel = new globalThis.BroadcastChannel(CHANNEL_NAME);
          this.channel.onmessage = (event: MessageEvent<unknown>) => {
            const notification = this.parseNotification(event.data);
            if (notification) this.dispatchNotification(notification);
          };
          return;
        }

        this.setupStorageFallback();
      })();
    }
    try {
      await this.initialization;
    } catch (error) {
      this.initialization = null;
      throw error;
    }
  }

  subscribe(callback: (notification: StorageNotification) => void): () => void {
    this.listeners.add(callback);
    return () => this.listeners.delete(callback);
  }

  notify(kind: StorageNotificationKind, vaultId: string, revision?: number): void {
    if (!vaultId || vaultId.includes('|')) {
      throw WalletError.invalidInput('vaultId', 'invalid notification vault identifier');
    }
    if (revision !== undefined && (!Number.isInteger(revision) || revision < 0)) {
      throw WalletError.invalidInput('revision', 'invalid notification revision');
    }

    const notification: StorageNotification = {
      kind,
      vaultId,
      revision,
      notificationId: this.randomNotificationId(),
    };

    if (this.channel) {
      this.channel.postMessage(notification);
      return;
    }

    try {
      globalThis.localStorage?.setItem(FALLBACK_NOTIFICATION_KEY, JSON.stringify(notification));
      globalThis.localStorage?.removeItem(FALLBACK_NOTIFICATION_KEY);
    } catch {
      throw WalletError.storageUnavailable(new Error('Cross-context notification unavailable'));
    }
  }

  async read(namespace: string): Promise<StorageRecord | null> {
    await this.initialize();
    const raw = await this.readPersisted(namespace);
    if (!raw) return null;
    if (raw.kind !== 'vault') {
      throw WalletError.storageUnavailable(new Error('Storage namespace type mismatch'));
    }
    return this.parseVaultRecord(raw);
  }

  async write(
    namespace: string,
    record: StorageRecord,
    expectedRevision: number | null,
  ): Promise<StorageWriteResult> {
    await this.initialize();
    const manifest = validateManifest(record.manifest);
    if (
      manifest.vaultId !== record.vaultId
      || manifest.revision !== record.revision
    ) {
      return { ok: false, error: 'STORAGE_UNAVAILABLE' };
    }

    const persisted: PersistedVaultRecord = {
      namespace,
      kind: 'vault',
      vaultId: record.vaultId,
      revision: record.revision,
      manifest,
    };

    const result = await this.writePersisted(namespace, persisted, expectedRevision);
    if (!result.ok) return result;
    if (result.record.kind !== 'vault') {
      return { ok: false, error: 'STORAGE_UNAVAILABLE' };
    }
    return { ok: true, record: this.parseVaultRecord(result.record) };
  }

  async delete(
    namespace: string,
    expectedRevision: number | null,
  ): Promise<StorageDeleteResult> {
    await this.initialize();
    return this.deletePersisted(namespace, expectedRevision, 'vault');
  }

  async readAuthThrottle(namespace: string): Promise<AuthThrottleRecord | null> {
    await this.initialize();
    const raw = await this.readPersisted(namespace);
    if (!raw) return null;
    if (raw.kind !== 'auth-throttle') {
      throw WalletError.storageUnavailable(new Error('Storage namespace type mismatch'));
    }
    return this.parseThrottleRecord(raw.record);
  }

  async writeAuthThrottle(
    namespace: string,
    record: AuthThrottleRecord,
    expectedRevision: number | null,
  ): Promise<MetadataWriteResult> {
    await this.initialize();
    const validated = this.parseThrottleRecord(record);
    const persisted: PersistedThrottleRecord = {
      namespace,
      kind: 'auth-throttle',
      revision: validated.revision,
      record: validated,
    };
    const result = await this.writePersisted(namespace, persisted, expectedRevision);
    if (!result.ok) return result;
    if (result.record.kind !== 'auth-throttle') {
      return { ok: false, error: 'STORAGE_UNAVAILABLE' };
    }
    return { ok: true, record: this.parseThrottleRecord(result.record.record) };
  }

  async deleteAuthThrottle(
    namespace: string,
    expectedRevision: number | null,
  ): Promise<StorageDeleteResult> {
    await this.initialize();
    return this.deletePersisted(namespace, expectedRevision, 'auth-throttle');
  }

  async readSession(namespace: string): Promise<PersistedSessionRecord | null> {
    await this.initialize();
    const raw = await this.readPersisted(namespace);
    if (!raw) return null;
    if (raw.kind !== 'session') {
      throw WalletError.storageUnavailable(new Error('Storage namespace type mismatch'));
    }
    return this.parseSessionRecord(raw);
  }

  async writeSession(
    namespace: string,
    record: PersistedSessionRecord,
  ): Promise<void> {
    await this.initialize();
    const validated = this.parseSessionRecord(record);

    for (let attempt = 0; attempt < 5; attempt++) {
      const existing = await this.readPersisted(namespace);
      if (existing && existing.kind !== 'session') {
        throw WalletError.storageUnavailable(new Error('Storage namespace type mismatch'));
      }
      const revision = (existing?.revision ?? 0) + 1;
      const persisted: PersistedSessionStorageRecord = {
        namespace,
        kind: 'session',
        revision,
        ...validated,
      };
      const result = await this.writePersisted(
        namespace,
        persisted,
        existing?.revision ?? null,
      );
      if (result.ok) return;
      if (result.error !== 'WRITE_CONFLICT') {
        throw WalletError.storageUnavailable(
          new Error(`Session persistence failed: ${result.error}`),
        );
      }
    }

    throw WalletError.writeConflict();
  }

  async deleteSession(namespace: string): Promise<void> {
    await this.initialize();
    const result = await this.deletePersisted(namespace, null, 'session');
    if (!result.ok) {
      throw WalletError.storageUnavailable(
        new Error(`Session deletion failed: ${result.error}`),
      );
    }
  }

  async close(): Promise<void> {
    if (this.initialization) {
      try {
        await this.initialization;
      } catch {
        // Nothing was opened.
      }
    }
    this.channel?.close();
    this.channel = null;

    if (this.storageListener && typeof globalThis.removeEventListener === 'function') {
      globalThis.removeEventListener('storage', this.storageListener as EventListener);
    }
    this.storageListener = null;

    this.db?.close();
    this.db = null;
    this.initialization = null;
    this.listeners.clear();
  }

  private setupStorageFallback(): void {
    if (typeof globalThis.addEventListener !== 'function') {
      throw WalletError.storageUnavailable(
        new Error('Cross-context notification API is unavailable'),
      );
    }

    this.storageListener = (event: StorageEvent) => {
      if (event.key !== FALLBACK_NOTIFICATION_KEY || !event.newValue) return;
      try {
        const notification = this.parseNotification(JSON.parse(event.newValue));
        if (notification) this.dispatchNotification(notification);
      } catch {
        return;
      }
    };
    globalThis.addEventListener('storage', this.storageListener as EventListener);
  }

  private dispatchNotification(notification: StorageNotification): void {
    for (const listener of this.listeners) {
      listener(notification);
    }
  }

  private async readPersisted(namespace: string): Promise<PersistedRecord | null> {
    const db = this.requireDb();
    return new Promise((resolve, reject) => {
      const tx = db.transaction(STORE_NAME, 'readonly');
      const request = tx.objectStore(STORE_NAME).get(namespace);
      let value: PersistedRecord | null = null;

      request.onsuccess = () => {
        value = (request.result as PersistedRecord | undefined) ?? null;
      };
      request.onerror = () => reject(
        WalletError.storageUnavailable(request.error ?? undefined),
      );
      tx.oncomplete = () => resolve(value);
      tx.onabort = () => reject(WalletError.storageUnavailable(tx.error ?? undefined));
      tx.onerror = () => {
        // onabort provides the terminal error.
      };
    });
  }

  private async writePersisted(
    namespace: string,
    record: PersistedRecord,
    expectedRevision: number | null,
  ): Promise<
    | { ok: true; record: PersistedRecord }
    | { ok: false; error: StorageFailure }
  > {
    const db = this.requireDb();
    return new Promise((resolve) => {
      const tx = db.transaction(STORE_NAME, 'readwrite');
      const store = tx.objectStore(STORE_NAME);
      const read = store.get(namespace);
      let failure: StorageFailure | null = null;
      let verified: PersistedRecord | null = null;

      read.onsuccess = () => {
        const existing = read.result as PersistedRecord | undefined;
        const revisionMatches = expectedRevision === null
          ? existing === undefined
          : existing?.revision === expectedRevision;
        if (!revisionMatches) {
          failure = 'WRITE_CONFLICT';
          tx.abort();
          return;
        }

        const put = store.put(record);
        put.onerror = () => {
          failure = mapStorageFailure(put.error);
          tx.abort();
        };
        put.onsuccess = () => {
          const verify = store.get(namespace);
          verify.onerror = () => {
            failure = mapStorageFailure(verify.error);
            tx.abort();
          };
          verify.onsuccess = () => {
            const written = verify.result as PersistedRecord | undefined;
            if (
              !written
              || written.kind !== record.kind
              || written.revision !== record.revision
            ) {
              failure = 'STORAGE_UNAVAILABLE';
              tx.abort();
              return;
            }
            verified = written;
          };
        };
      };
      read.onerror = () => {
        failure = mapStorageFailure(read.error);
        tx.abort();
      };

      tx.oncomplete = () => {
        if (!verified) {
          resolve({ ok: false, error: 'STORAGE_UNAVAILABLE' });
          return;
        }
        resolve({ ok: true, record: verified });
      };
      tx.onabort = () => resolve({
        ok: false,
        error: failure ?? mapStorageFailure(tx.error),
      });
      tx.onerror = () => {
        // onabort is the single terminal path.
      };
    });
  }

  private async deletePersisted(
    namespace: string,
    expectedRevision: number | null,
    expectedKind: PersistedRecord['kind'],
  ): Promise<StorageDeleteResult> {
    const db = this.requireDb();
    return new Promise((resolve) => {
      const tx = db.transaction(STORE_NAME, 'readwrite');
      const store = tx.objectStore(STORE_NAME);
      const read = store.get(namespace);
      let failure: StorageFailure | null = null;
      let deleted = false;

      read.onsuccess = () => {
        const existing = read.result as PersistedRecord | undefined;
        if (!existing) return;
        if (
          existing.kind !== expectedKind
          || (expectedRevision !== null && existing.revision !== expectedRevision)
        ) {
          failure = 'WRITE_CONFLICT';
          tx.abort();
          return;
        }

        const deletion = store.delete(namespace);
        deletion.onsuccess = () => {
          deleted = true;
        };
        deletion.onerror = () => {
          failure = mapStorageFailure(deletion.error);
          tx.abort();
        };
      };
      read.onerror = () => {
        failure = mapStorageFailure(read.error);
        tx.abort();
      };
      tx.oncomplete = () => resolve({ ok: true, deleted });
      tx.onabort = () => resolve({
        ok: false,
        error: failure ?? mapStorageFailure(tx.error),
      });
      tx.onerror = () => {
        // onabort is the single terminal path.
      };
    });
  }

  private parseVaultRecord(raw: PersistedVaultRecord): StorageRecord {
    const manifest = validateManifest(raw.manifest);
    if (
      raw.vaultId !== manifest.vaultId
      || raw.revision !== manifest.revision
    ) {
      throw WalletError.corruptVault('Stored vault record is internally inconsistent');
    }
    return {
      vaultId: raw.vaultId,
      revision: raw.revision,
      manifest,
    };
  }

  private parseThrottleRecord(value: unknown): AuthThrottleRecord {
    if (!value || typeof value !== 'object' || Array.isArray(value)) {
      throw WalletError.storageUnavailable(new Error('Invalid throttle record'));
    }
    const raw = value as Record<string, unknown>;
    const keys = Object.keys(raw).sort().join(',');
    if (keys !== 'failedAttempts,lockedUntil,revision,updatedAt,vaultId,version') {
      throw WalletError.storageUnavailable(new Error('Invalid throttle record fields'));
    }
    if (
      raw.version !== 1
      || typeof raw.vaultId !== 'string'
      || !Number.isInteger(raw.revision)
      || (raw.revision as number) < 1
      || !Number.isInteger(raw.failedAttempts)
      || (raw.failedAttempts as number) < 0
      || !Number.isFinite(raw.lockedUntil)
      || (raw.lockedUntil as number) < 0
      || !Number.isFinite(raw.updatedAt)
      || (raw.updatedAt as number) < 0
    ) {
      throw WalletError.storageUnavailable(new Error('Invalid throttle record values'));
    }
    return raw as unknown as AuthThrottleRecord;
  }

  private parseSessionRecord(value: unknown): PersistedSessionRecord {
    if (!value || typeof value !== 'object' || Array.isArray(value)) {
      throw WalletError.storageUnavailable(new Error('Invalid session record'));
    }
    const raw = value as Record<string, unknown>;
    const vaultKey = raw.vaultKey as CryptoKey | undefined;
    const algorithm = vaultKey?.algorithm as AesKeyAlgorithm | undefined;
    const allowedFields = new Set([
      'version',
      'vaultId',
      'vaultKey',
      'inactivityDeadline',
      'inactivityMs',
      'namespace',
      'kind',
      'revision',
    ]);
    if (
      Object.keys(raw).some((key) => !allowedFields.has(key))
      || raw.version !== 1
      || typeof raw.vaultId !== 'string'
      || raw.vaultId.length < 1
      || raw.vaultId.length > 128
      || raw.vaultId.includes('|')
      || !vaultKey
      || vaultKey.type !== 'secret'
      || vaultKey.extractable
      || algorithm?.name !== 'AES-GCM'
      || algorithm.length !== 256
      || !vaultKey.usages.includes('encrypt')
      || !vaultKey.usages.includes('decrypt')
      || !Number.isSafeInteger(raw.inactivityDeadline)
      || (raw.inactivityDeadline as number) < 0
      || !Number.isSafeInteger(raw.inactivityMs)
      || (raw.inactivityMs as number) < 1_000
      || (raw.inactivityMs as number) > 24 * 60 * 60 * 1_000
    ) {
      throw WalletError.storageUnavailable(new Error('Invalid session record values'));
    }
    return {
      version: 1,
      vaultId: raw.vaultId,
      vaultKey,
      inactivityDeadline: raw.inactivityDeadline as number,
      inactivityMs: raw.inactivityMs as number,
    };
  }

  private parseNotification(value: unknown): StorageNotification | null {
    if (!value || typeof value !== 'object' || Array.isArray(value)) return null;
    const raw = value as Record<string, unknown>;
    if (
      (raw.kind !== 'lock' && raw.kind !== 'revision_change')
      || typeof raw.vaultId !== 'string'
      || typeof raw.notificationId !== 'string'
      || (
        raw.revision !== undefined
        && (!Number.isInteger(raw.revision) || (raw.revision as number) < 0)
      )
    ) {
      return null;
    }
    return {
      kind: raw.kind,
      vaultId: raw.vaultId,
      notificationId: raw.notificationId,
      revision: raw.revision as number | undefined,
    };
  }

  private randomNotificationId(): string {
    if (typeof globalThis.crypto?.randomUUID === 'function') {
      return globalThis.crypto.randomUUID();
    }
    const bytes = new Uint8Array(16);
    globalThis.crypto.getRandomValues(bytes);
    return Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('');
  }

  private requireDb(): IDBDatabase {
    if (!this.db) {
      throw WalletError.storageUnavailable(new Error('Database not initialized'));
    }
    return this.db;
  }
}
