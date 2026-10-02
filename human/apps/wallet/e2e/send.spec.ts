import { expect, test, type Page } from '@playwright/test';
import { formatEther } from 'ethers';
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
const OBSERVED_METHODS = new Set(['eth_chainId', 'eth_getTransactionReceipt', 'eth_getTransactionByHash', 'eth_getTransactionCount', 'eth_getBlockByNumber']);
const WRONG_RPC = process.env.WALLET_TRANSFER_TRUTH_WRONG_RPC_URL ?? '';
const FINAL_RPC = process.env.PAXEER_X_TRANSFER_FINAL_RPC_URL ?? '';
const EXPLORER = process.env.PAXEER_X_EXPLORER_STATUS_URL ?? '';
const WALLET_RPC = process.env.WALLET_TRANSFER_TRUTH_BROWSER_RPC_URL ?? '';
let observationRpc = ANVIL_URL;
const RECIPIENT = '0x00000000000000000000000000000000000000b1';
const REVERTER = '0x00000000000000000000000000000000000000fe';

async function rpcAt(url: string, method: string, params: readonly unknown[] = []): Promise<any> {
    const response = await fetch(url, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ jsonrpc: '2.0', id: 1, method, params }),
    });
    const body = (await response.json()) as { result?: unknown; error?: { message: string } };
    if (body.error) throw new Error(`${method}: ${body.error.message}`);
    return body.result;
}

async function anvil(method: string, params: readonly unknown[] = []): Promise<any> {
    return rpcAt(ANVIL_URL, method, params);
}

async function submit(tx: Record<string, string>): Promise<string> {
    return anvil('eth_sendTransaction', [{ from: ANVIL_SENDER, to: RECIPIENT, value: '0x1', gas: '0x186a0', ...tx }]);
}

async function observe(page: Page, hash: string): Promise<void> {
    const transaction = await rpcAt(observationRpc, 'eth_getTransactionByHash', [hash]);
    if (!transaction || transaction.hash.toLowerCase() !== hash.toLowerCase() ||
        transaction.from.toLowerCase() !== ANVIL_SENDER.toLowerCase() || typeof transaction.to !== 'string') {
        throw new Error('Observed transaction must belong to the authenticated controlled wallet');
    }
    await page.evaluate(
        ({ hash: h, sender, recipient, amount, amountRaw }) => {
            window.localStorage.setItem(`paxeer.wallet.submittedTransfer:125:${sender.toLowerCase()}`, JSON.stringify({
                hash: h, chainId: 125, sender, symbol: 'PAX', decimals: 18, recipient, amount, intent: { sender, recipient, amountRaw },
            }));
        },
        { hash, sender: ANVIL_SENDER, recipient: transaction.to, amount: formatEther(BigInt(transaction.value)), amountRaw: BigInt(transaction.value).toString() },
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
        observationRpc = ANVIL_URL;
        if (!WALLET_RPC) throw new Error('WALLET_TRANSFER_TRUTH_BROWSER_RPC_URL must identify the prebuilt wallet RPC');
        await page.route(WALLET_RPC, async (route) => {
            const call = route.request().postDataJSON() as { method?: string } | null;
            if (!call || !OBSERVED_METHODS.has(call.method ?? '')) return route.continue();
            const response = await route.fetch({ url: observationRpc });
            await route.fulfill({ response });
        });
        await page.goto('/');
        await expect(bottomTab(page, 'Settings')).toBeVisible();
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

    test('sealed and final presentation requires real bound explorer and receipt evidence', async ({ page }) => {
        const sealed = process.env.PAXEER_X_EXPLORER_SEALED_TX ?? '';
        const final = process.env.PAXEER_X_EXPLORER_FINAL_TX ?? '';
        if (!FINAL_RPC || !WRONG_RPC || !EXPLORER || !/^0x[0-9a-fA-F]{64}$/.test(sealed) || !/^0x[0-9a-fA-F]{64}$/.test(final)) {
            throw new Error('Real sealed and final transaction identities, their RPC and explorer are required');
        }
        observationRpc = FINAL_RPC;
        await page.route('**/api/v2/transactions/*/status', async (route) => {
            const transaction = new URL(route.request().url()).pathname.match(/transactions\/(0x[0-9a-fA-F]{64})\/status$/)?.[1];
            if (!transaction) throw new Error('Malformed explorer request');
            const response = await route.fetch({ url: `${EXPLORER.replace(/\/$/, '')}/api/v2/transactions/${transaction}/status` });
            await route.fulfill({ response });
        });
        await observe(page, sealed);
        await expect(page.locator('li[data-rung="sealed"]')).toHaveAttribute('data-reached', 'true');
        await expect(page.locator('li[data-rung="final"]')).toHaveAttribute('data-reached', 'false');
        await expect(page.getByText('Transfer Completed')).toHaveCount(0);
        await observe(page, final);
        await expect(page.getByText('Transfer Completed')).toBeVisible();
        await expect(page.locator('li[data-rung="final"]')).toHaveAttribute('data-source', 'explorer');
        observationRpc = WRONG_RPC;
        await page.reload();
        await expect(page.getByRole('alert').filter({ hasText: 'connected to chain 126' })).toBeVisible();
        await expect(page.getByText('Transfer Completed')).toHaveCount(0);
        await expect(page.locator('[data-retained-evidence]')).toBeVisible();
        observationRpc = FINAL_RPC;
        await expect(page.getByText('Transfer Completed')).toBeVisible();
        await expect(page.locator('li[data-rung="final"]')).toHaveAttribute('data-source', 'explorer');
    });

    test('a canonical rollback removes previously verified inclusion without inventing finality', async ({ page }) => {
        const snapshot = await anvil('evm_snapshot');
        const hash = await submit({});
        await anvil('evm_mine');
        await observe(page, hash);
        await expect(title(page)).toHaveAttribute('data-truth', 'included');
        await anvil('evm_revert', [snapshot]);
        await expect(title(page)).toHaveAttribute('data-truth', 'unknown');
        await expect(page.locator('li[data-rung="instant"]')).toHaveAttribute('data-reached', 'false');
        await expect(page.getByText('Transfer Completed')).toHaveCount(0);
        await expect(page.locator('[data-retained-evidence]')).toBeVisible();
    });

});
