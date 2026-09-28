'use client';

import { WalletProvider } from '@/providers/WalletProvider';
import { PWAProvider } from '@/providers/PWAProvider';
import { CapacitorProvider } from '@/providers/CapacitorProvider';
import { LocaleProvider } from '@/providers/LocaleProvider';
import { ShellWidget } from '@/widgets/shell';
import { SplashScreen } from '@/components/SplashScreen';
import { InstallBanner, NotificationPrompt, FloatingAppIcon, UpdateBanner, OfflineIndicator } from '@/components/pwa/PWAComponents';

export default function Page() {
    return (
        <SplashScreen>
            <LocaleProvider>
                <CapacitorProvider>
                    <PWAProvider>
                        <WalletProvider>
                            <ShellWidget />
                        </WalletProvider>
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
