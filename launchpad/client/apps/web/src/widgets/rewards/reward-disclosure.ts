import { exactAmount } from '../profile/account-disclosure';
import { showsTechnicalDetail, userFacingMessage } from '@/core/user-facing-error';
/**
 * Frozen capabilities.json and contracts/periphery/FeesRouter.sol source inventory.
 * `authority`, `settlement`, `conditions` and `blocker` are developer detail; people see
 * `title`, `summary` and the plain status from rewardProgramView.
 */
export const rewardPrograms = [
  { type: 'creator-fees', title: 'Creator fees', summary: "A share of trading fees on coins you created, paid in USDL", authority: 'FeesRouter NFT current owner and strategy', settlement: 'FeesRouter.claimFees(nftId)', denomination: 'USDL6', conditions: 'Current owner and strategy eligibility read on chain125; authentic successful receipt', blocker: 'Entitlement API, approved deployment and wallet lane integration not qualified' },
  { type: 'airdrop', title: 'Airdrops', summary: "USDL sent to holders when a creator funds an airdrop", authority: 'FeesRouter funded airdrop strategy', settlement: 'Authorized FeesRouter strategy distribution', denomination: 'USDL6', conditions: 'Verified strategy allocation and recipient eligibility', blocker: 'No qualified per-recipient entitlement/distribution journey' },
  { type: 'referral', title: 'Referrals', summary: "Rewards when people you invite start trading. Clicks alone don't count", authority: 'Server qualified-event ledger required', settlement: null, denomination: null, conditions: 'Server-issued attribution, no self-referrals; qualified actions only', blocker: 'Funded settlement authority unavailable; clicks do not imply earnings' },
  { type: 'bounty', title: 'Bounties', summary: "Payouts for winning a bounty", authority: 'Authorized funded program ledger required', settlement: null, denomination: null, conditions: 'Program-specific eligibility revision required', blocker: 'Funded settlement authority unavailable' },
  { type: 'cup', title: 'Cup prizes', summary: "Prizes for placing in a trading cup", authority: 'Cup results ledger (IF_CUP_RESULTS prize ladder) written into the reward authority', settlement: null, denomination: 'USDL6', conditions: 'Frozen final standing inside the cup window; eligibility rules of that cup', blocker: 'No service serves /v2/cups/*; no cup program is produced by the reward ledger yet' },
  { type: 'other', title: 'Other programs', summary: "More programs show up here when they launch", authority: 'Program-specific authority required', settlement: null, denomination: null, conditions: 'No inference from activity counts', blocker: 'Unregistered reward program unavailable' },
] as const;
export type EntitlementState = 'unavailable' | 'claimable' | 'pending' | 'claimed' | 'expired' | 'ineligible';
function entitlementAmount(amount: bigint): bigint {
  if (amount < 0n || amount >= (1n << 256n)) throw new Error('Invalid uint256 entitlement');
  return amount;
}
export function rewardAmount(amount: bigint | null): string { return exactAmount(amount === null ? null : entitlementAmount(amount).toString()); }
export function claimableSubtotal(amounts: readonly (bigint | null)[]) {
  if (amounts.length > 100) throw new Error('Reward page exceeds frozen100-item bound');
  return { raw: amounts.reduce<bigint>((sum, value) => sum + (value === null ? 0n : entitlementAmount(value)), 0n), coverage: amounts.some(value => value === null) ? 'partial' as const : 'complete' as const };
}

export type RewardProgram = typeof rewardPrograms[number];
/** Machine status every listed program carries until its settlement authority is served; userFacingMessage turns it into plain words. */
const programUnavailable = 'NOT_SERVED';
export interface RewardProgramView { type: string; title: string; summary: string; status: string; technical: string | null }
/**
 * What the rewards disclosure shows for a program. Status text goes through
 * userFacingMessage; the authority, settlement, conditions and blocker (backend
 * detail) are returned as `technical` only outside production builds, for a
 * title / data attribute, never body text.
 */
export function rewardProgramView(program: RewardProgram, env: string | undefined = process.env.NODE_ENV): RewardProgramView {
  const technical = showsTechnicalDetail(env) ? [`authority: ${program.authority}`, `settlement: ${program.settlement ?? 'none'}`, `conditions: ${program.conditions}`, `blocker: ${program.blocker}`].join(' · ') : null;
  return { type: program.type, title: program.title, summary: program.summary, status: userFacingMessage(programUnavailable), technical };
}
