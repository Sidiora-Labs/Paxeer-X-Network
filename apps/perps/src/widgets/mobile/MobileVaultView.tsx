import { usePerpsWallet } from "@/hooks/perps/usePerpsWallet";
import { usePortfolioStats } from "@/hooks/perps/usePortfolioStats";
import { usePerfdbTraderStats } from "@/hooks/perps/usePerfdbTraderStats";

function parse18(val: string | null | undefined): number {
  if (!val) return 0;
  return parseFloat(val) / 1e18;
}

function fmtUsd(val: number): string {
  if (Math.abs(val) >= 1_000_000) return `$${(val / 1_000_000).toFixed(2)}M`;
  return `$${val.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
}

export function MobileVaultView() {
  const { isConnected, address, connect } = usePerpsWallet();
  const { totalVaultUsd, freeCollateral, unrealizedPnl, totalLeverage, positions } = usePortfolioStats();
  const { stats: traderStats } = usePerfdbTraderStats(address ?? undefined);

  const realizedPnl = traderStats?.totalPnl ? parse18(traderStats.totalPnl) : 0;
  const totalTrades = traderStats?.totalTrades ?? 0;
  const winRate = totalTrades > 0 && traderStats ? ((traderStats.winCount / totalTrades) * 100).toFixed(1) : "—";
  const openPositions = positions.length;
  const lockedMargin = totalVaultUsd - freeCollateral;
  const marginUsage = totalVaultUsd > 0 ? ((lockedMargin / totalVaultUsd) * 100).toFixed(1) : "0.0";

  if (!isConnected) {
    return (
      <div
        className="flex flex-col items-center justify-center gap-4 px-6 py-16 md:hidden"
        style={{ background: "var(--bg)" }}
      >
        <div
          style={{
            fontFamily: "var(--font-display)",
            fontSize: 18,
            fontWeight: 500,
            color: "var(--fg)",
          }}
        >
          Connect to view your vault
        </div>
        <button
          onClick={connect}
          className="px-6 py-2.5 rounded-lg text-sm font-semibold"
          style={{ background: "var(--accent-solid)", color: "#fff" }}
        >
          Connect Wallet
        </button>
      </div>
    );
  }

  const uPnlColor = unrealizedPnl >= 0 ? "var(--success)" : "var(--danger)";
  const rPnlColor = realizedPnl >= 0 ? "var(--success)" : "var(--danger)";

  return (
    <div
      className="flex flex-col gap-3 px-4 py-4 md:hidden"
      style={{ background: "var(--bg)" }}
    >
      {/* Total Vault Balance — hero card */}
      <div
        style={{
          padding: "20px 16px",
          background: "linear-gradient(135deg, rgba(0, 76, 237, 0.12), rgba(0, 76, 237, 0.04))",
          border: "1px solid var(--border-soft)",
          borderRadius: 16,
        }}
      >
        <div
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 9,
            letterSpacing: "0.14em",
            textTransform: "uppercase",
            color: "var(--fg-subtle)",
            marginBottom: 6,
          }}
        >
          Total Vault Balance
        </div>
        <div
          className="tabular-nums"
          style={{
            fontFamily: "var(--font-display)",
            fontSize: 28,
            fontWeight: 600,
            color: "var(--fg)",
            letterSpacing: "-0.02em",
          }}
        >
          {fmtUsd(totalVaultUsd)}
        </div>
        <div
          className="tabular-nums"
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 11,
            color: "var(--fg-muted)",
            marginTop: 4,
          }}
        >
          Available: {fmtUsd(freeCollateral)}
        </div>
      </div>

      {/* PnL cards — side by side */}
      <div className="grid grid-cols-2 gap-3">
        <div
          style={{
            padding: "14px 14px",
            background: "var(--bg-subtle)",
            border: "1px solid var(--border-soft)",
            borderRadius: 12,
          }}
        >
          <div
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 9,
              letterSpacing: "0.12em",
              textTransform: "uppercase",
              color: "var(--fg-subtle)",
              marginBottom: 6,
            }}
          >
            Unrealized PnL
          </div>
          <div
            className="tabular-nums"
            style={{
              fontFamily: "var(--font-display)",
              fontSize: 18,
              fontWeight: 500,
              color: uPnlColor,
            }}
          >
            {unrealizedPnl >= 0 ? "+" : ""}{fmtUsd(unrealizedPnl)}
          </div>
        </div>

        <div
          style={{
            padding: "14px 14px",
            background: "var(--bg-subtle)",
            border: "1px solid var(--border-soft)",
            borderRadius: 12,
          }}
        >
          <div
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 9,
              letterSpacing: "0.12em",
              textTransform: "uppercase",
              color: "var(--fg-subtle)",
              marginBottom: 6,
            }}
          >
            Realized PnL
          </div>
          <div
            className="tabular-nums"
            style={{
              fontFamily: "var(--font-display)",
              fontSize: 18,
              fontWeight: 500,
              color: rPnlColor,
            }}
          >
            {realizedPnl >= 0 ? "+" : ""}{fmtUsd(realizedPnl)}
          </div>
        </div>
      </div>

      {/* Account Metrics */}
      <div
        style={{
          padding: "14px 14px",
          background: "var(--bg-subtle)",
          border: "1px solid var(--border-soft)",
          borderRadius: 12,
        }}
      >
        <div
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 9,
            letterSpacing: "0.14em",
            textTransform: "uppercase",
            color: "var(--fg-subtle)",
            marginBottom: 12,
          }}
        >
          Account Metrics
        </div>
        <div className="space-y-3">
          <MetricRow label="Open Positions" value={String(openPositions)} />
          <MetricRow label="Avg. Leverage" value={totalLeverage > 0 ? `${totalLeverage.toFixed(1)}x` : "—"} />
          <MetricRow label="Margin Used" value={`${marginUsage}%`} />
          <MetricRow label="Locked Margin" value={fmtUsd(lockedMargin)} />
          <MetricRow label="Total Trades" value={String(totalTrades)} />
          <MetricRow label="Win Rate" value={typeof winRate === "string" ? `${winRate}%` : "—"} />
        </div>
      </div>
    </div>
  );
}

function MetricRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between">
      <span
        style={{
          fontFamily: "var(--font-mono)",
          fontSize: 11,
          color: "var(--fg-muted)",
          letterSpacing: "0.04em",
        }}
      >
        {label}
      </span>
      <span
        className="tabular-nums"
        style={{
          fontFamily: "var(--font-mono)",
          fontSize: 11,
          color: "var(--fg)",
          fontWeight: 500,
        }}
      >
        {value}
      </span>
    </div>
  );
}
