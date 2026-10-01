import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import { createRequire } from 'node:module';
import { execFileSync } from 'node:child_process';

const root = process.env.WALLET_FIXTURE_ROOT;
const evidence = process.env.WALLET_FIXTURE_EVIDENCE;
const dependencies = process.env.WALLET_FIXTURE_DEPENDENCIES;
if (!root || !evidence || !dependencies) throw new Error('Explicit repository, private evidence and dependency roots are required');
const require = createRequire(path.join(dependencies, 'package.json'));
const digest = (bytes) => crypto.createHash('sha256').update(bytes).digest('hex');
const record = (file, value) => fs.writeFileSync(file, JSON.stringify(value, null, 2) + '\n', { mode: 0o600 });
const sourceSha = () => execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim();

if (process.argv.includes('--build-entry')) {
    const { build } = require('esbuild');
    const bundle = path.join(evidence, 'bundle');
    fs.mkdirSync(bundle, { recursive: true, mode: 0o700 });
    const built = await build({
        absWorkingDir: root,
        entryPoints: ['tools/qualification/paxeer-x/wallet-injected-entry.tsx'],
        outfile: path.join(bundle, 'app.js'),
        bundle: true, platform: 'browser', format: 'iife', jsx: 'automatic', metafile: true,
        nodePaths: [path.join(dependencies, 'node_modules')],
        alias: {
            '@': path.join(root, 'human/apps/wallet/src'),
            '@paxeer/wallet': path.join(root, 'human/wallet/sdk/src/index.ts'),
            '@sidiora/layerx-sdk/browser': path.join(root, 'agent/sdk/typescript/src/browser.ts'),
            '@sidiora/layerx-sdk': path.join(root, 'agent/sdk/typescript/src/index.ts'),
        },
        define: { 'process.env.NODE_ENV': '"production"', 'process.env': '{}' },
        logLevel: 'warning',
    });
    fs.writeFileSync(path.join(bundle, 'index.html'), '<!doctype html><html><head><meta charset="utf-8"><title>Isolated wallet qualification</title></head><body><div id="root"></div><script src="/app.js"></script></body></html>', { mode: 0o600 });
    const hashes = {};
    for (const input of Object.keys(built.metafile.inputs)) {
        const absolute = path.resolve(root, input);
        if (!input.startsWith('<') && absolute.startsWith(root + path.sep)) hashes[path.relative(root, absolute)] = digest(fs.readFileSync(absolute));
    }
    record(path.join(evidence, 'build.json'), { source_sha: sourceSha(), source_hashes: hashes, bundle_sha256: digest(fs.readFileSync(path.join(bundle, 'app.js'))), dependency_versions: { esbuild: require('esbuild/package.json').version, playwright: require('@playwright/test/package.json').version } });
    console.log('Explicit production-component browser target built');
} else if (process.argv.includes('--verify-baseline')) {
    const { chromium, expect } = require('@playwright/test');
    const { Wallet, verifyMessage, verifyTypedData } = require('ethers');
    const extension = process.env.WALLET_FIXTURE_EXTENSION;
    const archive = process.env.WALLET_FIXTURE_EXTENSION_ARCHIVE;
    if (!extension || !archive || digest(fs.readFileSync(archive)) !== 'b759caca275dec1a10edfebb9d1de1d26589a92104d8b0523ec442930e20e47c') throw new Error('Pinned official extension archive is missing or differs');
    if (require('@playwright/test/package.json').version !== '1.62.0') throw new Error('Browser automation runtime differs from repository pin');
    const extensionManifest = JSON.parse(fs.readFileSync(path.join(extension, 'manifest.json'), 'utf8'));
    if (extensionManifest.version !== '13.50.0.0' || extensionManifest.version_name !== '13.50.0') throw new Error('Unexpected unpacked extension version');
    const run = process.env.WALLET_FIXTURE_RUN;
    const chains = JSON.parse(process.env.WALLET_FIXTURE_CHAINS);
    for (const endpoint of Object.values(chains)) {
        if (new URL(endpoint).hostname !== '127.0.0.1') throw new Error('Only isolated loopback chains are allowed');
    }
    const context = await chromium.launchPersistentContext(path.join(run, 'profile'), {
        channel: 'chromium', headless: process.env.WALLET_FIXTURE_HEADED !== '1',
        args: [`--disable-extensions-except=${extension}`, `--load-extension=${extension}`, '--no-sandbox'],
    });
    const cases = [];
    const observations = {};
    const mark = (name) => { cases.push(name); console.log('completed ' + name); };
    let stage = 'extension_start';
    try {
        const worker = context.serviceWorkers()[0] ?? await context.waitForEvent('serviceworker', { timeout: 60000 });
        const id = new URL(worker.url()).hostname;
        const extensionPage = await context.newPage();
        await extensionPage.goto(`chrome-extension://${id}/home.html`);
        const fresh = Wallet.createRandom();
        const password = crypto.randomBytes(24).toString('base64url');
        async function clickOptional(page, selector) {
            const item = page.locator(selector).first();
            if (await item.isVisible().catch(() => false)) { await item.click(); return true; }
            return false;
        }
        async function clickId(name) {
            await extensionPage.getByTestId(name).click({ timeout: 20000 });
        }
        stage = 'extension_import';
        await clickId('onboarding-import-wallet');
        await clickId('onboarding-import-with-srp-button');
        const phrase = extensionPage.getByTestId('srp-input-import__srp-note');
        await phrase.waitFor({ state: 'visible', timeout: 30000 });
        const words = fresh.mnemonic.phrase.split(' ');
        for (let index = 0; index < words.length; index++) {
            const field = index === 0 ? phrase : extensionPage.getByTestId(`import-srp__srp-word-${index}`);
            await field.click({ timeout: 20000 });
            await extensionPage.keyboard.insertText(words[index]);
            if (index + 1 < words.length) await extensionPage.keyboard.press('Space');
        }
        await clickId('import-srp-confirm');
        await extensionPage.getByTestId('create-password-new-input').fill(password);
        await extensionPage.getByTestId('create-password-confirm-input').fill(password);
        await clickId('create-password-terms');
        await clickId('create-password-submit');
        for (let attempt = 0; attempt < 120; attempt++) {
            if (await extensionPage.getByTestId('account-options-menu-button').isVisible().catch(() => false)) break;
            await clickOptional(extensionPage, '[data-testid="metametrics-i-agree"]');
            await clickOptional(extensionPage, '[data-testid="onboarding-complete-done"]');
            await new Promise((resolve) => setTimeout(resolve, 250));
        }
        await extensionPage.getByTestId('account-options-menu-button').waitFor({ state: 'visible' });
        const page = await context.newPage();
        await page.goto(process.env.WALLET_FIXTURE_APP);
        await expect(page.getByTestId('connect')).toBeVisible({ timeout: 30000 });
        mark('discovery');
        await expect(page.getByTestId('embedded')).toHaveText('false');
        if (!(await page.evaluate(() => window.walletFixture.state())).configError) throw new Error('Missing embedded configuration was not exercised');
        mark('missing_embedded_configuration');

        async function settlePopup(approve, done) {
            const end = Date.now() + 60000;
            while (Date.now() < end) {
                if (await done()) return;
                for (const popup of context.pages()) {
                    if (!popup.url().startsWith(`chrome-extension://${id}/`) || popup === extensionPage) continue;
                    const pattern = approve ? /^(Connect|Confirm|Approve|Add network|Switch network|Next|Got it)$/i : /^(Cancel|Reject)$/i;
                    const candidates = popup.getByRole('button', { name: pattern });
                    for (let index = (await candidates.count()) - 1; index >= 0; index--) {
                        if (await candidates.nth(index).isVisible().catch(() => false)) {
                            await candidates.nth(index).click().catch(() => undefined);
                            break;
                        }
                    }
                }
                await new Promise((resolve) => setTimeout(resolve, 250));
            }
            throw new Error('Actual extension approval/refusal did not resolve');
        }
        async function request(method, params = [], approve = true) {
            await page.evaluate(({ method, params }) => {
                window.walletFixtureResult = { done: false };
                window.walletFixture.request({ method, params }).then((value) => { window.walletFixtureResult = { done: true, value }; }, (error) => { window.walletFixtureResult = { done: true, code: error.code }; });
            }, { method, params });
            await settlePopup(approve, () => page.evaluate(() => window.walletFixtureResult.done));
            return page.evaluate(() => window.walletFixtureResult);
        }
        stage = 'connect_rejection';
        await page.getByTestId('connect').click();
        await settlePopup(false, async () => (await page.getByTestId('result').textContent()) === 'refused');
        await expect(page.getByTestId('status')).toHaveText('signed-out');
        mark('connect_rejection');
        stage = 'connection';
        await page.getByTestId('connect').click();
        await settlePopup(true, async () => (await page.getByTestId('status').textContent()) === 'ready');
        await expect(page.getByTestId('address')).toHaveText(fresh.address, { ignoreCase: true });
        mark('connection');
        async function chain(idNumber) {
            const chainId = `0x${idNumber.toString(16)}`;
            const added = await request('wallet_addEthereumChain', [{ chainId, chainName: `Isolated ${idNumber}`, rpcUrls: [chains[String(idNumber)]], nativeCurrency: { name: 'Isolated gas', symbol: 'PAX', decimals: 18 } }]);
            if (added.code) throw new Error('Actual wallet refused local chain configuration');
            const switched = await request('wallet_switchEthereumChain', [{ chainId }]);
            if (switched.code) throw new Error('Actual wallet refused network change');
            const current = await request('eth_chainId');
            if (current.value !== chainId) throw new Error('Actual provider did not switch chains');
        }
        async function rpc(endpoint, method, params) {
            const response = await fetch(endpoint, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ jsonrpc: '2.0', id: 1, method, params }) });
            const reply = await response.json();
            if (reply.error) throw new Error('Isolated chain RPC refused ' + method);
            return reply.result;
        }
        async function receipt(endpoint, hash) {
            for (let count = 0; count < 100; count++) {
                const value = await rpc(endpoint, 'eth_getTransactionReceipt', [hash]);
                if (value) return value;
                await new Promise((resolve) => setTimeout(resolve, 100));
            }
            throw new Error('Isolated transaction has no receipt');
        }
        stage = 'network_switch';
        await chain(125);
        await chain(126);
        observations.admitted_after_switch = await page.evaluate(() => window.walletFixture.state());
        await chain(125);
        mark('network_switch');
        for (const endpoint of Object.values(chains)) {
            const [genesis] = await rpc(endpoint, 'eth_accounts', []);
            const hash = await rpc(endpoint, 'eth_sendTransaction', [{ from: genesis, to: fresh.address, value: '0x8ac7230489e80000' }]);
            if ((await receipt(endpoint, hash)).status !== '0x1') throw new Error('Isolated genesis funding failed');
        }
        stage = 'message_signing';
        for (const method of ['signMessage', 'signTypedData']) {
            await page.evaluate((method) => {
                window.walletFixtureResult = { done: false };
                window.walletFixture[method]().then((value) => { window.walletFixtureResult = { done: true, value }; }, () => { window.walletFixtureResult = { done: true, failed: true }; });
            }, method);
            await settlePopup(true, () => page.evaluate(() => window.walletFixtureResult.done));
            const signed = await page.evaluate(() => window.walletFixtureResult);
            if (signed.failed) throw new Error('Actual wallet signing failed');
            const recovered = method === 'signMessage' ? verifyMessage('Isolated injected-wallet qualification', signed.value) : verifyTypedData({ name: 'Isolated qualification', chainId: 125 }, { Note: [{ name: 'value', type: 'string' }] }, { value: 'qualification' }, signed.value);
            if (recovered.toLowerCase() !== fresh.address.toLowerCase()) throw new Error('Signature does not recover actual fixture account');
            mark(method === 'signMessage' ? 'message_signing' : 'typed_signing');
        }
        const [destination] = await rpc(chains['125'], 'eth_accounts', []);
        await page.getByLabel('Recipient', { exact: true }).fill(destination);
        async function sendThroughConfirmation() {
            await page.getByRole('button', { name: 'Send', exact: true }).click();
            await page.getByRole('dialog').getByRole('button', { name: /Confirm Send/i }).click();
            await settlePopup(true, async () => /^0x[0-9a-f]{64}$/i.test(await page.getByTestId('result').textContent()));
            return page.getByTestId('result').textContent();
        }
        stage = 'controlled_receipt';
        const hash = await sendThroughConfirmation();
        if ((await receipt(chains['125'], hash)).status !== '0x1') throw new Error('Controlled chain125 send reverted');
        mark('controlled_receipt');
        stage = 'mid_confirmation_switch';
        await page.getByRole('button', { name: 'Send', exact: true }).click();
        observations.confirmation_before_switch = await page.getByRole('dialog').innerText();
        await chain(126);
        const staleVisible = await page.getByRole('dialog').isVisible();
        observations.stale_confirmation_visible = staleVisible;
        observations.wrong_chain_broadcast_observed = false;
        if (staleVisible && (await page.getByTestId('status').textContent()) === 'ready') {
            await page.getByRole('dialog').getByRole('button', { name: /Confirm Send/i }).click();
            await settlePopup(true, async () => {
                const value = await page.getByTestId('result').textContent();
                return value === 'refused' || (/^0x[0-9a-f]{64}$/i.test(value) && value !== hash);
            });
            const changedHash = await page.getByTestId('result').textContent();
            if (changedHash !== 'refused') {
                const actual = await receipt(chains['126'], changedHash);
                observations.wrong_chain_broadcast_observed = actual.status === '0x1';
                observations.other_chain_receipt = { transactionHash: actual.transactionHash, status: actual.status, chainId: 126 };
            }
        }
        mark('mid_confirmation_switch');
        stage = 'lock';
        await extensionPage.goto(`chrome-extension://${id}/home.html`);
        await extensionPage.getByTestId('account-options-menu-button').click();
        await extensionPage.getByRole('button', { name: /Lock MetaMask|Lock wallet|Lock/i }).click();
        await extensionPage.getByTestId('unlock-password').waitFor({ state: 'visible' });
        observations.after_lock = await page.evaluate(() => window.walletFixture.state());
        mark('lock');
        stage = 'reconnect';
        await extensionPage.getByTestId('unlock-password').fill(password);
        await extensionPage.getByTestId('unlock-submit').click();
        await page.getByTestId('connect').click();
        await settlePopup(true, async () => (await page.getByTestId('status').textContent()) === 'ready');
        mark('reconnect');
        stage = 'reload';
        observations.provider_events_before_reload = await page.evaluate(() => window.walletFixture.events());
        await page.reload();
        await expect(page.getByTestId('connect')).toBeVisible();
        await expect(page.getByTestId('status')).not.toHaveText('loading');
        observations.after_reload = await page.evaluate(() => window.walletFixture.state());
        mark('reload');
        observations.provider_events = await page.evaluate(() => window.walletFixture.events());
        record(path.join(run, 'baseline.json'), { source_sha: sourceSha(), extension: extensionManifest.version, completed_cases: cases, observations, scope: 'Genuine fixture readiness and baseline behavior only; not task14.2 qualification' });
    } catch (error) {
        record(path.join(run, 'baseline.json'), { source_sha: sourceSha(), completed_cases: cases, failed_stage: stage, error: error instanceof Error ? error.message.split('\n')[0] : 'failure', observations });
        console.error('Real fixture failed at ' + stage);
        process.exitCode = 1;
    } finally {
        await context.close();
    }
} else {
    throw new Error('Explicit build or verify-baseline mode required');
}
