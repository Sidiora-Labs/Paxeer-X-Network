import { usePerpsWallet } from "@/hooks/perps/usePerpsWallet";
import { usePortfolioStats } from "@/hooks/perps/usePortfolioStats";

function fmtUsd(val: number): string {
  if (Math.abs(val) >= 1_000_000) return `$${(val / 1_000_000).toFixed(2)}M`;
  return `$${val.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
}

export function MobileBalancePanel() {
  const { isConnected, connect } = usePerpsWallet();
  const { totalVaultUsd, freeCollateral, unrealizedPnl, isLoading } = usePortfolioStats();

  if (!isConnected) {
    return (
      <div
        className="flex items-center justify-between px-4 py-3 md:hidden"
        style={{
          background: "var(--bg-subtle)",
          borderTop: "1px solid var(--border-soft)",
          borderBottom: "1px solid var(--border-soft)",
        }}
      >
        <span
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 10,
            letterSpacing: "0.1em",
            textTransform: "uppercase",
            color: "var(--fg-subtle)",
          }}
        >
          Balance
        </span>
        <button
          onClick={connect}
          className="px-3 py-1 rounded-md text-xs font-semibold"
          style={{ background: "var(--accent-solid)", color: "#fff" }}
        >
          Connect
        </button>
      </div>
    );
  }

  const hasData = totalVaultUsd !== 0 || freeCollateral !== 0 || unrealizedPnl !== 0;

  if (!isLoading && !hasData) {
    return (
      <div
        className="flex flex-col items-center justify-center px-4 py-4 gap-1 md:hidden"
        style={{
          background: "var(--bg-subtle)",
          borderTop: "1px solid var(--border-soft)",
          borderBottom: "1px solid var(--border-soft)",
        }}
      >
        <span
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 10,
            letterSpacing: "0.1em",
            textTransform: "uppercase",
            color: "var(--fg-subtle)",
          }}
        >
          No balance data
        </span>
        <span
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 9,
            color: "var(--fg-subtle)",
            opacity: 0.7,
          }}
        >
          Deposit funds to get started
        </span>
      </div>
    );
  }

  const pnlColor = unrealizedPnl >= 0 ? "var(--success)" : "var(--danger)";

  return (
    <div
      className="grid grid-cols-3 gap-3 px-4 py-3 md:hidden"
      style={{
        background: "var(--bg-subtle)",
        borderTop: "1px solid var(--border-soft)",
        borderBottom: "1px solid var(--border-soft)",
      }}
    >
      <div>
        <div
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 9,
            letterSpacing: "0.12em",
            textTransform: "uppercase",
            color: "var(--fg-subtle)",
          }}
        >
          Vault
        </div>
        <div
          className="tabular-nums"
          style={{
            fontFamily: "var(--font-display)",
            fontSize: 14,
            fontWeight: 500,
            color: "var(--fg)",
            marginTop: 2,
          }}
        >
          {fmtUsd(totalVaultUsd)}
        </div>
      </div>
      <div>
        <div
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 9,
            letterSpacing: "0.12em",
            textTransform: "uppercase",
            color: "var(--fg-subtle)",
          }}
        >
          Available
        </div>
        <div
          className="tabular-nums"
          style={{
            fontFamily: "var(--font-display)",
            fontSize: 14,
            fontWeight: 500,
            color: "var(--fg)",
            marginTop: 2,
          }}
        >
          {fmtUsd(freeCollateral)}
        </div>
      </div>
      <div>
        <div
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 9,
            letterSpacing: "0.12em",
            textTransform: "uppercase",
            color: "var(--fg-subtle)",
          }}
        >
          uPnL
        </div>
        <div
          className="tabular-nums"
          style={{
            fontFamily: "var(--font-display)",
            fontSize: 14,
            fontWeight: 500,
            color: pnlColor,
            marginTop: 2,
          }}
        >
          {unrealizedPnl >= 0 ? "+" : ""}{fmtUsd(unrealizedPnl)}
        </div>
      </div>
    </div>
  );
}
