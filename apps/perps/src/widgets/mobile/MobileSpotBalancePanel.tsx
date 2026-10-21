import { usePerpsWallet } from "@/hooks/perps/usePerpsWallet";
import { useSpotPortfolio } from "@/hooks/spot/useSpotPortfolio";
import { dStrToToken, dStrToUsd } from "@/core/spot/api";
import { SPOT_STABLECOINS } from "@/core/spot/config";

/* ══════════════════════════════════════════════════════════════════════
   MobileSpotBalancePanel — SPOT mirror of MobileBalancePanel.

   Three-cell strip, identical layout to the perps version:
     · Stables   — sum of USDL + USDC + USDT (PAXSCAN-truthed)
     · Holdings  — count of non-zero ERC-20 + native PAX
     · Realized  — total realized PnL across all positions
   ══════════════════════════════════════════════════════════════════════ */

const STABLE_ADDR_SET = new Set(
  SPOT_STABLECOINS.map((s) => s.address.toLowerCase()),
);

function fmtUsd(val: number): string {
  if (Math.abs(val) >= 1_000_000) return `$${(val / 1_000_000).toFixed(2)}M`;
  return `$${val.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
}

export function MobileSpotBalancePanel() {
  const { isConnected, connect, address } = usePerpsWallet();
  const { holdings, positions, isLoading } = useSpotPortfolio(address);

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
          Spot Balance
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

  let stablesUsd = 0;
  let nonZeroCount = 0;

  if (holdings && holdings.native && Array.isArray(holdings.erc20)) {
    if (dStrToToken(holdings.native.balance, holdings.native.decimals) > 0) {
      nonZeroCount += 1;
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
      nonZeroCount += 1;
      if (STABLE_ADDR_SET.has(e.token.toLowerCase())) {
        stablesUsd += qty;
      }
    }
  }

  const positionsList = Array.isArray(positions) ? positions : [];
  const realizedTotalUsd = positionsList.reduce((acc, p) => {
    if (!p || !p.derived) return acc;
    return acc + dStrToUsd(p.derived.realizedPnlUsd, 0);
  }, 0);

  const hasData = !!holdings || positionsList.length > 0;
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
          No spot balances
        </span>
        <span
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 9,
            color: "var(--fg-subtle)",
            opacity: 0.7,
          }}
        >
          Bridge funds to start trading
        </span>
      </div>
    );
  }

  const realizedColor = realizedTotalUsd >= 0 ? "var(--success)" : "var(--danger)";

  return (
    <div
      className="grid grid-cols-3 gap-3 px-4 py-3 md:hidden"
      style={{
        background: "var(--bg-subtle)",
        borderTop: "1px solid var(--border-soft)",
        borderBottom: "1px solid var(--border-soft)",
      }}
    >
      <Cell label="Stables" value={fmtUsd(stablesUsd)} />
      <Cell label="Holdings" value={`${nonZeroCount}`} />
      <Cell
        label="Realized"
        value={`${realizedTotalUsd >= 0 ? "+" : ""}${fmtUsd(realizedTotalUsd)}`}
        valueColor={realizedColor}
      />
    </div>
  );
}

function Cell({
  label,
  value,
  valueColor,
}: {
  label: string;
  value: string;
  valueColor?: string;
}) {
  return (
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
        {label}
      </div>
      <div
        className="tabular-nums"
        style={{
          fontFamily: "var(--font-display)",
          fontSize: 14,
          fontWeight: 500,
          color: valueColor ?? "var(--fg)",
          marginTop: 2,
        }}
      >
        {value}
      </div>
    </div>
  );
}
