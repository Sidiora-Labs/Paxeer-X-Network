import { readFileSync } from 'node:fs';
import { createServer, type IncomingMessage, type Server, type ServerResponse } from 'node:http';
import type { AddressInfo } from 'node:net';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import {
  JOURNEY_STATES,
  decodeJourney,
  decodeNetwork,
  readExplorerStatus,
  statusLadder,
  type ExplorerTransactionStatus,
  type JourneyState,
} from '../src/index.js';

const fixtureDir = new URL('./fixtures/endpoint/', import.meta.url);
const schemaDir = new URL('../../../schema/human-api/', import.meta.url);

const explorerStatuses = JSON.parse(readFileSync(new URL('explorer-status.json', fixtureDir), 'utf8')) as Record<
  string,
  ExplorerTransactionStatus
>;

let server: Server;
let base: string;

function handle(req: IncomingMessage, res: ServerResponse): void {
  const match = /^\/api\/v2\/transactions\/(0x[0-9a-f]{64})\/status$/.exec(req.url ?? '');
  const body = match ? explorerStatuses[match[1] as string] : undefined;
  if (req.method !== 'GET' || body === undefined) {
    res.writeHead(404, { 'content-type': 'application/json' });
    res.end(JSON.stringify({ message: 'Not found' }));
    return;
  }
  res.writeHead(200, { 'content-type': 'application/json' });
  res.end(JSON.stringify(body));
}

beforeAll(async () => {
  server = createServer(handle);
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  base = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
});

afterAll(async () => {
  await new Promise<void>((resolve, reject) => server.close((err) => (err ? reject(err) : resolve())));
});

function schemaJourneyStates(): string[] {
  const text = readFileSync(new URL('journeys.kvx', schemaDir), 'utf8');
  const block = text.split('[type.JourneyState]')[1] ?? '';
  const variants = /variants = (\[[^\]]*\])/.exec(block);
  return JSON.parse(variants?.[1] ?? '[]') as string[];
}

describe('statusLadder', () => {
  it('ladder_explorer_table_maps_every_explorer_rung', () => {
    expect(statusLadder.tables.explorer).toEqual({ pending: null, instant: 'instant', sealed: 'sealed', final: 'final' });
    for (const [state, rung] of Object.entries(statusLadder.tables.explorer)) {
      expect(statusLadder.fromExplorer(state as ExplorerTransactionStatus['rung'])).toEqual({ rung, source: 'explorer', state });
    }
  });

  it('ladder_journey_table_maps_every_journey_state_in_the_schema', () => {
    expect(Object.keys(statusLadder.tables.journey)).toEqual(schemaJourneyStates());
    expect(Object.keys(statusLadder.tables.journey)).toEqual([...JOURNEY_STATES]);
    expect(statusLadder.tables.journey).toEqual({
      'getting-ready': null,
      sending: null,
      processing: 'instant',
      done: 'sealed',
      'done-finalised': 'final',
      'still-checking': null,
      refused: null,
      'waiting-for-you': null,
    });
    for (const [state, rung] of Object.entries(statusLadder.tables.journey)) {
      expect(statusLadder.fromJourney(state as JourneyState)).toEqual({ rung, source: 'journey', state });
    }
  });

  it('ladder_anchor_table_maps_every_anchor_state_the_endpoint_reports', () => {
    const exchanges = JSON.parse(readFileSync(new URL('available.json', fixtureDir), 'utf8')) as {
      request: { method: string };
      response: { result: unknown };
    }[];
    const network = decodeNetwork(exchanges.find((entry) => entry.request.method === 'px_getNetwork')?.response.result);
    expect(Object.keys(statusLadder.tables.anchor)).toEqual(Object.values(network.anchor?.status_ladder ?? {}));
    expect(statusLadder.tables.anchor).toEqual({ unknown: null, submitted: 'sealed', final: 'final' });
    for (const [state, rung] of Object.entries(statusLadder.tables.anchor)) {
      expect(statusLadder.fromAnchor(state as 'unknown' | 'submitted' | 'final')).toEqual({ rung, source: 'anchor', state });
    }
    expect(statusLadder.fromAnchor(network.anchor?.status_name ?? 'unknown')).toEqual({ rung: 'final', source: 'anchor', state: 'final' });
  });

  it('ladder_reads_recorded_explorer_statuses_onto_the_ladder', async () => {
    const rungs: Record<string, string | null> = {};
    for (const hash of Object.keys(explorerStatuses)) {
      const status = await readExplorerStatus(base, hash);
      expect(status).toEqual(explorerStatuses[hash]);
      rungs[status.rung] = statusLadder.fromExplorer(status).rung;
    }
    expect(rungs).toEqual({ pending: null, instant: 'instant', sealed: 'sealed', final: 'final' });
    await expect(readExplorerStatus(base, `0x${'00'.repeat(32)}`)).rejects.toThrow('HTTP 404');
  });

  it('ladder_maps_the_golden_journey_and_its_stages', () => {
    const recorded = JSON.parse(readFileSync(new URL('golden/journey.get.response.json', schemaDir), 'utf8')) as {
      body: { result: unknown };
    };
    const journey = decodeJourney(recorded.body.result);
    expect(statusLadder.fromJourney(journey.state)).toEqual({ rung: 'instant', source: 'journey', state: 'processing' });
    const stages = journey.stages.map((stage) => statusLadder.fromJourney(stage.state));
    expect(stages.map((step) => step.rung)).toEqual(['sealed', 'sealed', 'instant']);
    expect(statusLadder.highest(stages)).toEqual({ rung: 'sealed', source: 'journey', state: 'done' });
  });

  it('ladder_highest_picks_the_top_rung_across_sources', () => {
    const steps = [
      statusLadder.fromJourney('processing'),
      statusLadder.fromExplorer('sealed'),
      statusLadder.fromAnchor('unknown'),
    ];
    expect(statusLadder.highest(steps)).toEqual({ rung: 'sealed', source: 'explorer', state: 'sealed' });
    expect(statusLadder.highest([statusLadder.fromJourney('refused')])).toEqual({ rung: null, source: 'journey', state: 'refused' });
    expect(statusLadder.highest([])).toBeNull();
    expect(statusLadder.rungs).toEqual(['instant', 'sealed', 'final']);
  });

  it('ladder_refuses_a_state_outside_its_tables', () => {
    expect(() => statusLadder.fromJourney('pending' as JourneyState)).toThrow(RangeError);
    expect(() => statusLadder.fromExplorer('confirmed' as ExplorerTransactionStatus['rung'])).toThrow(RangeError);
    expect(() => statusLadder.fromAnchor('sealed' as 'final')).toThrow(RangeError);
  });
});
