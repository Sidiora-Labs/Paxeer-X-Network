export { VaultManager } from './vault-manager';
export type { VaultCreateParams, VaultUnlockResult, VaultPasswordReplaceParams } from './vault-manager';
export { SessionManager } from './session-manager';
export type { SessionCreateParams, SessionStatus } from './session-manager';
export { AuthenticationManager } from './authentication-manager';
export type {
  AuthenticationOptions,
  AuthenticationThrottleStatus,
} from './authentication-manager';
export { WalletCoreV2 } from './wallet-core';
export { VaultSigner } from './vault-signer';
export { TransactionServiceV2 } from './transaction-service';
export { LegacyMigrationManager } from './legacy-migration-manager';
export type { LegacyMigrationResult } from './legacy-migration-manager';
export {
  validateManifest,
  validatePayload,
  validateEnvelope,
  validateKeySlot,
  validateKdf,
  validateAccount,
  validateVaultId,
  validateSlotId,
  validateRevision,
  validateTimestamp,
  validateBase64Url,
  validateEthAddress,
  validateDerivationPath,
  validateAccountName,
} from './vault-validators';
