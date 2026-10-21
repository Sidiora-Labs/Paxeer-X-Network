import { expect, test, type Page, type Locator } from '@playwright/test';
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

test('injected chain pinning uses the real official extension and isolated chains', async () => {
    const fs = await import('node:fs/promises');
    const path = await import('node:path');
    const manifestPath = process.env.WALLET_INJECTED_CHAIN_RUNTIME;
    if (!manifestPath) throw new Error('The private source-bound injected chain runtime is required');
    const manifest = JSON.parse(await fs.readFile(manifestPath, 'utf8'));
    if (manifest.schema !== 'paxeer-x.wallet-injected-chain-runtime.v1') throw new Error('Invalid injected chain runtime');
    const { chromium } = await import('@playwright/test');
    const context = await chromium.launchPersistentContext(manifest.profile_dir, {
        headless: true, channel: 'chromium',
        args: [`--disable-extensions-except=${manifest.extension_path}`, `--load-extension=${manifest.extension_path}`,
            '--host-resolver-rules=MAP * ~NOTFOUND, EXCLUDE localhost, EXCLUDE 127.0.0.1'],
    });
    const cases: Record<string, unknown> = {};
    try {
        const worker = context.serviceWorkers().find((value) => value.url().startsWith('chrome-extension://'))
            ?? await context.waitForEvent('serviceworker');
        const extensionId = new URL(worker.url()).hostname;
        expect(extensionId).toMatch(/^[a-p]{32}$/);
        const home = await context.newPage();
        await home.goto(`chrome-extension://${extensionId}/home.html`);
        const password = (await fs.readFile(manifest.password_file, 'utf8')).trim();
        const ui = async (id: string) => {
            let found: Locator | undefined;
            await expect.poll(async () => {
                for (const page of context.pages()) {
                    if (!page.url().startsWith(`chrome-extension://${extensionId}/`)) continue;
                    const candidate = page.getByTestId(id);
                    if (await candidate.count() === 1 && await candidate.isVisible()) { found = candidate; return true; }
                }
                return false;
            }, { timeout: 20_000 }).toBe(true);
            return found!;
        };
        const click = async (id: string) => (await ui(id)).click();
        const fill = async (id: string, value: string) => (await ui(id)).fill(value);
        await click('onboarding-create-wallet');
        await click('onboarding-create-with-srp-button');
        await fill('create-password-new-input', password);
        await fill('create-password-confirm-input', password);
        await click('create-password-terms');
        await click('create-password-submit');
        await click('passkey-maybe-later-button');
        await click('recovery-phrase-remind-later');
        const metrics = await ui('metametrics-checkbox');
        if (await metrics.getAttribute('data-checked') === 'true') await metrics.click();
        await click('metametrics-i-agree');
        await click('onboarding-complete-done');
        for (const chain of [125, 126]) {
            await home.goto(`chrome-extension://${extensionId}/home.html#/networks?view=add`);
            await fill('network-form-network-name', `Paxeer isolated ${chain}`);
            await fill('network-form-chain-id', String(chain));
            await home.locator('#nativeCurrency').fill('PAX');
            await click('test-add-rpc-drop-down');
            await home.getByRole('button', { name: 'Add RPC URL', exact: true }).click();
            await fill('rpc-url-input-test', manifest[`rpc${chain}`]);
            await fill('rpc-name-input-test', `Paxeer isolated ${chain}`);
            await click('page-container-footer-next');
            await click('page-container-footer-next');
        }
        const switchChain = async (chain: number) => {
            await home.goto(`chrome-extension://${extensionId}/home.html#`);
            await click('dapp-connection-control-bar__network-button');
            await click(`Paxeer isolated ${chain}`);
        };
        await switchChain(125);
        const app = await context.newPage();
        await app.goto(manifest.app_url);
        await expect(app.getByTestId('embedded-config')).toHaveText('unavailable');
        await expect(app.getByTestId('bootstrap-config-mode')).toHaveText('default');
        const connect = async (approve: boolean) => {
            await app.bringToFront();
            await app.locator('[data-testid="injected-connect"][data-provider-rdns="io.metamask"]').click();
            if (approve) {
                await expect.poll(async () => {
                    if (await app.getByTestId('wallet-status').textContent() === 'ready') return 'ready';
                    for (const page of context.pages()) {
                        if (page.url().startsWith(`chrome-extension://${extensionId}/`)
                            && await page.getByTestId('confirm-btn').isVisible()) {
                            await page.getByTestId('confirm-btn').click(); return 'approved';
                        }
                    }
                    return '';
                }).not.toBe('');
            } else await click('cancel-btn');
        };
        await connect(false);
        await expect(app.getByTestId('wallet-status')).toHaveText('signed-out');
        await expect(app.getByTestId('wallet-address')).toHaveText('');
        cases.rejected_connection = true;
        await connect(true);
        await expect(app.getByTestId('wallet-status')).toHaveText('ready');
        const account = (await app.getByTestId('wallet-address').textContent())!.trim();
        expect(account).toMatch(/^0x[0-9a-fA-F]{40}$/);
        await expect(app.getByTestId('wallet-chain')).toHaveText('125');
        cases.missing_embedded_config = { account, chain: 125 };
        for (const chain of [125, 126]) {
            const accounts = await rpcAt(manifest[`rpc${chain}`], 'eth_accounts');
            const hash = await rpcAt(manifest[`rpc${chain}`], 'eth_sendTransaction', [{
                from: accounts[0], to: account, value: '0xde0b6b3a7640000', chainId: `0x${chain.toString(16)}`,
            }]);
            await expect.poll(async () => (await rpcAt(manifest[`rpc${chain}`], 'eth_getTransactionReceipt', [hash]))?.status).toBe('0x1');
        }
        await app.getByTestId('wallet-refresh').click();
        await app.getByTestId('wallet-read-chain').click();
        await expect(app.getByTestId('wallet-chain-status')).toHaveText('observed');
        const open = async () => {
            await app.getByTestId('wallet-clear-transfer').click();
            await app.getByTestId('send-recipient').fill(manifest.recipient);
            await app.getByTestId('send-amount').fill('0.0001');
            await app.getByRole('button', { name: 'Send native PAX', exact: true }).click();
            await expect(app.getByRole('dialog', { name: 'Confirm send transaction', exact: true })).toBeVisible();
        };
        await open();
        await app.getByRole('button', { name: 'Confirm send', exact: true }).click();
        await click('confirm-footer-button');
        await expect(app.getByTestId('transaction-hash')).toHaveText(/^0x[0-9a-fA-F]{64}$/);
        const hash = (await app.getByTestId('transaction-hash').textContent())!.trim();
        await expect.poll(async () => (await rpcAt(manifest.rpc125, 'eth_getTransactionReceipt', [hash]))?.status).toBe('0x1');
        const transaction = await rpcAt(manifest.rpc125, 'eth_getTransactionByHash', [hash]);
        expect(BigInt(transaction.chainId)).toBe(125n);
        expect(transaction.from.toLowerCase()).toBe(account.toLowerCase());
        expect(transaction.to.toLowerCase()).toBe(manifest.recipient.toLowerCase());
        expect(BigInt(transaction.value)).toBe(100000000000000n);
        expect(await rpcAt(manifest.rpc126, 'eth_getTransactionReceipt', [hash])).toBeNull();
        cases.expected_chain_transaction = { hash, chainId: 125 };
        const nonce126 = await rpcAt(manifest.rpc126, 'eth_getTransactionCount', [account, 'pending']);
        await open();
        await switchChain(126);
        await expect(app.getByTestId('wallet-status')).toHaveText('signed-out');
        await expect(app.getByTestId('wallet-address')).toHaveText('');
        await expect(app.getByTestId('wallet-chain')).toHaveText('');
        await expect(app.getByRole('dialog', { name: 'Confirm send transaction', exact: true })).toHaveCount(0);
        expect(await rpcAt(manifest.rpc126, 'eth_getTransactionCount', [account, 'pending'])).toBe(nonce126);
        cases.mid_confirmation_switch = { chainId: 126, nonceUnchanged: true };
        const sdkRefusals = await app.evaluate(async () => {
            const detail = await new Promise<{ provider: any; info: any }>((resolve, reject) => {
                const timer = window.setTimeout(() => { window.removeEventListener('eip6963:announceProvider', listener); reject(new Error('Actual provider discovery timed out')); }, 5000);
                const listener = (event: Event) => {
                    const value = (event as CustomEvent).detail;
                    if (value?.info?.rdns !== 'io.metamask') return;
                    window.clearTimeout(timer);
                    window.removeEventListener('eip6963:announceProvider', listener);
                    resolve(value);
                };
                window.addEventListener('eip6963:announceProvider', listener);
                window.dispatchEvent(new Event('eip6963:requestProvider'));
            });
            const sdkPath = '/sdk.js';
            const { WalletInterface } = await import(sdkPath);
            const actual = new WalletInterface(detail.provider, detail.info);
            const results = [];
            try {
                for (const action of [() => actual.signMessage('isolated chain admission'),
                    () => actual.signTypedData({ domain: { chainId: 125 }, primaryType: 'Admission',
                        types: { Admission: [{ name: 'chain', type: 'uint256' }] }, message: { chain: 125 } }),
                    () => actual.sendTransaction({ to: '0x00000000000000000000000000000000000000b1', value: 1n, chainId: 125 })]) {
                    try { await action(); throw new Error('A wrong-chain SDK request was admitted'); }
                    catch (error) {
                        if ((error as { code?: number }).code !== 4901) throw error;
                        results.push(4901);
                    }
                }
            } finally { actual.dispose(); }
            return results;
        });
        expect(sdkRefusals).toEqual([4901, 4901, 4901]);
        expect(await rpcAt(manifest.rpc126, 'eth_getTransactionCount', [account, 'pending'])).toBe(nonce126);
        cases.sdk_wrong_chain_signatures = sdkRefusals;
        await app.reload();
        await expect(app.getByTestId('wallet-status')).toHaveText('signed-out');
        await expect(app.getByTestId('wallet-address')).toHaveText('');
        cases.wrong_chain_reload = true;
        await switchChain(125);
        await connect(true);
        await expect(app.getByTestId('wallet-status')).toHaveText('ready');
        await expect(app.getByTestId('wallet-chain')).toHaveText('125');
        await expect(app.getByTestId('wallet-address')).toHaveText(account);
        await app.reload();
        await expect(app.getByTestId('wallet-status')).toHaveText('ready');
        await expect(app.getByTestId('wallet-chain')).toHaveText('125');
        cases.reconnect_reload = { account, chainId: 125 };
        await open();
        await home.goto(`chrome-extension://${extensionId}/home.html#`);
        await click('account-options-menu-button');
        await click('global-menu-lock');
        await expect(app.getByTestId('wallet-status')).toHaveText('signed-out');
        await expect(app.getByRole('dialog', { name: 'Confirm send transaction', exact: true })).toHaveCount(0);
        cases.accounts_changed_lock = true;
        await fill('unlock-password', password);
        await click('unlock-submit');
        await connect(true);
        await expect(app.getByTestId('wallet-status')).toHaveText('ready');
        await open();
        await app.getByTestId('wallet-sign-out').click();
        await expect(app.getByTestId('wallet-status')).toHaveText('signed-out');
        await expect(app.getByRole('dialog', { name: 'Confirm send transaction', exact: true })).toHaveCount(0);
        cases.sign_out = true;
        await connect(true);
        await expect(app.getByTestId('wallet-status')).toHaveText('ready');
        await open();
        await home.goto(`chrome-extension://${extensionId}/home.html#`);
        await click('account-options-menu-button');
        await click('global-menu-connected-sites');
        await click('disconnect-all-button');
        await click('disconnect-all-sites-confirm');
        await expect(app.getByTestId('wallet-status')).toHaveText('signed-out');
        await expect(app.getByTestId('wallet-address')).toHaveText('');
        await expect(app.getByRole('dialog', { name: 'Confirm send transaction', exact: true })).toHaveCount(0);
        expect(await rpcAt(manifest.rpc126, 'eth_getTransactionCount', [account, 'pending'])).toBe(nonce126);
        cases.revoked_connection = true;
        await fs.writeFile(path.join(manifest.output_dir, 'chain-result.json'), JSON.stringify({
            schema: 'paxeer-x.wallet-injected-chain-result.v1', source_hashes: manifest.source_hashes,
            cases, completed_cases: Object.keys(cases).length, skipped_cases: 0, exit_code: 0,
        }), { mode: 0o600 });
    } finally {
        await context.close();
    }
});
