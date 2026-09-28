import type { VaultManifestV2 } from './vault';

export interface StorageRecord {
  readonly vaultId: string;
  readonly revision: number;
  readonly manifest: VaultManifestV2;
}

export interface AuthThrottleRecord {
  readonly version: 1;
  readonly vaultId: string;
  readonly revision: number;
  readonly failedAttempts: number;
  readonly lockedUntil: number;
  readonly updatedAt: number;
}

export interface PersistedSessionRecord {
  readonly version: 1;
  readonly vaultId: string;
  readonly vaultKey: CryptoKey;
  readonly inactivityDeadline: number;
  readonly inactivityMs: number;
}

export interface StorageCapabilities {
  readonly indexedDb: boolean;
  readonly atomicTransactions: boolean;
  readonly revisionChecks: boolean;
  readonly crossContextNotifications: boolean;
}

export type StorageFailure =
  | 'WRITE_CONFLICT'
  | 'STORAGE_UNAVAILABLE'
  | 'QUOTA_EXCEEDED';

export type StorageWriteResult =
  | { readonly ok: true; readonly record: StorageRecord }
  | { readonly ok: false; readonly error: StorageFailure };

export type MetadataWriteResult =
  | { readonly ok: true; readonly record: AuthThrottleRecord }
  | { readonly ok: false; readonly error: StorageFailure };

export type StorageDeleteResult =
  | { readonly ok: true; readonly deleted: boolean }
  | { readonly ok: false; readonly error: StorageFailure };

export type StorageNotificationKind = 'lock' | 'revision_change';

export interface StorageNotification {
  readonly kind: StorageNotificationKind;
  readonly vaultId: string;
  readonly revision?: number;
  readonly notificationId: string;
}
