import {createPublicClient,http,parseAbi,parseAbiItem,toEventSelector,decodeEventLog,type Hex} from 'viem';
import { afterEach, expect, it } from 'vitest';
import type { Session } from '../../packages/production-contracts/src/types.gen';
import { AccountApiError, accountEpoch, clearAccountSession, installAccountSession, publicProfile, patchProfile, accountSession, putWatch, deleteWatch, watchlistPage, accountRequest, readPortfolio, readInterval, registerAccountInvalidator, assertAccount, extendedProfile } from '../../apps/web/src/widgets/profile/account-transport';
import { QueryClient } from '../../apps/web/node_modules/@tanstack/react-query/build/modern/index.js';
import { renderToStaticMarkup } from '../../apps/web/node_modules/react-dom/server.node.js';
import {PositionFigures} from '../../apps/web/src/widgets/profile/positions-tab';
import {exactAmount} from '../../apps/web/src/widgets/profile/account-disclosure';
import NetWorthChart from '../../apps/web/src/widgets/profile/net-worth-chart';
import { PublicShareFigures } from '../../apps/web/src/widgets/pnl/pnl-card-landing';
import { checkedSharePreview } from '../../apps/web/src/widgets/pnl/share-disclosure';
import { previewShare, publishShare, revokeShare, readPublicShare, ownerShare } from '../../apps/web/src/widgets/pnl/share-transport';
import { JSDOM } from '../../apps/web/node_modules/jsdom/lib/api.js';
import { localWatchlist, mutateWatch, refreshWatchlist, watchState } from '../../apps/web/src/widgets/profile/watchlist-state';
import { validatedExtendedProfile } from '../../apps/web/src/widgets/profile/profile-edit-state';
import { commitProfile } from '../../apps/web/src/widgets/profile/profile-edit-state';

function fixture(name: string): string {
  const value = process.env[name];
  if (!value) throw new Error(`Real isolated account fixture missing ${name}`);
  return value;
}
function loopback(name: string) {
  const url = new URL(fixture(name));
  if (!['127.0.0.1', '[::1]', 'localhost'].includes(url.hostname)) throw new Error('Account journey fixtures must use isolated loopback services');
  return url;
}
async function authenticateFixture() {
  loopback('NEXT_PUBLIC_USER_API_URL');
  const gateway = loopback('NEXT_PUBLIC_GATEWAY_URL');
  const token = fixture('L31_REAL_ACCESS_TOKEN'), expected = fixture('L31_REAL_SESSION_ID');
  const response = await fetch(new URL('/v2/auth/sessions', gateway), { headers: { Authorization: `Bearer ${token}` }, cache: 'no-store' });
  expect(response.status).toBe(200);
  const body = await response.json() as { data: { items: Session[] } };
  const session = body.data.items.find(value => value.sessionId === expected);
  if (!session) throw new Error('Real identity fixture did not authorize expected session');
  clearAccountSession(); installAccountSession(session, token, accountEpoch());
  return session;
}
afterEach(() => clearAccountSession());
it('real authenticated profile races retain rejected draft and commit exactly one revision', async () => {
  const session = await authenticateFixture();
  const initial = (await publicProfile(session.wallet)).data.profile;
  const left = { bio: 'L31 concurrent left' }, right = { bio: 'L31 concurrent right' };
  try {
    const results = await Promise.all([commitProfile(session.wallet, initial.revision, left), commitProfile(session.wallet, initial.revision, right)]);
    expect(results.filter(value => value.kind === 'committed')).toHaveLength(1);
    const rejected = results.find(value => value.kind === 'conflict');
    expect(rejected?.kind).toBe('conflict');
    if (rejected?.kind !== 'conflict') throw new Error('No authoritative revision conflict');
    expect([left.bio, right.bio]).toContain(rejected.draft.bio);
    const saved = (await publicProfile(session.wallet)).data.profile;
    expect(BigInt(saved.revision)).toBe(BigInt(initial.revision) + 1n);
    expect([left.bio, right.bio]).toContain(saved.bio);
  } finally {
    const latest = (await publicProfile(session.wallet)).data.profile;
    await patchProfile(session.wallet, { expectedRevision: latest.revision, bio: initial.bio });
  }
});
it('real watchlist retry persists one normalized pool and sign-out removes bearer authority', async () => {
  const session = await authenticateFixture();
  const pool = fixture('L31_REAL_POOL_ADDRESS').toLowerCase();
  const before = await watchlistPage();
  const existed = before.data.items.some(item => item.poolAddress.toLowerCase() === pool);
  try {
    await putWatch(pool); await putWatch(pool);
    let cursor: string | undefined;
    const pools: string[] = [];
    do { const page = await watchlistPage(cursor); pools.push(...page.data.items.map(item => item.poolAddress.toLowerCase())); cursor = page.nextCursor ?? undefined; } while (cursor);
    expect(pools.filter(item => item === pool)).toHaveLength(1);
    expect(new Set(pools).size).toBe(pools.length);
    expect((await publicProfile(session.wallet)).data.profile.wallet.toLowerCase()).toBe(session.wallet.toLowerCase());
  } finally { if (!existed) await deleteWatch(pool); }
  const old = accountSession(); clearAccountSession();
  expect(old?.signal.aborted).toBe(true);
  expect(accountSession()).toBeNull();
  await expect(Promise.resolve().then(() => putWatch(pool))).rejects.toBeInstanceOf(AccountApiError);
});

