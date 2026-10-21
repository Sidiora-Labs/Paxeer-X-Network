import fs from 'node:fs';
import ActualWebSocket from 'ws';
import { SdkClient, Operations } from '../dist/index.js';
const config = JSON.parse(fs.readFileSync(process.env.SDK_CONNECTED_CONFIG, 'utf8'));
const session = config.accessToken ? { mode: 'bearer', token: () => config.accessToken } : undefined;
globalThis.WebSocket = class extends ActualWebSocket { constructor(url, protocols) { super(url, protocols, { origin: config.applicationOrigin }); } };
const browserFetch = (url, init) => { const headers = new Headers(init?.headers); headers.set('Origin', config.applicationOrigin); return fetch(url, { ...init, headers }); };
const client = new SdkClient({ ...config.transport, ...(session ? { session } : {}) }, browserFetch);
const api = new Operations(client);
const signal = AbortSignal.timeout(30000);
let cursor;
const identities = new Set();
for (let page = 0; page < 100; page++) {
  const response = await api.call('IF_TRADES', { path: { poolAddress: config.poolAddress }, query: { limit: 100, ...(cursor ? { cursor } : {}) } }, { signal });
  for (const trade of response.data.items) { if (identities.has(trade.eventId)) throw new Error('Duplicate paginated trade'); identities.add(trade.eventId); console.log(JSON.stringify(trade)); }
  if (!response.nextCursor) break;
  if (response.nextCursor === cursor) throw new Error('Pagination cursor did not advance');
  cursor = response.nextCursor;
  if (page === 99) throw new Error('Pagination bound exceeded');
}
