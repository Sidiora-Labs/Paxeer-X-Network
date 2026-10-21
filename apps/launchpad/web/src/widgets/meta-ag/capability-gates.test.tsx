import { readFileSync,appendFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { createHash } from 'node:crypto';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { expect, test } from 'vitest';
import BestRouteSwapPanel from './best-route-swap';
import OrderForm from './order-form';
import OpenOrdersList from './open-orders-list';
import LiquidityPosition from './liquidity-position';

// Rendering without wallet/query providers proves unavailable capabilities never
// mount their legacy read or transaction hooks. No service or hook is mocked.
for (const [name, Component] of [
  ['liquidity', LiquidityPosition],
  ['routing', BestRouteSwapPanel],
  ['placing orders', OrderForm],
  ['cancelling orders', OpenOrdersList],
] as const) {
  test(`unqualified ${name} renders unavailable without mounting wallet consumers`, () => {
    const html = renderToStaticMarkup(<Component />);
    expect(html).toContain('role="status"');
    expect(html).toContain('not available yet');
    expect(html).not.toContain('<button');
    expect(html).not.toContain('<input');
    if (name === 'liquidity' && process.env.L28_OBSERVATION_DIRECTORY && process.env.L28_SOURCE_MANIFEST_JSON && process.env.L28_CHAIN_FIXTURE_JSON) {
      const sourceRaw = readFileSync(process.env.L28_SOURCE_MANIFEST_JSON,'utf8'), source = JSON.parse(sourceRaw), fixture = JSON.parse(readFileSync(process.env.L28_CHAIN_FIXTURE_JSON,'utf8')), manifest = JSON.parse(readFileSync(fixture.manifestFile,'utf8'));
      appendFileSync(resolve(process.env.L28_OBSERVATION_DIRECTORY,'scenarios.jsonl'),JSON.stringify({ type: 'scenario',scenario: 'scenario_7',runId: source.runId,manifestSha256: fixture.manifestSha256,sourceManifestSha256: createHash('sha256').update(sourceRaw).digest('hex'),deploymentRevision: manifest.revision,chainId: 125,observedAtUnixMs: Date.now(),raw: { html,withdrawControls: 0,ownershipInterface: 'unsupported',actualOwnershipAmount: null,browserQualification: false } })+'\n',{ mode: 0o600 });
    }
  });
}
