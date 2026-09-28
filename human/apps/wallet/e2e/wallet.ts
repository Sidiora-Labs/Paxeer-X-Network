import { expect, type Page } from '@playwright/test';
import { EMAIL, EMAIL_CODE, fixtureBody } from '../src/wallet/test/gateway';

export { EMAIL, EMAIL_CODE };

export function walletAddress(): string {
    const body = fixtureBody('default', 'GET', '/v1/wallet/me') as { wallet: { address: string } };
    return body.wallet.address;
}

export async function openSignIn(page: Page): Promise<void> {
    await page.goto('/');
    await expect(page.getByRole('heading', { name: 'Paxeer Wallet' })).toBeVisible();
    const embedded = page.getByRole('button', { name: /Sign in with email or social/ });
    await expect(embedded).toBeEnabled();
    await embedded.click();
    await expect(page.getByRole('heading', { name: 'Sign in to Paxeer' })).toBeVisible();
}

export async function requestCode(page: Page, email: string = EMAIL): Promise<void> {
    await page.getByLabel('Email', { exact: true }).fill(email);
    await page.getByRole('button', { name: 'Send code' }).click();
    await expect(page.getByRole('heading', { name: 'Check Your Inbox' })).toBeVisible();
}

export async function enterCode(page: Page, code: string): Promise<void> {
    await page.getByLabel('Email code').fill(code);
    await page.getByRole('button', { name: 'Verify code' }).click();
}

export function bottomTab(page: Page, label: string) {
    return page.getByRole('button', { name: label, exact: true });
}

export async function signIn(page: Page): Promise<void> {
    await openSignIn(page);
    await requestCode(page);
    await enterCode(page, EMAIL_CODE);
    await expect(bottomTab(page, 'Settings')).toBeVisible();
}
