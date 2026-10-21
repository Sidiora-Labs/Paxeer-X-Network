import { useRef, useState } from "react";
import { ArrowDownToLine, ArrowUpFromLine, ChevronRight, Clock, Loader2, RefreshCw, Settings } from "lucide-react";
import { cn } from "@/ui/utils/cn";
import { Button } from "@/ui/atoms/button";
import { SettingsModal } from "@/app/modals/SettingsModal";
import { fmtUsdCompact as fmtUsd, fmtDate, shortAddr, parse18 } from "@/ui/utils/format";
import { OnChainHistoryPanel } from "./OnChainHistoryPanel";

interface TokenInfo {
  symbol: string;
  decimals: number;
}

interface TokenBalance {
  symbol: string;
  address: `0x${string}`;
  decimals: number;
  vaultBalance: bigint;
  lockedBalance: bigint;
  walletBalance: bigint;
  available: bigint;
}

interface UserStats {
  totalPositions: number;
  openPositions: number;
  closedPositions: number;
  liquidatedPositions: number;
  totalTrades: number;
  totalRealizedPnl: string;
  totalOrders: number;
  activeOrders: number;
}

interface MobilePortfolioOverviewProps {
  address: string | null;
  totalVaultUsd: number;
  totalPnl: number;
  userStats: UserStats | null;
  balances: TokenBalance[];
  trades: any[];
  vaultEvents: any[];
  isLoading: boolean;
  onRefresh: () => void;
  action: "deposit" | "withdraw";
  onActionChange: (action: "deposit" | "withdraw") => void;
  selectedToken: string;
  onSelectedTokenChange: (value: string) => void;
  tokenOptions: { label: string; value: string }[];
  amount: string;
  onAmountChange: (value: string) => void;
  maxAmount: string;
  selectedTokenInfo?: TokenInfo;
  isProcessing: boolean;
  txStep: "idle" | "approving" | "executing" | "confirming";
  error: string | null;
  success: string | null;
  onSubmit: () => void;
  isConnected: boolean;
  hasVault: boolean;
  isCreatingVault: boolean;
  onConnect: () => void;
  onCreateVault: () => void;
}

function fmtBal(raw: bigint, decimals: number): string {
  const num = Number(raw) / 10 ** decimals;
  return num.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: decimals > 6 ? 4 : 2 });
}

