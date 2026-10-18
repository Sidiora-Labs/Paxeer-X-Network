'use client';
import { type ReactNode } from 'react';
import { CircleCheck, RefreshCw, Wallet } from 'lucide-react';
import { Button, KeyValueRow, NoticeBar, Skeleton } from '@/ui/primitives';
import { cn } from '@/lib/utils';
import { formatTokenAmount, shortAddress } from '@/utils/format';
import CopyAddress from '@/ui/shared/copy-address';
import { CORE_DEPLOYMENT } from '@/core/network/deployment';
import { BountyReward } from './bounty-reward';
import { usePreparedTransaction, type SigningState } from './use-bounty-mutations';
import type { Bounty } from './bounties-contract';
import type { TransactionRequest } from './prepared-transaction';

function paxAmount(raw: bigint): string {
  return `${formatTokenAmount(raw, CORE_DEPLOYMENT.nativeDecimals, { maxFraction: 6, compact: false })} ${CORE_DEPLOYMENT.nativeSymbol}`;
}

function AddressValue({ address, note }: { address: string; note?: string }) {
  return (
    <span className="inline-flex min-w-0 items-center gap-1">
      {note ? <span className="text-text-muted">{note}</span> : null}
      <span className="font-mono" title={address}>{shortAddress(address, 6, 4)}</span>
      <CopyAddress address={address} variant="icon" />
    </span>
  );
}

function statusLine(state: SigningState): { tone: 'info' | 'warning' | 'danger' | 'success' | 'neutral'; text: string } | null {
  switch (state.kind) {
    case 'invalid': return { tone: 'danger', text: state.error };
    case 'estimating': return { tone: 'neutral', text: 'Estimating network fee...' };
    case 'gas-unavailable': return { tone: 'warning', text: state.error };
    case 'wallet': return { tone: 'info', text: 'Confirm in your wallet' };
    case 'pending': return { tone: 'info', text: 'Sent, waiting for confirmation...' };
    case 'confirmed': return { tone: 'success', text: 'Confirmed!' };
    case 'reverted': return { tone: 'danger', text: 'Transaction failed, nothing was sent' };
    case 'failed': return { tone: 'danger', text: state.error };
    case 'uncertain': return { tone: 'warning', text: state.error };
    default: return null;
  }
}

export interface PreparedTransactionReviewProps {
  title: string;
  request: TransactionRequest;
  /** Who ends up with the funds (escrow contract when funding, the winner when settling). */
  recipient: { label: string; address: string };
  amount: ReactNode;
  actionLabel: string;
  /** Called once the receipt confirms. */
  confirmedAction?: ReactNode;
  className?: string;
}

/**
 * Review step shown before any wallet prompt: signer, recipient, contract
 * (with its deployment role), amount and the PAX gas upper bound. The sign
 * button exists only once all of them are known and validated.
 */
export function PreparedTransactionReview({ title, request, recipient, amount, actionLabel, confirmedAction, className }: PreparedTransactionReviewProps) {
  const { state, sign, retry } = usePreparedTransaction(request);
  const tx = 'tx' in state ? state.tx : null;
  const status = statusLine(state);
  const hash = 'hash' in state ? state.hash : null;
  const cost = state.kind === 'review' || state.kind === 'wallet' ? state.costRaw : null;

  return (
    <section aria-label={title} className={cn('flex flex-col gap-4 rounded-lg bg-surface-2 p-4', className)}>
      <h3 className="text-md font-bold text-text">{title}</h3>
      <dl className="flex flex-col gap-2 text-sm">
        <KeyValueRow label="From" value={<AddressValue address={request.from} />} />
        <KeyValueRow label="To" value={<AddressValue address={recipient.address} note={recipient.label} />} />
        <KeyValueRow label="Contract" value={<AddressValue address={request.to} note={tx ? tx.contractRole : undefined} />} />
        <KeyValueRow label="Amount" value={amount} />
        <KeyValueRow label="Network" value="Paxeer" />
        <KeyValueRow
          label="Network fee (max)"
          value={cost !== null ? <span data-numeric className="tabular-nums">{paxAmount(cost)}</span> : state.kind === 'estimating' ? <Skeleton width={96} height={16} label="Estimating fee" /> : 'Not estimated yet'}
        />
      </dl>
      {status ? (
        <NoticeBar tone={status.tone} announce surface={2}>
          <span>{status.text}</span>
          {hash ? <span className="mt-1 block break-all font-mono text-xs text-text-muted">Transaction {hash}</span> : null}
        </NoticeBar>
      ) : null}
      <div className="flex flex-wrap items-center gap-2">
        {state.kind === 'review' || state.kind === 'wallet' || state.kind === 'estimating' ? (
          <Button variant="primary" size="md" icon={<Wallet />} loading={state.kind === 'wallet'} disabled={state.kind !== 'review'} onClick={() => void sign()}>
            {actionLabel}
          </Button>
        ) : null}
        {state.kind === 'failed' || state.kind === 'gas-unavailable' || state.kind === 'invalid' ? (
          <Button variant="secondary" size="md" icon={<RefreshCw />} onClick={retry}>Check again</Button>
        ) : null}
        {state.kind === 'confirmed' ? (
          <span className="inline-flex items-center gap-2 text-sm text-success"><CircleCheck aria-hidden="true" className="size-icon-sm" />Done</span>
        ) : null}
        {state.kind === 'confirmed' ? confirmedAction : null}
      </div>
    </section>
  );
}

export interface FundingStepProps {
  bounty: Bounty;
  request: TransactionRequest | null;
  className?: string;
}

/**
 * Funding step after IF_BOUNTY_CREATE: the service's `fundingTransactionRequest`
 * moves the budget into escrow. A null request is reported as it is; the
 * bounty stays in its server state until a funding transaction exists.
 */
export function FundingStep({ bounty, request, className }: FundingStepProps) {
  const href = `/bounties/${encodeURIComponent(bounty.bountyId)}`;
  if (!request) {
    return (
      <NoticeBar tone="warning" variant="card" title="Funding not ready yet" className={className}
        action={<Button variant="secondary" size="sm" href={href}>Open bounty</Button>}>
        Your bounty is saved but not funded yet. Check back soon
      </NoticeBar>
    );
  }
  return (
    <PreparedTransactionReview
      title="Fund your bounty"
      request={request}
      recipient={{ label: 'Bounty reward', address: request.to }}
      amount={<BountyReward bounty={bounty} />}
      actionLabel="Fund in wallet"
      confirmedAction={<Button variant="secondary" size="md" href={href}>View bounty</Button>}
      className={className}
    />
  );
}
