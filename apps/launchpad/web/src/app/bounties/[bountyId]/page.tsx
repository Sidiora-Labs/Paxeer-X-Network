import { notFound } from 'next/navigation';
import BountyDetailPage from '@/page-modules/bounties/detail';

interface Props { params: Promise<{ bountyId: string }> }

const BOUNTY_ID = /^[a-zA-Z0-9_-]{1,128}$/;

export default async function BountyPage({ params }: Props) {
  const { bountyId } = await params;
  const id = decodeURIComponent(bountyId);
  if (!BOUNTY_ID.test(id)) notFound();
  return <BountyDetailPage bountyId={id} />;
}
