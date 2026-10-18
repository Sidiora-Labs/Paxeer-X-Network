'use client';
import { UserCheck, UserPlus } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Button, IconButton, type ControlSize } from '@/ui/primitives';
import { shortAddress } from '@/utils/format';
import { useFollow } from './use-follow';

export interface FollowButtonProps {
  wallet: string;
  /** Display name used in the accessible name ("Follow hdegroot"); falls back to the short address. */
  handle?: string | null;
  size?: Extract<ControlSize, 'sm' | 'md'>;
  variant?: 'text' | 'icon';
  className?: string;
}

/**
 * Follow control (components.md 50). Secondary "Follow" when not following,
 * accent-soft "Following" that reads "Unfollow" in danger text on hover,
 * aria-pressed mirrors the server state, hidden for the viewer's own wallet,
 * and a signed-out press opens the sign-in modal.
 */
export function FollowButton({ wallet, handle, size = 'sm', variant = 'text', className }: FollowButtonProps) {
  const { stats, following, pending, self, signedIn, toggle } = useFollow(wallet);
  if (self) return null;
  const name = handle?.trim() || shortAddress(wallet);
  const unsupported = stats.state === 'unsupported';
  const loading = stats.state === 'loading' || pending;
  const active = following === true;
  const title = unsupported ? 'Following is not available yet' : undefined;
  const label = active ? `Unfollow ${name}` : `Follow ${name}`;
  const disabled = unsupported || (signedIn && following === null && !loading);

  if (variant === 'icon') {
    return (
      <IconButton
        label={label}
        icon={active ? <UserCheck /> : <UserPlus />}
        variant="round"
        size={size}
        pressed={active}
        loading={loading}
        disabled={disabled}
        title={title}
        onClick={toggle}
        className={className}
      />
    );
  }

  return (
    <Button
      variant="secondary"
      size={size}
      loading={loading}
      disabled={disabled}
      aria-pressed={active}
      aria-label={label}
      title={title}
      onClick={toggle}
      className={cn('group', active && 'bg-accent-soft text-accent hover:bg-danger-soft hover:text-danger', className)}
    >
      {active ? (
        <>
          <span className="group-hover:hidden">Following</span>
          <span className="hidden group-hover:inline">Unfollow</span>
        </>
      ) : 'Follow'}
    </Button>
  );
}

export default FollowButton;