export function MobilePortfolioOverview({
  address,
  totalVaultUsd,
  totalPnl,
  userStats,
  balances,
  trades,
  vaultEvents,
  isLoading,
  onRefresh,
  action,
  onActionChange,
  selectedToken,
  onSelectedTokenChange,
  tokenOptions,
  amount,
  onAmountChange,
  maxAmount,
  selectedTokenInfo,
  isProcessing,
  txStep,
  error,
  success,
  onSubmit,
  isConnected,
  hasVault,
  isCreatingVault,
  onConnect,
  onCreateVault,
}: MobilePortfolioOverviewProps) {
  const formRef = useRef<HTMLDivElement>(null);
  const activityRef = useRef<HTMLDivElement>(null);
  const [isSettingsOpen, setIsSettingsOpen] = useState(false);

  const scrollToForm = (nextAction: "deposit" | "withdraw") => {
    onActionChange(nextAction);
    formRef.current?.scrollIntoView({ behavior: "smooth", block: "start" });
  };

  const scrollToActivity = () => {
    activityRef.current?.scrollIntoView({ behavior: "smooth", block: "start" });
  };

  return (
    <div className="flex-1 min-h-0 overflow-y-auto bg-[var(--brand-black)] pb-4">

      {/* ── Inline status banner (connect wallet / create vault) ── */}
      {!isConnected ? (
        <div className="mx-4 mt-4 rounded-2xl border border-[var(--brand-blue)]/40 bg-[var(--brand-blue)]/10 px-4 py-3 flex items-center justify-between gap-3">
          <div>
            <div className="text-sm font-semibold text-white">Connect your wallet</div>
            <div className="text-xs text-[#AFAFAF] mt-0.5">Link your wallet to view live portfolio data.</div>
          </div>
          <button
            onClick={onConnect}
            className="px-3 py-1.5 rounded-lg text-xs font-semibold text-white bg-[var(--brand-blue)] whitespace-nowrap"
          >
            Connect
          </button>
        </div>
      ) : !hasVault ? (
        <div className="mx-4 mt-4 rounded-2xl border border-[var(--brand-blue)]/40 bg-[var(--brand-blue)]/10 px-4 py-3 flex items-center justify-between gap-3">
          <div>
            <div className="text-sm font-semibold text-white">Create your vault</div>
            <div className="text-xs text-[#AFAFAF] mt-0.5">A vault stores your collateral on-chain.</div>
          </div>
          <button
            onClick={onCreateVault}
            disabled={isCreatingVault}
            className="px-3 py-1.5 rounded-lg text-xs font-semibold text-white bg-[var(--brand-blue)] disabled:opacity-50 whitespace-nowrap inline-flex items-center gap-1.5"
          >
            {isCreatingVault && <Loader2 className="h-3 w-3 animate-spin" />}
            Create Vault
          </button>
        </div>
      ) : null}

      <div className="mx-4 mt-4 flex flex-col gap-3">

        <div className="flex items-center justify-end gap-2 px-1">
          <button
            onClick={onRefresh}
            disabled={isLoading || !hasVault}
            className="rounded-full border border-[var(--brand-border)] bg-[var(--brand-surface)] p-2 text-[#828282] disabled:opacity-40"
          >
            <RefreshCw className={cn("h-3.5 w-3.5", isLoading && "animate-spin")} />
          </button>
        </div>

        {/* ── Portfolio Value card ── */}
        <div className="rounded-2xl border border-[var(--brand-border)] bg-[var(--brand-surface)] p-4 flex items-center justify-between">
          <div>
            <div className="text-[10px] uppercase tracking-wider text-[#828282] font-medium">Portfolio Value</div>
            <div className="mt-1.5 text-3xl font-bold font-mono text-white">{fmtUsd(totalVaultUsd)}</div>
            {address && <div className="mt-1 text-xs text-[#AFAFAF] font-mono">Wallet: {shortAddr(address)}</div>}
          </div>
          <ChevronRight className="h-5 w-5 text-[#404040] shrink-0" />
        </div>

        {/* ── Unrealized PnL card ── */}
        <div className="rounded-2xl border border-[var(--brand-border)] bg-[var(--brand-surface)] p-4 flex items-center justify-between">
          <div>
            <div className="text-[10px] uppercase tracking-wider text-[#828282] font-medium">Unrealized PnL</div>
            <div className={cn("mt-1.5 text-2xl font-bold font-mono", totalPnl >= 0 ? "text-[var(--brand-green)]" : "text-[var(--brand-red)]")}>
              {totalPnl >= 0 ? "+" : ""}{fmtUsd(totalPnl)}
            </div>
            <div className="mt-2 flex items-center gap-4 text-xs text-[#AFAFAF]">
              <span>Open: <span className="text-white font-semibold">{userStats?.openPositions ?? 0}</span></span>
              <span>Orders: <span className="text-white font-semibold">{userStats?.activeOrders ?? 0}</span></span>
            </div>
          </div>
          <div className="text-right shrink-0">
            <div className="text-[10px] text-[#828282] font-medium uppercase tracking-wider">Performance</div>
            <div className="mt-1 text-sm font-mono font-semibold text-white">
              {userStats ? `${parse18(userStats.totalRealizedPnl) >= 0 ? "+" : ""}${fmtUsd(parse18(userStats.totalRealizedPnl))}` : "$0.00"}
            </div>
            <div className="text-[10px] text-[#AFAFAF] mt-1">{userStats?.totalTrades ?? 0} trades</div>
          </div>
        </div>

        {/* ── Quick actions grid ── */}
        <div className="grid grid-cols-3 gap-2">
          {[
            { label: "Deposit", Icon: ArrowDownToLine, action: () => scrollToForm("deposit") },
            { label: "Withdraw", Icon: ArrowUpFromLine, action: () => scrollToForm("withdraw") },
            { label: "History", Icon: Clock, action: scrollToActivity },
          ].map(({ label, Icon, action: act }) => (
            <button
              key={label}
              className="flex flex-col items-center justify-center gap-1.5 rounded-2xl border border-[var(--brand-border)] bg-[var(--brand-surface)] py-4"
              onClick={act}
            >
              <div className="flex h-8 w-8 items-center justify-center rounded-full bg-[var(--brand-blue)]/10">
                <Icon className="h-4 w-4 text-[var(--brand-blue)]" />
              </div>
              <span className="text-xs font-medium text-[#DDDDDD]">{label}</span>
            </button>
          ))}
        </div>

        {/* ── Balances ── */}
        {balances.length > 0 && (
          <div className="rounded-2xl border border-[var(--brand-border)] bg-[var(--brand-surface)] overflow-hidden">
            <div className="border-b border-[var(--brand-border)] px-4 py-3 text-xs font-semibold text-[#DDDDDD] uppercase tracking-wider">Balances</div>
            <div className="divide-y divide-[var(--brand-border)]">
              {balances.map((balance) => (
                <div key={balance.address} className="flex items-center justify-between gap-3 px-4 py-3">
                  <div>
                    <div className="text-sm font-semibold text-white">{balance.symbol}</div>
                    <div className="text-xs text-[#828282]">Wallet: {fmtBal(balance.walletBalance, balance.decimals)}</div>
                  </div>
                  <div className="text-right">
                    <div className="text-sm font-semibold font-mono text-white">{fmtBal(balance.vaultBalance, balance.decimals)}</div>
                    <div className="text-xs text-[var(--brand-green)]">Avail: {fmtBal(balance.available, balance.decimals)}</div>
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* ── Affiliate card (blue border) ── */}
        <div className="rounded-2xl border border-[var(--brand-blue)]/30 bg-[var(--brand-surface)] p-4">
          <div className="flex items-center justify-between">
            <div>
              <div className="text-sm font-semibold text-white">Affiliate Program</div>
              <div className="mt-1 text-xs text-[#AFAFAF]">Earn from referral trading volume</div>
            </div>
            <div className="rounded-xl border border-[var(--brand-blue)]/40 bg-[var(--brand-blue)]/10 px-3 py-1.5 text-xs font-medium text-[var(--brand-blue)]">
              Coming soon
            </div>
          </div>
        </div>

        {/* ── Settings row ── */}
        <button
          className="flex items-center justify-between rounded-2xl border border-[var(--brand-border)] bg-[var(--brand-surface)] px-4 py-3"
          onClick={() => setIsSettingsOpen(true)}
        >
          <div className="flex items-center gap-3">
            <div className="flex h-8 w-8 items-center justify-center rounded-full bg-[var(--brand-border)]">
              <Settings className="h-4 w-4 text-[#AFAFAF]" />
            </div>
            <div className="text-left">
              <div className="text-sm font-semibold text-white">Settings</div>
              <div className="text-xs text-[#828282]">Theme and trading preferences</div>
            </div>
          </div>
          <ChevronRight className="h-4 w-4 text-[#404040]" />
        </button>

        {/* ── Deposit / Withdraw form ── */}
        <section ref={formRef} className="rounded-2xl border border-[var(--brand-border)] bg-[var(--brand-surface)] overflow-hidden">
          {/* Tab toggle */}
          <div className="flex border-b border-[var(--brand-border)]">
            <button
              className={cn(
                "flex-1 py-3 text-sm font-semibold border-b-2 transition-colors",
                action === "deposit" ? "border-[var(--brand-blue)] text-white" : "border-transparent text-[#828282]"
              )}
              onClick={() => onActionChange("deposit")}
            >
              <span className="inline-flex items-center gap-1.5"><ArrowDownToLine className="h-4 w-4" />Deposit</span>
            </button>
            <button
              className={cn(
                "flex-1 py-3 text-sm font-semibold border-b-2 transition-colors",
                action === "withdraw" ? "border-[var(--brand-blue)] text-white" : "border-transparent text-[#828282]"
              )}
              onClick={() => onActionChange("withdraw")}
            >
              <span className="inline-flex items-center gap-1.5"><ArrowUpFromLine className="h-4 w-4" />Withdraw</span>
            </button>
          </div>

          <div className="p-4 space-y-4">
            <div>
              <label className="mb-1.5 block text-[10px] font-semibold uppercase tracking-wider text-[#828282]">Token</label>
              <select
                className="w-full rounded-xl border border-[var(--brand-border)] bg-[var(--brand-black)] px-3 py-2.5 text-sm text-white outline-none"
                value={selectedToken}
                onChange={(event) => onSelectedTokenChange(event.target.value)}
              >
                {tokenOptions.map((option) => (
                  <option key={option.value} value={option.value}>{option.label}</option>
                ))}
              </select>
            </div>

            <div>
              <div className="mb-1.5 flex items-center justify-between">
                <label className="text-[10px] font-semibold uppercase tracking-wider text-[#828282]">Amount</label>
                <button className="text-[10px] font-semibold text-[var(--brand-green)]" onClick={() => onAmountChange(maxAmount)}>
                  Max: {maxAmount} {selectedTokenInfo?.symbol}
                </button>
              </div>
              <input
                className="w-full rounded-xl border border-[var(--brand-border)] bg-[var(--brand-black)] px-3 py-2.5 text-sm font-mono text-white outline-none placeholder:text-[#404040]"
                placeholder="0.00"
                type="number"
                step="0.01"
                min="0"
                value={amount}
                onChange={(event) => onAmountChange(event.target.value)}
              />
            </div>

            {txStep !== "idle" ? (
              <div className="flex items-center gap-2 rounded-xl bg-[var(--brand-border)]/50 px-3 py-2.5 text-xs text-[#AFAFAF]">
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
                {txStep === "approving" && "Approving token spend..."}
                {txStep === "executing" && (action === "deposit" ? "Depositing..." : "Withdrawing...")}
                {txStep === "confirming" && "Confirming on-chain..."}
              </div>
            ) : null}

            {error ? (
              <div className="rounded-xl bg-[var(--brand-red)]/10 px-3 py-2.5 text-xs text-[var(--brand-red)]">{error}</div>
            ) : null}
            {success ? (
              <div className="rounded-xl bg-[var(--brand-green)]/10 px-3 py-2.5 text-xs text-[var(--brand-green)]">{success}</div>
            ) : null}

            <button
              className="w-full rounded-xl py-3.5 text-sm font-semibold text-white transition-opacity disabled:opacity-40"
              style={{ backgroundColor: "var(--brand-blue)" }}
              disabled={isProcessing || !amount || parseFloat(amount) <= 0 || !isConnected || !hasVault}
              onClick={onSubmit}
            >
              {isProcessing ? (
                <span className="inline-flex items-center gap-2"><Loader2 className="h-4 w-4 animate-spin" />Processing...</span>
              ) : !isConnected ? (
                <span>Connect wallet</span>
              ) : !hasVault ? (
                <span>Create vault first</span>
              ) : (
                <span className="inline-flex items-center gap-2">
                  {action === "deposit" ? <ArrowDownToLine className="h-4 w-4" /> : <ArrowUpFromLine className="h-4 w-4" />}
                  {action === "deposit" ? "Deposit" : "Withdraw"} {selectedTokenInfo?.symbol}
                </span>
              )}
            </button>
          </div>
        </section>

        {/* ── Activity ── */}
        <div ref={activityRef} className="flex flex-col gap-3">
          <div className="rounded-2xl border border-[var(--brand-border)] bg-[var(--brand-surface)] overflow-hidden">
            <div className="border-b border-[var(--brand-border)] px-4 py-3 text-xs font-semibold text-[#DDDDDD] uppercase tracking-wider">Recent Trades</div>
            {trades.length === 0 ? (
              <div className="px-4 py-8 text-center text-sm text-[#828282]">{isLoading ? "Loading..." : "No trades yet"}</div>
            ) : (
              <div className="divide-y divide-[var(--brand-border)]">
                {trades.slice(0, 5).map((trade: any) => (
                  <div key={trade.id} className="flex items-center justify-between gap-3 px-4 py-3 text-xs">
                    <div>
                      <div className={cn("font-semibold", trade.isLong ? "text-[var(--brand-green)]" : "text-[var(--brand-red)]")}>
                        {trade.isLong ? "Long" : "Short"} {trade.marketId}
                      </div>
                      <div className="text-[#828282]">{trade.tradeType}</div>
                    </div>
                    <div className="text-right">
                      <div className="font-mono text-white">{fmtUsd(parse18(trade.sizeUsd))}</div>
                      <div className="text-[#828282]">{fmtDate(trade.blockTimestamp)}</div>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>

          {address && (
            <OnChainHistoryPanel address={address} variant="mobile" />
          )}

          <div className="rounded-2xl border border-[var(--brand-border)] bg-[var(--brand-surface)] overflow-hidden">
            <div className="border-b border-[var(--brand-border)] px-4 py-3 text-xs font-semibold text-[#DDDDDD] uppercase tracking-wider">Deposits & Withdrawals</div>
            {vaultEvents.length === 0 ? (
              <div className="px-4 py-8 text-center text-sm text-[#828282]">No transactions yet</div>
            ) : (
              <div className="divide-y divide-[var(--brand-border)]">
                {vaultEvents.slice(0, 5).map((event: any) => (
                  <div key={event.id} className="flex items-center justify-between gap-3 px-4 py-3 text-xs">
                    <div>
                      <div className="font-semibold text-white capitalize">{event.eventType}</div>
                      <div className="text-[#828282]">{fmtDate(event.blockTimestamp)}</div>
                    </div>
                    <div className="text-right font-mono text-white">{fmtUsd(parse18(event.amount))}</div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>

      </div>
      <SettingsModal isOpen={isSettingsOpen} onClose={() => setIsSettingsOpen(false)} />
    </div>
  );
}
