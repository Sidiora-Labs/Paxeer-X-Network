import React, { useCallback, useEffect, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { ethers } from 'ethers';
import { PAXEER_CHAIN_ID, WalletInterface } from '@paxeer/wallet';

import { LocaleProvider } from '../../../human/apps/wallet/src/providers/LocaleProvider';
import { WalletProvider, useWallet } from '../../../human/apps/wallet/src/wallet/WalletProvider';
import { readWalletConfig, type WalletConfig } from '../../../human/apps/wallet/src/wallet/config';
import { transferTransaction } from '../../../human/apps/wallet/src/wallet/session';
import { SendConfirmation } from '../../../human/apps/wallet/src/widgets/send/SendConfirmation';
import { useSendForm } from '../../../human/apps/wallet/src/widgets/send/useSendForm';
import type { SendableToken } from '../../../human/apps/wallet/src/widgets/send/useSendableTokens';

interface FixtureBootstrap {
    readonly recipient: string;
    readonly amount: string;
    readonly config?: WalletConfig | null;
}

declare global {
    interface Window {
        __PAXEER_INJECTED_FIXTURE__?: FixtureBootstrap;
    }
}

function bootstrap(): FixtureBootstrap {
    const value = window.__PAXEER_INJECTED_FIXTURE__;
    if (!value || Array.isArray(value)
        || Object.keys(value).some((key) => !['recipient', 'amount', 'config'].includes(key))
        || typeof value.recipient !== 'string' || typeof value.amount !== 'string') {
        throw new Error('The isolated wallet fixture requires explicit recipient and amount');
    }
    transferTransaction({ to: value.recipient, value: value.amount, decimals: 18 });
    if (value.config === undefined || value.config === null) {
        return Object.freeze({ recipient: value.recipient, amount: value.amount,
            ...(value.config === null ? { config: null } : {}) });
    }
    const config = value.config;
    if (typeof config !== 'object' || Array.isArray(config)
        || Object.keys(config).some((key) => !['gatewayUrl', 'rpcUrl', 'identityUrl', 'identityKey', 'authRedirectUrl', 'chainId'].includes(key))
        || config.chainId !== PAXEER_CHAIN_ID
        || typeof config.gatewayUrl !== 'string' || typeof config.rpcUrl !== 'string'
        || typeof config.identityUrl !== 'string' || typeof config.identityKey !== 'string'
        || (config.authRedirectUrl !== null && typeof config.authRedirectUrl !== 'string')) {
        throw new Error('The supplied public wallet configuration does not match the production contract');
    }
    const admitted = readWalletConfig({
        NEXT_PUBLIC_PAXEER_WALLET_API: config.gatewayUrl,
        NEXT_PUBLIC_PAXEER_RPC_URL: config.rpcUrl,
        NEXT_PUBLIC_SUPABASE_URL: config.identityUrl,
        NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY: config.identityKey,
        ...(config.authRedirectUrl === null ? {} : { NEXT_PUBLIC_AUTH_REDIRECT_URL: config.authRedirectUrl }),
    });
    return Object.freeze({ recipient: value.recipient, amount: value.amount, config: Object.freeze(admitted) });
}

function errorMessage(error: unknown): string {
    return error instanceof Error ? error.message : 'The wallet operation failed';
}

function InjectedWalletFixture({ initial }: { readonly initial: FixtureBootstrap }) {
    const wallet = useWallet();
    const form = useSendForm();
    const [chain, setChain] = useState<number | null>(null);
    const [chainStatus, setChainStatus] = useState<'unavailable' | 'loading' | 'observed' | 'refused'>('unavailable');
    const [nativeToken, setNativeToken] = useState<SendableToken | null>(null);
    const [operationError, setOperationError] = useState('');
    const [operationPending, setOperationPending] = useState(false);
    const readGeneration = useRef(0);

    useEffect(() => {
        form.setTo(initial.recipient);
        form.setAmount(initial.amount);
    }, [initial, form.setTo, form.setAmount]);

    const readChain = useCallback(async () => {
        const generation = ++readGeneration.current;
        const connected = wallet.wallet;
        const address = wallet.address;
        setChain(null);
        setNativeToken(null);
        setOperationError('');
        if (!(connected instanceof WalletInterface) || !address || wallet.status !== 'ready') {
            setChainStatus('unavailable');
            return;
        }
        setChainStatus('loading');
        try {
            const currentChain = await connected.chainId();
            const balance = await connected.provider.request({ method: 'eth_getBalance', params: [address, 'latest'] });
            if (typeof balance !== 'string' || !/^0x[0-9a-fA-F]+$/.test(balance)) {
                throw new Error('The injected provider returned a malformed native balance');
            }
            if (await connected.chainId() !== currentChain) {
                throw new Error('The injected provider changed chains during the balance read');
            }
            if (generation !== readGeneration.current) return;
            const raw = BigInt(balance);
            setChain(currentChain);
            setNativeToken({ symbol: 'PAX', name: 'Paxeer', decimals: 18,
                balance: ethers.formatEther(raw), balanceRaw: raw.toString() });
            setChainStatus('observed');
        } catch (error) {
            if (generation !== readGeneration.current) return;
            setChainStatus('refused');
            setOperationError(errorMessage(error));
        }
    }, [wallet.wallet, wallet.address, wallet.status]);

    useEffect(() => {
        void readChain();
        return () => { readGeneration.current += 1; };
    }, [readChain]);

    const perform = async (operation: () => Promise<unknown>) => {
        setOperationPending(true);
        setOperationError('');
        try {
            await operation();
        } catch (error) {
            setOperationError(errorMessage(error));
        } finally {
            setOperationPending(false);
        }
    };

    return (
        <main data-testid="fixture-ready">
            <h1>Injected wallet fixture</h1>
            <dl>
                <dt>Status</dt><dd data-testid="wallet-status">{wallet.status}</dd>
                <dt>Account</dt><dd data-testid="wallet-address">{wallet.address ?? ''}</dd>
                <dt>Custody</dt><dd data-testid="wallet-mode">{wallet.mode ?? ''}</dd>
                <dt>Observed chain</dt><dd data-testid="wallet-chain">{chain ?? ''}</dd>
                <dt>Chain read</dt><dd data-testid="wallet-chain-status">{chainStatus}</dd>
                <dt>Configured Paxeer chain</dt><dd data-testid="wallet-expected-chain">{PAXEER_CHAIN_ID}</dd>
                <dt>Embedded wallet</dt><dd data-testid="embedded-config">{wallet.embeddedAvailable ? 'available' : 'unavailable'}</dd>
                <dt>Configuration</dt><dd data-testid="bootstrap-config-mode">{initial.config === undefined ? 'default' : initial.config === null ? 'unconfigured' : 'supplied'}</dd>
            </dl>
            <p data-testid="config-error">{wallet.configError ?? ''}</p>
            <p data-testid="provider-error" role="status">{wallet.error ?? ''}</p>
            <section data-testid="injected-discovery" aria-label="Discovered injected wallets">
                {wallet.injected.map((detail) => (
                    <button key={detail.info.uuid} type="button" data-testid="injected-connect"
                        data-provider-uuid={detail.info.uuid} data-provider-rdns={detail.info.rdns}
                        disabled={wallet.busy || operationPending}
                        onClick={() => { void perform(() => wallet.connectInjected(detail.info.uuid)); }}>
                        Connect {detail.info.name}
                    </button>
                ))}
            </section>
            <button type="button" data-testid="wallet-read-chain" onClick={() => { void readChain(); }}>Read actual chain</button>
            <button type="button" data-testid="wallet-refresh" disabled={wallet.busy || operationPending}
                onClick={() => { void perform(wallet.refresh); }}>Refresh wallet</button>
            <button type="button" data-testid="wallet-sign-out" disabled={wallet.busy || operationPending}
                onClick={() => { void perform(wallet.signOut); }}>Sign out</button>
            <label>Recipient<input data-testid="send-recipient" value={form.to}
                onChange={(event) => { form.setTo(event.target.value); }} /></label>
            <label>PAX amount<input data-testid="send-amount" value={form.amount}
                onChange={(event) => { form.setAmount(event.target.value); }} /></label>
            <div data-testid="confirmation-host">
                <SendConfirmation triggerLabel="Send native PAX" token={{ symbol: 'PAX' }}
                    amount={form.amount} to={form.to} loading={form.loading}
                    disabled={form.loading || wallet.status !== 'ready' || nativeToken === null}
                    onOpen={() => form.validateBeforeConfirm(nativeToken)}
                    onConfirm={() => form.submit(nativeToken)} />
            </div>
            <p data-testid="operation-error" role="status">{operationError || form.error}</p>
            <output data-testid="transaction-hash">{form.txHash}</output>
            <button type="button" data-testid="wallet-clear-transfer" onClick={form.clearTransfer}>Clear recorded transfer</button>
        </main>
    );
}

const element = document.getElementById('root');
if (!element) throw new Error('The isolated wallet fixture mount is absent');
const root = createRoot(element);
try {
    const initial = bootstrap();
    root.render(
        <LocaleProvider>
            <WalletProvider {...(initial.config === undefined ? {} : { config: initial.config })}>
                <InjectedWalletFixture initial={initial} />
            </WalletProvider>
        </LocaleProvider>,
    );
} catch (error) {
    root.render(<main><p data-testid="fixture-bootstrap-error" role="alert">{errorMessage(error)}</p></main>);
}
