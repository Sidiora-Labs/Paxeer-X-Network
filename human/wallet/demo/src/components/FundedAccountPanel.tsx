'use client';

import { useState } from 'react';
import type { PaxeerWallet } from '@paxeer/wallet';
import { PaxeerWalletError } from '@paxeer/wallet';
import type {
  FundedAccountStatus,
  FundedSelfResponse,
  FundedDenyCode,
  FundedDenyDetail,
} from '@paxeer/wallet';
import { useFundedAccount } from '@paxeer/wallet/react';
import { truncateAddress } from '@/lib/format';

/**
 * FundedAccountPanel — full funded-account lifecycle UI rendered inside the
 * WalletModal. Three internal states:
 *
 *   1. Loading / signed-out / API error      → spinner / error card
 *   2. No funded account yet                  → ProvisionView (CTA)
 *   3. Funded account exists                  → OverviewView + Lifecycle tests
 *
 * The hook polls `/v1/funded/me` every 5s while this panel is mounted so the
 * UI tracks the evaluator (which ticks every 10s server-side). After every
 * lifecycle test we call `refresh()` directly to skip the poll wait.
 */

interface FundedAccountPanelProps {
  paxeer: PaxeerWallet;
  explorerUrl?: string | null;
  onBack: () => void;
}

type SubView = 'overview' | 'tests';

export function FundedAccountPanel({ paxeer, explorerUrl, onBack }: FundedAccountPanelProps) {
  const { data, loading, error, refresh } = useFundedAccount(paxeer, { pollMs: 5000 });
  const [subView, setSubView] = useState<SubView>('overview');

  if (loading && !data) {
    return <Loading label="Loading funded account…" onBack={onBack} />;
  }
  if (error && !data) {
    return <ErrorCard message={error.message} onBack={onBack} onRetry={refresh} />;
  }

  if (!data) {
    return <ProvisionView paxeer={paxeer} onProvisioned={refresh} onBack={onBack} />;
  }

  if (subView === 'tests') {
    return (
      <LifecycleTestPanel
        paxeer={paxeer}
        self={data}
        explorerUrl={explorerUrl ?? null}
        onResult={refresh}
        onBack={() => setSubView('overview')}
      />
    );
  }

  return (
    <OverviewView
      self={data}
      explorerUrl={explorerUrl ?? null}
      onBack={onBack}
      onRunTests={() => setSubView('tests')}
    />
  );
}

/* -------------------------------------------------------------------------- */
/* Provision view — when the user has no funded account yet                    */
/* -------------------------------------------------------------------------- */

