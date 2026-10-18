import {describe,it,expect} from 'vitest';
import {privateKeyToAccount} from 'viem/accounts';
import type {StreamSigningContext} from '../../core/clients/livestream-api';

const fixture=['L40_TEST_WEB_API_URL','L40_TEST_WEB_PRIVATE_KEY','L40_TEST_WEB_ACCESS_TOKEN','L40_TEST_WEB_POOL','L40_TEST_WEB_ACTION_ENVIRONMENT'] as const;
const missing=fixture.filter(name=>!process.env[name]);
function required(name:typeof fixture[number]):string{const value=process.env[name];if(!value)throw new Error(`${name} is required for the real connected livestream fixture`);return value}

describe.skipIf(missing.length>0)(`actual connected livestream service and signed creator path (needs a local livestream fixture; missing: ${missing.join(', ')||'none'})`,()=>{
 it('returns one durable operation for a replay and rejects stale revision without fabricating provider state',async()=>{
  const endpoint=required('L40_TEST_WEB_API_URL');const url=new URL(endpoint);if(!['127.0.0.1','localhost','[::1]'].includes(url.hostname))throw new Error('Owned local fixture is required');
  process.env.NEXT_PUBLIC_LIVESTREAM_API_URL=endpoint;
  const api=await import('../../core/clients/livestream-api');const account=privateKeyToAccount(required('L40_TEST_WEB_PRIVATE_KEY') as `0x${string}`);const token=required('L40_TEST_WEB_ACCESS_TOKEN');const pool=required('L40_TEST_WEB_POOL');const environment=required('L40_TEST_WEB_ACTION_ENVIRONMENT');
  const signing:StreamSigningContext={wallet:account.address.toLowerCase(),environment,signMessage:message=>account.signMessage({message}),assertCurrent:()=>{if(!token)throw new Error('fixture session expired')}};
  const key=crypto.randomUUID();const first=await api.requestStream(pool,'Connected broadcast',key,token,signing);const again=await api.requestStream(pool,'Connected broadcast',key,token,signing);expect(again.operationId).toBe(first.operationId);expect(['requested','provisioning','ready','live','ending','ended','failed','suspended']).toContain(again.status);
  const edited=await api.editStream(first.streamId,again.revision,'Connected revised title',token,signing);expect(BigInt(edited.revision)).toBeGreaterThan(BigInt(again.revision));await expect(api.editStream(first.streamId,again.revision,'Stale title',token,signing)).rejects.toMatchObject({status:409});
  const current=await api.getStreamOperation(first.operationId,token);expect(BigInt(current.revision)).toBeGreaterThanOrEqual(BigInt(edited.revision));const page=await api.getLivePage();for(const row of page.streams){expect(row.revision).toMatch(/^[1-9][0-9]*$/);expect(row).not.toHaveProperty('streamKey');expect(row).not.toHaveProperty('rtmpUrl')}
 });

 it('feeds the discovery page from the live directory walk: only live rows, once each, ranked without inventing values',async()=>{
  process.env.NEXT_PUBLIC_LIVESTREAM_API_URL=required('L40_TEST_WEB_API_URL');
  const directory=await import('../../widgets/live/use-live-directory');const card=await import('../../widgets/live/stream-card');
  const {streams}=await directory.collectLivePages();expect(new Set(streams.map(stream=>stream.id)).size).toBe(streams.length);for(const stream of streams)expect(stream.status).toBe('live');
  const entries=directory.assembleDirectory(streams,new Map(),new Map(),{});const ranked=directory.rankByViewers(entries);
  for(let i=1;i<ranked.length;i++){const a=ranked[i-1].viewerCount,b=ranked[i].viewerCount;if(a!==null&&b!==null)expect(a).toBeGreaterThanOrEqual(b);if(a===null)expect(b).toBeNull()}
  for(const entry of entries){expect(entry.mcapRaw).toBeNull();expect(card.streamTile(entry).kind).toBe(entry.thumbnailUrl?'thumbnail':'none')}
 });
});
