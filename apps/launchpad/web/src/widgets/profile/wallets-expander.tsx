'use client';

import { useState } from 'react';
import { Hexagon, Wallet } from 'lucide-react';
import { CountPill, DisclosureRow, ShortAddress, Skeleton, UnavailableState } from '@/ui/primitives';
import { isSignedOutError } from '@/core/market/unsupported';
import { SignedOutState } from '@/ui/shared/signed-out-state';
import { AccountApiError } from './account-transport';
import { usePrivateAccountQuery } from './use-private-account-query';
import { useProfileIssue } from './profile-issues';
import { mergeProfileWallets, readWalletLinks } from './wallet-links-transport';

export interface WalletsExpanderProps {
  wallet: string;
  isOwner: boolean;
  className?: string;
}

const explorerAddress = (wallet: string) => `https://paxscan.paxeer.app/address/${wallet}`;

/**
 * "Wallets" row with the chain mark and the linked-wallet count. Public viewers
 * see the route wallet only; the owner also sees verified links from
 * GET /v2/me/wallet-links on the gateway.
 */
export default function WalletsExpander({ wallet, isOwner, className }: WalletsExpanderProps) {
  const [open, setOpen] = useState(false);
  const links = usePrivateAccountQuery('wallet-links', readWalletLinks, [], wallet);
  const owner = isOwner && links.owner;
  const wallets = mergeProfileWallets(wallet, owner ? links.data ?? [] : []);
  const linksUnavailable = owner && links.error instanceof AccountApiError && [404, 501, 503].includes(links.error.status) ? links.error : null;
  const linksFailed = owner && !linksUnavailable && links.error && !isSignedOutError(links.error) ? links.error : null;
  // On the profile page the failure joins the single page notice; standalone, the row keeps its own line.
  const collected = useProfileIssue('wallet-links', linksFailed ? { label: 'Linked wallets', error: linksFailed, retry: () => { void links.refetch(); } } : null);

  return (
    <div className={className}>
      <DisclosureRow
        id="profile-wallets"
        label="Wallets"
        icon={Wallet}
        open={open}
        onToggle={setOpen}
        trailing={(
          <span className="inline-flex items-center gap-2 text-xs text-text-muted">
            <span className="inline-flex items-center gap-1" title="Paxeer">
              <Hexagon aria-hidden="true" className="size-icon-sm text-accent" />
              <span className="sr-only">Paxeer network</span>
            </span>
            {owner && links.isPending ? <Skeleton variant="pill" width={28} height={20} /> : <CountPill count={wallets.length} label={wallets.length === 1 ? 'wallet' : 'wallets'} />}
          </span>
        )}
      >
        <ul className="flex flex-col gap-1">
          {wallets.map((address, index) => (
            <li key={address} className="flex h-10 items-center justify-between gap-3 rounded-sm bg-surface-3 px-3">
              <span className="text-xs font-bold text-text-muted">{index === 0 ? 'Main wallet' : 'Linked wallet'}</span>
              <ShortAddress address={address} head={6} tail={4} explorerHref={explorerAddress(address)} />
            </li>
          ))}
        </ul>
        {linksUnavailable ? (
          <UnavailableState variant="inline" title="Linked wallets" plural="auto" operation="GET /v2/me/wallet-links" status={linksUnavailable.status} className="mt-2" />
        ) : owner && links.error && isSignedOutError(links.error) ? (
          <SignedOutState variant="inline" title="Sign in to see your linked wallets" className="mt-2 text-xs" />
        ) : linksFailed && !collected ? (
          <p role="alert" className="mt-2 text-xs text-danger">Can't load linked wallets right now</p>
        ) : null}
      </DisclosureRow>
    </div>
  );
}
