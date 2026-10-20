import { useState } from "react";
import Link from "next/link";
import { Settings } from "lucide-react";
import { usePerpsWallet } from "@/hooks/perps/usePerpsWallet";
import { useWalletStore } from "@/hooks/wallet/useWalletStore";
import { MobileNavTrigger, MobileNavDrawer, useMobileNav } from "@/app/shell/MobileNav";
import { SettingsModal } from "@/app/modals/SettingsModal";
import { HyperPaxWordmark } from "@/ui/atoms/HyperPaxWordmark";
import { LanguageToggle } from "./MobileLanguageToggle";

export function MobileHeader() {
  const { open: mobileNavOpen, openNav, closeNav } = useMobileNav();
  const { isConnected, shortAddress, connect } = usePerpsWallet();
  const openConnectModal = useWalletStore((s) => s.openConnectModal);
  const [settingsOpen, setSettingsOpen] = useState(false);

  return (
    <>
      <header
        className="sticky top-0 z-40 flex items-center justify-between px-3 md:hidden"
        style={{
          height: 52,
          background: "var(--bg)",
          borderBottom: "1px solid var(--border-soft)",
        }}
      >
        {/* Left: hamburger + logo */}
        <div className="flex items-center gap-2 shrink-0">
          <MobileNavTrigger onClick={openNav} />
          <Link href="/" className="flex items-center">
            <HyperPaxWordmark size="sm" />
          </Link>
        </div>

        {/* Right: wallet + language + settings */}
        <div className="flex items-center gap-2 shrink-0">
          {/* Wallet connection */}
          {!isConnected ? (
            <button
              className="px-3 py-1.5 rounded-lg text-xs font-semibold text-white"
              style={{ backgroundColor: "var(--accent-solid)" }}
              onClick={connect}
            >
              Connect
            </button>
          ) : (
            <button
              className="px-2.5 py-1 rounded-lg text-[10px] font-mono font-semibold border"
              style={{
                color: "var(--fg)",
                borderColor: "var(--border-soft)",
                background: "var(--bg-subtle)",
              }}
              onClick={openConnectModal}
            >
              {shortAddress}
            </button>
          )}

          {/* Language toggle */}
          <LanguageToggle />

          {/* Settings */}
          <button
            onClick={() => setSettingsOpen(true)}
            className="flex items-center justify-center h-8 w-8 rounded-md transition-colors"
            style={{ color: "var(--fg-muted)" }}
            aria-label="Settings"
          >
            <Settings className="w-4 h-4" />
          </button>
        </div>
      </header>

      <MobileNavDrawer open={mobileNavOpen} onClose={closeNav} />
      <SettingsModal isOpen={settingsOpen} onClose={() => setSettingsOpen(false)} />
    </>
  );
}
