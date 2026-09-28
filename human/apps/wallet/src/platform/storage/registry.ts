import { reportBackgroundFailure } from '@/platform/status/background-failures';

export type StorageArea = 'local' | 'session';
export type StorageSensitivity =
  | 'public'
  | 'preferences'
  | 'private-metadata'
  | 'security-relevant';
export type StorageCorruption = 'reset-and-signal' | 'fail-closed';
export type StorageLifecycle =
  | 'reset'
  | 'logout'
  | 'custody-switch'
  | 'account-removal'
  | 'uninstall';

export interface StorageRegistration {
  readonly id: string;
  readonly key: string;
  readonly owner: string;
  readonly schema: string;
  readonly version: number;
  readonly area: StorageArea;
  readonly sensitivity: StorageSensitivity;
  readonly retention: string;
  readonly quotaBytes: number;
  readonly migration: string;
  readonly resetOn: readonly StorageLifecycle[];
  readonly corruption: StorageCorruption;
  readonly prohibitedData: readonly string[];
}

interface StorageEnvelope {
  version: number;
  writtenAt: number;
  value: unknown;
}

interface RepositoryDefinition<T> extends StorageRegistration {
  readonly fallback: () => T;
  readonly parse: (input: unknown) => T;
  readonly migrateLegacy?: (storage: Storage) => unknown | undefined;
  readonly legacyKeys?: readonly string[];
}

export interface StorageRepository<T> {
  readonly registration: StorageRegistration;
  read(): T;
  write(value: T): void;
  update(updater: (current: T) => T): T;
  remove(): void;
}

export class StorageCorruptionError extends Error {
  constructor(readonly storageId: string) {
    super('A security-relevant storage record is corrupt');
    this.name = 'StorageCorruptionError';
  }
}

const definitions = new Map<string, RepositoryDefinition<unknown>>();

function browserStorage(area: StorageArea): Storage | null {
  try {
    if (area === 'local') return globalThis.localStorage ?? null;
    return globalThis.sessionStorage ?? null;
  } catch {
    return null;
  }
}

function envelope(input: unknown, version: number): unknown {
  if (
    typeof input !== 'object' ||
    input === null ||
    Array.isArray(input) ||
    (input as { version?: unknown }).version !== version ||
    typeof (input as { writtenAt?: unknown }).writtenAt !== 'number' ||
    !Number.isSafeInteger((input as { writtenAt: number }).writtenAt) ||
    (input as { writtenAt: number }).writtenAt < 0 ||
    !('value' in input)
  ) {
    throw new TypeError('Storage envelope is invalid');
  }
  return (input as StorageEnvelope).value;
}

function signal(definition: StorageRegistration, code: string): void {
  reportBackgroundFailure({
    domain: 'storage',
    kind: 'corrupt',
    code,
    message: 'Saved application data was reset because it could not be read.',
    retryable: false,
  });
  if (definition.corruption === 'fail-closed') {
    throw new StorageCorruptionError(definition.id);
  }
}

export function defineStorageRepository<T>(
  definition: RepositoryDefinition<T>,
): StorageRepository<T> {
  if (definitions.has(definition.id) || definitions.has(definition.key)) {
    throw new TypeError(`Duplicate storage registration: ${definition.id}`);
  }
  definitions.set(definition.id, definition as RepositoryDefinition<unknown>);
  definitions.set(definition.key, definition as RepositoryDefinition<unknown>);

  const removeLegacy = (storage: Storage) => {
    for (const key of definition.legacyKeys ?? []) storage.removeItem(key);
  };

  const writeTo = (storage: Storage, value: T): void => {
    const parsed = definition.parse(value);
    const serialized = JSON.stringify({
      version: definition.version,
      writtenAt: Date.now(),
      value: parsed,
    });
    if (new TextEncoder().encode(serialized).byteLength > definition.quotaBytes) {
      signal(definition, 'STORAGE_QUOTA_EXCEEDED');
      return;
    }
    storage.setItem(definition.key, serialized);
  };

  return {
    registration: definition,
    read(): T {
      const storage = browserStorage(definition.area);
      if (!storage) return definition.fallback();
      const raw = storage.getItem(definition.key);
      if (raw !== null) {
        try {
          if (new TextEncoder().encode(raw).byteLength > definition.quotaBytes) {
            throw new TypeError('Storage record exceeds quota');
          }
          return definition.parse(
            envelope(JSON.parse(raw) as unknown, definition.version),
          );
        } catch {
          storage.removeItem(definition.key);
          signal(definition, 'STORAGE_CORRUPT');
          return definition.fallback();
        }
      }
      if (!definition.migrateLegacy) return definition.fallback();
      try {
        const legacy = definition.migrateLegacy(storage);
        if (legacy === undefined) return definition.fallback();
        const parsed = definition.parse(legacy);
        writeTo(storage, parsed);
        removeLegacy(storage);
        return parsed;
      } catch {
        removeLegacy(storage);
        signal(definition, 'STORAGE_MIGRATION_FAILED');
        return definition.fallback();
      }
    },
    write(value: T): void {
      const storage = browserStorage(definition.area);
      if (!storage) {
        reportBackgroundFailure({
          domain: 'storage',
          kind: 'unavailable',
          code: 'STORAGE_UNAVAILABLE',
          message: 'Changes could not be saved on this device.',
          retryable: true,
        });
        return;
      }
      try {
        writeTo(storage, value);
      } catch {
        reportBackgroundFailure({
          domain: 'storage',
          kind: 'unavailable',
          code: 'STORAGE_WRITE_FAILED',
          message: 'Changes could not be saved on this device.',
          retryable: true,
        });
      }
    },
    update(updater: (current: T) => T): T {
      const next = definition.parse(updater(this.read()));
      this.write(next);
      return next;
    },
    remove(): void {
      const storage = browserStorage(definition.area);
      if (!storage) return;
      storage.removeItem(definition.key);
      removeLegacy(storage);
    },
  };
}

export function storageCatalog(): readonly StorageRegistration[] {
  return [...new Map(
    [...definitions.values()].map((definition) => [definition.id, definition]),
  ).values()].map((definition) => ({
    id: definition.id,
    key: definition.key,
    owner: definition.owner,
    schema: definition.schema,
    version: definition.version,
    area: definition.area,
    sensitivity: definition.sensitivity,
    retention: definition.retention,
    quotaBytes: definition.quotaBytes,
    migration: definition.migration,
    resetOn: definition.resetOn,
    corruption: definition.corruption,
    prohibitedData: definition.prohibitedData,
  }));
}

export function resetStorageForLifecycle(lifecycle: StorageLifecycle): void {
  for (const definition of new Map(
    [...definitions.values()].map((item) => [item.id, item]),
  ).values()) {
    if (!definition.resetOn.includes(lifecycle)) continue;
    const storage = browserStorage(definition.area);
    storage?.removeItem(definition.key);
    for (const legacyKey of definition.legacyKeys ?? []) {
      storage?.removeItem(legacyKey);
    }
  }
}
