import type { AppProps } from "next/app";
import { useRouter } from "next/router";
import { useIsMobile } from "@/hooks/ui/useMobile";
import { Header } from "@/app/shell/Header";
import { MobileTradeHeader } from "@/app/shell/MobileTradeHeader";
import { DesktopSidebar } from "@/app/shell/DesktopSidebar";
import { Footer } from "@/app/shell/Footer";
import { SplashScreen } from "@/app/shell/SplashScreen";
import { Toaster } from "@/ui/atoms/toaster";

type PageComponent = AppProps["Component"] & {
  shell?: "trade";
  useShell?: boolean;
};

interface AppShellProps {
  Component: PageComponent;
  pageProps: AppProps["pageProps"];
}

export function AppShell({ Component, pageProps }: AppShellProps) {
  const isMobile = useIsMobile();
  const router = useRouter();

  const useShell = Component.useShell !== false;

  if (!useShell) {
    return (
      <div className="relative flex flex-col h-[100dvh]">
        <div className="flex-1 min-h-0 overflow-y-auto" id="app-scroll">
          <Component {...pageProps} />
        </div>
        <div className="relative z-50">
          <Toaster />
        </div>
        <SplashScreen />
      </div>
    );
  }

  // Mobile trade pages render their own full-screen layout (MobileAppLayout)
  // with header, bottom nav, ticker bar, etc. — skip the shell chrome.
  if (isMobile && Component.shell === "trade") {
    return (
      <div className="relative">
        <Component {...pageProps} />
        <div className="relative z-50">
          <Toaster />
        </div>
        <SplashScreen />
      </div>
    );
  }

  return (
    <div className="relative flex h-[100dvh]">
      {/* Desktop-only left sidebar — grouped nav with collapsible icon rail. */}
      {!isMobile && <DesktopSidebar />}

      <div className="flex flex-col flex-1 min-w-0">
        <div className="shrink-0 z-40">
          <Header />
        </div>
        <div className="flex-1 min-h-0 overflow-y-auto" id="app-scroll">
          <Component {...pageProps} />
        </div>
        {!isMobile && (
          <div className="shrink-0 relative z-20">
            <Footer />
          </div>
        )}
      </div>

      <div className="relative z-50">
        <Toaster />
      </div>
      <SplashScreen />
    </div>
  );
}
