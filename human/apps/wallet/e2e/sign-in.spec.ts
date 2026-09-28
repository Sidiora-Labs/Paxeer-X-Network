import { expect, test } from '@playwright/test';
import { bottomTab, EMAIL_CODE, enterCode, openSignIn, requestCode } from './wallet';

test.describe('sign-in shell', () => {
    test('offers the embedded wallet on the welcome screen', async ({ page }) => {
        await page.goto('/');
        await expect(page.getByRole('heading', { name: 'Paxeer Wallet' })).toBeVisible();
        await expect(page.getByRole('button', { name: /Sign in with email or social/ })).toBeEnabled();
        await expect(page.getByText('Unavailable')).toHaveCount(0);
    });

    test('refuses a send-code request for an invalid email', async ({ page }) => {
        await openSignIn(page);
        await page.getByLabel('Email', { exact: true }).fill('not-an-email');
        await expect(page.getByRole('button', { name: 'Send code' })).toBeDisabled();
    });

    test('shows the identity error for a rejected code and stays signed out', async ({ page }) => {
        await openSignIn(page);
        await requestCode(page);
        const wrong = String(EMAIL_CODE) === '000000' ? '111111' : '000000';
        await enterCode(page, wrong);
        await expect(page.getByText(/invalid|expired/i)).toBeVisible();
        await expect(bottomTab(page, 'Settings')).toHaveCount(0);
    });

    test('signs in with the emailed code and opens the wallet', async ({ page }) => {
        await openSignIn(page);
        await requestCode(page);
        await enterCode(page, EMAIL_CODE);
        await expect(bottomTab(page, 'Wallet')).toBeVisible();
        await expect(bottomTab(page, 'Settings')).toBeVisible();
    });
});
