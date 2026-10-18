'use client';

import { useState, type ButtonHTMLAttributes, type MouseEvent, type ReactNode } from 'react';
import Link from 'next/link';
import {
  Activity,
  Check,
  CircleCheck,
  Copy,
  Crown,
  ExternalLink,
  Flame,
  HeartHandshake,
  Lock,
  Sparkles,
  TrendingDown,
  TrendingUp,
  Trophy,
} from 'lucide-react';
import { cn } from '@/lib/utils';
import { formatCount, formatPercent, relativeAge, shortAddress } from '@/utils/format';

/* ---------------------------------------------------------------- Chip */

export type ChipVariant = 'filter' | 'preset' | 'token';

export interface ChipProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'children'> {
  children: ReactNode;
  variant?: ChipVariant;
  size?: 'sm' | 'md';
  selected?: boolean;
  /** Renders as a toggle button with aria-pressed. */
  pressed?: boolean;
  count?: number;
  icon?: ReactNode;
  href?: string;
}

export function Chip({ children, variant = 'filter', size = 'sm', selected = false, pressed, count, icon, href, className, disabled, type = 'button', ...rest }: ChipProps) {
  const isOn = selected || pressed === true;
  const classes = cn(
    'inline-flex shrink-0 items-center gap-1.5 whitespace-nowrap rounded-pill font-bold transition-colors duration-fast',
    size === 'sm' ? 'h-control-height-sm px-3 text-sm' : 'h-control-height-md px-4 text-sm',
    variant === 'token' && 'h-[22px] px-2 text-xs',
    isOn
      ? variant === 'preset'
        ? 'bg-accent text-on-accent'
        : 'bg-accent-soft-strong text-accent'
      : 'bg-surface-2 text-text-muted hover:bg-surface-hover hover:text-text',
    disabled && 'cursor-not-allowed text-text-disabled hover:bg-surface-2',
    className,
  );
  const body = (
    <>
      {icon ? <span aria-hidden="true" className="inline-flex [&>svg]:size-icon-sm">{icon}</span> : null}
      <span>{children}</span>
      {count !== undefined ? <span className="text-xs text-text-faint" data-numeric>{formatCount(count)}</span> : null}
    </>
  );
  if (href) return <Link href={href} className={classes} aria-current={selected ? 'page' : undefined}>{body}</Link>;
  if (!rest.onClick && pressed === undefined) return <span className={classes}>{body}</span>;
  return (
    <button type={type} className={classes} aria-pressed={pressed} disabled={disabled} {...rest}>
      {body}
    </button>
  );
}

/* --------------------------------------------------------- PercentPill */

export interface PercentPillProps {
  value: number | null | undefined;
  variant?: 'pill' | 'text';
  size?: 'sm' | 'md' | 'lg';
  arrow?: boolean;
  decimals?: number;
  flash?: 'up' | 'down' | null;
  className?: string;
}

export function PercentPill({ value, variant = 'pill', size = 'sm', arrow = false, decimals = 2, flash = null, className }: PercentPillProps) {
  if (value === null || value === undefined || !Number.isFinite(value)) {
    return <span className={cn('text-text-faint', size === 'lg' ? 'text-md' : size === 'md' ? 'text-sm' : 'text-xs', className)} aria-label="No change yet">{'—'}</span>;
  }
  const direction = value > 0 ? 'up' : value < 0 ? 'down' : 'flat';
  const tone = direction === 'up'
    ? 'text-percent-up'
    : direction === 'down'
      ? 'text-percent-down'
      : 'text-percent-flat';
  const bg = variant === 'pill'
    ? direction === 'up' ? 'bg-percent-up-soft' : direction === 'down' ? 'bg-percent-down-soft' : 'bg-surface-3'
    : '';
  const sizing = size === 'lg'
    ? 'text-md'
    : variant === 'pill'
      ? size === 'md' ? 'h-[26px] px-2 text-sm' : 'h-[22px] px-2 text-xs'
      : size === 'md' ? 'text-sm' : 'text-xs';
  const Arrow = direction === 'down' ? TrendingDown : TrendingUp;
  return (
    <span
      data-numeric
      className={cn(
        'inline-flex shrink-0 items-center gap-1 whitespace-nowrap rounded-pill font-bold tabular-nums',
        sizing,
        tone,
        bg,
        flash === 'up' && 'sl-flash-up',
        flash === 'down' && 'sl-flash-down',
        className,
      )}
    >
      {direction !== 'flat' ? <span className="sr-only">{direction}</span> : null}
      {arrow && direction !== 'flat' ? <Arrow aria-hidden="true" className="size-3" /> : null}
      <span>{formatPercent(value, { decimals })}</span>
    </span>
  );
}

/* --------------------------------------------------------- StatusBadge */

