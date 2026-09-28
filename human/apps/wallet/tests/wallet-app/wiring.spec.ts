import { expect, test } from '@playwright/test';

test('self-custody UI preserves an unexpired session across reload', async ({
  page,
}) => {
  const pageErrors: string[] = [];
  page.on('pageerror', error => pageErrors.push(error.message));
  await page.addInitScript(() => {
    localStorage.setItem('paxeer_whats_new_seen', '1.2.0');
  });

  await page.goto('/', { waitUntil: 'domcontentloaded' });
  await page.getByRole('button', { name: /Self-Custody Wallet/i }).click();
  await page.getByRole('button', { name: /Create New Wallet/i }).click();

  const pin = '482915';
  const passwordFields = page.locator('input[type="password"]');
  await passwordFields.nth(0).fill(pin);
  await passwordFields.nth(1).fill(pin);
  await page.getByRole('button', { name: 'Continue' }).click();

  await expect(
    page.getByRole('heading', { name: 'Back Up Your Phrase' }),
  ).toBeVisible();
  await page.getByRole('button', { name: /I've saved it/i }).click();
  await expect(page.getByText('Account 1', { exact: true }).first()).toBeVisible();

  await page.evaluate(() => {
    (window as Window & { __paxportNavigationProbe?: string }).__paxportNavigationProbe =
      'alive';
  });
  await page.evaluate(() => {
    window.dispatchEvent(
      new CustomEvent('paxeer:deeplink', { detail: { route: 'send' } }),
    );
  });
  await expect(page.getByRole('heading', { name: 'Send' })).toBeVisible();
  await expect(page.locator('img[alt="PAX"]').first()).toBeVisible();
  const sendIconSize = await page.locator('img[alt="PAX"]').first().evaluate(image => {
    const box = image.getBoundingClientRect();
    return { width: box.width, height: box.height };
  });
  expect(sendIconSize.width).toBeLessThanOrEqual(40);
  expect(sendIconSize.height).toBeLessThanOrEqual(40);
  await page.getByRole('button', { name: 'Back' }).click();
  await expect(page.getByText('Account 1', { exact: true }).first()).toBeVisible();
  expect(
    await page.evaluate(
      () => (window as Window & { __paxportNavigationProbe?: string }).__paxportNavigationProbe,
    ),
  ).toBe('alive');

  await page.getByRole('button', { name: 'Swap', exact: true }).last().click();
  await expect(page.getByRole('heading', { name: 'Swap' })).toBeVisible();
  await expect(page.locator('img[alt="PAX"]').first()).toBeVisible();
  const oversizedSwapIcons = await page
    .locator('img[alt="PAX"], img[alt="USDC"]')
    .evaluateAll(images =>
      images
        .map(image => {
          const box = image.getBoundingClientRect();
          return { width: box.width, height: box.height };
        })
        .filter(box => box.width > 40 || box.height > 40),
    );
  expect(oversizedSwapIcons).toEqual([]);

  await page.reload({ waitUntil: 'domcontentloaded' });
  await expect(page.getByRole('heading', { name: 'Swap' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Wallet Locked' })).toHaveCount(0);

  await page.getByRole('button', { name: 'Settings' }).click();
  await page.getByRole('button', { name: /Export Recovery Phrase/i }).click();
  await expect(
    page.getByRole('heading', { name: 'Fresh Authentication' }),
  ).toBeVisible();
  await page.locator('input[type="password"]').fill(pin);
  await page.getByRole('button', { name: 'Confirm' }).click();
  await expect(page.getByRole('button', { name: 'Copy phrase' })).toBeVisible();

  const storage = await page.evaluate(async () => {
    const custodyEnvelope = JSON.parse(
      localStorage.getItem('paxport:v1:custody-choice') ?? 'null',
    ) as { value?: unknown } | null;
    return {
      databases: (await indexedDB.databases()).map(database => database.name),
      custodyKind: custodyEnvelope?.value ?? null,
      forbiddenLocalKeys: Object.keys(localStorage).filter(key =>
        /paxeer_wallet|pin_hash|active_account|session_data|biometric/i.test(key)),
      walletSessionKeys: Object.keys(sessionStorage).filter(key =>
        /pax|wallet|vault|mnemonic|private.?key/i.test(key)),
    };
  });
  expect(storage).toEqual({
    databases: ['paxport-wallet-v2'],
    custodyKind: 'self-custody',
    forbiddenLocalKeys: [],
    walletSessionKeys: [],
  });
  await page.getByRole('button', { name: 'Lock Wallet' }).click();
  await expect(page.getByRole('heading', { name: 'Wallet Locked' })).toBeVisible();
  expect(pageErrors).toEqual([]);
});
