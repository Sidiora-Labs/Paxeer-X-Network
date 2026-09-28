import { expect, test } from '@playwright/test';

test('IndexedDB adapter provides real atomic CAS and cross-context coordination', async ({
  page,
}) => {
  await page.goto('/tests/wallet-browser/harness.html');
  const result = await page.evaluate(() => window.runStorageAcceptance());

  expect(result).toEqual({
    createCommitted: true,
    createOnlyConflict: true,
    revisionUpdateCommitted: true,
    concurrentConflict: true,
    abortedConflictPreserved: true,
    throttleMetadataSeparated: true,
    crossContextNotification: true,
    deleteConflictPreserved: true,
    deleteCommitted: true,
    unavailableStorageFailsClosed: true,
  });
});

test('authentication uses durable backoff and reload-persistent bounded sessions', async ({ page }) => {
  await page.goto('/tests/wallet-browser/harness.html');
  const result = await page.evaluate(() => window.runAuthenticationAcceptance());

  expect(result).toEqual({
    failedAttemptsPersist: true,
    successClearsFailures: true,
    noPersistedPlaintextOrExtractableKey: true,
    reloadPreservesValidSession: true,
    fifthFailureStartsBackoff: true,
    crossContextSessionRevoked: true,
    pageHidePreservesSession: true,
  });
});

test('legacy CryptoJS wallets migrate losslessly and one-way', async ({ page }) => {
  await page.goto('/tests/wallet-browser/harness.html');
  const result = await page.evaluate(() => window.runLegacyMigrationAcceptance());

  expect(result).toEqual({
    wrongPinPreservesLegacy: true,
    corruptCiphertextPreservesLegacy: true,
    mixedAccountsMigrated: true,
    legacyDeletedAfterVerifiedCommit: true,
    alreadyMigratedRecovery: true,
    mismatchedRecoveryPreservesLegacy: true,
    migratedStorageContainsNoPlaintext: true,
  });
});

test('production facade enforces the complete browser security lifecycle', async ({
  page,
}) => {
  await page.goto('/tests/wallet-browser/harness.html');
  const result = await page.evaluate(() => window.runProductionAcceptance());

  expect(result).toEqual({
    productionCreateAndDerive: true,
    facadeEncapsulation: true,
    facadeSnapshotAndStepUp: true,
    keylessDeterministicSigning: true,
    reloadPreservesValidSession: true,
    lockRevokesExistingSigner: true,
    revisionChangeRevokesStaleContext: true,
    productionCrossContextLock: true,
    tamperedVaultRejected: true,
    reopensUnchangedWallet: true,
    lifecycleStorageForensics: true,
    resetCleanup: true,
  });
});
