import { describe, expect, it } from 'vitest';
import {
  decideTransition,
  _internals,
} from '../src/jobs/fundedEvaluator.js';

/**
 * Pure-function tests for the funded evaluator's state machine.
 *
 * These are the rules that decide when an account breaches drawdown, hits
 * scale, or graduates to payout-eligible. Bugs here could lose user funds
 * or hand out payouts incorrectly, so every transition has explicit coverage.
 *
 * Tier under test mirrors the migration seed for starter_25k:
 *   - $25,000 starting
 *   - 15% daily DD  → daily breach at -$3,750 from daily start
 *   - 25% max DD    → max breach at -$6,250 from starting (= equity $18,750)
 *   - $40k peak     → scale_eligible
 *   - $50k peak     → payout_eligible
 */

const TIER = {
  max_daily_dd_bps: 1500,
  max_total_dd_bps: 2500,
  scale_threshold_usd: 40000,
  payout_threshold_usd: 50000,
};

const BASE_ACCOUNT = {
  status: 'active' as const,
  starting_value_usd: '25000.000000',
  peak_value_usd: '25000.000000',
  daily_start_value_usd: '25000.000000',
  daily_start_at: '2026-05-18T00:00:00.000Z', // earlier today UTC
};

// Same-day "now" (no rollover) for the active-trading scenarios.
const NOW = new Date('2026-05-18T12:00:00.000Z');

// -----------------------------------------------------------------------------
// Breach detection
// -----------------------------------------------------------------------------

describe('decideTransition — breach detection', () => {
  it('transitions to breached_max at exactly -25% from starting', () => {
    const d = decideTransition({
      account: BASE_ACCOUNT,
      tier: TIER,
      equity_usd: 18_750, // $25,000 × (1 - 0.25)
      now: NOW,
    });
    expect(d.status).toBe('breached_max');
    expect(d.breached_reason).toMatch(/maxDD/);
  });

  it('does NOT breach max at -24.99% (one cent above max threshold)', () => {
    // To isolate the MAX-DD edge case, pin the daily window so daily-DD
    // can't fire at this equity. With daily_start=$19,000 the daily
    // breach threshold is $16,150 — well below $18,750.01 — so any
    // transition we see here is unambiguously about max-DD.
    const d = decideTransition({
      account: { ...BASE_ACCOUNT, daily_start_value_usd: '19000.000000' },
      tier: TIER,
      equity_usd: 18_750.01,
      now: NOW,
    });
    expect(d.status).toBeUndefined();
  });

  it('transitions to breached_daily at exactly -15% from daily start', () => {
    const d = decideTransition({
      account: BASE_ACCOUNT,
      tier: TIER,
      equity_usd: 21_250, // $25,000 × (1 - 0.15)
      now: NOW,
    });
    expect(d.status).toBe('breached_daily');
    expect(d.breached_reason).toMatch(/dailyDD/);
  });

  it('uses the current daily_start as the daily DD baseline (not starting)', () => {
    // Account is up to $30k daily start. Today: a 15% drop from $30k = $25,500.
    // Equity $25_499 should breach DAILY even though it's still above starting.
    const d = decideTransition({
      account: { ...BASE_ACCOUNT, daily_start_value_usd: '30000.000000' },
      tier: TIER,
      equity_usd: 25_499,
      now: NOW,
    });
    expect(d.status).toBe('breached_daily');
  });

  it('max breach takes precedence over daily breach when both would fire', () => {
    // -50% from starting = $12_500. Both daily (-15%) and max (-25%) breached.
    const d = decideTransition({
      account: BASE_ACCOUNT,
      tier: TIER,
      equity_usd: 12_500,
      now: NOW,
    });
    expect(d.status).toBe('breached_max');
  });

  it('does NOT re-emit the same breached status on subsequent ticks', () => {
    const d = decideTransition({
      account: { ...BASE_ACCOUNT, status: 'breached_max' },
      tier: TIER,
      equity_usd: 12_500,
      now: NOW,
    });
    expect(d.status).toBeUndefined();
    expect(d.breached_reason).toBeUndefined();
  });
});

// -----------------------------------------------------------------------------
// Milestone transitions
// -----------------------------------------------------------------------------

describe('decideTransition — milestones', () => {
  it('marks scale_eligible at exactly $40k peak', () => {
    const d = decideTransition({
      account: { ...BASE_ACCOUNT, peak_value_usd: '40000.000000' },
      tier: TIER,
      equity_usd: 38_000,
      now: NOW,
    });
    expect(d.status).toBe('scale_eligible');
  });

  it('marks payout_eligible at exactly $50k peak (higher wins)', () => {
    const d = decideTransition({
      account: { ...BASE_ACCOUNT, peak_value_usd: '49000.000000' },
      tier: TIER,
      equity_usd: 50_500,
      now: NOW,
    });
    expect(d.status).toBe('payout_eligible');
    expect(d.peak_value_usd).toBe('50500.000000');
  });

  it('stays at scale_eligible after a temporary dip below $40k (high-watermark)', () => {
    const d = decideTransition({
      account: { ...BASE_ACCOUNT, status: 'scale_eligible', peak_value_usd: '42000.000000' },
      tier: TIER,
      equity_usd: 38_500,
      now: NOW,
    });
    expect(d.status).toBeUndefined(); // no transition: stays scale_eligible
  });

  it('advances from scale_eligible to payout_eligible on hitting $50k peak', () => {
    const d = decideTransition({
      account: { ...BASE_ACCOUNT, status: 'scale_eligible', peak_value_usd: '45000.000000' },
      tier: TIER,
      equity_usd: 51_000,
      now: NOW,
    });
    expect(d.status).toBe('payout_eligible');
  });

  it('does NOT downgrade payout_eligible to scale_eligible on a dip', () => {
    const d = decideTransition({
      account: { ...BASE_ACCOUNT, status: 'payout_eligible', peak_value_usd: '52000.000000' },
      tier: TIER,
      equity_usd: 41_000,
      now: NOW,
    });
    expect(d.status).toBeUndefined();
  });
});

