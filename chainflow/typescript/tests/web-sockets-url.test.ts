import { test } from 'node:test';
import assert from 'node:assert/strict';

import ChainflowDataGateway from '../dist/esm/index.js';
import { buildURL as buildPriceFeedURL } from '../dist/esm/resources/web-sockets/connectPriceFeed-ws-internal-base.js';
import { buildURL as buildMarketsURL } from '../dist/esm/resources/web-sockets/connectMarkets-ws-internal-base.js';
import { buildURL as buildExplorerURL } from '../dist/esm/resources/web-sockets/connectExplorer-ws-internal-base.js';
import { buildURL as buildExplorerAliasURL } from '../dist/esm/resources/web-sockets/connectExplorerAlias-ws-internal-base.js';

const baseURL = 'https://gateway.example.test';

const endpoints = [
  { name: 'priceFeed', build: buildPriceFeedURL, parameters: { market: 'BTC-USD' }, path: '/ws/BTC-USD' },
  { name: 'markets', build: buildMarketsURL, parameters: {}, path: '/ws/markets' },
  { name: 'explorer', build: buildExplorerURL, parameters: {}, path: '/ws/explorer' },
  { name: 'explorerAlias', build: buildExplorerAliasURL, parameters: {}, path: '/explorer' },
];

for (const endpoint of endpoints) {
  test(`${endpoint.name} handshake URL carries the wsToken as token`, () => {
    const client = new ChainflowDataGateway({ baseURL, restAPIKey: 'rest-key', wsToken: 'ws-token-value' });
    const url = endpoint.build(client, endpoint.parameters);
    assert.equal(url.protocol, 'wss:');
    assert.equal(url.pathname, endpoint.path);
    assert.equal(url.searchParams.get('token'), 'ws-token-value');
  });

  test(`${endpoint.name} handshake URL resolves a wsToken provider`, () => {
    const client = new ChainflowDataGateway({ baseURL, restAPIKey: 'rest-key', wsToken: () => 'provided-token' });
    const url = endpoint.build(client, endpoint.parameters);
    assert.equal(url.searchParams.get('token'), 'provided-token');
  });

  test(`${endpoint.name} handshake URL keeps defaultQuery alongside the token`, () => {
    const client = new ChainflowDataGateway({
      baseURL,
      restAPIKey: 'rest-key',
      wsToken: 'ws-token-value',
      defaultQuery: { region: 'eu' },
    });
    const url = endpoint.build(client, endpoint.parameters);
    assert.equal(url.searchParams.get('region'), 'eu');
    assert.equal(url.searchParams.get('token'), 'ws-token-value');
  });

  test(`${endpoint.name} handshake URL has no token when wsToken is unset`, () => {
    const client = new ChainflowDataGateway({ baseURL, restAPIKey: 'rest-key', wsToken: null });
    const url = endpoint.build(client, endpoint.parameters);
    assert.equal(url.pathname, endpoint.path);
    assert.equal(url.searchParams.has('token'), false);
  });
}
