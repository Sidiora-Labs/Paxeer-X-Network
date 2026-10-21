import { NextRequest } from 'next/server';
import { proxyWalletRead } from '@/server/wallet-read-proxy';

export async function GET(request: NextRequest) {
  return proxyWalletRead(request, 'fx-usd');
}