export type BadgeStatus = 'open' | 'paid' | 'submission' | 'ending' | 'finished' | 'live' | 'recent' | 'detected' | 'neutral';

const statusLabel: Record<BadgeStatus, string> = {
  open: 'Open',
  paid: 'Paid',
  submission: 'Submissions',
  ending: 'Ending soon',
  finished: 'Finished',
  live: 'Live',
  recent: 'Recent',
  detected: 'Detected',
  neutral: '',
};

const statusClass: Record<BadgeStatus, string> = {
  open: 'bg-status-open text-on-accent rounded-xs',
  paid: 'bg-status-paid text-on-warning rounded-xs',
  submission: 'bg-status-submission text-on-info rounded-xs',
  ending: 'bg-status-ending text-on-warning rounded-xs',
  finished: 'bg-danger-soft text-status-finished rounded-xs',
  live: 'bg-accent text-on-accent rounded-pill',
  recent: 'bg-accent-soft text-accent rounded-pill',
  detected: 'bg-accent-soft text-accent rounded-pill',
  neutral: 'bg-surface-3 text-text-muted rounded-xs',
};

export interface StatusBadgeProps {
  status: BadgeStatus;
  children?: ReactNode;
  /** Dot variant: small accent dot plus plain text (live) or a 6px dot inside the pill (recent/detected). */
  dot?: boolean;
  size?: 'sm' | 'md';
  className?: string;
}

export function StatusBadge({ status, children, dot = false, size = 'sm', className }: StatusBadgeProps) {
  const label = children ?? statusLabel[status];
  if (status === 'live' && dot) {
    return (
      <span className={cn('inline-flex items-center gap-1.5 text-xs font-bold text-text', className)}>
        <span aria-hidden="true" className="size-2 rounded-pill bg-accent" />
        <span>{label}</span>
      </span>
    );
  }
  return (
    <span
      className={cn(
        'inline-flex shrink-0 items-center gap-1 whitespace-nowrap px-1.5 font-bold uppercase tracking-[.04em]',
        size === 'md' ? 'h-6 text-xs' : 'h-5 text-2xs',
        statusClass[status],
        className,
      )}
    >
      {dot ? <span aria-hidden="true" className="size-1.5 rounded-pill bg-current" /> : null}
      {status === 'paid' ? <Trophy aria-hidden="true" className="size-3" /> : null}
      <span>{label}</span>
    </span>
  );
}

/* ----------------------------------------------------------- RankBadge */

export interface RankBadgeProps {
  rank: number;
  variant?: 'inline' | 'media' | 'disc';
  className?: string;
}

const rankTone = ['', 'text-rank-1', 'text-rank-2', 'text-rank-3'];

export function RankBadge({ rank, variant = 'inline', className }: RankBadgeProps) {
  const top = rank >= 1 && rank <= 3;
  const tone = top ? rankTone[rank] : 'text-text-faint';
  if (variant === 'media') {
    return (
      <span className={cn('inline-flex h-5 items-center rounded-xs bg-media-overlay px-1.5 text-2xs font-bold text-text', className)}>
        <span className="sr-only">Rank </span>#{rank}
      </span>
    );
  }
  if (variant === 'disc') {
    return (
      <span className={cn('inline-flex size-5 items-center justify-center rounded-pill bg-surface-3 text-2xs font-bold', tone, className)} data-numeric>
        <span className="sr-only">Rank </span>{rank}
      </span>
    );
  }
  return (
    <span className={cn('inline-flex items-center gap-1 text-sm font-bold tabular-nums', tone, className)} data-numeric>
      <span className="sr-only">Rank </span>
      {top ? <Crown aria-hidden="true" className="size-3.5" /> : null}
      <span>{rank}</span>
    </span>
  );
}

/* ---------------------------------------------------------- StageBadge */

export type Stage = 'graduated' | 'curve' | 'soon' | 'new' | 'agent' | 'locked' | 'charity';

const stageMeta: Record<Stage, { label: string; icon: typeof Sparkles; className: string }> = {
  graduated: { label: 'Graduated', icon: CircleCheck, className: 'bg-surface-3 text-accent' },
  curve: { label: 'On the curve', icon: TrendingUp, className: 'bg-surface-3 text-accent' },
  soon: { label: 'Graduating soon', icon: Flame, className: 'bg-warning-soft text-warning' },
  new: { label: 'New', icon: Sparkles, className: 'bg-info-soft text-info' },
  agent: { label: 'Agent trading', icon: Activity, className: 'bg-accent-soft text-accent' },
  locked: { label: 'Liquidity locked', icon: Lock, className: 'bg-surface-3 text-text-muted' },
  charity: { label: 'Charity', icon: HeartHandshake, className: 'bg-surface-3 text-danger' },
};

