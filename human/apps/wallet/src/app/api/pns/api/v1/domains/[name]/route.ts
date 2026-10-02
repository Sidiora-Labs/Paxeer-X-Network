import { NextRequest } from 'next/server';
import { proxyWalletRead } from '@/server/wallet-read-proxy';

export async function GET(
  request: NextRequest,
  { params }: { params: Promise<{ name: string }> },
) {
  const { name } = await params;
  return proxyWalletRead(request, 'pns-domain', { name });
}
