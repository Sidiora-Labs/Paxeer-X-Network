import { NextRequest } from 'next/server';
import { proxyWalletRead } from '@/server/wallet-read-proxy';

export async function GET(
  request: NextRequest,
  { params }: { params: Promise<{ address: string }> },
) {
  const { address } = await params;
  return proxyWalletRead(request, 'pns-address', { address });
}
