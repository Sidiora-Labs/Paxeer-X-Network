import { usePerpsWallet } from "@/hooks/perps/usePerpsWallet";
import { useSpotPortfolio } from "@/hooks/spot/useSpotPortfolio";
import { dStrToToken, dStrToUsd } from "@/core/spot/api";
import { SPOT_STABLECOINS } from "@/core/spot/config";

/* ══════════════════════════════════════════════════════════════════════
   MobileSpotAccountView — SPOT mirror of MobileAccountView.

   Sections:
     · Account Equity   — Spot stables + Spot non-stables (rough USD)
     · Spot Overview    — Holdings count, Open Orders, Realized PnL,
                          Closed Trade Count
   ══════════════════════════════════════════════════════════════════════ */

const STABLE_ADDR_SET = new Set(
  SPOT_STABLECOINS.map((s) => s.address.toLowerCase()),
);

function fmtUsd(val: number): string {
  if (Math.abs(val) >= 1_000_000) return `$${(val / 1_000_000).toFixed(2)}M`;
  return `$${val.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
}

export function MobileSpotAccountView() {
  const { isConnected, connect, address } = usePerpsWallet();
  const { holdings, positions, openOrders, pnlSummary } = useSpotPortfolio(address);

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
          Connect to view your spot account
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

  let stablesUsd = 0;
  let nonStableHoldingCount = 0;
  let totalHoldingCount = 0;

  if (holdings && holdings.native && Array.isArray(holdings.erc20)) {
    if (dStrToToken(holdings.native.balance, holdings.native.decimals) > 0) {
      totalHoldingCount += 1;
      nonStableHoldingCount += 1; // native PAX is not a stable
    }
    for (const e of holdings.erc20) {
      if (!e || typeof e.token !== "string") continue;
      const dec =
        typeof e.decimals === "number"
          ? e.decimals
          : typeof e.decimals === "string"
            ? parseInt(e.decimals, 10) || 0
            : 0;
      const qty = dStrToToken(e.balance, dec);
      if (qty <= 0) continue;
      totalHoldingCount += 1;
      if (STABLE_ADDR_SET.has(e.token.toLowerCase())) {
        stablesUsd += qty;
      } else {
        nonStableHoldingCount += 1;
      }
    }
  }

  const positionsList = Array.isArray(positions) ? positions : [];
  const ordersList = Array.isArray(openOrders) ? openOrders : [];

  const realizedTotalUsd = pnlSummary
    ? dStrToUsd(pnlSummary.realizedUsd, 0)
    : positionsList.reduce(
        (acc, p) => (p && p.derived ? acc + dStrToUsd(p.derived.realizedPnlUsd, 0) : acc),
        0,
      );

  const tradeCount =
    pnlSummary?.tradeCount ??
    positionsList.reduce(
      (acc, p) => (p && p.derived ? acc + (p.derived.tradeCount || 0) : acc),
      0,
    );

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

        <MetricRow label="Stables (USD)" value={fmtUsd(stablesUsd)} />
        <MetricRow
          label="Non-Stable Holdings"
          value={`${nonStableHoldingCount}`}
        />
      </div>

      {/* Divider */}
      <div
        className="mx-4"
        style={{ height: 1, background: "var(--border-soft)" }}
      />

      {/* Spot Overview section */}
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
          Spot Overview
        </div>

        <MetricRow label="Total Assets" value={`${totalHoldingCount}`} />
        <MetricRow label="Open Orders" value={`${ordersList.length}`} />
        <MetricRow
          label="Realized PnL"
          value={`${realizedTotalUsd >= 0 ? "+" : ""}${fmtUsd(realizedTotalUsd)}`}
          valueColor={realizedTotalUsd >= 0 ? "var(--success)" : "var(--danger)"}
        />
        <MetricRow label="Trade Count" value={`${tradeCount}`} />
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
        Spot balances are PAXSCAN-truthed and update on every transfer.
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