export interface StageBadgeProps {
  stage: Stage;
  size?: 14 | 16;
  /** When the stage is stated elsewhere in the row text, the badge is decorative. */
  decorative?: boolean;
  /** Parent surface for the 2px halo. */
  surface?: 1 | 2 | 3;
  className?: string;
}

const haloBg = { 1: 'bg-surface-1', 2: 'bg-surface-2', 3: 'bg-surface-3' } as const;

export function StageBadge({ stage, size = 16, decorative = false, surface = 1, className }: StageBadgeProps) {
  const meta = stageMeta[stage];
  const Icon = meta.icon;
  return (
    <span
      role={decorative ? undefined : 'img'}
      aria-label={decorative ? undefined : meta.label}
      aria-hidden={decorative || undefined}
      title={meta.label}
      tabIndex={decorative ? undefined : 0}
      className={cn('inline-flex rounded-pill p-0.5', haloBg[surface], className)}
    >
      <span className={cn('inline-flex items-center justify-center rounded-pill', meta.className, size === 16 ? 'size-4' : 'size-3.5')}>
        <Icon aria-hidden="true" className={size === 16 ? 'size-2.5' : 'size-2'} />
      </span>
    </span>
  );
}

export function stageLabel(stage: Stage): string {
  return stageMeta[stage].label;
}

/* ----------------------------------------------------------- CountPill */

export interface CountPillProps {
  count: number;
  /** Visually hidden noun, e.g. "holders". */
  label?: string;
  className?: string;
}

export function CountPill({ count, label, className }: CountPillProps) {
  return (
    <span data-numeric className={cn('inline-flex h-5 items-center rounded-pill bg-surface-3 px-1.5 text-xs font-bold tabular-nums text-text-muted', className)}>
      {formatCount(count)}
      {label ? <span className="sr-only"> {label}</span> : null}
    </span>
  );
}

/* ------------------------------------------------------------- AgeChip */

export interface AgeChipProps {
  iso: string | number | null | undefined;
  /** "5m" instead of "5m ago". */
  compact?: boolean;
  now?: number;
  className?: string;
}

export function AgeChip({ iso, compact = true, now, className }: AgeChipProps) {
  if (iso === null || iso === undefined || iso === '') {
    return <span className={cn('text-xs text-text-faint', className)}>{'—'}</span>;
  }
  const ms = typeof iso === 'number' ? (iso > 1e12 ? iso : iso * 1000) : Date.parse(iso);
  const date = Number.isNaN(ms) ? null : new Date(ms);
  const age = relativeAge(iso, now);
  return (
    <time
      dateTime={date ? date.toISOString() : undefined}
      title={date ? date.toLocaleString() : undefined}
      className={cn('text-xs text-text-faint tabular-nums', className)}
    >
      {compact ? age : `${age} ago`}
    </time>
  );
}

/* -------------------------------------------------------- ShortAddress */

export interface ShortAddressProps {
  address: string;
  head?: number;
  tail?: number;
  copy?: boolean;
  explorerHref?: string | null;
  /** Copy confirmation callback (toast lives in the caller). */
  onCopied?: () => void;
  className?: string;
}

export function ShortAddress({ address, head = 4, tail = 4, copy = true, explorerHref = null, onCopied, className }: ShortAddressProps) {
  const [copied, setCopied] = useState(false);
  const short = shortAddress(address, head, tail);

  async function handleCopy(event: MouseEvent<HTMLButtonElement>) {
    event.stopPropagation();
    event.preventDefault();
    try {
      await navigator.clipboard.writeText(address);
      setCopied(true);
      onCopied?.();
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      setCopied(false);
    }
  }

  return (
    <span className={cn('inline-flex items-center gap-1 font-mono text-xs text-text-faint', className)}>
      <span title={address}>
        <span aria-hidden="true">{short}</span>
        <span className="sr-only">{address}</span>
      </span>
      {copy ? (
        <button
          type="button"
          onClick={handleCopy}
          aria-label={`Copy address ${short}`}
          className={cn('inline-flex size-5 items-center justify-center rounded-xs transition-colors hover:bg-surface-hover hover:text-text', copied && 'text-accent')}
        >
          {copied ? <Check aria-hidden="true" className="size-icon-sm" /> : <Copy aria-hidden="true" className="size-icon-sm" />}
          <span className="sr-only" role="status">{copied ? 'Address copied' : ''}</span>
        </button>
      ) : null}
      {explorerHref ? (
        <a
          href={explorerHref}
          target="_blank"
          rel="noreferrer noopener"
          onClick={event => event.stopPropagation()}
          aria-label="View on Paxeer explorer, opens in new tab"
          className="inline-flex size-5 items-center justify-center rounded-xs transition-colors hover:bg-surface-hover hover:text-text"
        >
          <ExternalLink aria-hidden="true" className="size-icon-sm" />
        </a>
      ) : null}
    </span>
  );
}
