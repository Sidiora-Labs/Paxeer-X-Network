import { usePerpsWallet } from "@/hooks/perps/usePerpsWallet";
import { usePortfolioStats } from "@/hooks/perps/usePortfolioStats";

function fmtUsd(val: number): string {
  if (Math.abs(val) >= 1_000_000) return `$${(val / 1_000_000).toFixed(2)}M`;
  return `$${val.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
}

export function MobileAccountView() {
  const { isConnected, connect } = usePerpsWallet();
  const { totalVaultUsd, freeCollateral, unrealizedPnl, totalLeverage } = usePortfolioStats();

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
          Connect to view your account
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

  const lockedMargin = totalVaultUsd - freeCollateral;
  const marginRatio = totalVaultUsd > 0 ? ((lockedMargin / totalVaultUsd) * 100).toFixed(2) : "0.00";
  const maintenanceMargin = totalVaultUsd > 0 ? (lockedMargin * 0.05) : 0;

  return (
    <div
      className="flex flex-col md:hidden"
      style={{ background: "var(--bg)" }}
    >
      {/* Account Equity section */}
      <div className="px-4 pt-4 pb-3">
        <div
          style={{
            fontFamily: "var(--font-display)",
            fontSize: 14,
            fontWeight: 600,
            color: "var(--fg)",
            marginBottom: 12,
          }}
        >
          Account Equity
        </div>

        <MetricRow label="Spot" value={fmtUsd(0)} />
        <MetricRow label="Perps" value={fmtUsd(totalVaultUsd)} />
      </div>

      {/* Divider */}
      <div
        className="mx-4"
        style={{ height: 1, background: "var(--border-soft)" }}
      />

      {/* Perps Overview section */}
      <div className="px-4 pt-3 pb-4">
        <div
          style={{
            fontFamily: "var(--font-display)",
            fontSize: 14,
            fontWeight: 600,
            color: "var(--fg)",
            marginBottom: 12,
          }}
        >
          Perps Overview
        </div>

        <MetricRow label="Balance" value={fmtUsd(totalVaultUsd)} />
        <MetricRow
          label="Unrealized PNL"
          value={`${unrealizedPnl >= 0 ? "+" : ""}${fmtUsd(unrealizedPnl)}`}
          valueColor={unrealizedPnl >= 0 ? "var(--success)" : "var(--danger)"}
        />
        <MetricRow label="Cross Margin Ratio" value={`${marginRatio}%`} />
        <MetricRow label="Maintenance Margin" value={fmtUsd(maintenanceMargin)} />
        <MetricRow
          label="Cross Account Leverage"
          value={totalLeverage > 0 ? `${totalLeverage.toFixed(2)}x` : "—"}
        />
      </div>

      {/* Footer */}
      <div
        className="text-center py-4"
        style={{
          fontFamily: "var(--font-mono)",
          fontSize: 10,
          color: "var(--fg-subtle)",
          letterSpacing: "0.04em",
        }}
      >
        Deposits and withdrawals are not allowed.
      </div>
    </div>
  );
}

function MetricRow({
  label,
  value,
  valueColor,
}: {
  label: string;
  value: string;
  valueColor?: string;
}) {
  return (
    <div className="flex items-center justify-between py-2">
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
          color: valueColor ?? "var(--fg)",
          fontWeight: 500,
        }}
      >
        {value}
      </span>
    </div>
  );
}
