// @vitest-environment jsdom
import { it, expect, vi } from 'vitest';
import { createRoot } from '../../apps/web/node_modules/react-dom/client.js';
import { act } from '../../apps/web/node_modules/react/index.js';
const state = vi.hoisted(() => ({ wallet: '0x'+'a'.repeat(40), reads: 0, balance:1n, ids:[{result:42n}] as any[] }));
vi.mock('wagmi', () => ({
  useAccount: () => ({ address: state.wallet }),
  usePublicClient: () => client,
  useReadContracts: ({contracts}: any) => ({ data: contracts[0]?.functionName === 'balanceOf' ? [{result:state.balance}] : state.ids }),
  useWriteContract: () => ({ isPending:false, writeContract: () => { throw Error('Unexpected dispatch'); } }),
}));
const client = { readContract: async () => { state.reads++; throw Error('RPC unavailable'); } };
vi.mock('@/hooks/tx/use-optimistic-receipt', () => ({ useOptimisticReceipt: () => ({ receipt:undefined }) }));
vi.mock('@/core/network/contracts', () => ({ FEES_ROUTER_ADDRESS:'0x'+'d'.repeat(40), useReadFeesRouterNftContract:()=>({data:'0x'+'e'.repeat(40)}), useReadQuoterGetPoolsByCreator:()=>({data:[]}), ERC721_ENUMERABLE_ABI:[] }));
vi.mock('@/core/sdk-config', () => ({ dataApiUrl:(path:string)=>path }));
vi.mock('@/core/clients/metadata', () => ({ fetchTokenMetadataBatch:()=>{ throw Error('Unexpected metadata'); } }));
vi.mock('@/utils/format', () => ({ formatAddress:(address:string)=>address, formatCurrency:(n:number)=>String(n) }));
import RewardsView from '../../apps/web/src/widgets/rewards/rewards-view';
it('actual RewardsView read failure renders unavailable and disables claim without invented pool identity', async () => {
  (globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
  const host = document.createElement('div'); document.body.append(host); const root = createRoot(host);
  await act(async()=> { root.render(<RewardsView/>); await new Promise(resolve=>setTimeout(resolve,0)); });
  expect(state.reads).toBeGreaterThan(0);
  expect(host.textContent).toContain('excludes unavailable entitlements');
  expect(host.textContent).toContain('External wallet connection is not an authenticated application account');
  expect(host.textContent).toContain('NFT #42');
  const claim = [...host.querySelectorAll('button')].find(button=>button.textContent==='Unavailable');
  expect(claim?.disabled).toBe(true);
  expect(host.querySelector('a[href^="/token/"]')).toBeNull();
  await act(async()=>root.unmount()); host.remove();
});

it('actual RewardsView unresolved NFT enumeration never reports complete zero', async () => {
  state.balance = 2n; state.ids = [{result:42n},{status:'failure'}];
  const host = document.createElement('div'); document.body.append(host); const root = createRoot(host);
  await act(async()=> { root.render(<RewardsView/>); await new Promise(resolve=>setTimeout(resolve,0)); });
  expect(host.textContent).toContain('1 NFT rights unresolved');
  expect(host.textContent).toContain('Partial known claimable subtotal');
  state.ids = [{result:43n},{status:'failure'}];
  await act(async()=> { root.render(<RewardsView/>); await new Promise(resolve=>setTimeout(resolve,0)); });
  expect(host.textContent).toContain('NFT #43'); expect(host.textContent).not.toContain('NFT #42');
  state.ids = [];
  await act(async()=> { root.render(<RewardsView/>); await new Promise(resolve=>setTimeout(resolve,0)); });
  expect(host.textContent).toContain('Unavailable — fee-rights read pending or failed');
  expect(host.textContent).toContain('2 NFT rights unresolved');
  await act(async()=>root.unmount()); host.remove();
});
