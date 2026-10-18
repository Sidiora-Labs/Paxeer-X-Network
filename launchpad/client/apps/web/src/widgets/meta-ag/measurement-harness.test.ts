import { expect, test } from 'vitest';
import { percentile, summarizeOperations, validateWorkload, assertActualPage, type MeasurementIdentity, type OperationObservation, type WorkloadParameters } from './measurement-harness';
const owner = '0x1111111111111111111111111111111111111111' as const;
const other = '0x2222222222222222222222222222222222222222' as const;
const identity: MeasurementIdentity = { runId: '00000000-0000-4000-8000-000000000001', manifestSha256: 'a'.repeat(64), sourceManifestSha256: 'b'.repeat(64), deploymentRevision: 'arithmetic-test', chainId: 125 };
const parameters: WorkloadParameters = { profile: 'peak', durationMs: 1000, offeredRatePerSecond: 100, maxInFlight: 4, maximumSchedulerLagMs: 10, operationTimeoutMs: 1000, weights: { capabilities: 1,pagination: 1,eligibility: 1,quotes: 1,cancels: 1,approvals: 1,accountSwitches: 1,customRoutes: 1,positions: 1 } };
const sample: OperationObservation = { ...identity,type: 'operation',sequence: 0,kind: 'quotes',account: owner,plannedAtMs: 100,startedAtMs: 150,finishedAtMs: 200,outcome: 'verified',raw: { arithmeticInputOnly: true } };

test('nearest rank measured quantiles retain tail and exclude no data', () => {
 expect(percentile([], .95)).toBeNull(); expect(percentile([100,250,750], .95)).toBe(750); expect(percentile([100,250,750], .99)).toBe(750);
 expect(() => percentile([NaN], .95)).toThrow(); expect(() => percentile([-1], .95)).toThrow(); expect(() => percentile([1],0)).toThrow();
});
test('offered timing includes scheduler delay and rejected requests never count as verified throughput', () => {
 const result = summarizeOperations([sample,{ ...sample,sequence: 1,outcome: 'unknown' },{ ...sample,sequence: 2,startedAtMs: null,outcome: 'dropped' }],identity,1000);
 const quote = result.operations.quotes;
 expect(quote.offered).toBe(3); expect(quote.admitted).toBe(2); expect(quote.verified).toBe(1); expect(quote.unknown).toBe(1); expect(quote.dropped).toBe(1);
 expect(quote.p95Ms).toBe(100); expect(quote.serviceMs).toEqual([50,50]); expect(quote.verifiedPerSecond).toBe(1); expect(result.globalCapacityAccepted).toBe(false);
});
test('foreign source, duplicate request and impossible timings are rejected', () => {
 expect(() => summarizeOperations([],identity,1000)).toThrow(); expect(() => summarizeOperations([sample,sample],identity,1000)).toThrow('Duplicate');
 for (const change of [{ sourceManifestSha256: 'c'.repeat(64) },{ manifestSha256: 'c'.repeat(64) },{ startedAtMs: 201 },{ finishedAtMs: 99 },{ startedAtMs: null },{ chainId: 1 }]) expect(() => summarizeOperations([{ ...sample,...change } as OperationObservation],identity,1000)).toThrow();
});
test('lane driver admits no absent account workload, fabricated budget or partial mix', () => {
 validateWorkload(parameters);
 for (const change of [{ offeredRatePerSecond: 20001 },{ maxInFlight: 513 },{ durationMs: 600001 },{ operationTimeoutMs: 60001 },{ offeredRatePerSecond: NaN },{ weights: { ...parameters.weights,quotes: 0 } }]) expect(() => validateWorkload({ ...parameters,...change })).toThrow();
});
test('owner-specific actual pagination cannot leak another account or exceed100', () => {
 assertActualPage({ items: [{ owner }] },owner,25);
 expect(() => assertActualPage({ items: [{ owner: other }] },owner,25)).toThrow('ownership');
 expect(() => assertActualPage({ items: [{ owner },{ owner }] },owner,1)).toThrow('bound');
 expect(() => assertActualPage({ items: [] },owner,101)).toThrow();
});
