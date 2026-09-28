import { expect, test } from '@playwright/test';
import { bottomTab, signIn } from './wallet';

test.describe('send', () => {
    test.beforeEach(async ({ page }) => {
        await signIn(page);
    });

    test('opens the send form from the wallet home and returns', async ({ page }) => {
        await page.getByRole('button', { name: 'Send', exact: true }).click();
        await expect(page.getByRole('heading', { name: 'Send' })).toBeVisible();
        await expect(bottomTab(page, 'Settings')).toHaveCount(0);
        await page.getByRole('button', { name: 'Back' }).click();
        await expect(bottomTab(page, 'Settings')).toBeVisible();
    });

    test('opens the send form from its route', async ({ page }) => {
        await page.goto('/?screen=send');
        await expect(page.getByRole('heading', { name: 'Send' })).toBeVisible();
    });
});