it('real in-flight private reads cannot survive sign-out or a replacement session and cached browser-back data is cleared', async () => {
  const session=await authenticateFixture(),token=fixture('L31_REAL_ACCESS_TOKEN');
  loopback('NEXT_PUBLIC_PNL_API_URL');
  const client=new QueryClient();
  registerAccountInvalidator(client,()=>{void client.cancelQueries({predicate:q=>q.queryKey[0]==='private-account'});client.removeQueries({predicate:q=>q.queryKey[0]==='private-account'});});
  const old=accountSession();if(!old)throw new Error('Missing real account scope');
  const authorized=await readPortfolio();
  const key=['private-account',session.sessionId,session.wallet.toLowerCase(),'125',String(old.epoch),'portfolio'];
  client.setQueryData(key,authorized);
  const pending=readPortfolio();
  clearAccountSession();
  await expect(pending).rejects.toBeDefined();
  expect(old.signal.aborted).toBe(true);
  expect(client.getQueryData(key)).toBeUndefined();
  expect(()=>assertAccount(old)).toThrow(AccountApiError);
  installAccountSession(session,token,accountEpoch());
  const replacement=accountSession();if(!replacement)throw new Error('Missing replacement scope');
  const request=readPortfolio();
  installAccountSession(session,token,accountEpoch());
  await expect(request).rejects.toBeDefined();
  expect(replacement.signal.aborted).toBe(true);
  expect(accountSession()?.epoch).toBeGreaterThan(replacement.epoch);
  expect(client.getQueryData(key)).toBeUndefined();
  client.clear();
});
it('actual owner, other authenticated wallet and anonymous profile payloads exclude private account fields', async () => {
  const session=await authenticateFixture();
  const user=loopback('NEXT_PUBLIC_USER_API_URL'),gateway=loopback('NEXT_PUBLIC_GATEWAY_URL');
  const otherToken=fixture('L31_REAL_OTHER_ACCESS_TOKEN');
  for(const token of [null,fixture('L31_REAL_ACCESS_TOKEN'),otherToken]){
    const response=await fetch(new URL(`/v3/profiles/${session.wallet.toLowerCase()}`,user),{headers:token?{Authorization:`Bearer ${token}`}:{},cache:'no-store'});
    expect(response.status).toBe(200);
    const body=await response.json();
    const profile=body.data.profile;
    expect(Object.keys(profile).sort()).toEqual(['avatarAssetId','bannerAssetId','bio','creatorPools','displayName','revision','socials','wallet','website'].sort());
    for(const field of ['preferences','sessionId','accessToken','notificationDestinations','email','privateActivity'])expect(profile).not.toHaveProperty(field);
  }
  const denied=await fetch(new URL('/v3/me/preferences',gateway),{headers:{Authorization:`Bearer ${otherToken}`},cache:'no-store'});
  expect(denied.status).toBe(200);
  const ownerAsset=fixture('L31_REAL_OWNER_ASSET_ID');
  const foreign=await fetch(new URL(`/v2/accounts/${session.wallet.toLowerCase()}/assets/${ownerAsset}`,gateway),{headers:{Authorization:`Bearer ${otherToken}`},cache:'no-store'});
  expect(foreign.status).toBe(403);
  const anonymous=await fetch(new URL('/v3/me/preferences',gateway),{cache:'no-store'});
  expect(anonymous.status).toBe(401);
  expect((await extendedProfile(session.wallet)).data.profile.wallet.toLowerCase()).toBe(session.wallet.toLowerCase());
});
it('an actually expired server credential stops authorization retries and clears all current account authority', async () => {
  const session=await authenticateFixture();
  clearAccountSession();
  installAccountSession(session,fixture('L31_REAL_EXPIRED_ACCESS_TOKEN'),accountEpoch());
  await expect(accountRequest(new URL('/v3/me/preferences',loopback('NEXT_PUBLIC_GATEWAY_URL')).toString())).rejects.toMatchObject({status:401});
  expect(accountSession()).toBeNull();
});
it('real anonymous card publishes only selected profit fields and duplicate publication retains the same identity', async () => {
  await authenticateFixture();loopback('NEXT_PUBLIC_PNL_API_URL');
  const portfolio=await readPortfolio();
  const selected={fields:['realizedPnlRaw'] as const,includeWallet:false,expiresAt:new Date(Date.now()+3600000).toISOString()};
  const selection={...selected,fields:[...selected.fields]};
  const preview=checkedSharePreview(await previewShare({snapshotId:portfolio.data.summary.snapshotId,...selection}),selection,portfolio.data.summary.wallet);
  const mutation=crypto.randomUUID();
  const owner=await publishShare(preview,mutation);
  try{
    const duplicate=await publishShare(preview,mutation);
    expect(duplicate.id).toBe(owner.id);
    expect(owner.share.fields).toEqual(['realizedPnlRaw']);
    expect(owner.share.wallet??null).toBeNull();
    let authoritative=duplicate;
    for(let attempt=0;authoritative.state==='pending'&&attempt<3;attempt++){await new Promise(resolve=>setTimeout(resolve,500));authoritative=await ownerShare(owner.id);}
    expect(authoritative.state).toBe('public');
    const wireResponse=await fetch(new URL(`/v3/portfolio/shares/${owner.id}`,loopback('NEXT_PUBLIC_PNL_API_URL')),{cache:'no-store',credentials:'omit'});
    expect(wireResponse.status).toBe(200);
    const wire=await wireResponse.json();
    expect(Object.keys(wire.values)).toEqual(['realizedPnlRaw']);
    expect(JSON.stringify(wire)).not.toContain(portfolio.data.summary.wallet);
    const publicCard=await readPublicShare(owner.id);
    expect(publicCard.values).not.toHaveProperty('completeValueUsdlRaw');
    expect(publicCard.values).not.toHaveProperty('knownRealizedSubtotalUsdlRaw');
    expect(publicCard.wallet??null).toBeNull();
    const html=renderToStaticMarkup(<PublicShareFigures share={publicCard}/>);
    expect(html).not.toContain(portfolio.data.summary.wallet);
    expect(html).not.toContain('completeValueUsdlRaw');
  }finally{await revokeShare(owner.id);}
  await expect(readPublicShare(owner.id)).rejects.toMatchObject({status:410});
});
it('real deposit-only accounting does not present cash inflow as investment profit', async () => {
  await authenticateFixture();loopback('NEXT_PUBLIC_PNL_API_URL');
  const from=fixture('L31_REAL_DEPOSIT_FROM_UTC'),to=fixture('L31_REAL_DEPOSIT_TO_UTC');
  const scope=accountSession();if(!scope)throw new Error('Missing authenticated deposit fixture');
  const deposit=await realReceiptTransfers('L31_REAL_DEPOSIT_TRANSACTION_HASH',fixture('L31_REAL_USDL_ADDRESS'),scope.session.wallet);
  expect(deposit.incoming-deposit.outgoing).toBe(100000000n);
  expect(Number(deposit.block.timestamp)*1000).toBeGreaterThanOrEqual(Date.parse(from));
  expect(Number(deposit.block.timestamp)*1000).toBeLessThan(Date.parse(to));
  const series=await readInterval({from,to,interval:'1d'});
  expect(series.entries.length).toBeGreaterThan(0);
  for(const entry of series.entries){expect(entry.tradeCount).toBe(0);expect(entry.saleCount).toBe(0);expect(entry.realizedPnlUsdlRaw).toBe('0');expect(entry.knownNetGainUsdlRaw).toBe('0');}
  const html=renderToStaticMarkup(<NetWorthChart series={series}/>);
  expect(html).toContain('No realized profit: 0.000000 USDL');
  expect(html).not.toContain('Gain: 100.000000 USDL');
});

