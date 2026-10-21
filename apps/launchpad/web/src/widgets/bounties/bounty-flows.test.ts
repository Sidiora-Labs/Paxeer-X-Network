import { describe, expect, it } from 'vitest';
import { bountiesContract, type Bounty } from './bounties-contract';
import { evidenceFileProblem, parseDecimalAmount, validateAccepted, validateCreated, type BountyAcceptResponse, type BountyCreateBody, type BountyCreateResponse } from './use-bounty-mutations';
import { validateSubmissions, type BountySubmission, type BountySubmissionsResponse } from './use-bounty';

const creator = '0x1111111111111111111111111111111111111111';
const author = '0x5555555555555555555555555555555555555555';
const pool = '0x2222222222222222222222222222222222222222';

function envelope<T>(data: T) {
  return { data, generatedAt: '2026-10-12T12:00:00Z', nextCursor: null, quality: 'complete' as const, requestId: 'r-1', revision: '3', watermark: null };
}

const body: BountyCreateBody = { title: 'Make a chart', description: 'Post a chart of the pool', assetAddress: bountiesContract.usdlAddress, budgetRaw: '25000000', poolAddress: pool };
function bounty(overrides: Partial<Bounty> = {}): Bounty {
  return { bountyId: 'b-1', title: body.title, description: body.description, creatorWallet: creator, assetAddress: body.assetAddress, budgetRaw: body.budgetRaw, poolAddress: pool, state: 'funding', ...overrides };
}
function submission(overrides: Partial<BountySubmission> = {}): BountySubmission {
  return { submissionId: 's-1', bountyId: 'b-1', authorWallet: author, body: 'Posted', evidenceUrls: ['https://cdn.example/a.png'], state: 'submitted', awardRaw: null, ...overrides };
}

describe('parseDecimalAmount', () => {
  it('converts plain decimals into raw units', () => {
    expect(parseDecimalAmount('25', 6)).toBe(25_000_000n);
    expect(parseDecimalAmount(' 0.000001 ', 6)).toBe(1n);
    expect(parseDecimalAmount('1.5', 18)).toBe(1_500_000_000_000_000_000n);
  });
  it('rejects zero, too many decimals and non-decimal text', () => {
    for (const text of ['0', '0.0', '1.0000001', '-1', '1e6', '', '.5', '01', 'abc']) expect(parseDecimalAmount(text, 6)).toBeNull();
    expect(parseDecimalAmount('1', -1)).toBeNull();
  });
});

describe('evidenceFileProblem', () => {
  it('follows the gateway asset rules', () => {
    expect(evidenceFileProblem({ type: 'image/png', size: 2 * 1024 * 1024 })).toBeNull();
    expect(evidenceFileProblem({ type: 'image/gif', size: 10 })).not.toBeNull();
    expect(evidenceFileProblem({ type: 'image/webp', size: 0 })).not.toBeNull();
    expect(evidenceFileProblem({ type: 'image/jpeg', size: 6 * 1024 * 1024 + 1 })).not.toBeNull();
  });
});

describe('validateCreated', () => {
  const created = (overrides: Partial<Bounty> = {}): BountyCreateResponse => envelope({ bounty: bounty(overrides), fundingTransactionRequest: null });
  it('accepts the requested bounty owned by the signed-in wallet', () => {
    const response = created();
    expect(validateCreated(response, body, creator.toUpperCase().replace('0X', '0x'))).toBe(response);
    const unlinked = { ...body, poolAddress: undefined };
    delete unlinked.poolAddress;
    expect(validateCreated(created({ poolAddress: null }), unlinked, creator).data.bounty.poolAddress).toBeNull();
  });
  it('rejects another owner, a changed budget or pool, and quarantined answers', () => {
    expect(() => validateCreated(created(), body, author)).toThrow();
    expect(() => validateCreated(created({ budgetRaw: '1' }), body, creator)).toThrow();
    expect(() => validateCreated(created({ poolAddress: null }), body, creator)).toThrow();
    expect(() => validateCreated({ ...created(), quality: 'quarantined' }, body, creator)).toThrow();
  });
});

describe('validateAccepted', () => {
  const accepted = (overrides: Partial<BountySubmission> = {}): BountyAcceptResponse => envelope({ submission: submission({ state: 'accepted', awardRaw: '5000000', ...overrides }), settlementRequest: null });
  it('accepts the matching accepted submission', () => {
    expect(validateAccepted(accepted(), 'b-1', 's-1', '5000000').data.submission.state).toBe('accepted');
  });
  it('rejects a different submission, award or state', () => {
    expect(() => validateAccepted(accepted(), 'b-1', 's-2', '5000000')).toThrow();
    expect(() => validateAccepted(accepted(), 'b-1', 's-1', '1')).toThrow();
    expect(() => validateAccepted(accepted({ state: 'submitted' }), 'b-1', 's-1', '5000000')).toThrow();
    expect(() => validateAccepted(accepted(), 'b-2', 's-1', '5000000')).toThrow();
  });
});

describe('validateSubmissions', () => {
  const list = (items: BountySubmission[]): BountySubmissionsResponse => envelope({ items });
  it('accepts unique submissions of the bounty', () => {
    const response = list([submission(), submission({ submissionId: 's-2' })]);
    expect(validateSubmissions(response, 'b-1')).toBe(response);
  });
  it('rejects duplicates, foreign bounties and malformed rows', () => {
    expect(() => validateSubmissions(list([submission(), submission()]), 'b-1')).toThrow();
    expect(() => validateSubmissions(list([submission({ bountyId: 'b-9' })]), 'b-1')).toThrow();
    expect(() => validateSubmissions(list([submission({ authorWallet: 'x' })]), 'b-1')).toThrow();
    expect(() => validateSubmissions(list([submission({ awardRaw: '1.5' })]), 'b-1')).toThrow();
  });
});
