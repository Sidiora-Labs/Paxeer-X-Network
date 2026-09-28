import type {
  AuthThrottleRecord,
  MetadataWriteResult,
  PersistedSessionRecord,
  StorageCapabilities,
  StorageDeleteResult,
  StorageNotification,
  StorageNotificationKind,
  StorageRecord,
  StorageWriteResult,
} from '../types/storage';

export interface StoragePort {
  capabilities(): StorageCapabilities;

  read(namespace: string): Promise<StorageRecord | null>;

  write(
    namespace: string,
    record: StorageRecord,
    expectedRevision: number | null,
  ): Promise<StorageWriteResult>;

  delete(namespace: string, expectedRevision: number | null): Promise<StorageDeleteResult>;

  readAuthThrottle(namespace: string): Promise<AuthThrottleRecord | null>;

  writeAuthThrottle(
    namespace: string,
    record: AuthThrottleRecord,
    expectedRevision: number | null,
  ): Promise<MetadataWriteResult>;

  deleteAuthThrottle(
    namespace: string,
    expectedRevision: number | null,
  ): Promise<StorageDeleteResult>;

  readSession(namespace: string): Promise<PersistedSessionRecord | null>;

  writeSession(
    namespace: string,
    record: PersistedSessionRecord,
  ): Promise<void>;

  deleteSession(namespace: string): Promise<void>;

  subscribe(callback: (notification: StorageNotification) => void): () => void;

  notify(kind: StorageNotificationKind, vaultId: string, revision?: number): void;
}
