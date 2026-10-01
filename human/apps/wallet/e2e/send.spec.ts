import { expect, test, type Page } from '@playwright/test';
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

const ANVIL_URL = process.env.WALLET_TRANSFER_TRUTH_ANVIL_URL ?? '';
const ANVIL_SENDER = process.env.WALLET_TRANSFER_TRUTH_SENDER ?? '';
const OBSERVED_METHODS = new Set(['eth_chainId', 'eth_getTransactionReceipt', 'eth_getTransactionByHash', 'eth_getTransactionCount']);
const RECIPIENT = '0x00000000000000000000000000000000000000b1';
const REVERTER = '0x00000000000000000000000000000000000000fe';

async function anvil(method: string, params: readonly unknown[] = []): Promise<any> {
    const response = await fetch(ANVIL_URL, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ jsonrpc: '2.0', id: 1, method, params }),
    });
    const body = (await response.json()) as { result?: unknown; error?: { message: string } };
    if (body.error) throw new Error(`${method}: ${body.error.message}`);
    return body.result;
}

async function submit(tx: Record<string, string>): Promise<string> {
    return anvil('eth_sendTransaction', [{ from: ANVIL_SENDER, to: RECIPIENT, value: '0x1', gas: '0x186a0', ...tx }]);
}

async function observe(page: Page, hash: string): Promise<void> {
    await page.evaluate(
        ({ hash: h }) => {
            window.localStorage.setItem('paxeer.wallet.submittedTransfer', JSON.stringify({ hash: h, chainId: 125, to: '0x00000000000000000000000000000000000000b1', amount: '0.000000000000000001' }));
        },
        { hash },
    );
    await page.goto('/?screen=send');
}

const title = (page: Page) => page.locator('h2[data-truth]');

test.describe('send transfer truth', () => {
    test.beforeAll(async () => {
        if (!ANVIL_URL || !/^0x[0-9a-fA-F]{40}$/.test(ANVIL_SENDER)) {
            throw new Error('WALLET_TRANSFER_TRUTH_ANVIL_URL and WALLET_TRANSFER_TRUTH_SENDER must name a running controlled chain');
        }
        await anvil('evm_setAutomine', [false]);
        await anvil('anvil_setCode', [REVERTER, '0xfe']);
    });

    test.beforeEach(async ({ page }) => {
        await page.route('**/rpc', async (route) => {
            const call = route.request().postDataJSON() as { method?: string } | null;
            if (!call || !OBSERVED_METHODS.has(call.method ?? '')) return route.continue();
            const response = await route.fetch({ url: ANVIL_URL });
            await route.fulfill({ response });
        });
        await signIn(page);
    });

    test('a returned hash is pending until a successful receipt, which is not finality, and reload keeps the evidence', async ({ page }) => {
        const hash = await submit({});
        await observe(page, hash);
        await expect(title(page)).toHaveAttribute('data-truth', 'pending');
        await page.waitForTimeout(5000);
        await expect(title(page)).toHaveAttribute('data-truth', 'pending');
        await expect(page.getByText('Transfer Completed')).toHaveCount(0);
        await anvil('evm_mine');
        await expect(title(page)).toHaveAttribute('data-truth', 'included');
        await expect(page.locator('li[data-rung="instant"]')).toHaveAttribute('data-source', 'receipt');
        await expect(page.locator('li[data-rung="final"]')).toHaveAttribute('data-reached', 'false');
        await expect(page.getByText('Transfer Completed')).toHaveCount(0);
        await page.reload();
        await expect(title(page)).toHaveAttribute('data-truth', 'included');
        await expect(page.locator('li[data-rung="instant"]')).toHaveAttribute('data-source', 'receipt');
        await expect(page.getByText('Transfer Completed')).toHaveCount(0);
    });

    test('a reverted transfer never displays completion', async ({ page }) => {
        const hash = await submit({ to: REVERTER });
        await anvil('evm_mine');
        await observe(page, hash);
        await expect(title(page)).toHaveAttribute('data-truth', 'reverted');
        await expect(page.getByText('Transfer Reverted')).toBeVisible();
        await expect(page.getByText('Transfer Completed')).toHaveCount(0);
        await expect(page.locator('li[data-rung="instant"]')).toHaveAttribute('data-reached', 'false');
    });

    test('a replaced transfer is distinct from success', async ({ page }) => {
        const nonce = await anvil('eth_getTransactionCount', [ANVIL_SENDER, 'pending']);
        const original = await submit({ nonce, maxFeePerGas: '0x77359400', maxPriorityFeePerGas: '0x3b9aca00' });
        await observe(page, original);
        await expect(title(page)).toHaveAttribute('data-truth', 'pending');
        await submit({ nonce, value: '0x2', maxFeePerGas: '0xee6b2800', maxPriorityFeePerGas: '0x77359400' });
        await anvil('evm_mine');
        await expect(title(page)).toHaveAttribute('data-truth', 'replaced');
        await expect(page.getByText('Transfer Completed')).toHaveCount(0);
    });

    test('a dropped transfer becomes unknown, and reconnecting does not invent finality', async ({ page }) => {
        const hash = await submit({});
        await observe(page, hash);
        await expect(title(page)).toHaveAttribute('data-truth', 'pending');
        await anvil('anvil_dropTransaction', [hash]);
        await expect(title(page)).toHaveAttribute('data-truth', 'unknown');
        await page.reload();
        await expect(title(page)).not.toHaveAttribute('data-truth', 'included');
        await expect(page.getByText('Transfer Completed')).toHaveCount(0);
    });
});
