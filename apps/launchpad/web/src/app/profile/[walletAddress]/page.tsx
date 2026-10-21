import type { Metadata } from 'next';
import { gatewayUrl } from '@/core/sdk-config';
import { shortAddress } from '@/utils/format';
import ProfileWalletModule from '@/page-modules/profile/wallet';

interface Props { params: Promise<{ walletAddress: string }>; }

const APP_NAME = 'Sidiora Launch';
const isWallet = (value: string) => /^0x[a-fA-F0-9]{40}$/.test(value);

async function publishedDisplayName(wallet: string): Promise<string | null> {
  try {
    // Public GET /v3/profiles/{wallet} through media/gateway (media/user is mTLS-only in production).
    const response = await fetch(gatewayUrl(`/v3/profiles/${wallet.toLowerCase()}`), { signal: AbortSignal.timeout(2500), next: { revalidate: 60 } });
    if (!response.ok) return null;
    const body: unknown = await response.json();
    const profile = (body as { data?: { profile?: { displayName?: unknown } } })?.data?.profile;
    return typeof profile?.displayName === 'string' && profile.displayName.trim() ? profile.displayName.trim() : null;
  } catch {
    return null;
  }
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { walletAddress } = await params;
  if (!isWallet(walletAddress)) return { title: `Profile, ${APP_NAME}`, robots: { index: false, follow: false } };
  const name = (await publishedDisplayName(walletAddress)) ?? shortAddress(walletAddress, 6, 4);
  return { title: `${name}, ${APP_NAME}`, description: `Public profile and trading activity of ${name} on ${APP_NAME}.` };
}

export default async function ProfilePage({ params }: Props) {
  const { walletAddress } = await params;
  return <ProfileWalletModule walletAddress={walletAddress} />;
}
