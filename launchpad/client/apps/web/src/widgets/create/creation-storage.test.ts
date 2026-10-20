import { describe, expect, it } from 'vitest';
import { review, selectOptical, verifyCost } from './creation-domain';
import { checkedAttempt, type CreationDraftV2, type LaunchAttempt } from './creation-storage';

const a = (n: number) => '0x' + n.toString(16).padStart(40, '0');
const h = (n: number) => '0x' + n.toString(16).padStart(64, '0');
const creator = a(4);
const day = 86400000;

function attempt(updatedAt: number, intentId: string, state: LaunchAttempt['state']): LaunchAttempt {
  const draft: CreationDraftV2 = { draftId: `d-${intentId}`, version: 2, creator, chainId: 125, name: 'Token', symbol: 'TKN', description: '', tags: [], socialLinks: [], uploadRefs: [], firstBuyRaw: '0', opticalSelection: 'standard', updatedAt, feeStrategy: 0 };
  const cost = verifyCost({ revision: 'r1', chainId: 125, usdl: a(1), router: a(2), factory: a(3) }, { revision: 'r1', decimals: 6, feeRaw: '1000000', gasRaw: '10' });
  const reviewed = review(intentId, draft, cost, selectOptical('standard', 'r1', []), updatedAt);
  const resolved = state === 'resolved' ? { createTxHash: h(1), poolAddress: a(5), tokenAddress: a(6) } : {};
  return { intentId, creator, chainId: 125, draftId: draft.draftId, reviewedSnapshot: reviewed.snapshot, draft, cost, deploymentRevision: 'r1', createdAt: updatedAt, state, metadataStatus: 'metadata_pending', metadataAttemptCount: 0, purchaseState: 'not_requested', ...resolved };
}

describe('saved launch attempts', () => {
  it('keep a completed attempt older than the draft window readable while an expired intent cannot enter signing', () => {
    const aged = attempt(Date.now() - 30 * day - 1, 'old', 'resolved');
    expect(checkedAttempt(aged, creator)).toBe(aged);
    const fresh = attempt(Date.now(), 'new', 'signing_locked');
    expect(checkedAttempt(fresh, creator, true)).toBe(fresh);
    expect(() => checkedAttempt(attempt(Date.now() - 30 * day - 1, 'stale', 'signing_locked'), creator, true)).toThrow('Saved draft expired or is invalid.');
  });
});
