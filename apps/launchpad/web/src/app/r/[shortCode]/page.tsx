import { notFound, redirect } from 'next/navigation';
import type { Metadata } from 'next';

interface PageProps {
  params: Promise<{ shortCode: string }>;
  searchParams: Promise<{ cardId?: string | string[] }>;
}

export const dynamic = 'force-dynamic';

export async function generateMetadata({ searchParams }: PageProps): Promise<Metadata> {
  const { cardId } = await searchParams;
  if (cardId) {
    return {
      title: 'PnL card — Sidiora',
      description: 'See this trade on Sidiora, the Paxeer Network launchpad.',
    };
  }
  return {
    title: 'Join Sidiora',
    description: 'Trade on Sidiora, the Paxeer Network launchpad.',
  };
}

export default async function ReferralRedirect({ params, searchParams }: PageProps) {
  const { shortCode } = await params;
  const { cardId } = await searchParams;

  if (!/^[A-Za-z0-9_-]{1,128}$/.test(shortCode)) notFound();
  if (cardId !== undefined) {
    if (typeof cardId !== 'string' || !/^[A-Za-z0-9_-]{1,128}$/.test(cardId)) notFound();
    redirect(`/pnl/${encodeURIComponent(cardId)}?ref=${encodeURIComponent(shortCode)}`);
  }

  redirect(`/?ref=${encodeURIComponent(shortCode)}`);
}
