import type { Metadata } from 'next';
import { OfflineFallback } from '@/pwa/OfflineFallback';

export const metadata: Metadata = {
    title: 'Offline - Paxeer Wallet',
};

export default function OfflinePage() {
    return <OfflineFallback />;
}
