import { expect, test } from '@playwright/test';

test('welcome screen offers only embedded and funded custody', async ({ page }) => {
  const pageErrors: string[] = [];
  page.on('pageerror', error => pageErrors.push(error.message));
  await page.addInitScript(() => {
    localStorage.setItem('paxeer_whats_new_seen', '1.2.0');
  });

  await page.goto('/', { waitUntil: 'domcontentloaded' });
  await expect(page.getByRole('heading', { name: 'Paxeer Wallet' })).toBeVisible();
  await expect(page.getByRole('button', { name: /Paxeer Wallet/i })).toBeVisible();
  await expect(page.getByRole('button', { name: /Funded Account/i })).toBeVisible();
  await expect(page.getByRole('button', { name: /Self-Custody/i })).toHaveCount(0);
  await expect(page.getByRole('button', { name: /Restore/i })).toHaveCount(0);

  const storage = await page.evaluate(async () => ({
    databases: (await indexedDB.databases()).map(database => database.name),
    forbiddenLocalKeys: Object.keys(localStorage).filter(key =>
      /paxeer_wallet|pin_hash|active_account|session_data|biometric|dapp/i.test(key)),
  }));
  expect(storage).toEqual({ databases: [], forbiddenLocalKeys: [] });
  expect(pageErrors).toEqual([]);
});
