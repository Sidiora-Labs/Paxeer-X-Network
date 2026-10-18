'use client';
import ProfileView from '@/widgets/profile/profile-view';
import { profileBlocks } from '../profile-blocks';
interface ProfileWalletModuleProps { walletAddress: string; }
export default function ProfileWalletModule({ walletAddress }: ProfileWalletModuleProps) {
  return <ProfileView walletAddress={walletAddress} blocks={profileBlocks} />;
}
