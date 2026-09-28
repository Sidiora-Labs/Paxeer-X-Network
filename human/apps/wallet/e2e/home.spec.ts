import { expect, test } from '@playwright/test';
import { bottomTab, signIn } from './wallet';

test.describe('wallet home', () => {
    test.beforeEach(async ({ page }) => {
        await signIn(page);
    });

    test('shows the wallet header, the actions and every tab', async ({ page }) => {
        await expect(page.getByRole('button', { name: 'Send', exact: true })).toBeVisible();
        await expect(page.getByRole('button', { name: 'Receive', exact: true })).toBeVisible();
        for (const label of ['Wallet', 'Activity', 'Swap', 'Discover', 'Settings']) {
            await expect(bottomTab(page, label)).toBeVisible();
        }
    });

    test('keeps the session and the wallet across a reload', async ({ page }) => {
        await page.reload();
        await expect(bottomTab(page, 'Settings')).toBeVisible();
        await expect(page.getByRole('button', { name: /Sign in with email or social/ })).toHaveCount(0);
    });
});