it('real browser storage rejects corrupt choices and denied persistence retains visible session-local edits', async () => {
  clearAccountSession();
  const windowDescriptor=Object.getOwnPropertyDescriptor(globalThis,'window'),storageDescriptor=Object.getOwnPropertyDescriptor(globalThis,'localStorage');
  const browser=new JSDOM('',{url:'https://account.test'});
  Object.defineProperty(globalThis,'window',{value:browser.window,configurable:true});
  Object.defineProperty(globalThis,'localStorage',{get:()=>browser.window.localStorage,configurable:true});
  try{
    browser.window.localStorage.setItem('sidiora_watchlist','{"not":"an array"}');
    expect(localWatchlist()).toEqual([]);
    await refreshWatchlist();expect(watchState().error).toContain('No stored choices were overwritten');
    expect(browser.window.localStorage.getItem('sidiora_watchlist')).toBe('{"not":"an array"}');
    browser.reconfigure({url:'about:blank'});
    const pool='0x'+'a'.repeat(40);
    await mutateWatch(pool,true);
    expect(watchState().list).toEqual([pool]);
    expect(watchState().error).toContain('this page session only');
    await mutateWatch(pool,false);expect(watchState().list).toEqual([]);
    expect(()=>validatedExtendedProfile({expectedRevision:'1',displayName:' ',website:'javascript:alert(1)'})).toThrow(AccountApiError);
    expect(()=>validatedExtendedProfile({expectedRevision:'1',displayName:'Published',website:'https://user:password@example.org'})).toThrow(AccountApiError);
  }finally{
    browser.window.close();
    if(windowDescriptor)Object.defineProperty(globalThis,'window',windowDescriptor);else Reflect.deleteProperty(globalThis,'window');
    if(storageDescriptor)Object.defineProperty(globalThis,'localStorage',storageDescriptor);else Reflect.deleteProperty(globalThis,'localStorage');
  }
});