// -----------------------------------------------------------------------------
// Daily rollover behaviour
// -----------------------------------------------------------------------------

describe('decideTransition — daily rollover', () => {
  it('snapshots daily_start_value_usd when crossing the UTC reset boundary', () => {
    const tomorrow = new Date('2026-05-19T00:00:00.500Z'); // 0.5s past midnight UTC
    const d = decideTransition({
      account: BASE_ACCOUNT,
      tier: TIER,
      equity_usd: 26_800,
      now: tomorrow,
    });
    expect(d.daily_start_value_usd).toBe('26800.000000');
    expect(d.daily_start_at).toBe(tomorrow.toISOString());
  });

  it('does NOT roll over within the same UTC day', () => {
    const sameDayLater = new Date('2026-05-18T23:59:00.000Z');
    const d = decideTransition({
      account: BASE_ACCOUNT,
      tier: TIER,
      equity_usd: 26_000,
      now: sameDayLater,
    });
    expect(d.daily_start_value_usd).toBeUndefined();
    expect(d.daily_start_at).toBeUndefined();
  });

  it('does not fire a daily breach on the same tick as a rollover', () => {
    // User had a big winning day ending at $30k, then immediately drops to
    // $20k after midnight. Old daily_start=$30k would say -33% breach. But
    // since we rolled the window, the NEW daily start is $20k.
    const tomorrow = new Date('2026-05-19T00:00:00.500Z');
    const d = decideTransition({
      account: { ...BASE_ACCOUNT, daily_start_value_usd: '30000.000000' },
      tier: TIER,
      equity_usd: 20_000,
      now: tomorrow,
    });
    // $20k is still -20% from STARTING ($25k) → max DD (25%) NOT yet hit.
    // Daily DD is fresh, so no daily breach either.
    expect(d.status).toBeUndefined();
    expect(d.daily_start_value_usd).toBe('20000.000000');
  });

  it('first-ever evaluation seeds daily_start (no daily_start_at yet)', () => {
    const d = decideTransition({
      account: { ...BASE_ACCOUNT, daily_start_at: null, daily_start_value_usd: null },
      tier: TIER,
      equity_usd: 25_000,
      now: NOW,
    });
    expect(d.daily_start_value_usd).toBe('25000.000000');
    expect(d.daily_start_at).toBe(NOW.toISOString());
  });
});

// -----------------------------------------------------------------------------
// Peak tracking
// -----------------------------------------------------------------------------

describe('decideTransition — peak tracking', () => {
  it('updates peak_value_usd when equity exceeds it', () => {
    const d = decideTransition({
      account: { ...BASE_ACCOUNT, peak_value_usd: '30000.000000' },
      tier: TIER,
      equity_usd: 35_000,
      now: NOW,
    });
    expect(d.peak_value_usd).toBe('35000.000000');
  });

  it('does NOT update peak when equity is below it', () => {
    const d = decideTransition({
      account: { ...BASE_ACCOUNT, peak_value_usd: '35000.000000' },
      tier: TIER,
      equity_usd: 30_000,
      now: NOW,
    });
    expect(d.peak_value_usd).toBeUndefined();
  });
});

// -----------------------------------------------------------------------------
// Pure helpers
// -----------------------------------------------------------------------------

describe('windowIndex / crossedUtcDayBoundary', () => {
  const { windowIndex, crossedUtcDayBoundary } = _internals;

  it('rolls when crossing UTC midnight (resetHour=0)', () => {
    const before = new Date('2026-05-18T23:59:59.999Z');
    const after = new Date('2026-05-19T00:00:00.000Z');
    expect(windowIndex(before, 0)).toBeLessThan(windowIndex(after, 0));
    expect(crossedUtcDayBoundary(before, after)).toBe(true);
  });

  it('does not roll within the same day', () => {
    const a = new Date('2026-05-18T01:00:00.000Z');
    const b = new Date('2026-05-18T23:00:00.000Z');
    expect(crossedUtcDayBoundary(a, b)).toBe(false);
  });

  it('respects a custom resetHour (e.g. resetHour=4 → 4am UTC)', () => {
    const before = new Date('2026-05-19T03:59:00.000Z');
    const after = new Date('2026-05-19T04:00:00.001Z');
    expect(windowIndex(before, 4)).toBeLessThan(windowIndex(after, 4));
  });
});

describe('unitsToUsdNumber / weiToUsdNumber', () => {
  const { unitsToUsdNumber, weiToUsdNumber } = _internals;

  it('converts 25,000 USDL (6dp) to $25,000.00', () => {
    expect(unitsToUsdNumber(25_000_000000n, 6)).toBe(25_000);
  });

  it('handles fractional USDL units', () => {
    expect(unitsToUsdNumber(123_456789n, 6)).toBeCloseTo(123.456789, 6);
  });

  it('converts 15 PAX wei at $13.17 to ~$197.55', () => {
    const fifteenPax = 15_000_000_000_000_000_000n;
    expect(weiToUsdNumber(fifteenPax, 18, 13.17)).toBeCloseTo(15 * 13.17, 4);
  });
});
