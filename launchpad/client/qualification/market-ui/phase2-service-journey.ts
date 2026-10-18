import { SdkClient, HttpError, type TransportConfig } from '../../packages/sdk/src/index';
import { MarketSession, marketAddress, marketLimits, type MarketLiveState } from '../../apps/web/src/page-modules/token/market-session';
import type { RankingsRequest } from '../../packages/production-contracts/src/types.gen';
export interface MarketJourneyInput { transport: TransportConfig; poolAddress: string; emptyCategory: RankingsRequest['category']; rankingCategory: RankingsRequest['category']; }
export async function observeMarketJourney(input: MarketJourneyInput) {
  const client = new SdkClient(input.transport);
  const pool = marketAddress(input.poolAddress);
  const [detail, head, holders, rankings, empty] = await Promise.all([
    client.request(`/v1/pools/${pool}`, 'PoolDetailResponse', { idempotentRead: true }),
    client.requestV2(`/v2/pools/${pool}/trades?limit=2`, 'TradesResponse', { idempotentRead: true }),
    client.request(`/v1/pools/${pool}/holders?limit=100`, 'HoldersResponse', { idempotentRead: true }),
    client.request(`/v1/rankings/${input.rankingCategory}?limit=2`, 'RankingsResponse', { idempotentRead: true }),
    client.request(`/v1/rankings/${input.emptyCategory}?limit=2`, 'RankingsResponse', { idempotentRead: true }),
  ]);
  if (!head.nextCursor) throw new Error('Real fixture must contain an older trade page');
  if (!rankings.nextCursor) throw new Error('Real fixture must contain another ranking page');
  const [older, rankingPage] = await Promise.all([
    client.requestV2(`/v2/pools/${pool}/trades?limit=2&cursor=${encodeURIComponent(head.nextCursor)}`, 'TradesResponse', { idempotentRead: true }),
    client.request(`/v1/rankings/${input.rankingCategory}?limit=2&cursor=${encodeURIComponent(rankings.nextCursor)}`, 'RankingsResponse', { idempotentRead: true }),
  ]);
  let rejectedCursorStatus: number | null = null;
  try { await client.requestV2(`/v2/pools/${pool}/trades?limit=2&cursor=invalid`, 'TradesResponse'); }
  catch (error) { if (!(error instanceof HttpError)) throw error; rejectedCursorStatus = error.status; }
  if (rejectedCursorStatus !== 400) throw new Error('Actual invalid cursor request must return400');
  return { observedAt: new Date().toISOString(), poolAddress: pool, detail, head, older, holders, rankings, rankingPage, empty, rejectedCursorStatus };
}

export async function observeInterruptedMarketJourney(input: MarketJourneyInput) {
  const client = new SdkClient(input.transport), session = new MarketSession(client, input.poolAddress);
  const controller = new AbortController();
  let gapObserved = false;
  const trace: Array<{ at: number; stream: MarketLiveState['stream']; revision: string | null; sequence: string | null; rowCount: number; error: string | null }> = [];
  const unsubscribe = session.subscribe(() => {
    const state = session.getSnapshot();
    if (state.stream === 'resync-required' && state.error?.includes('Sequence gap')) gapObserved = true;
    if (trace.length >= 128) trace.shift();
    trace.push({ at: Date.now(), stream: state.stream, revision: state.revision, sequence: state.sequence, rowCount: state.rows.length, error: state.error });
  });
  const waitFor = (predicate: (state: MarketLiveState) => boolean, label: string) => new Promise<MarketLiveState>((resolve, reject) => {
    let stop: (() => void) | undefined;
    const timer = setTimeout(() => { stop?.(); reject(new Error(`Actual market ${label} did not occur within30s`)); }, 30_000);
    const check = () => { const state = session.getSnapshot(); if (predicate(state)) { clearTimeout(timer); stop?.(); resolve(state); } };
    stop = session.subscribe(check); check();
  });
  try {
    await session.start(controller.signal);
    const before = await waitFor(state => state.stream === 'live' && state.stats !== null, 'initial replay checkpoint');
    session.visibility(true);
    const hiddenState = session.getSnapshot();
    await new Promise(resolve => setTimeout(resolve, 31_000));
    if (session.status(Date.now()) === 'current') throw new Error('Hidden31s market incorrectly remained current');
    if (session.getSnapshot().rows !== hiddenState.rows) throw new Error('Hidden market repainted queued fills');
    await session.start(controller.signal);
    const resumed = await waitFor(state => gapObserved && state.stream === 'live' && state.stats !== null && state.statsGeneratedAt !== before.statsGeneratedAt, 'dropped-sequence reconciliation with revised statistics');
    if (resumed.rows.length > marketLimits.fills) throw new Error('Recovered market exceeded bounded history');
    if (session.status(Date.now()) !== 'current') throw new Error('Reconciled market has no current snapshot and heartbeat boundary');
    return { observedAt: new Date().toISOString(), poolAddress: session.poolAddress, before, resumed, trace, gapObserved };
  } finally { unsubscribe(); controller.abort(); session.close(); }
}
