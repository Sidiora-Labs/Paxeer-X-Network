'use client';

import { WalletProvider } from '@/providers/WalletProvider';
import { WalletKindProvider } from '@/providers/WalletKindProvider';
import { PWAProvider } from '@/providers/PWAProvider';
import { CapacitorProvider } from '@/providers/CapacitorProvider';
import { LocaleProvider } from '@/providers/LocaleProvider';
import { EmbeddedWalletProvider } from '@/lib/wallet';
import { ShellWidget } from '@/widgets/shell';
import { SplashScreen } from '@/components/SplashScreen';
import { InstallBanner, NotificationPrompt, FloatingAppIcon, UpdateBanner, OfflineIndicator } from '@/components/pwa/PWAComponents';

export default function Page() {
    return (
        <SplashScreen>
            <LocaleProvider>
                <CapacitorProvider>
                    <PWAProvider>
                        <EmbeddedWalletProvider>
                            <WalletKindProvider>
                                <WalletProvider>
                                    <ShellWidget />
                                </WalletProvider>
                            </WalletKindProvider>
                        </EmbeddedWalletProvider>
                        <InstallBanner />
                        <NotificationPrompt />
                        <FloatingAppIcon />
                        <UpdateBanner />
                        <OfflineIndicator />
                    </PWAProvider>
                </CapacitorProvider>
            </LocaleProvider>
        </SplashScreen>
    );
}
