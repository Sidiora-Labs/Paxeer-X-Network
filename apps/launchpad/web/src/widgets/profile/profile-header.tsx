'use client';

import { useEffect, useState, type ReactNode } from 'react';
import { Camera, Pencil } from 'lucide-react';
import { cn } from '@/lib/utils';
import { shortAddress } from '@/utils/format';
import { Avatar, Button, IconButton, ShortAddress, Skeleton } from '@/ui/primitives';

export type ProfileEditField = 'displayName' | 'bio' | 'avatar' | 'banner' | 'socials';

export interface ProfileHeaderLinks {
  website?: string | null;
  twitter?: string | null;
  telegram?: string | null;
  discord?: string | null;
}

export interface ProfileHeaderProps {
  wallet: string;
  displayName: string | null;
  bio: string;
  avatarUrl: string | null;
  bannerUrl: string | null;
  links: ProfileHeaderLinks;
  isOwner: boolean;
  loading?: boolean;
  onEdit?: (field: ProfileEditField) => void;
  /** Right side of the action row: Follow for visitors, share for everyone. */
  actions?: ReactNode;
  /** Rendered under the name: follower counts and joined date. */
  stats?: ReactNode;
  /** Last row of the header (the one-line wallets row). */
  extra?: ReactNode;
  className?: string;
}

const linkLabel: Record<keyof ProfileHeaderLinks, string> = { website: 'Website', twitter: 'X', telegram: 'Telegram', discord: 'Discord' };

function isHttps(value: string | null | undefined): value is string {
  if (!value) return false;
  try { return new URL(value).protocol === 'https:'; } catch { return false; }
}

/**
 * Name the avatar fallback is drawn from: the display name, else the wallet
 * address itself. The Avatar primitive turns a hex address into an
 * address-derived mark, so the header does not derive its own initials.
 */
export function avatarFallbackName(wallet: string, displayName: string | null): string {
  return displayName?.trim() || wallet;
}

export default function ProfileHeader({ wallet, displayName, bio, avatarUrl, bannerUrl, links, isOwner, loading = false, onEdit, actions, stats, extra, className }: ProfileHeaderProps) {
  const [bannerFailed, setBannerFailed] = useState(false);
  useEffect(() => { setBannerFailed(false); }, [bannerUrl]);
  const named = Boolean(displayName?.trim());
  const name = displayName?.trim() || shortAddress(wallet, 6, 4);
  const showBanner = Boolean(bannerUrl) && !bannerFailed;
  const linkEntries = (Object.keys(linkLabel) as Array<keyof ProfileHeaderLinks>).filter(key => isHttps(links[key]));

  return (
    <header className={cn('rounded-lg bg-surface-1 pb-4', className)}>
      <div data-profile-banner className="relative h-[100px] overflow-hidden bg-surface-2 md:h-[200px] xl:h-[240px]">
        {loading ? <Skeleton variant="block" className="size-full rounded-none" /> : showBanner ? (
          <img src={bannerUrl as string} alt="" className="size-full object-cover" onError={() => setBannerFailed(true)} />
        ) : null}
        {isOwner && onEdit ? (
          <IconButton label="Change banner" icon={<Camera />} variant="on-media" onClick={() => onEdit('banner')} className="absolute right-3 top-3" />
        ) : null}
      </div>
      <div className="flex items-start justify-between gap-3 px-4 md:px-5">
        <div data-profile-avatar className="relative z-[1] -mt-7 shrink-0 rounded-pill md:-mt-12">
          <Avatar src={avatarUrl} name={avatarFallbackName(wallet, displayName)} size="2xl" loading={loading} className="max-md:size-14 max-md:text-base md:size-24 md:text-xl" />
        </div>
        <div className="flex min-h-11 flex-wrap items-center justify-end gap-2 pt-3">
          {isOwner && onEdit ? <Button variant="secondary" icon={<Pencil />} onClick={() => onEdit('displayName')}>Edit profile</Button> : null}
          {actions}
        </div>
      </div>
      <div className="flex flex-col gap-3 px-4 pt-2 md:px-5">
        {loading ? (
          <div className="flex flex-col gap-2">
            <Skeleton width={180} height={24} />
            <Skeleton width={120} height={14} />
          </div>
        ) : (
          <div className="flex min-w-0 flex-col gap-0.5">
            <h1 className="truncate text-lg font-bold text-text" title={named ? name : wallet}>{name}</h1>
            <ShortAddress address={wallet} head={4} tail={4} explorerHref={`https://paxscan.paxeer.app/address/${wallet}`} className="self-start text-xs text-text-faint" />
          </div>
        )}
        {stats}
        {!loading && bio ? <p className="max-w-prose whitespace-pre-line text-sm text-text">{bio}</p> : null}
        {!loading && linkEntries.length > 0 ? (
          <ul className="flex flex-wrap gap-2" aria-label="Links">
            {linkEntries.map(key => (
              <li key={key}>
                <a
                  href={links[key] as string}
                  target="_blank"
                  rel="noreferrer noopener"
                  className="inline-flex h-control-height-sm items-center rounded-pill bg-surface-2 px-3 text-sm font-bold text-text-muted transition-colors duration-fast hover:bg-surface-hover hover:text-text"
                >
                  {linkLabel[key]}
                </a>
              </li>
            ))}
          </ul>
        ) : null}
        {extra}
      </div>
    </header>
  );
}
