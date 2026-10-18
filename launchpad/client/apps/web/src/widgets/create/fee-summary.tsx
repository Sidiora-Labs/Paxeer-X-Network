'use client';

import type { ReactNode } from 'react';
import { CircleAlert, CircleCheck, Info, LoaderCircle } from 'lucide-react';
import { cn } from '@/lib/utils';
import { KeyValueRow } from '@/ui/primitives';
import { formatTokenAmount, formatUsdl } from '@/utils/format';
import { CREATION_FEE_RAW } from './creation-domain';
import type { creationFunding, FundingRead } from './creation-adapters';

export type FundingPlan = ReturnType<typeof creationFunding>;

export const fundingCopy: Record<FundingPlan['state'], string> = {
  balance_loading: 'Checking your balance...',
  funding_unavailable: 'Something changed. Refresh and try again',
  insufficient_usdl: 'Not enough USDL. You need the 1 USDL fee plus your first buy',
  insufficient_gas: 'Not enough PAX to pay the network fee',
  approval_required: "You'll approve the 1 USDL fee first, then create your coin",
  signable: 'Ready to create',
};

export interface FeeSummaryProps {
  read: FundingRead | null;
  plan: FundingPlan | null;
  firstBuyRaw: string;
  /** True when the read shown is older than the live review window. */
  stale?: boolean;
  error?: string | null;
  action?: ReactNode;
  className?: string;
}

export function FeeSummary({ read, plan, firstBuyRaw, stale = false, error = null, action, className }: FeeSummaryProps) {
  const feeRaw = read?.cost.protocolFeeRaw ?? CREATION_FEE_RAW.toString();
  const gasLabel = read ? (read.gasStage === 'approval' ? 'Network fee (approval)' : 'Network fee') : 'Network fee';
  const gas = read ? `${formatTokenAmount(read.cost.gasEstimatePaxRaw, 18, { maxFraction: 6, compact: false })} PAX` : null;
  const firstBuy = firstBuyRaw !== '0' ? formatUsdl(firstBuyRaw) : null;
  const tone = plan?.state === 'signable' ? 'success' : plan?.state === 'approval_required' || plan?.state === 'balance_loading' ? 'muted' : plan ? 'danger' : 'muted';
  const StatusIcon = plan?.state === 'signable' ? CircleCheck : plan?.state === 'balance_loading' ? LoaderCircle : plan && plan.state !== 'approval_required' ? CircleAlert : Info;

  return (
    <div className={cn('flex flex-col gap-3', className)}>
      <dl className="flex flex-col">
        <KeyValueRow label="Creation fee" value={formatUsdl(feeRaw)} />
        <KeyValueRow label={gasLabel} value={gas} trailing={!read ? <span className="text-xs text-text-faint">shown before you confirm</span> : null} />
        {firstBuy ? <KeyValueRow label="First buy" value={firstBuy} /> : null}
        {read ? <KeyValueRow label="Your USDL" value={formatUsdl(read.balances.usdl)} /> : null}
        {read ? <KeyValueRow label="Your PAX" value={`${formatTokenAmount(read.balances.pax, 18, { maxFraction: 4, compact: false })} PAX`} /> : null}
      </dl>
      {error ? (
        <p role="alert" className="inline-flex items-start gap-1 text-xs text-danger">
          <CircleAlert aria-hidden="true" className="mt-0.5 size-icon-sm shrink-0" />
          <span>{error}</span>
        </p>
      ) : plan ? (
        <p role="status" className={cn('inline-flex items-start gap-1 text-xs', tone === 'success' ? 'text-success' : tone === 'danger' ? 'text-danger' : 'text-text-muted')}>
          <StatusIcon aria-hidden="true" className={cn('mt-0.5 size-icon-sm shrink-0', plan.state === 'balance_loading' && 'animate-spin')} />
          <span>{fundingCopy[plan.state]}{stale ? '. Updates when you create' : ''}</span>
        </p>
      ) : (
        <p className="inline-flex items-start gap-1 text-xs text-text-faint">
          <Info aria-hidden="true" className="mt-0.5 size-icon-sm shrink-0" />
          <span>Creating a coin costs 1 USDL plus a small network fee in PAX</span>
        </p>
      )}
      {action}
    </div>
  );
}
