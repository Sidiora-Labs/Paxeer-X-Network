'use client';

import { Suspense, useState, type ReactNode } from 'react';
import { UserX } from 'lucide-react';
import { Button, EmptyState, NoticeBar, humanError } from '@/ui/primitives';
import { FollowButton } from '@/widgets/social/follow-button';
import RewardsView from '../rewards/rewards-view';
import { WatchlistWidget } from '../home/watchlist-widget';
import AccountPanel from './account-panel';
import AccountPreferences from './account-preferences';
import { BalanceVisibilityProvider } from './balance-visibility';
import PositionsTab from './positions-tab';
import ProfileEditModal from './profile-edit-modal';
import ProfileHeader, { type ProfileEditField } from './profile-header';
import ProfileLayout from './profile-layout';
import ProfileStats from './profile-stats';
import ProfileTabs, { type ProfileTabKey } from './profile-tabs';
import ReferralsTab from './referrals-tab';
import ShareProfileButton from './share-profile-button';
import { useAccountSession } from './use-account-session';
import { useProfileData } from './use-profile-data';
import { ProfileIssuesNotice, ProfileIssuesProvider, useProfileIssue } from './profile-issues';
import WalletsExpander from './wallets-expander';

/** Blocks later packages contribute, built per wallet once ownership is known. */
export interface ProfileBlocks {
  /** Left rail slot (leaderboard panel). */
  left?: ReactNode;
  /** Extra right-rail content under the account panel (top trades). */
  rightExtra?: ReactNode;
  /** Centre blocks between the wallets row and the tabs (portfolio block, PnL summary). */
  centerExtra?: ReactNode;
  /** Tab panels; owner-only panels are added by the view itself. */
  panels?: Partial<Record<ProfileTabKey, ReactNode>>;
}

export interface ProfileViewProps {
  walletAddress: string;
  blocks?: (context: { wallet: string; isOwner: boolean }) => ProfileBlocks;
}

/** Hands the published-profile read failure to the page notice (renders nothing). */
function ProfileReadIssue({ error, retry }: { error: string | null; retry: () => void }) {
  useProfileIssue('published-profile', error ? { label: 'Profile', error, retry } : null);
  return null;
}

export default function ProfileView({ walletAddress, blocks }: ProfileViewProps) {
  const auth = useAccountSession();
  const data = useProfileData(walletAddress);
  const wallet = walletAddress.toLowerCase();
  const owner = data.isOwner;
  const connectedHere = !owner && data.myAddress?.toLowerCase() === wallet;
  const [edit, setEdit] = useState<{ open: boolean; field: ProfileEditField | null }>({ open: false, field: null });
  const [signOutError, setSignOutError] = useState<string | null>(null);
  const [signingOut, setSigningOut] = useState(false);

  if (!data.isValidWallet) {
    return (
      <div className="mx-auto max-w-content-narrow-max-width p-4 sm:p-6">
        <EmptyState icon={UserX} title="This profile does not exist" body="Check the link and try again" action={<Button href="/explore" variant="secondary" size="sm">Browse coins</Button>} className="rounded-lg bg-surface-1" />
      </div>
    );
  }

  const openEdit = (field: ProfileEditField) => setEdit({ open: true, field });

  async function signOut() {
    setSigningOut(true); setSignOutError(null);
    try { await auth.signOut(); } catch (error) { setSignOutError(humanError(error, "Couldn't sign out, try again")); } finally { setSigningOut(false); }
  }

  const { left, centerExtra, rightExtra, panels } = blocks?.({ wallet, isOwner: owner }) ?? {};
  const ownerPanels: Partial<Record<ProfileTabKey, ReactNode>> = owner ? {
    rewards: <RewardsView />,
    referrals: <ReferralsTab walletAddress={walletAddress} />,
    watchlist: <WatchlistWidget standalone />,
    accounting: <PositionsTab walletAddress={walletAddress} canShare defaultOpen />,
    settings: <AccountPreferences />,
  } : {};

  const header = (
    <ProfileHeader
      wallet={wallet}
      displayName={data.profile?.displayName ?? null}
      bio={data.profile?.bio ?? ''}
      avatarUrl={data.avatarUrl}
      bannerUrl={data.bannerUrl}
      links={data.profile?.socials ?? {}}
      isOwner={owner}
      loading={data.profileLoading}
      onEdit={owner ? openEdit : undefined}
      actions={(
        <>
          <ShareProfileButton wallet={wallet} displayName={data.profile?.displayName ?? null} />
          {!owner ? <FollowButton wallet={wallet} handle={data.profile?.displayName ?? null} size="md" /> : null}
        </>
      )}
      stats={<ProfileStats wallet={wallet} createdAt={data.profile?.createdAt ?? null} />}
      extra={<WalletsExpander wallet={wallet} isOwner={owner} className="-mx-3" />}
      className="rounded-none bg-transparent"
    />
  );

  const center = (
    <>
      {header}
      <ProfileReadIssue error={data.profileError} retry={data.refreshProfile} />
      <ProfileIssuesNotice />
      {connectedHere ? (
        <NoticeBar tone="info" variant="card" className="mx-4 mt-3 md:mx-5" title="This is your wallet" action={<Button variant="primary" size="sm" loading={auth.authenticating} disabled={auth.authenticating} onClick={() => { void auth.authenticate(); }}>Sign in to manage</Button>}>
          Sign in to edit your profile and see your private positions and settings.
        </NoticeBar>
      ) : null}
      {auth.error ? <p role="alert" className="px-4 pt-2 text-xs text-danger md:px-5">{humanError(auth.error)}</p> : null}
      {centerExtra}
      <Suspense fallback={<div aria-busy="true" className="h-11" />}>
        <ProfileTabs isOwner={owner} panels={{ ...panels, ...ownerPanels }} />
      </Suspense>
    </>
  );

  const right = owner ? (
    <>
      <AccountPanel onLinkSocials={() => openEdit('socials')} onSignOut={signOut} signingOut={signingOut} signOutError={signOutError} />
      {rightExtra}
    </>
  ) : rightExtra;

  return (
    <BalanceVisibilityProvider>
      <ProfileIssuesProvider>
        <ProfileLayout left={left} center={center} right={right} rightLabel={owner ? 'Account and top trades' : 'Top trades'} />
      </ProfileIssuesProvider>
      {owner ? (
        <ProfileEditModal
          key={`${auth.session?.session.sessionId}:${auth.session?.epoch}`}
          open={edit.open}
          initialField={edit.field}
          onClose={() => setEdit({ open: false, field: null })}
          onSaved={data.refreshProfile}
        />
      ) : null}
    </BalanceVisibilityProvider>
  );
}
