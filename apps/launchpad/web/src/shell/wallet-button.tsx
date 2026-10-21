'use client';

import { useEffect } from 'react';
import { usePathname } from 'next/navigation';
import { TriangleAlert } from 'lucide-react';
import { useAccount } from 'wagmi';
import { Avatar, Button, Skeleton } from '@/ui/primitives';
import { formatUsdl, shortAddress } from '@/utils/format';
import { useProfile } from '@/core/market/use-profile';
import { readPortfolio, signOutAccount } from '@/widgets/profile/account-transport';
import { useAccountSession } from '@/widgets/profile/use-account-session';
import { usePrivateAccountQuery } from '@/widgets/profile/use-private-account-query';
import { useAuthModal } from './auth-modal-store';
import SignInModal from './sign-in-modal';
import WalletModal from './wallet-modal';
import { closeAccountMenu, openAccountMenu, useAccountMenu } from './account-menu-store';
import { WalletMark } from './sign-in-wallet-row';

/**
 * Header account cluster. Guest: Sign in (opens the shared sign-in dialog).
 * Signed in: Deposit, balance pill from IF_CONNECTED_PORTFOLIO_V3, avatar with
 * the connector mark; each opens the account menu.
 */
export default function WalletButton() {
  const { session } = useAccountSession();
  const { address, isConnected, chainId, connector } = useAccount();
  const { openModal } = useAuthModal();
  const pathname = usePathname();
  const menu = useAccountMenu();
  useEffect(() => { closeAccountMenu(); }, [address, chainId, pathname]);

  const wallet = session?.session.wallet ?? null;
  const profile = useProfile(wallet);
  const portfolio = usePrivateAccountQuery('portfolio-summary', () => readPortfolio(), [], wallet ?? undefined);

  /** Ends the application session only; the wallet connection stays so the user can sign in again or keep trading. */
  const logOut = async () => { await signOutAccount(); };

  if (!session || !wallet) {
    return (
      <div className="inline-flex items-center gap-2">
        {isConnected && chainId !== 125 ? (
          <Button variant="secondary" size="sm" icon={<TriangleAlert className="text-warning" />} onClick={() => openModal('Your wallet is on another network. Switch to Paxeer to continue')}>Wrong network</Button>
        ) : null}
        <Button variant="primary" size="sm" onClick={() => openModal()}>Sign in</Button>
        <SignInModal />
      </div>
    );
  }

  const summary = portfolio.data?.data.summary;
  const balanceRaw = summary ? summary.completeValueUsdlRaw ?? summary.pricedSubtotalUsdlRaw : null;
  const balanceTitle = summary ? (summary.completeValueUsdlRaw === null ? 'Some coins have no price yet' : 'Portfolio value') : portfolio.isError ? "Can't load your balance right now" : 'Loading balance';
  const displayName = profile.data?.displayName ?? null;

  return (
    <div className="inline-flex items-center gap-2">
      <Button variant="secondary" size="sm" onClick={() => openAccountMenu('receive')}>Deposit</Button>
      <button type="button" onClick={() => openAccountMenu('account')} title={balanceTitle} aria-label={`Balance ${balanceRaw ? formatUsdl(balanceRaw, { compact: true }) : 'not available yet'}, open account menu`}
        className="flex h-control-height-sm items-center gap-1.5 rounded-pill bg-surface-2 px-3 text-num-sm text-text transition-colors duration-fast hover:bg-surface-hover active:bg-surface-active" data-numeric>
        {portfolio.isPending && !summary ? <Skeleton variant="text" width={64} label="Loading balance" /> : balanceRaw ? formatUsdl(balanceRaw, { compact: true }) : <span className="text-text-faint">—</span>}
      </button>
      <button type="button" onClick={() => openAccountMenu('account')} aria-label={`Account ${displayName ?? shortAddress(wallet)}; open account menu`}
        className="rounded-pill transition-opacity duration-fast hover:opacity-90">
        <Avatar src={profile.data?.avatarUrl ?? null} name={displayName ?? shortAddress(wallet)} size="md" decorative
          badge={<WalletMark icon={connector?.icon} name={connector?.name ?? 'Wallet'} size="sm" className="size-4 rounded-pill bg-surface-1" />} />
      </button>
      <WalletModal open={menu !== null} tab={menu ?? 'account'} onTabChange={openAccountMenu} onClose={closeAccountMenu} address={wallet}
        displayName={displayName} avatarUrl={profile.data?.avatarUrl ?? null} connectorName={connector?.name ?? null} connectorIcon={connector?.icon} onLogOut={logOut} />
      <SignInModal />
    </div>
  );
}
