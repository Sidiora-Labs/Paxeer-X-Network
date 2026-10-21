import { expect, test, vi } from 'vitest';
import { createSiweMessage } from 'viem/siwe';
import { getAddress, type EIP1193Provider } from 'viem';
import { nativePAX, usdlAsset, parseAssetAmount, formatAssetAmount, rawAmount, assetKey } from './assets';
import { WalletSessionController, StaleWalletSessionError } from './session';
import { validateLoginChallenge, type AuthChallenge } from './auth';
import { fundingURI, depositIntent } from '../../widgets/funding/funding';
import { fiatFundingAvailability, openFiatCheckout } from '../../widgets/funding/vendor-gate';
import { maximumNativeTransfer } from '../../widgets/funding/transfer';
const A = getAddress('0x0000000000000000000000000000000000000001');
const B = getAddress('0x0000000000000000000000000000000000000002');
const usdl = usdlAsset('0x85FcD13735F4309833A503EE804ea32395851479');
test('synthetic arithmetic: distinct asset precision, canonical uint256, gas reserve', () => {
  expect(formatAssetAmount(nativePAX, '1000000000000000000')).toBe('1 PAX');
  expect(formatAssetAmount(usdl, '1000000')).toBe('1 USDL');
  expect(parseAssetAmount(nativePAX, '1')).toBe(10n ** 18n);
  expect(parseAssetAmount(usdl, '1')).toBe(1000000n);
  expect(assetKey(nativePAX)).not.toBe(assetKey(usdl));
  for (const amount of ['1.0000001', '-1', '1e6', '1.', ' 1', '01', 'NaN']) expect(() => parseAssetAmount(usdl, amount)).toThrow();
  for (const raw of ['01', '-1', '1.0', (1n << 256n).toString()]) expect(() => rawAmount(raw)).toThrow();
  expect(() => usdlAsset('0x0000000000000000000000000000000000000000')).toThrow();
  expect(maximumNativeTransfer(10n, 10n)).toBe(0n);
  expect(() => maximumNativeTransfer(9n, 10n)).toThrow('Insufficient');
});
test('A response released after B selection is cancelled and discarded', async () => {
  const clear = vi.fn(); const sessions = new WalletSessionController(clear);
  sessions.select('injected', 'injected', A, 125, ['injected']); const old = sessions.capture();
  let release!: (n: string) => void;
  const result = sessions.run(() => new Promise<string>(resolve => { release = resolve; }));
  sessions.select('injected', 'injected', B, 125, ['injected']);
  expect(old.signal.aborted).toBe(true); expect(clear).toHaveBeenCalledTimes(2);
  release('A allowance'); await expect(result).rejects.toBeInstanceOf(StaleWalletSessionError);
  expect(sessions.current?.account).toBe(B); sessions.dispose();
});
test('synthetic provider events cancel immediately, batch refresh and remove listeners', async () => {
  vi.useFakeTimers();
  const handlers = new Map<string, (...args: any[]) => void>();
  const provider = { on: (event: string, cb: (...args: any[]) => void) => handlers.set(event, cb), removeListener: (event: string) => handlers.delete(event) } as unknown as EIP1193Provider;
  const refresh = vi.fn(); const sessions = new WalletSessionController(vi.fn(), refresh);
  sessions.select('injected', 'injected', A, 125, ['injected']); const detach = sessions.bind(provider); const scope = sessions.capture();
  handlers.get('accountsChanged')!([B]); handlers.get('chainChanged')!('0x1');
  expect(scope.signal.aborted).toBe(true); expect(sessions.current?.sessionEpoch).toBe(3);
  expect(() => sessions.assertCurrent(sessions.capture(), true)).toThrow('chain 125');
  await vi.advanceTimersByTimeAsync(50); expect(refresh).toHaveBeenCalledTimes(1);
  detach(); expect(handlers.size).toBe(0); expect(sessions.current).toBeNull(); vi.useRealTimers();
});
test('funding URI binds chain, asset and amount; unqualified fiat checkout stays blocked', () => {
  expect(fundingURI(depositIntent(A, nativePAX, '1000000000000000000'))).toBe(`ethereum:${A}@125?value=1000000000000000000`);
  expect(fundingURI(depositIntent(A, usdl, '1000000'))).toContain('@125/transfer?address=');
  expect(fundingURI(depositIntent(A, usdl, '1000000'))).toContain('&uint256=1000000');
  expect(fiatFundingAvailability.available).toBe(false); expect(() => openFiatCheckout()).toThrow('unqualified');
});
function challengeFor(): AuthChallenge {
  const issued = new Date(); const expiry = new Date(issued.getTime() + 300000);
  const c: AuthChallenge = { challengeId: '00000000-0000-4000-8000-000000000001', wallet: A, nonce: 'ab'.repeat(32), domain: 'sidiora.fun', uri: 'https://sidiora.fun', chainId: '125', purpose: 'login', issuedAt: issued.toISOString(), expiresAt: expiry.toISOString(), message: '' };
  c.message = createSiweMessage({ domain: c.domain, address: A, uri: c.uri, version: '1', chainId: 125, nonce: c.nonce, issuedAt: issued, expirationTime: expiry, statement: 'Sign in to Sidiora.' });
  return c;
}
test('login envelope rejects domain/account/purpose/expiry/signing-byte substitution', () => {
  const c = challengeFor();
  expect(validateLoginChallenge(c, 'https://sidiora.fun', A, 'Sign in to Sidiora.').message).toBe(c.message);
  const identified = { ...c, message: c.message + `\nRequest ID: ${c.challengeId}` };
  expect(validateLoginChallenge(identified, 'https://sidiora.fun', A, 'Sign in to Sidiora.').challengeId).toBe(c.challengeId);
  expect(() => validateLoginChallenge({ ...identified, message: c.message + '\nRequest ID: another-challenge' }, 'https://sidiora.fun', A, 'Sign in to Sidiora.')).toThrow();
  for (const bad of [{ ...c, domain: 'evil.example' }, { ...c, uri: 'https://evil.example' }, { ...c, wallet: B }, { ...c, message: c.message + '\nSend all funds' }, { ...c, purpose: 'step_up' as const }, { ...c, expiresAt: new Date(0).toISOString() }]) expect(() => validateLoginChallenge(bad, 'https://sidiora.fun', A, 'Sign in to Sidiora.')).toThrow();
});
test('connectorProvider rejects the wagmi storage stub connector and resolves a real EIP-1193 provider', async () => {
  const { connectorProvider } = await import('./client');
  // wagmi persists `{ id, name, type, uid }` and exposes it as `useAccount().connector` while `status === "reconnecting"` with `isConnected` already true.
  const stub = { id: 'io.metamask', name: 'MetaMask', type: 'injected', uid: 'a1b2c3' };
  await expect(connectorProvider(stub)).rejects.toThrow('restoring');
  await expect(connectorProvider(undefined)).rejects.toThrow('restoring');
  await expect(connectorProvider({ ...stub, getProvider: async () => ({ request: 1 }) })).rejects.toThrow('unavailable');
  const provider = { request: async () => '0x7d', on: () => provider, removeListener: () => provider } as unknown as EIP1193Provider;
  const connector = { ...stub, getProvider: async function (this: unknown) { if (this !== connector) throw new Error('lost this'); return provider; } };
  await expect(connectorProvider(connector)).resolves.toBe(provider);
});
