import { expect, test } from '@playwright/test';
import { signIn, walletAddress } from './wallet';

test.describe('receive', () => {
    test.beforeEach(async ({ page }) => {
        await signIn(page);
    });

    test('shows the provisioned wallet address from the gateway', async ({ page }) => {
        await page.getByRole('button', { name: 'Receive', exact: true }).click();
        await expect(page.getByRole('heading', { name: 'Receive' })).toBeVisible();
        await expect(page.getByText('Your address')).toBeVisible();
        await expect(page.getByText(new RegExp(`^${walletAddress()}$`, 'i'))).toBeVisible();
    });

    test('opens the receive screen from its route', async ({ page }) => {
        await page.goto('/?screen=receive');
        await expect(page.getByRole('heading', { name: 'Receive' })).toBeVisible();
        await expect(page.getByText(new RegExp(`^${walletAddress()}$`, 'i'))).toBeVisible();
    });
});