it('actual buy-fullsell-buy remaining basis is rendered exactly from the authoritative reconciled position', async () => {
  await authenticateFixture();loopback('NEXT_PUBLIC_PNL_API_URL');
  const token=fixture('L31_REAL_REBUY_TOKEN_ADDRESS').toLowerCase();
  const expectedBasis=fixture('L31_REAL_REBUY_REMAINING_BASIS_USDL_RAW');
  const scope=accountSession();if(!scope)throw new Error('Missing authenticated rebuy fixture');
  const first=await realReceiptTransfers('L31_REAL_FIRST_BUY_TRANSACTION_HASH',token,scope.session.wallet);
  const sale=await realReceiptTransfers('L31_REAL_FULL_SELL_TRANSACTION_HASH',token,scope.session.wallet);
  const rebuy=await realReceiptTransfers('L31_REAL_REBUY_TRANSACTION_HASH',token,scope.session.wallet);
  const opening=await first.client.readContract({address:token as `0x${string}`,abi:parseAbi(['function balanceOf(address wallet) view returns(uint256)']),functionName:'balanceOf',args:[scope.session.wallet as `0x${string}`],blockNumber:first.receipt.blockNumber-1n});
  expect(opening).toBe(0n);
  expect(first.incoming-first.outgoing).toBeGreaterThan(0n);
  expect(sale.outgoing-sale.incoming).toBe(first.incoming-first.outgoing);
  expect(rebuy.incoming-rebuy.outgoing).toBeGreaterThan(0n);
  expect(first.receipt.blockNumber).toBeLessThan(sale.receipt.blockNumber);
  expect(sale.receipt.blockNumber).toBeLessThan(rebuy.receipt.blockNumber);
  const portfolio=await readPortfolio();
  const position=portfolio.data.positions.find(row=>row.tokenAddress.toLowerCase()===token);
  if(!position)throw new Error('Real buy-fullsell-buy position missing from first bounded fixture page');
  expect(position.balanceRaw).toBe((rebuy.incoming-rebuy.outgoing).toString());
  expect(position.basisQuality).toBe('complete');
  expect(position.remainingCostUsdlRaw).toBe(expectedBasis);
  expect(position.knownRemainingCostUsdlRaw).toBe(expectedBasis);
  const html=renderToStaticMarkup(<PositionFigures row={position} errors={portfolio.positionErrors[position.positionId]}/>);
  expect(html).toContain(`complete cost: ${exactAmount(expectedBasis)}`);
  expect(html).toContain(`Known cost: ${exactAmount(expectedBasis)}`);
  expect(html).toContain(position.balanceRaw);
});

const transferEvent=parseAbiItem('event Transfer(address indexed from,address indexed to,uint256 value)');
async function realReceiptTransfers(name:string,asset:string,wallet:string){
  const rpc=loopback('L31_REAL_RPC_URL');
  const client=createPublicClient({transport:http(rpc.toString(),{retryCount:0})});
  expect(await client.getChainId()).toBe(125);
  const hash=fixture(name);if(!/^0x[0-9a-fA-F]{64}$/.test(hash))throw new Error('Real transaction receipt fixture required');
  const receipt=await client.getTransactionReceipt({hash:hash as Hex});
  expect(receipt.status).toBe('success');
  const block=await client.getBlock({blockNumber:receipt.blockNumber});
  const transfers=receipt.logs.filter(log=>log.address.toLowerCase()===asset.toLowerCase()&&log.topics[0]?.toLowerCase()===toEventSelector(transferEvent).toLowerCase()).map(log=>decodeEventLog({abi:[transferEvent],data:log.data,topics:log.topics}));
  const incoming=transfers.filter(log=>log.args.to.toLowerCase()===wallet.toLowerCase()).reduce((sum,log)=>sum+log.args.value,0n);
  const outgoing=transfers.filter(log=>log.args.from.toLowerCase()===wallet.toLowerCase()).reduce((sum,log)=>sum+log.args.value,0n);
  return {incoming,outgoing,block,receipt,client};
}
