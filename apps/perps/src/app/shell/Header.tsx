import Link from "next/link";
import { useTheme } from "next-themes";
import { Button } from "@/ui/atoms/button";
import { ArrowDownToLine } from "lucide-react";
import { IcoWallet, IcoSettings } from "@/ui/atoms/custom-icon";
import { SettingsModal } from "@/app/modals/SettingsModal";
import { DepositWithdrawModal } from "@/app/modals/DepositWithdrawModal";
import { WalletControlPanel } from "@/app/modals/WalletControlPanel";
import { useState } from "react";
import { usePerpsWallet } from "@/hooks/perps/usePerpsWallet";
import { useWalletStore } from "@/hooks/wallet/useWalletStore";
import { MobileNavTrigger, MobileNavDrawer, useMobileNav } from "@/app/shell/MobileNav";
import { usePortfolioStats } from "@/hooks/perps/usePortfolioStats";
import { cn } from "@/ui/utils/cn";
import { PaxscanSearch } from "@/widgets/paxscan-search/PaxscanSearch";
import { fmtUsdCompact as fmtUsd } from "@/ui/utils/format";
import { HyperPaxWordmark } from "@/ui/atoms/HyperPaxWordmark";
import { NetworkPill } from "@/widgets/network-pill";
import { useIsMobile } from "@/hooks/ui/useMobile";

export function Header() {
  const isMobile = useIsMobile();
  const [isSettingsOpen, setIsSettingsOpen] = useState(false);
  const [isDepositOpen, setIsDepositOpen] = useState(false);
  const [isWalletPanelOpen, setIsWalletPanelOpen] = useState(false);
  const { theme } = useTheme();
  const { isConnected, shortAddress, connect, disconnect } = usePerpsWallet();
  const openConnectModal = useWalletStore((s: { openConnectModal: () => void; }) => s.openConnectModal);
  const status = useWalletStore((s: { status: string; }) => s.status);
  const { open: mobileNavOpen, openNav, closeNav } = useMobileNav();
  const stats = usePortfolioStats();

  return (
    <>
    <header className="flex items-center justify-between px-4 mb-1 md:px-6 h-[50px] glass">
      <div className="flex items-center space-x-3 h-full translate-y-[2px]">
        <MobileNavTrigger onClick={openNav} />
        <Link href="/" className="hover:opacity-80 flex items-center h-full cursor-pointer">
          <div className="hidden md:flex items-center">
            <HyperPaxWordmark size="md" />
          </div>
          <div className="flex md:hidden items-center">
            <HyperPaxWordmark size="sm" />
          </div>
        </Link>

        <PaxscanSearch className="hidden md:block w-48 lg:w-64" />
      </div>

      <div className="flex items-center space-x-2 h-full translate-y-[2px]">
        {/* ── Portfolio stats strip — desktop, connected only ── */}
        {isConnected && (
          <div className="hidden md:flex items-center gap-0 mr-1 rounded-[var(--radius-lg)] bg-surface-1 overflow-hidden">
            <div className="flex flex-col items-start px-3 py-1 leading-none">
              <span className="text-[9px] font-medium text-muted-foreground uppercase tracking-wider">Free Collateral</span>
              <span className="text-[11px] font-mono font-semibold mt-0.5">{stats.isLoading ? "…" : fmtUsd(stats.freeCollateral)}</span>
            </div>
            <div className="flex flex-col items-start px-3 py-1 leading-none">
              <span className="text-[9px] font-medium text-muted-foreground uppercase tracking-wider">Total Value</span>
              <span className="text-[11px] font-mono font-semibold mt-0.5">{stats.isLoading ? "…" : fmtUsd(stats.totalVaultUsd)}</span>
            </div>
            <div className="flex flex-col items-start px-3 py-1 leading-none">
              <span className="text-[9px] font-medium text-muted-foreground uppercase tracking-wider">Unr. PnL</span>
              <span className={cn("text-[11px] font-mono font-semibold mt-0.5", stats.unrealizedPnl >= 0 ? "text-emerald-500" : "text-red-500")}>
                {stats.isLoading ? "…" : `${stats.unrealizedPnl >= 0 ? "+" : ""}${fmtUsd(stats.unrealizedPnl)}`}
              </span>
            </div>
            <div className="flex flex-col items-start px-3 py-1 leading-none">
              <span className="text-[9px] font-medium text-muted-foreground uppercase tracking-wider">Leverage</span>
              <span className="text-[11px] font-mono font-semibold mt-0.5">{stats.isLoading ? "…" : stats.totalLeverage > 0 ? `${stats.totalLeverage.toFixed(1)}x` : "—"}</span>
            </div>
          </div>
        )}
        {!isConnected ? (
          <Button
            variant="outline"
            className="h-8 px-3 text-sm"
            onClick={connect}
          >
            <IcoWallet size={16} className="mr-1" /> Connect Wallet
          </Button>
        ) : (
          <div className="flex items-center gap-1.5">
            {/* Deposit — always visible */}
            <Button
              variant="outline"
              className="h-8 px-3 text-xs hidden md:flex items-center gap-1"
              onClick={() => setIsDepositOpen(true)}
            >
              <ArrowDownToLine className="w-3.5 h-3.5" /> Deposit
            </Button>
            <Button
              variant="outline"
              size="icon"
              className="h-8 w-8 flex md:hidden"
              onClick={() => setIsDepositOpen(true)}
              title="Deposit"
            >
              <ArrowDownToLine className="w-4 h-4" />
            </Button>

            {/* Address chip — opens the WalletControlPanel popover. ONE
                Popover.Root only; responsive sizing happens on the inner
                Button. Rendering two Roots that share open state causes the
                second one's outside-click to instantly close the first. */}
            <WalletControlPanel
              open={isWalletPanelOpen}
              onOpenChange={setIsWalletPanelOpen}
              onDeposit={() => setIsDepositOpen(true)}
              onWithdraw={() => setIsDepositOpen(true)}
              trigger={
                <Button
                  variant="outline"
                  className="h-8 font-mono px-2 text-[10px] md:px-3 md:text-sm"
                >
                  {shortAddress}
                </Button>
              }
            />
          </div>
        )}

        {/* Network pill — live chain status, block, latency */}
        <div className="hidden md:block">
          {!isMobile && <NetworkPill />}
        </div>

        <Button
          variant="outline"
          size="icon"
          className="h-8 w-8"
          onClick={() => setIsSettingsOpen(!isSettingsOpen)}
        >
          <IcoSettings size={16} />
        </Button>

      </div>
    </header>

    <MobileNavDrawer open={mobileNavOpen} onClose={closeNav} />

    <SettingsModal
      isOpen={isSettingsOpen}
      onClose={() => setIsSettingsOpen(false)}
    />

    <DepositWithdrawModal
      isOpen={isDepositOpen}
      onClose={() => setIsDepositOpen(false)}
    />
    </>
  );
}
