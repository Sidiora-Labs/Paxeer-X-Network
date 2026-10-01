import React, { useEffect, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { WalletProvider, useWallet } from '../../../human/apps/wallet/src/wallet/WalletProvider';
import { LocaleProvider } from '../../../human/apps/wallet/src/providers/LocaleProvider';
import { SendConfirmation } from '../../../human/apps/wallet/src/widgets/send/SendConfirmation';

function Controls() {
    const wallet = useWallet();
    const [recipient, setRecipient] = useState('');
    const [result, setResult] = useState('');
    const observedEvents = useRef<unknown[]>([]);
    useEffect(() => {
        const events = observedEvents.current;
        const provider = wallet.injected[0]?.provider;
        const listeners = ['accountsChanged', 'chainChanged', 'disconnect'].map((event) => {
            const listener = (value: unknown) => events.push({ event, value: event === 'disconnect' ? 'disconnected' : value });
            provider?.on(event as 'accountsChanged', listener);
            return { event, listener };
        });
        Object.assign(window, { walletFixture: {
            state: () => ({ status: wallet.status, address: wallet.address, embeddedAvailable: wallet.embeddedAvailable, configError: wallet.configError }),
            request: (args: { method: string; params?: unknown[] }) => {
                if (!provider) throw new Error('No actual injected provider announced');
                return provider.request(args);
            },
            events: () => events,
            signMessage: () => wallet.signMessage('Isolated injected-wallet qualification'),
            signTypedData: () => wallet.signTypedData({ domain: { name: 'Isolated qualification', chainId: 125 }, types: { Note: [{ name: 'value', type: 'string' }] }, primaryType: 'Note', message: { value: 'qualification' } }),
        }});
        return () => {
            for (const { event, listener } of listeners) provider?.removeListener(event as 'accountsChanged', listener);
        };
    }, [wallet]);
    return <main>
        <output data-testid="status">{wallet.status}</output>
        <output data-testid="address">{wallet.address}</output>
        <output data-testid="embedded">{String(wallet.embeddedAvailable)}</output>
        {wallet.injected.map((detail) => <button key={detail.info.uuid} data-testid="connect" onClick={() => void wallet.connectInjected(detail.info.uuid).catch(() => setResult('refused'))}>Connect {detail.info.name}</button>)}
        <button data-testid="sign-out" onClick={() => void wallet.signOut()}>Sign out</button>
        <input aria-label="Recipient" value={recipient} onChange={(event) => setRecipient(event.target.value)} />
        <SendConfirmation amount="0.001" token={{ symbol: 'PAX' }} to={recipient} disabled={wallet.status !== 'ready'} onConfirm={async () => {
            setResult('pending');
            try { setResult(await wallet.send({ to: recipient, value: '0.001' })); }
            catch { setResult('refused'); }
        }} />
        <output data-testid="result">{result}</output>
    </main>;
}
createRoot(document.getElementById('root')!).render(<LocaleProvider><WalletProvider config={null} identity={null}><Controls /></WalletProvider></LocaleProvider>);
