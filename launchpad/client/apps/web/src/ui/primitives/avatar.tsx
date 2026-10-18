'use client';

import { useState, type ReactNode } from 'react';
import { cn } from '@/lib/utils';

export type AvatarSize = 'xs' | 'sm' | 'md' | 'lg' | 'xl' | '2xl' | 'profile';

const avatarSizeClass: Record<AvatarSize, string> = {
  xs: 'size-avatar-xs text-2xs',
  sm: 'size-avatar-sm text-2xs',
  md: 'size-avatar-md text-xs',
  lg: 'size-avatar-lg text-sm',
  xl: 'size-avatar-xl text-base',
  '2xl': 'size-avatar-2xl text-lg',
  profile: 'size-avatar-profile text-xl',
};

/** True when the display name is (or begins with) a 0x address rather than a human name: "0xAbC…", "0x1234…5678", "$0xdead". */
export function isAddressName(name: string): boolean {
  return /^0x/i.test(name.trim().replace(/^[$@]/, ''));
}

/** The hex digits of an address-like name, lowercased, without the 0x prefix; "" when there are none. */
function addressHex(name: string): string {
  return name.trim().replace(/^[$@]/, '').replace(/^0x/i, '').toLowerCase().replace(/[^0-9a-f]/g, '');
}

/**
 * Deterministic mark for an address-like name: the last `length` hex digits (lowercase) and a
 * hue (0-359) hashed from all of its digits, so the same address always reads the same.
 */
export function addressMarkOf(name: string, length = 4): { text: string; hue: number } {
  const hex = addressHex(name);
  let hash = 0x811c9dc5;
  for (let i = 0; i < hex.length; i += 1) {
    hash ^= hex.charCodeAt(i);
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  return { text: hex.slice(-length) || '?', hue: hash % 360 };
}

export function initialsOf(name: string): string {
  // An address is not a name: "0x…" would read as the initials "0X". Use its last hex digits instead.
  if (isAddressName(name)) return addressMarkOf(name, 2).text;
  const parts = name.trim().replace(/^[$@]/, '').split(/\s+/).filter(Boolean);
  if (parts.length === 0) return '?';
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
}

export interface AvatarProps {
  src: string | null | undefined;
  name: string;
  size?: AvatarSize;
  shape?: 'round' | 'square';
  /** Set when the name is adjacent on screen, so the image is decorative. */
  decorative?: boolean;
  badge?: ReactNode;
  loading?: boolean;
  className?: string;
}

export function Avatar({ src, name, size = 'md', shape = 'round', decorative = false, badge, loading = false, className }: AvatarProps) {
  const [failed, setFailed] = useState(false);
  const radius = shape === 'round' ? 'rounded-pill' : 'rounded-sm';
  const showImage = Boolean(src) && !failed && !loading;

  return (
    <span className={cn('relative inline-flex shrink-0', avatarSizeClass[size], className)}>
      {loading ? (
        <span aria-hidden="true" className={cn('sl-skeleton size-full', radius)} />
      ) : showImage ? (
        <img
          src={src as string}
          alt={decorative ? '' : name}
          width={1}
          height={1}
          onError={() => setFailed(true)}
          className={cn('size-full object-cover', radius)}
        />
      ) : isAddressName(name) ? (
        <AddressMark name={name} size={size} radius={radius} decorative={decorative} />
      ) : (
        <span
          role={decorative ? undefined : 'img'}
          aria-label={decorative ? undefined : name}
          aria-hidden={decorative || undefined}
          className={cn('flex size-full items-center justify-center bg-surface-3 font-bold uppercase text-text-muted', radius)}
        >
          {initialsOf(name)}
        </span>
      )}
      {badge ? <span className="absolute -bottom-0.5 -right-0.5 flex">{badge}</span> : null}
    </span>
  );
}

/** Sizes too small for four mono digits show the last two. */
const compactMark: ReadonlySet<AvatarSize> = new Set(['xs', 'sm']);

function AddressMark({ name, size, radius, decorative }: { name: string; size: AvatarSize; radius: string; decorative: boolean }) {
  const { text, hue } = addressMarkOf(name, compactMark.has(size) ? 2 : 4);
  return (
    <span
      role={decorative ? undefined : 'img'}
      aria-label={decorative ? undefined : name}
      aria-hidden={decorative || undefined}
      data-address-mark={text}
      // Hue from the address bytes, as a translucent tint over the surface so it holds in light and dark themes.
      style={{ backgroundColor: `hsl(${hue} 55% 50% / 0.28)` }}
      className={cn('flex size-full items-center justify-center bg-surface-3 font-mono font-semibold lowercase tracking-tight text-text', radius)}
    >
      {text}
    </span>
  );
}

export interface AvatarStackItem {
  src: string | null | undefined;
  name: string;
}

export interface AvatarStackProps {
  items: AvatarStackItem[];
  size?: Extract<AvatarSize, 'xs' | 'sm' | 'md'>;
  max?: number;
  /** Accessible summary, e.g. "5 quote assets". */
  label: string;
  /** Surface the stack sits on; the halo between avatars is this surface showing through. */
  surface?: 1 | 2 | 3;
  shape?: 'round' | 'square';
  className?: string;
}

const haloClass = { 1: 'bg-surface-1', 2: 'bg-surface-2', 3: 'bg-surface-3' } as const;

export function AvatarStack({ items, size = 'sm', max = 5, label, surface = 1, shape = 'round', className }: AvatarStackProps) {
  const shown = items.slice(0, max);
  const rest = items.length - shown.length;
  const radius = shape === 'round' ? 'rounded-pill' : 'rounded-sm';
  return (
    <span role="img" aria-label={label} className={cn('inline-flex items-center', className)}>
      {shown.map((item, index) => (
        <span key={`${item.name}-${index}`} className={cn('inline-flex p-0.5', radius, haloClass[surface], index > 0 && '-ml-2')}>
          <Avatar src={item.src} name={item.name} size={size} shape={shape} decorative />
        </span>
      ))}
      {rest > 0 ? (
        <span className={cn('-ml-2 inline-flex items-center justify-center p-0.5', radius, haloClass[surface])}>
          <span className={cn('flex items-center justify-center bg-surface-3 text-2xs font-bold text-text-muted', avatarSizeClass[size], radius)}>
            +{rest}
          </span>
        </span>
      ) : null}
    </span>
  );
}
