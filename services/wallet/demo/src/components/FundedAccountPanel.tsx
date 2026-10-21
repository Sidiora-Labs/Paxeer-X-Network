'use client';

import { useEffect, useMemo, useState } from 'react';
import {
  CUSTODY_TARGET, decodeCustodyAuthorization, EndpointClient,
  HumanClient, KernelAvailability, PaxeerProvider, WalletInterface,
  type Eip6963ProviderDetail, type Hex, type PaxeerWallet, type PlanIntentRequest,
  type TypedDataPayload,
} from '@paxeer/wallet';

export function unifiedOrigin(): string {
  const value = process.env.NEXT_PUBLIC_PAXEER_WALLET_API;
  if (!value) throw new Error('Configure NEXT_PUBLIC_PAXEER_WALLET_API as the canonical unified API origin.');
  const url = new URL(value);
  if (!['https:', 'http:'].includes(url.protocol) || url.username || url.password ||
    url.pathname !== '/' || url.search || url.hash ||
    (url.protocol === 'http:' && !['localhost', '127.0.0.1', '[::1]'].includes(url.hostname))) {
    throw new Error('The unified API requires an HTTPS origin, or a loopback HTTP origin, without a path or credentials.');
  }
  return url.origin;
}

export function SupportedWalletPanel({ paxeer, origin, injected }: {
  paxeer: PaxeerWallet; origin: string; injected?: Eip6963ProviderDetail;
}) {
  const [account, setAccount] = useState<Hex | null>(null);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState('Connect explicitly to begin.');
  const [failed, setFailed] = useState(false);
  const [to, setTo] = useState('');
  const [value, setValue] = useState('');
  const [data, setData] = useState('0x');
  const [typed, setTyped] = useState('');
  const [plan, setPlan] = useState('');
  const [custody, setCustody] = useState<Hex | null>(null);
  const [custodyApproved, setCustodyApproved] = useState(false);

  const token = useMemo(() => async () => (await paxeer.getSession())?.access_token ?? null, [paxeer]);
  const provider = useMemo(() => injected?.provider ?? new PaxeerProvider({
    gatewayUrl: origin, rpcUrl: `${origin}/`, token,
    confirm: request => window.confirm(`Approve ${request.method}?\n${JSON.stringify(request.params)}`),
  }), [injected, origin, token]);
  const wallet = useMemo(() => new WalletInterface(provider, injected?.info), [provider, injected]);
  const endpoint = useMemo(() => new EndpointClient({
    url: `${origin}/`, fetch: async (input, init) => {
      const authorization = await token();
      const headers = new Headers(init?.headers);
      if (authorization) headers.set('Authorization', `Bearer ${authorization}`);
      return fetch(input, { ...init, headers });
    }
  }), [origin, token]);
  const kernel = useMemo(() => new KernelAvailability(endpoint), [endpoint]);
  const human = useMemo(() => new HumanClient({ url: origin, kernel, authorization: token }), [origin, kernel, token]);
  const storageKey = account ? `paxeer-demo:custody:125:${account.toLowerCase()}` : null;

  useEffect(() => {
    const reset = () => { setAccount(null); setCustody(null); setCustodyApproved(false); setResult('Wallet changed. Connect again.'); };
    provider.on('accountsChanged', reset); provider.on('chainChanged', reset);
    return () => {
      provider.removeListener('accountsChanged', reset); provider.removeListener('chainChanged', reset);
      if (provider instanceof PaxeerProvider) provider.disconnect();
    };
  }, [provider]);

  useEffect(() => {
    setCustody(null); setCustodyApproved(false);
    if (!storageKey || !account || wallet.mode !== 'embedded') return;
    try {
      const saved = localStorage.getItem(storageKey);
      if (!saved) return;
      const decoded = decodeCustodyAuthorization(saved as Hex);
      if (decoded.account.toLowerCase() !== account.toLowerCase() || decoded.chainId !== 125n) throw new Error('Retained custody belongs to another account or chain.');
      setCustody(saved as Hex);
      setResult('Retained custody loaded. Read status or recover the exact approval before sending.');
    } catch (cause) { setFailed(true); setResult(cause instanceof Error ? cause.message : 'Retained custody unavailable'); }
  }, [storageKey, account, wallet.mode]);

  async function action(run: () => Promise<unknown>) {
    if (busy) return;
    setBusy(true); setFailed(false);
    try {
      const answer = await run();
      setResult(typeof answer === 'string' ? answer : JSON.stringify(answer, (_key, item: unknown) => typeof item === 'bigint' ? item.toString() : item, 2));
    } catch (cause) { setFailed(true); setResult(cause instanceof Error ? cause.message : 'Action unavailable'); }
    finally { setBusy(false); }
  }

  async function connected() {
    const chain = await wallet.chainId();
    if (chain !== 125) throw new Error(`Wrong network: ${chain}. Select HyperPaxeer chain 125 in your wallet.`);
    const accounts = await wallet.accounts();
    if (!accounts[0]) throw new Error('The wallet exposed no account.');
    if (account && accounts[0].toLowerCase() !== account.toLowerCase()) throw new Error('Account changed. Connect again.');
    return accounts[0];
  }

  async function prepareCustody() {
    const address = await connected();
    if (wallet.mode !== 'embedded') throw new Error('Custody handoff is supported by the embedded wallet only.');
    const bytes = await provider.request({ method: 'paxeer_prepareCustody', params: [{ account: address, chainId: '125', to: CUSTODY_TARGET, value, data }] });
    if (typeof bytes !== 'string') throw new Error('Custody preparation returned no canonical bytes.');
    const decoded = decodeCustodyAuthorization(bytes as Hex);
    if (decoded.account.toLowerCase() !== address.toLowerCase() || decoded.chainId !== 125n) throw new Error('Custody preparation changed account or network.');
    localStorage.setItem(`paxeer-demo:custody:125:${address.toLowerCase()}`, bytes);
    setCustody(bytes as Hex); setCustodyApproved(false);
    return decoded;
  }

  async function sendCustody() {
    await connected();
    if (!custody || !custodyApproved) throw new Error('Recover or explicitly approve the exact retained custody first.');
    const exact = decodeCustodyAuthorization(custody);
    if (!Number.isSafeInteger(Number(exact.nonce))) throw new Error('Custody nonce exceeds the wallet transaction integer bound.');
    return wallet.sendTransaction({
      to: exact.to, value: exact.value, data: exact.data,
      nonce: Number(exact.nonce), chainId: Number(exact.chainId), gas: exact.gas,
      maxFeePerGas: exact.maxFeePerGas, maxPriorityFeePerGas: exact.maxPriorityFeePerGas
    });
  }

  const disabled = busy || !account;
  return <section data-supported-wallet={wallet.mode} className="flex flex-col gap-3">
    <p className="text-sm text-neutral-400">{wallet.mode === 'embedded' ? 'Embedded threshold custody. The gateway coordinates authenticated threshold signing.' : 'Injected wallet. Your wallet controls signing and custody.'}</p>
    <button type="button" data-wallet-accounts disabled={busy} onClick={() => action(async () => { const address = await connected(); setAccount(address); return address; })}>Connect {wallet.mode} wallet</button>
    {account && <code className="break-all text-sm">{account}</code>}
    <label className="text-sm">Recipient<input aria-label="Transaction recipient" value={to} onChange={e => setTo(e.target.value)} className="w-full rounded bg-neutral-800 p-2" /></label>
    <label className="text-sm">Amount in wei<input aria-label="Amount in wei" value={value} onChange={e => setValue(e.target.value)} className="w-full rounded bg-neutral-800 p-2" /></label>
    <label className="text-sm">Canonical calldata<textarea aria-label="Canonical calldata" value={data} onChange={e => setData(e.target.value)} className="w-full rounded bg-neutral-800 p-2" /></label>
    <button type="button" data-wallet-send disabled={disabled} onClick={() => action(async () => {
      await connected();
      if (!/^0x[0-9a-fA-F]{40}$/.test(to) || !/^(0|[1-9][0-9]*)$/.test(value) || !/^0x(?:[0-9a-fA-F]{2})*$/.test(data)) throw new Error('A valid recipient, integer wei amount and byte calldata are required.');
      return wallet.sendTransaction({ to: to as Hex, value: BigInt(value), data: data as Hex, chainId: 125 });
    })}>Review and send transaction</button>
    <label className="text-sm">Typed data JSON<textarea aria-label="Typed data JSON" value={typed} onChange={e => setTyped(e.target.value)} className="w-full rounded bg-neutral-800 p-2" /></label>
    <button type="button" data-wallet-typed disabled={disabled} onClick={() => action(async () => { await connected(); return wallet.signTypedData(JSON.parse(typed) as TypedDataPayload); })}>Review and sign typed data</button>
    {wallet.mode === 'embedded' && <>
      <p className="text-sm text-neutral-400">Custody uses the canonical custody target and deposit calldata above. Preparation does not authorize or broadcast.</p>
      <button type="button" data-custody-prepare disabled={disabled || !!custody} onClick={() => action(prepareCustody)}>Prepare custody handoff</button>
      {custody && <>
        <code className="max-h-24 overflow-y-auto break-all text-xs" data-retained-custody>{custody}</code>
        <button type="button" data-custody-sign disabled={disabled} onClick={() => action(async () => { await connected(); const signature = await wallet.signCustody(custody); setCustodyApproved(true); return signature; })}>Review and approve exact custody</button>
        <button type="button" data-custody-recover disabled={disabled} onClick={() => action(async () => { await connected(); const signature = await provider.request({ method: 'paxeer_recoverCustody', params: [{ custody }] }); setCustodyApproved(true); return signature; })}>Recover retained custody approval</button>
        <button type="button" data-custody-send disabled={disabled || !custodyApproved} onClick={() => action(sendCustody)}>Submit approved custody</button>
        <button type="button" data-custody-status disabled={disabled} onClick={() => action(async () => { await connected(); return provider.request({ method: 'paxeer_custodyStatus', params: [{ custody }] }); })}>Read custody status</button>
      </>}
    </>}
    <label className="text-sm">Kernel intent JSON<textarea aria-label="Kernel intent JSON" value={plan} onChange={e => setPlan(e.target.value)} className="w-full rounded bg-neutral-800 p-2" /></label>
    <button type="button" data-kernel-plan disabled={disabled} onClick={() => action(async () => {
      await connected(); kernel.invalidate(); const availability = await kernel.current();
      if (!availability.available) return availability;
      return human.planIntent(JSON.parse(plan) as PlanIntentRequest);
    })}>Read availability and plan intent</button>
    <pre role={failed ? 'alert' : 'status'} data-wallet-result className="max-h-48 overflow-auto whitespace-pre-wrap break-all rounded bg-neutral-800 p-3 text-xs">{result}</pre>
  </section>;
}
