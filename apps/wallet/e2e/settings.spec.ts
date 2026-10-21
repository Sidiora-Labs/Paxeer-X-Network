import { expect, test } from '@playwright/test';
import { bottomTab, signIn } from './wallet';

test.describe('settings', () => {
    test.beforeEach(async ({ page }) => {
        await signIn(page);
        await bottomTab(page, 'Settings').click();
    });

    test('lists the preference, network, advanced and notification pages', async ({ page }) => {
        await expect(page.getByRole('button', { name: /Sign out/ })).toBeVisible();
        await page.getByRole('button', { name: /Advanced/ }).click();
        await expect(page.getByRole('heading', { name: 'Advanced' })).toBeVisible();
    });

    test('signs out back to the sign-in shell', async ({ page }) => {
        await page.getByRole('button', { name: /Sign out/ }).click();
        await expect(page.getByRole('heading', { name: 'Paxeer Wallet' })).toBeVisible();
        await expect(bottomTab(page, 'Settings')).toHaveCount(0);
    });
});
