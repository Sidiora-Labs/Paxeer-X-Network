'use client';

import type { ReactNode } from 'react';
import { ChevronDown, ChevronUp, LoaderCircle, Wallet } from 'lucide-react';
import { cn } from '@/lib/utils';
import { StatusBadge } from '@/ui/primitives';

export type WalletRowBadge = 'recent' | 'detected' | null;

export interface SignInWalletRowProps {
  name: string;
  icon: string | null | undefined;
  badge?: WalletRowBadge;
  detail?: ReactNode;
  busy?: boolean;
  disabled?: boolean;
  trailing?: ReactNode;
  onSelect?: () => void;
  className?: string;
}

export function WalletMark({ icon, name, size = 'md', className }: { icon: string | null | undefined; name: string; size?: 'sm' | 'md'; className?: string }) {
  const box = size === 'sm' ? 'size-5 rounded-xs' : 'size-7 rounded-sm';
  if (icon) return <img src={icon} alt="" width={size === 'sm' ? 20 : 28} height={size === 'sm' ? 20 : 28} className={cn(box, 'shrink-0 object-contain', className)} />;
  return (
    <span aria-hidden="true" className={cn(box, 'flex shrink-0 items-center justify-center bg-surface-2 text-text-muted', className)} title={name}>
      <Wallet className={size === 'sm' ? 'size-3' : 'size-icon-md'} />
    </span>
  );
}

/** Wallet row for the sign-in dialog: 56px card on surface-3, provider mark, name, status badge in text. */
export default function SignInWalletRow({ name, icon, badge = null, detail, busy = false, disabled = false, trailing, onSelect, className }: SignInWalletRowProps) {
  const content = (
    <>
      <WalletMark icon={icon} name={name} />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-base font-bold text-text">{name}</span>
        {detail ? <span className="block truncate text-xs text-text-muted">{detail}</span> : null}
      </span>
      {badge ? <StatusBadge status={badge} dot /> : null}
      {busy ? <LoaderCircle aria-hidden="true" className="size-icon-md animate-spin text-text-muted" /> : trailing}
    </>
  );
  const classes = cn('flex h-14 w-full items-center gap-3 rounded-md bg-surface-3 px-3 text-left transition-colors duration-fast', onSelect && !disabled && 'hover:bg-surface-hover', disabled && 'text-text-disabled', className);
  if (!onSelect) return <div className={classes}>{content}</div>;
  return (
    <button type="button" onClick={onSelect} disabled={disabled || busy} aria-busy={busy || undefined} className={cn(classes, 'disabled:cursor-not-allowed')}>
      {content}
    </button>
  );
}

export function MoreWalletsRow({ open, count, onToggle, controls }: { open: boolean; count: number; onToggle: () => void; controls: string }) {
  const Icon = open ? ChevronUp : ChevronDown;
  return (
    <button type="button" onClick={onToggle} aria-expanded={open} aria-controls={controls}
      className="flex h-14 w-full items-center gap-3 rounded-md bg-surface-3 px-3 text-left transition-colors duration-fast hover:bg-surface-hover">
      <span aria-hidden="true" className="flex size-7 shrink-0 items-center justify-center rounded-sm bg-surface-2 text-text-muted"><Wallet className="size-icon-md" /></span>
      <span className="min-w-0 flex-1 text-base font-bold text-text">{open ? 'Fewer wallets' : 'More wallets'}</span>
      <span className="text-xs text-text-faint" data-numeric>{count}</span>
      <Icon aria-hidden="true" className="size-icon-md text-text-muted" />
    </button>
  );
}
