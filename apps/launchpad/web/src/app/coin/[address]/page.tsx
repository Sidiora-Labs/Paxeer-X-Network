import { notFound } from 'next/navigation';
import TokenModule from '@/page-modules/token';

interface Props { params: Promise<{ address: string }>; }

export default async function CoinPage({ params }: Props) {
  const { address } = await params;
  if (!/^0x[0-9a-fA-F]{40}$/.test(address)) notFound();
  return <TokenModule poolAddress={address.toLowerCase()} />;
}
