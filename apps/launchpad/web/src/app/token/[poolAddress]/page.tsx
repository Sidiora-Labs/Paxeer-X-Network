import { notFound, redirect } from 'next/navigation';

interface Props { params: Promise<{ poolAddress: string }>; }

/** Legacy pool route; coins live at /coin/[address]. */
export default async function TokenMarketPage({ params }: Props) {
  const { poolAddress } = await params;
  if (!/^0x[0-9a-fA-F]{40}$/.test(poolAddress)) notFound();
  redirect(`/coin/${poolAddress.toLowerCase()}`);
}
