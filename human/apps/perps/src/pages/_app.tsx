import "../styles/globals.css";
import "../styles/apple-ui.css";
import { useState, useEffect } from "react";
import type { AppProps } from "next/app";
import Script from "next/script";
import Head from "next/head";
import { ThemeProvider } from "next-themes";
import { WagmiProvider } from "wagmi";
import { reconnect } from "wagmi/actions";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { wagmiConfig } from "@/core/wagmi";
import { installPostMessageEip1193Provider } from "@/core/wallet/postMessageEip1193";
import { PaxeerProvider } from "@/core/wallet/embedded";
import { AppShell } from "@/app/shell/AppShell";
import { ConnectWalletModal } from "@/app/modals/ConnectWalletModal";
import { CookieBanner } from "@/app/shell/CookieBanner";
import { PaxscanProvider } from "@/lib/paxscan";
import { SquiCircleFilterStatic } from "@/ui/atoms/SquiCircleFilter";
import { ReportFeedbackModalHost } from "@/widgets/errors/ReportFeedbackModal";

function MyApp({ Component, pageProps }: AppProps) {
  const [queryClient] = useState(() => new QueryClient());

  useEffect(() => {
    const provider = installPostMessageEip1193Provider();
    reconnect(wagmiConfig);

    return () => {
      provider?.unmount();
    };
  }, []);

  return (
    <>
      {/* Global SVG <defs> for the Apple-style squicircle filter.
          Mounted once at the app root so any element across all
          routes can opt-in via the `au-squircle` class. */}
      <SquiCircleFilterStatic />
      <Head>
        <meta name="application-name" content="HyperPax" />
        <meta name="apple-mobile-web-app-capable" content="yes" />
        <meta name="apple-mobile-web-app-status-bar-style" content="default" />
        <meta name="apple-mobile-web-app-title" content="HyperPax" />
        <meta name="description" content="Perpetual Futures on Paxeer Network" />
        <meta name="format-detection" content="telephone=no" />
        <meta name="mobile-web-app-capable" content="yes" />
        <meta name="theme-color" content="#121317" />

        <link
          rel="preload"
          href="/Paxeer_Sans_Rounded/PPPangramSansRounded-Medium.otf"
          as="font"
          type="font/otf"
          crossOrigin="anonymous"
        />
        <link
          rel="preload"
          href="/Paxeer_Sans_Rounded/PPPangramSansRounded-Bold.otf"
          as="font"
          type="font/otf"
          crossOrigin="anonymous"
        />
        <link rel="manifest" href="/manifest.json" />
        <link rel="icon" type="image/png" sizes="32x32" href="/ios/32.png" />
        <link rel="icon" type="image/png" sizes="16x16" href="/ios/16.png" />
        <link rel="apple-touch-icon" sizes="180x180" href="/ios/180.png" />
        <link rel="apple-touch-icon" sizes="120x120" href="/ios/120.png" />
        <link rel="icon" type="image/png" sizes="192x192" href="/ios/192.png" />
        <link rel="icon" type="image/png" sizes="512x512" href="/ios/512.png" />
      </Head>
      {/* F-13: load SW registration from a static asset so we can */}
      {/* tighten script-src in the CSP and avoid `unsafe-inline`. */}
      <Script
        id="register-sw"
        src="/scripts/register-sw.js"
        strategy="afterInteractive"
      />
      {/*
        F-04: TradingView is intentionally NOT loaded here. Hoisting it
        into `_app` with `beforeInteractive` made every route (homepage,
        settings, leaderboard, etc.) parser-block on a ~1 MB script that
        only Chart.tsx ever uses. The script is now lazy-loaded from
        `src/widgets/trading/Chart.tsx` immediately before the widget
        instantiates, so non-chart routes ship without it.
      */}
      <WagmiProvider config={wagmiConfig}>
        <QueryClientProvider client={queryClient}>
          <PaxeerProvider>
            <PaxscanProvider>
              <ThemeProvider
                attribute="class"
                defaultTheme="dark"
                enableSystem
                themes={['light', 'dark', 'greenify', 'hotline', 'oled']}
              >
                <AppShell Component={Component} pageProps={pageProps} />
                <ConnectWalletModal />
                <CookieBanner />
                {/* Singleton host for the brand-matching error report
                    feedback modal — opened imperatively from any toast
                    or panel via `openReportModal(error)`. */}
                <ReportFeedbackModalHost />
              </ThemeProvider>
            </PaxscanProvider>
          </PaxeerProvider>
        </QueryClientProvider>
      </WagmiProvider>
    </>
  );
}

export default MyApp;