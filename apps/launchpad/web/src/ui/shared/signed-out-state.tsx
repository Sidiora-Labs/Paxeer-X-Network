'use client';

import type { LucideIcon } from 'lucide-react';
import { Wallet } from 'lucide-react';
import { Button, EmptyState, type EmptyStateProps } from '@/ui/primitives';
import { useAuthModal } from '@/shell/auth-modal-store';
import { cn } from '@/lib/utils';

export interface SignedOutStateProps {
  /** What signing in unlocks, e.g. "Sign in to see your watchlist". */
  title: string;
  body?: string;
  icon?: LucideIcon;
  variant?: EmptyStateProps['variant'];
  colSpan?: number;
  className?: string;
}

/**
 * The prompt a widget shows when it needs a session and the viewer has none
 * (including a 401/403 answered to an anonymous request). It is an empty state
 * with a sign-in action, never an error.
 */
export function SignedOutState({ title, body = 'Connect your wallet to continue', icon = Wallet, variant = 'panel', colSpan, className }: SignedOutStateProps) {
  const { openModal } = useAuthModal();
  if (variant === 'inline') {
    return (
      <p className={cn('text-sm text-text-faint', className)}>
        {title}.{' '}
        <button type="button" className="font-bold text-text underline-offset-2 hover:underline" onClick={() => openModal(title)}>Sign in</button>
      </p>
    );
  }
  return (
    <EmptyState
      icon={icon}
      title={title}
      body={body}
      variant={variant}
      colSpan={colSpan}
      className={className}
      action={<Button variant="primary" size="sm" onClick={() => openModal(title)}>Sign in</Button>}
    />
  );
}

export default SignedOutState;
