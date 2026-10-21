import { expect, test } from 'vitest';
import { renderToString } from 'react-dom/server';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { WagmiProvider } from 'wagmi';
import { readFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { getAddress, type Hex } from 'viem';
import { decodeV2Contract } from '@kindle/sdk';
import { getWagmiConfig } from '../wagmi-config';
import { WalletRuntimeProvider, useWalletRuntime } from './runtime';
import { WalletSessionController, StaleWalletSessionError } from './session';
import { approvedWalletAssets, checkedCommittedHead, isWalletPrivateQueryV2, type PublicNetworkConfigurationV1, type WalletNetworkAuthorityV2 } from './backend';
import { assertAssetOnChain, formatAssetAmount, nativePAX, parseAssetAmount } from './assets';
import { getPublicClient } from './client';
import { depositIntent, FundingTracker, fundingURI } from '../../widgets/funding/funding';
import { maximumNativeTransfer } from '../../widgets/funding/transfer';
import { fiatFundingAvailability, openFiatCheckout } from '../../widgets/funding/vendor-gate';
import { validateLoginChallenge } from './auth';

const wagmiConfig = getWagmiConfig();

const A = getAddress('0x0000000000000000000000000000000000000001');
const B = getAddress('0x0000000000000000000000000000000000000002');
function Snapshot() { const value = useWalletRuntime(); return <output>{value.session === null ? 'disconnected' : value.session.account}</output>; }
test('server render uses the actual registered providers without browser initialization or a second selected wallet', () => {
  expect(typeof window).toBe('undefined');
  const queries = new QueryClient();
  const rendered = renderToString(<QueryClientProvider client={queries}><WagmiProvider config={wagmiConfig}><WalletRuntimeProvider><Snapshot /><Snapshot /></WalletRuntimeProvider></WagmiProvider></QueryClientProvider>);
  expect(rendered.match(/disconnected/g)?.length).toBe(2);
  expect(wagmiConfig.chains[0].id).toBe(125);
  if (!process.env.NEXT_PUBLIC_WALLETCONNECT_PROJECT_ID) expect(wagmiConfig.connectors.some(connector => connector.type === 'walletConnect')).toBe(false);
  queries.clear();
});
test('real query cache loses account A allowance before account B is published; public markets survive', async () => {
  const queries = new QueryClient();
  const privateKey = ['readContract', { functionName: 'allowance', args: [A, B] }] as const;
  const publicKey = ['token-stats', B] as const;
  queries.setQueryData(privateKey, 'account-A-cache-marker'); queries.setQueryData(publicKey, 'public-market-cache-marker');
  const sessions = new WalletSessionController(previous => queries.removeQueries({ predicate: query => isWalletPrivateQueryV2(query, previous?.account) }));
  sessions.select('registered', 'injected', A, 125, ['registered']);
  let release: ((value: bigint) => void) | undefined;
  const delayed = sessions.run(() => new Promise<bigint>(resolve => { release = resolve; }));
  sessions.select('registered', 'injected', B, 125, ['registered']);
  expect(queries.getQueryData(privateKey)).toBeUndefined(); expect(queries.getQueryData(publicKey)).toBe('public-market-cache-marker');
  release!(123n); await expect(delayed).rejects.toBeInstanceOf(StaleWalletSessionError);
  sessions.dispose(); queries.clear();
});
test('native amount conversion and fee reserve preserve exact integers including zero maximum', () => {
  expect(formatAssetAmount(nativePAX, '1000000000000000000')).toBe('1 PAX');
  expect(parseAssetAmount(nativePAX, '1')).toBe(1000000000000000000n);
  expect(maximumNativeTransfer(21000000000000n, 21000000000000n)).toBe(0n);
  expect(() => maximumNativeTransfer(21000000000000n, 21000000000001n)).toThrow('Insufficient PAX');
});
test('private account contract keys are isolated without dropping public contract reads', () => {
  expect(isWalletPrivateQueryV2({ queryKey: ['readContract', { functionName: 'allowance', args: [A,B] }] }, A)).toBe(true);
  expect(isWalletPrivateQueryV2({ queryKey: ['readContract', { functionName: 'decimals', address: B }] }, A)).toBe(false);
  expect(isWalletPrivateQueryV2({ queryKey: ['production-market', 'pools'] }, A)).toBe(false);
  expect(isWalletPrivateQueryV2({ queryKey: ['private-v2'], meta: { private: true } }, A)).toBe(true);
  expect(isWalletPrivateQueryV2({ queryKey: ['readContracts', { contracts: [{ functionName: 'allowance', args: [A,B] }, { functionName: 'decimals' }] }] }, A)).toBe(true);
});
test('fiat vendor remains an explicit release blocker with no enabled checkout', () => {
  expect(fiatFundingAvailability.available).toBe(false);
  expect(fiatFundingAvailability.releaseBlocker).toContain('D09_008');
  expect(() => openFiatCheckout()).toThrow();
});
interface ActualQualificationInput {
  schemaVersion: 2; sourceSha256: string; network: PublicNetworkConfigurationV1;
  usdlRuntimeHash: Hex; committedHead: { number: string; hash: Hex };
  wallet: string; identityOrigin: string; fundingTransactionHash: Hex; unrelatedTransactionHash: Hex;
}
function qualification() {
  const file = process.env.L25_QUALIFICATION_CONFIG_FILE, digest = process.env.L25_QUALIFICATION_CONFIG_SHA256;
  if (!file || !digest) throw new Error('Actual admitted L25 network/head/deposit evidence and checksum are required; no synthetic qualification input is accepted');
  const bytes = readFileSync(file);
  expect(createHash('sha256').update(bytes).digest('hex')).toBe(digest);
  const input = JSON.parse(bytes.toString()) as ActualQualificationInput;
  expect(input.schemaVersion).toBe(2); expect(input.sourceSha256).toMatch(/^[0-9a-f]{64}$/);
  expect(input.sourceSha256).toBe(process.env.L25_ADMITTED_SOURCE_SHA256);
  const client = getPublicClient(input.network.publicRPC);
  const authority: WalletNetworkAuthorityV2 = { network: input.network, usdlRuntimeHash: input.usdlRuntimeHash, committedHead: async signal => { signal.throwIfAborted(); return { number: BigInt(input.committedHead.number), hash: input.committedHead.hash }; } };
  return { input, client, authority };
}
test('actual activated USDL code and six decimals bind to the committed canonical head', async () => {
  const { client, authority } = qualification();
  const head = await checkedCommittedHead(client, authority, new AbortController().signal);
  const assets = approvedWalletAssets(authority);
  await assertAssetOnChain(client, assets[1]!, head.number);
  expect(formatAssetAmount(assets[1]!, '1000000')).toBe('1 USDL');
  expect(parseAssetAmount(assets[1]!, '1')).toBe(1000000n);
  expect(() => parseAssetAmount(assets[1]!, '1.0000001')).toThrow('precision');
}, 20000);
test('actual recipient-specific USDL transfer verifies receipt and committed balance before spendable display', async () => {
  const { input, client, authority } = qualification();
  const wallet = getAddress(input.wallet), asset = approvedWalletAssets(authority)[1]!;
  const sessions = new WalletSessionController(() => {});
  sessions.select('qualification-external', 'injected', wallet, 125, ['qualification-external']);
  try {
    const tracker = new FundingTracker(client, sessions, depositIntent(wallet, asset, '1000000'), signal => checkedCommittedHead(client, authority, signal));
    expect(fundingURI(tracker.intent)).toContain('@125/transfer?address=');
    const result = await tracker.refresh(input.fundingTransactionHash);
    expect(result.state).toBe('balance_confirmed'); expect(result.detectedRaw).toBe('1000000');
    expect(BigInt(result.balanceRaw!)).toBeGreaterThanOrEqual(1000000n);
    const unrelated = await tracker.refresh(input.unrelatedTransactionHash);
    expect(unrelated.state).toBe('unknown_external_source'); expect(unrelated.balanceRaw).toBeUndefined();
  } finally { sessions.dispose(); }
}, 20000);
test('an actual backend-issued challenge rejects a foreign application origin or selected account before signing', async () => {
  const { input } = qualification();
  const origin = new URL(input.identityOrigin);
  expect(origin.protocol).toBe('https:'); expect(origin.origin).toBe(input.identityOrigin);
  const account = getAddress(input.wallet);
  const response = await fetch(new URL('/v2/auth/challenges', origin), { method: 'POST', headers: { 'Content-Type': 'application/json', Origin: origin.origin }, body: JSON.stringify({ wallet: account, origin: origin.origin }), redirect: 'error', signal: AbortSignal.timeout(15000) });
  expect(response.ok).toBe(true);
  const challenge = decodeV2Contract('ChallengeResponse', await response.json()).data.challenge;
  expect(validateLoginChallenge(challenge, origin.origin, account, 'Sign in to Sidiora.').account).toBe(account);
  expect(() => validateLoginChallenge(challenge, 'https://foreign.invalid', account, 'Sign in to Sidiora.')).toThrow('mismatch');
  expect(() => validateLoginChallenge(challenge, origin.origin, account === A ? B : A, 'Sign in to Sidiora.')).toThrow('mismatch');
}, 20000);
