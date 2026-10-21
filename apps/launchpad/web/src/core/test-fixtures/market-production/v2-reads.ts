import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { readFileSync } from 'node:fs';

/**
 * Recorded core/api v2 read responses (`v2Reads` in core-api-wire.json): the
 * exact status, headers and body the production handlers in
 * core/api/internal/rest (rest.RegisterProductionReads) wrote, captured with
 * `go test -overlay` against the container-free Postgres/Redis fixture. See the
 * fixture's `v2Reads.provenance`.
 */
export interface RecordedRead {
  method: 'GET' | 'POST';
  path: string;
  requestBody?: unknown;
  status: number;
  headers: Record<string, string>;
  body: unknown;
}

export type RecordedReadName =
  | 'rankingsTrending' | 'rankingsNew' | 'rankingsVolume'
  | 'statsBatch' | 'statsBatchReal' | 'statsBatchUnavailable'
  | 'poolDetailReal' | 'poolDetailFirst' | 'poolDetailNotFound' | 'poolDetailUnavailable'
  | 'poolListNew' | 'poolListTrending' | 'platform24h' | 'platform7d';

interface V2Reads {
  pools: { real: string; realToken: string; example: string; stateless: string; unknown: string };
  responses: Record<RecordedReadName, RecordedRead>;
}

const wire = JSON.parse(readFileSync(new URL('./core-api-wire.json', import.meta.url), 'utf8')) as { stats: RecordedRead; v2Reads: V2Reads };

export const v2Reads: V2Reads = wire.v2Reads;
/** The recorded IF_STATS response for the real pool (`stats` in core-api-wire.json). */
export const recordedStats: RecordedRead = { ...wire.stats, method: 'GET' };

const sortedQuery = (url: URL) => [...url.searchParams.entries()].sort(([a], [b]) => a.localeCompare(b)).map(([key, value]) => `${key}=${value}`).join('&');

/** True when a request is the one the entry was recorded for (method, path, query in any order, JSON body). */
export function sameRead(entry: RecordedRead, method: string, url: URL, body: string): boolean {
  const recorded = new URL(entry.path, 'http://recorded.invalid');
  if (entry.method !== method || recorded.pathname !== url.pathname || sortedQuery(recorded) !== sortedQuery(url)) return false;
  if (entry.requestBody === undefined) return true;
  try { return JSON.stringify(JSON.parse(body)) === JSON.stringify(entry.requestBody); } catch { return false; }
}

export type ReplayFallback = (method: string, url: URL, body: string) => { status: number; body?: unknown } | undefined;

export interface Replay { origin: string; requests: string[]; server: Server; close(): Promise<void> }

/**
 * A local http server that answers each recorded request with its recorded
 * status, headers and body; anything else goes to `fallback`, then a bare 404.
 * `override`, when it answers, replaces a recorded entry (failure-path tests).
 */
export async function serveRecorded(entries: readonly RecordedRead[], fallback?: ReplayFallback, override?: ReplayFallback): Promise<Replay> {
  const requests: string[] = [];
  const server = createServer((request, response) => {
    const chunks: Buffer[] = [];
    request.on('data', chunk => chunks.push(chunk as Buffer));
    request.on('end', () => {
      const url = new URL(request.url ?? '/', 'http://replay.invalid');
      const method = request.method ?? 'GET';
      const body = Buffer.concat(chunks).toString('utf8');
      requests.push(`${method} ${url.pathname}${url.search}`);
      response.setHeader('Access-Control-Allow-Origin', '*');
      const replaced = override?.(method, url, body);
      const recorded = replaced ? undefined : entries.find(entry => sameRead(entry, method, url, body));
      if (recorded) {
        response.writeHead(recorded.status, recorded.headers);
        response.end(JSON.stringify(recorded.body));
        return;
      }
      const answer = replaced ?? fallback?.(method, url, body);
      if (!answer) { response.writeHead(404); response.end(); return; }
      response.statusCode = answer.status;
      if (answer.body === undefined) { response.end(); return; }
      response.setHeader('Content-Type', 'application/json');
      response.end(JSON.stringify(answer.body));
    });
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  const origin = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  return { origin, requests, server, close: () => new Promise<void>(resolve => server.close(() => resolve())) };
}