function ProvisionView({
  paxeer,
  onProvisioned,
  onBack,
}: {
  paxeer: PaxeerWallet;
  onProvisioned: () => void;
  onBack: () => void;
}) {
  const [status, setStatus] = useState<
    | { kind: 'idle' }
    | { kind: 'provisioning' }
    | { kind: 'error'; message: string; code: string }
  >({ kind: 'idle' });

  async function provision() {
    setStatus({ kind: 'provisioning' });
    try {
      await paxeer.provisionFundedAccount('starter_25k');
      onProvisioned();
    } catch (err) {
      const e = err as PaxeerWalletError | Error;
      setStatus({
        kind: 'error',
        message: e.message,
        code: 'code' in e ? (e.code as string) : 'unknown',
      });
    }
  }

  return (
    <div className="flex flex-col gap-4 px-6 py-6">
      <BackButton onBack={onBack} />

      <div className="rounded-xl border border-[#004ced]/30 bg-[#004ced]/5 px-4 py-4">
        <p className="text-[12px] uppercase tracking-wider text-[#8FA8FF]">
          Funded · Starter $25K
        </p>
        <h3 className="mt-2 text-[20px] tracking-[-0.01em] text-neutral-100">
          Get $25,000 USDL of trading capital
        </h3>
        <p className="mt-2 text-[13px] leading-[1.55] text-neutral-400">
          A new server-custodied wallet, pre-funded with{' '}
          <span className="text-neutral-100">25,000 USDL</span> + 15 PAX gas, gated to the
          PECOR perps stack. 15% daily DD · 25% max DD · $40k → scale · $50k → payout.
        </p>
      </div>

      <ul className="flex flex-col gap-2 text-[13px] text-neutral-400">
        <Bullet>Funded by treasury — no upfront cost.</Bullet>
        <Bullet>Trade only against the whitelisted PECOR contracts.</Bullet>
        <Bullet>Drawdown enforced server-side every 10 s.</Bullet>
        <Bullet>Hit $40k peak → eligible to scale; $50k → payout.</Bullet>
      </ul>

      {status.kind === 'error' && (
        <div className="rounded-lg border border-[#ff5a65]/30 bg-[#ff5a65]/5 px-3 py-2 text-[13px] text-[#ff5a65]">
          <span className="font-mono">{status.code}</span> · {status.message}
        </div>
      )}

      <button
        type="button"
        onClick={provision}
        disabled={status.kind === 'provisioning'}
        className="
          mt-2 flex h-12 items-center justify-center rounded-xl
          bg-[#004ced] text-[15px] text-white
          transition-[background,opacity] duration-[var(--duration-snappy)]
          ease-[var(--ease-standard)]
          hover:bg-[#0040c9]
          disabled:cursor-not-allowed disabled:opacity-60
        "
      >
        {status.kind === 'provisioning'
          ? 'Provisioning + funding (~5 s)…'
          : 'Provision Starter $25K'}
      </button>

      <p className="text-center text-[11px] text-neutral-500">
        Two on-chain transfers: 25,000 USDL + 15 PAX from the Paxeer treasury.
      </p>
    </div>
  );
}

/* -------------------------------------------------------------------------- */
/* Overview — funded account exists                                            */
/* -------------------------------------------------------------------------- */

function OverviewView({
  self,
  explorerUrl,
  onBack,
  onRunTests,
}: {
  self: FundedSelfResponse;
  explorerUrl: string | null;
  onBack: () => void;
  onRunTests: () => void;
}) {
  const a = self.funded_account;
  const tier = self.tier;

  const equityUsd = parseFloat(a.current_value_usd ?? a.starting_value_usd);
  const startingUsd = parseFloat(a.starting_value_usd);
  const peakUsd = parseFloat(a.peak_value_usd);
  const dailyStartUsd = parseFloat(a.daily_start_value_usd ?? a.starting_value_usd);

  // Drawdown headroom in USD (clamped to 0 from below).
  const dailyDdLimitUsd = tier ? (dailyStartUsd * tier.max_daily_dd_bps) / 10_000 : 0;
  const dailyDdRemaining = Math.max(0, equityUsd - (dailyStartUsd - dailyDdLimitUsd));
  const dailyDdPct = Math.max(0, Math.min(100, (dailyDdRemaining / dailyDdLimitUsd) * 100));

  const maxDdLimitUsd = tier ? (startingUsd * tier.max_total_dd_bps) / 10_000 : 0;
  const maxDdRemaining = Math.max(0, equityUsd - (startingUsd - maxDdLimitUsd));
  const maxDdPct = Math.max(0, Math.min(100, (maxDdRemaining / maxDdLimitUsd) * 100));

  // Distance to the next milestone.
  const scaleTarget = tier ? parseFloat(tier.scale_threshold_usd) : 0;
  const payoutTarget = tier ? parseFloat(tier.payout_threshold_usd) : 0;
  const scaleDistance = Math.max(0, scaleTarget - peakUsd);
  const payoutDistance = Math.max(0, payoutTarget - peakUsd);

  const balances = self.balances;

  return (
    <div className="flex flex-col gap-4 px-6 py-5">
      <div className="flex items-center justify-between">
        <BackButton onBack={onBack} />
        <StatusBadge status={a.status} />
      </div>

      {/* Equity headline */}
      <div className="rounded-xl border border-neutral-700 bg-[#050505] px-4 py-4">
        <p className="text-[11px] uppercase tracking-wider text-neutral-500">Equity</p>
        <p className="mt-1 font-mono text-[28px] tracking-[-0.01em] text-neutral-100">
          {fmtUsd(equityUsd)}
        </p>
        <div className="mt-1 flex flex-wrap items-baseline gap-x-4 gap-y-1 text-[11px] text-neutral-500">
          <span>Peak {fmtUsd(peakUsd)}</span>
          <span>·</span>
          <span>Daily start {fmtUsd(dailyStartUsd)}</span>
          <span>·</span>
          <span>Started {fmtUsd(startingUsd)}</span>
        </div>
      </div>

      {/* Drawdown headroom */}
      {tier && (
        <div className="flex flex-col gap-3 rounded-xl border border-neutral-700 bg-neutral-800 px-4 py-4">
          <Gauge
            label={`Daily DD headroom · ${(tier.max_daily_dd_bps / 100).toFixed(0)}%`}
            valueUsd={dailyDdRemaining}
            pct={dailyDdPct}
            tone="warn"
          />
          <Gauge
            label={`Max DD headroom · ${(tier.max_total_dd_bps / 100).toFixed(0)}%`}
            valueUsd={maxDdRemaining}
            pct={maxDdPct}
            tone="danger"
          />
        </div>
      )}

      {/* Milestones */}
      {tier && a.status !== 'breached_daily' && a.status !== 'breached_max' && (
        <div className="flex items-stretch gap-2 text-[12px]">
          <Milestone
            label="To scale"
            target={scaleTarget}
            distance={scaleDistance}
            reached={a.status === 'scale_eligible' || a.status === 'payout_eligible'}
          />
          <Milestone
            label="To payout"
            target={payoutTarget}
            distance={payoutDistance}
            reached={a.status === 'payout_eligible'}
          />
        </div>
      )}

      {/* Address + balances */}
      <div className="rounded-xl border border-neutral-700 bg-neutral-800 px-4 py-3 text-[12px]">
        <div className="flex items-center justify-between">
          <span className="text-neutral-500">Funded address</span>
          <span className="font-mono text-neutral-200">{truncateAddress(self.wallet.address)}</span>
        </div>
        <div className="mt-1.5 flex items-center justify-between">
          <span className="text-neutral-500">USDL balance</span>
          <span className="font-mono text-neutral-200">
            {fmtUnits(balances.usdl_units, balances.usdl_decimals)} USDL
          </span>
        </div>
        <div className="mt-1.5 flex items-center justify-between">
          <span className="text-neutral-500">PAX gas</span>
          <span className="font-mono text-neutral-200">{fmtWei(balances.pax_wei, 4)} PAX</span>
        </div>
        {explorerUrl && (
          <a
            href={`${explorerUrl.replace(/\/$/, '')}/address/${self.wallet.address}`}
            target="_blank"
            rel="noreferrer"
            className="mt-2 block text-right text-[11px] text-[#8FA8FF] hover:underline"
          >
            View on explorer →
          </a>
        )}
      </div>

      {/* Breach reason (if any) */}
      {a.breached_reason && (
        <div className="rounded-lg border border-[#ff5a65]/30 bg-[#ff5a65]/5 px-3 py-2 text-[12px] text-[#ff5a65]">
          Breach: {a.breached_reason}
        </div>
      )}

      {/* CTAs */}
      <button
        type="button"
        onClick={onRunTests}
        className="
          flex h-11 items-center justify-center gap-2 rounded-xl
          border border-neutral-700 bg-neutral-800
          text-[14px] text-neutral-100
          transition-colors duration-[var(--duration-snappy)]
          ease-[var(--ease-standard)]
          hover:border-neutral-600 hover:bg-neutral-700
        "
      >
        ⚡ Run lifecycle tests
      </button>

      <p className="text-center text-[10px] uppercase tracking-wider text-neutral-600">
        Auto-refreshing every 5 s · evaluator ticks every 10 s
      </p>
    </div>
  );
}

/* -------------------------------------------------------------------------- */
/* Lifecycle test panel — exercises the funded policy end-to-end               */
/* -------------------------------------------------------------------------- */

interface TestCase {
  id: string;
  title: string;
  hint: string;
  expected: 'allow' | FundedDenyCode;
  build: (self: FundedSelfResponse) => { to: `0x${string}`; value?: string; data?: `0x${string}` };
}

/** USDL token + a known whitelisted spender (PECORRouter V4). */
const USDL_ADDRESS = '0x7c69c84daaee90b21eecabdb8f0387897e9b7b37' as const;
const PECOR_ROUTER_V4 = '0x1d5f3ac9de43dd0665c3f527913dd825f67b3daa' as const;
/** A guaranteed-not-whitelisted address used in deny scenarios. */
const NON_WHITELISTED = '0x000000000000000000000000000000000000dEaD' as const;

/** Encode `approve(address,uint256)` calldata without pulling in viem. */
function encodeApprove(spender: string, amount: bigint): `0x${string}` {
  const selector = '095ea7b3';
  const spenderHex = spender.toLowerCase().replace(/^0x/, '').padStart(64, '0');
  const amountHex = amount.toString(16).padStart(64, '0');
  return `0x${selector}${spenderHex}${amountHex}` as `0x${string}`;
}

const TEST_CASES: TestCase[] = [
  {
    id: 'native_transfer',
    title: 'Bare native PAX transfer',
    hint: 'Funded wallets cannot send PAX directly — gas is consumed only by whitelisted contract calls.',
    expected: 'WITHDRAWAL_BLOCKED',
    build: (self) => ({
      to: self.wallet.address,
      value: '1',
      data: '0x',
    }),
  },
  {
    id: 'random_contract',
    title: 'Call a non-whitelisted contract',
    hint: 'Any contract outside the tier whitelist is rejected before signing.',
    expected: 'CONTRACT_NOT_WHITELISTED',
    build: () => ({
      to: NON_WHITELISTED,
      value: '0',
      data: '0xabcdef12',
    }),
  },
  {
    id: 'approve_attacker',
    title: 'USDL.approve(attacker, 1)',
    hint: 'Approve sub-rule: spender must itself be on the whitelist. Approving 0x…dEaD is rejected.',
    expected: 'APPROVE_SPENDER_NOT_WHITELISTED',
    build: () => ({
      to: USDL_ADDRESS,
      value: '0',
      data: encodeApprove(NON_WHITELISTED, 1n),
    }),
  },
  {
    id: 'approve_router',
    title: 'USDL.approve(PECORRouter, 1)',
    hint: 'Spender is whitelisted → call passes the policy, gas is auto-topped, tx is signed and broadcast.',
    expected: 'allow',
    build: () => ({
      to: USDL_ADDRESS,
      value: '0',
      data: encodeApprove(PECOR_ROUTER_V4, 1n),
    }),
  },
];

type TestResult =
  | { kind: 'idle' }
  | { kind: 'pending' }
  | { kind: 'allowed'; tx_hash: string }
  | { kind: 'denied'; code: string; message: string; detail?: FundedDenyDetail }
  | { kind: 'error'; message: string };

function LifecycleTestPanel({
  paxeer,
  self,
  explorerUrl,
  onResult,
  onBack,
}: {
  paxeer: PaxeerWallet;
  self: FundedSelfResponse;
  explorerUrl: string | null;
  onResult: () => void;
  onBack: () => void;
}) {
  const [results, setResults] = useState<Record<string, TestResult>>({});

  async function runTest(tc: TestCase) {
    setResults((r) => ({ ...r, [tc.id]: { kind: 'pending' } }));
    const tx = tc.build(self);
    try {
      const res = await paxeer.sendFundedTransaction(tx);
      setResults((r) => ({ ...r, [tc.id]: { kind: 'allowed', tx_hash: res.tx_hash } }));
    } catch (err) {
      if (err instanceof PaxeerWalletError) {
        // The funded policy returns 403 with `error` = the deny code.
        setResults((r) => ({
          ...r,
          [tc.id]: {
            kind: 'denied',
            code: err.code,
            message: err.message,
            detail: err.detail as FundedDenyDetail | undefined,
          },
        }));
      } else {
        setResults((r) => ({
          ...r,
          [tc.id]: {
            kind: 'error',
            message: err instanceof Error ? err.message : String(err),
          },
        }));
      }
    } finally {
      onResult();
    }
  }

  return (
    <div className="flex max-h-[80vh] flex-col gap-3 overflow-y-auto px-6 py-5">
      <BackButton onBack={onBack} />

      <div className="rounded-xl border border-neutral-700 bg-neutral-800 px-4 py-3">
        <p className="text-[13px] text-neutral-300">
          Run each scenario to watch the funded policy decide in real time.
        </p>
        <p className="mt-1 text-[12px] text-neutral-500">
          Three should be <span className="text-[#ff5a65]">denied</span>; one should be{' '}
          <span className="text-[#05c168]">allowed</span> and produce a tx hash on chain 125.
        </p>
      </div>

      {TEST_CASES.map((tc) => (
        <TestCard
          key={tc.id}
          tc={tc}
          result={results[tc.id] ?? { kind: 'idle' }}
          explorerUrl={explorerUrl}
          onRun={() => runTest(tc)}
        />
      ))}

      <p className="mt-2 text-center text-[11px] text-neutral-500">
        All four tests run against the wallet API configured in{' '}
        <span className="font-mono">NEXT_PUBLIC_PAXEER_WALLET_API</span>.
      </p>
    </div>
  );
}

function TestCard({
  tc,
  result,
  explorerUrl,
  onRun,
}: {
  tc: TestCase;
  result: TestResult;
  explorerUrl: string | null;
  onRun: () => void;
}) {
  const expectedLabel = tc.expected === 'allow' ? 'allowed → tx hash' : tc.expected;
  const expectedTone = tc.expected === 'allow' ? 'success' : 'deny';

  const correct =
    (result.kind === 'allowed' && tc.expected === 'allow') ||
    (result.kind === 'denied' && result.code === tc.expected);

  return (
    <div className="flex flex-col gap-2 rounded-xl border border-neutral-700 bg-neutral-800 px-4 py-3">
      <div className="flex items-start justify-between gap-2">
        <div>
          <p className="text-[14px] text-neutral-100">{tc.title}</p>
          <p className="mt-0.5 text-[12px] leading-[1.45] text-neutral-500">{tc.hint}</p>
        </div>
        <ExpectBadge label={expectedLabel} tone={expectedTone} />
      </div>

      {result.kind === 'allowed' && (
        <ResultBox tone="success" title="Allowed">
          <span className="block break-all font-mono text-[11px]">{result.tx_hash}</span>
          {explorerUrl && (
            <a
              href={`${explorerUrl.replace(/\/$/, '')}/tx/${result.tx_hash}`}
              target="_blank"
              rel="noreferrer"
              className="mt-1 block text-[11px] text-[#8FA8FF] hover:underline"
            >
              View on explorer →
            </a>
          )}
        </ResultBox>
      )}
      {result.kind === 'denied' && (
        <ResultBox tone={correct ? 'success' : 'deny'} title={`Denied · ${result.code}`}>
          <span className="text-[11px] leading-[1.45]">{result.message}</span>
          {result.detail?.contract && (
            <span className="mt-1 block font-mono text-[10px] text-neutral-500">
              contract {truncateAddress(result.detail.contract)}
              {result.detail.selector ? ` · selector ${result.detail.selector}` : ''}
              {result.detail.spender ? ` · spender ${truncateAddress(result.detail.spender)}` : ''}
            </span>
          )}
        </ResultBox>
      )}
      {result.kind === 'error' && (
        <ResultBox tone="deny" title="Network / unknown error">
          <span className="text-[11px]">{result.message}</span>
        </ResultBox>
      )}

      <button
        type="button"
        onClick={onRun}
        disabled={result.kind === 'pending'}
        className="
          mt-1 flex h-9 items-center justify-center rounded-lg
          bg-[#004ced] text-[13px] text-white
          transition-[background,opacity] duration-[var(--duration-snappy)]
          ease-[var(--ease-standard)]
          hover:bg-[#0040c9]
          disabled:cursor-not-allowed disabled:opacity-60
        "
      >
        {result.kind === 'pending'
          ? 'Submitting…'
          : result.kind === 'idle'
            ? 'Run test'
            : 'Run again'}
      </button>
    </div>
  );
}

/* -------------------------------------------------------------------------- */
/* Small UI primitives                                                         */
/* -------------------------------------------------------------------------- */

function StatusBadge({ status }: { status: FundedAccountStatus }) {
  const palette = STATUS_PALETTE[status] ?? STATUS_PALETTE.active;
  return (
    <span
      className={`
        inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5
        text-[11px] uppercase tracking-wider
        ${palette.bg} ${palette.fg} ${palette.border}
      `}
    >
      <span className={`inline-block h-1.5 w-1.5 rounded-full ${palette.dot}`} />
      {palette.label}
    </span>
  );
}

const STATUS_PALETTE: Record<
  FundedAccountStatus,
  { bg: string; fg: string; border: string; dot: string; label: string }
> = {
  pending_funding: {
    bg: 'bg-neutral-800',
    fg: 'text-neutral-300',
    border: 'border border-neutral-700',
    dot: 'bg-neutral-500',
    label: 'Pending',
  },
  active: {
    bg: 'bg-[#05c168]/10',
    fg: 'text-[#05c168]',
    border: 'border border-[#05c168]/30',
    dot: 'bg-[#05c168]',
    label: 'Active',
  },
  scale_eligible: {
    bg: 'bg-[#FFB020]/10',
    fg: 'text-[#FFB020]',
    border: 'border border-[#FFB020]/30',
    dot: 'bg-[#FFB020]',
    label: 'Scale eligible',
  },
  payout_eligible: {
    bg: 'bg-[#8FA8FF]/10',
    fg: 'text-[#8FA8FF]',
    border: 'border border-[#8FA8FF]/30',
    dot: 'bg-[#8FA8FF]',
    label: 'Payout eligible',
  },
  breached_daily: {
    bg: 'bg-[#ff5a65]/10',
    fg: 'text-[#ff5a65]',
    border: 'border border-[#ff5a65]/30',
    dot: 'bg-[#ff5a65]',
    label: 'Breached · daily',
  },
  breached_max: {
    bg: 'bg-[#ff5a65]/10',
    fg: 'text-[#ff5a65]',
    border: 'border border-[#ff5a65]/30',
    dot: 'bg-[#ff5a65]',
    label: 'Breached · max',
  },
  closed: {
    bg: 'bg-neutral-800',
    fg: 'text-neutral-400',
    border: 'border border-neutral-700',
    dot: 'bg-neutral-500',
    label: 'Closed',
  },
};

function Gauge({
  label,
  valueUsd,
  pct,
  tone,
}: {
  label: string;
  valueUsd: number;
  pct: number;
  tone: 'warn' | 'danger';
}) {
  const fillColor = tone === 'danger' ? '#ff5a65' : '#FFB020';
  return (
    <div>
      <div className="flex items-baseline justify-between text-[12px]">
        <span className="text-neutral-400">{label}</span>
        <span className="font-mono text-neutral-200">{fmtUsd(valueUsd)} left</span>
      </div>
      <div className="mt-1.5 h-1.5 w-full overflow-hidden rounded-full bg-neutral-700">
        <div
          className="h-full rounded-full"
          style={{ width: `${pct}%`, background: fillColor }}
        />
      </div>
    </div>
  );
}

function Milestone({
  label,
  target,
  distance,
  reached,
}: {
  label: string;
  target: number;
  distance: number;
  reached: boolean;
}) {
  return (
    <div
      className={`
        flex flex-1 flex-col gap-0.5 rounded-xl border px-3 py-2
        ${reached ? 'border-[#8FA8FF]/40 bg-[#8FA8FF]/10' : 'border-neutral-700 bg-neutral-800'}
      `}
    >
      <span className="text-[10px] uppercase tracking-wider text-neutral-500">{label}</span>
      <span className="font-mono text-[14px] text-neutral-100">{fmtUsd(target)}</span>
      <span className="text-[11px] text-neutral-500">
        {reached ? 'reached' : `${fmtUsd(distance)} to go`}
      </span>
    </div>
  );
}

function ExpectBadge({ label, tone }: { label: string; tone: 'success' | 'deny' }) {
  const cls =
    tone === 'success'
      ? 'border-[#05c168]/30 bg-[#05c168]/10 text-[#05c168]'
      : 'border-[#ff5a65]/30 bg-[#ff5a65]/10 text-[#ff5a65]';
  return (
    <span
      className={`
        inline-flex shrink-0 items-center rounded-full border px-2 py-0.5
        font-mono text-[10px] uppercase tracking-wider
        ${cls}
      `}
    >
      {label}
    </span>
  );
}

function ResultBox({
  tone,
  title,
  children,
}: {
  tone: 'success' | 'deny';
  title: string;
  children: React.ReactNode;
}) {
  const cls =
    tone === 'success'
      ? 'border-[#05c168]/30 bg-[#05c168]/5 text-[#05c168]'
      : 'border-[#ff5a65]/30 bg-[#ff5a65]/5 text-[#ff5a65]';
  return (
    <div className={`rounded-lg border px-3 py-2 ${cls}`}>
      <p className="text-[12px] font-medium">{title}</p>
      <div className="mt-0.5 text-neutral-200">{children}</div>
    </div>
  );
}

function Bullet({ children }: { children: React.ReactNode }) {
  return (
    <li className="flex items-start gap-2">
      <span className="mt-1.5 inline-block h-1 w-1 shrink-0 rounded-full bg-neutral-500" />
      <span>{children}</span>
    </li>
  );
}

function BackButton({ onBack }: { onBack: () => void }) {
  return (
    <button
      type="button"
      onClick={onBack}
      className="
        -ml-1 flex w-fit items-center gap-1 rounded px-1 py-0.5
        text-[13px] text-neutral-400
        transition-colors duration-[var(--duration-snappy)]
        ease-[var(--ease-standard)]
        hover:text-neutral-100
      "
    >
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true">
        <path
          d="M15 18l-6-6 6-6"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
      Back
    </button>
  );
}

function Loading({ label, onBack }: { label: string; onBack: () => void }) {
  return (
    <div className="flex flex-col gap-3 px-6 py-6">
      <BackButton onBack={onBack} />
      <div className="flex flex-col items-center gap-3 py-12 text-center">
        <svg
          className="h-6 w-6 animate-spin text-neutral-500"
          viewBox="0 0 24 24"
          fill="none"
          aria-hidden="true"
        >
          <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" opacity="0.25" />
          <path fill="currentColor" d="M4 12a8 8 0 018-8v3a5 5 0 00-5 5H4z" />
        </svg>
        <p className="text-[13px] text-neutral-400">{label}</p>
      </div>
    </div>
  );
}

function ErrorCard({
  message,
  onBack,
  onRetry,
}: {
  message: string;
  onBack: () => void;
  onRetry: () => void;
}) {
  return (
    <div className="flex flex-col gap-3 px-6 py-6">
      <BackButton onBack={onBack} />
      <div className="flex flex-col items-center gap-3 py-8 text-center">
        <p className="text-[15px] text-neutral-100">Couldn&apos;t load funded account</p>
        <p className="break-all text-[12px] text-[#ff5a65]">{message}</p>
        <button
          type="button"
          onClick={onRetry}
          className="
            mt-1 rounded-xl border border-neutral-700 bg-neutral-800
            px-4 py-2 text-[13px] text-neutral-100
            hover:border-neutral-600 hover:bg-neutral-700
          "
        >
          Try again
        </button>
      </div>
    </div>
  );
}

/* -------------------------------------------------------------------------- */
/* Format helpers                                                              */
/* -------------------------------------------------------------------------- */

function fmtUsd(n: number): string {
  if (!isFinite(n)) return '—';
  return `$${n.toLocaleString('en-US', { maximumFractionDigits: 2, minimumFractionDigits: 2 })}`;
}

/** Render a token amount expressed as base units + decimals. */
function fmtUnits(units: string | null, decimals: number): string {
  if (units == null) return '—';
  try {
    const n = BigInt(units);
    if (decimals === 0) return n.toString();
    const whole = n / 10n ** BigInt(decimals);
    const frac = n % 10n ** BigInt(decimals);
    if (frac === 0n) return whole.toLocaleString();
    const fracStr = frac.toString().padStart(decimals, '0').slice(0, 2).replace(/0+$/, '');
    return fracStr ? `${whole.toLocaleString()}.${fracStr}` : whole.toLocaleString();
  } catch {
    return units;
  }
}

/** Render a wei value as a fixed-decimal native amount. */
function fmtWei(wei: string | null, decimals = 4): string {
  if (wei == null) return '—';
  try {
    const n = BigInt(wei);
    const whole = n / 10n ** 18n;
    const frac = n % 10n ** 18n;
    if (frac === 0n) return whole.toString();
    const fracStr = frac.toString().padStart(18, '0').slice(0, decimals).replace(/0+$/, '');
    return fracStr ? `${whole.toString()}.${fracStr}` : whole.toString();
  } catch {
    return wei;
  }
}
