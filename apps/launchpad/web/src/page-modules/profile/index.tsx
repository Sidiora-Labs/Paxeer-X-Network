'use client';
import { UserRound } from 'lucide-react';
import { useAccount } from 'wagmi';
import { SignedOutState } from '@/ui/shared/signed-out-state';
import ProfileView from '@/widgets/profile/profile-view';
import { profileBlocks } from './profile-blocks';
export default function ProfileModule() {
  const { address } = useAccount();
  if (address) return <ProfileView walletAddress={address} blocks={profileBlocks} />;
  return (
    <div className="mx-auto flex min-h-[60vh] w-full max-w-content-narrow-max-width items-center justify-center px-4 py-6">
      <SignedOutState icon={UserRound} title="Sign in to see your profile" body="Your positions, activity and replies live on your wallet's public profile." className="rounded-lg bg-surface-1" />
    </div>
  );
}
