"use client";

// AnalyticsLiquidStat — protocol-wide cycleable stat card.
//
// Wired to live sources only (zero mock fallback):
//   - `useVaultBalanceHistory()` → live TVL from on-chain central-vault balances
//     (three stablecoin collateral tokens, 6 decimals each) + running timeline.
//   - `useEnrichedGlobalStats()` → totalMarkets (chain-level) + orderly volumes
//     (perp_volume_last_1_day, perp_volume_last_7_days) for the 24h-vs-avg delta.
//   - `useRealtimePrices()` → per-market h24_volume distribution, used as the
//     spatial waveform for the "24h Volume" state since no temporal
//     protocol-wide volume series is indexed yet.
//
// `protocolSnapshots` is not populated on the current indexer, so it is
// intentionally NOT consumed here. When that snapshot stream is backfilled, a
// future revision can replace the timeline reconstruction with direct spark
// data from `tvlUsd` history.

import { useMemo } from "react";
import {
  LiquidStat,
  type LiquidStatState,
} from "@/ui/tokens/LiquidStat";
import { useVaultBalanceHistory } from "@/hooks/perps/useAnalytics";
import { useEnrichedGlobalStats } from "@/hooks/perps/useEnrichedStats";
import { useRealtimePrices } from "@/hooks/perps/useRealtimePrices";
import { COLLATERAL_TOKENS } from "@/core/perps/config";
import type { VaultBalanceRecord } from "@/types/perps";

const SPARK_POINTS = 15;
const SPARK_MIN = 20;
const SPARK_MAX = 100;
const DAY_MS = 86_400_000;
const CENTRAL_VAULT_TYPE = 0;

// ── Helpers ─────────────────────────────────────────────────────────────

const TOKEN_DECIMALS: Record<string, number> = Object.fromEntries(
  Object.values(COLLATERAL_TOKENS).map((t) => [
    t.address.toLowerCase(),
    t.decimals,
  ])
);

function tokenToUsd(addr: string, raw: string): number {
  const dec = TOKEN_DECIMALS[addr.toLowerCase()];
  if (dec === undefined) return 0; // unknown collateral — skip, don't guess
  const n = Number(raw);
  if (!Number.isFinite(n)) return 0;
  return n / 10 ** dec;
}

function normalizeSpark(raw: number[]): number[] {
  if (raw.length === 0) {
    return new Array(SPARK_POINTS).fill((SPARK_MIN + SPARK_MAX) / 2);
  }
  const min = Math.min(...raw);
  const max = Math.max(...raw);
  const span = max - min;
  const flat = (SPARK_MIN + SPARK_MAX) / 2;
  const scaled =
    span <= 0
      ? raw.map(() => flat)
      : raw.map(
          (v) => SPARK_MIN + ((v - min) / span) * (SPARK_MAX - SPARK_MIN)
        );
  if (scaled.length >= SPARK_POINTS) return scaled.slice(-SPARK_POINTS);
  const pad = new Array(SPARK_POINTS - scaled.length).fill(scaled[0]!);
  return [...pad, ...scaled];
}

/**
 * Rebuild the TVL timeline by walking vault-balance events in time order and
 * summing the running balance per token (central vault only). Returns
 * {timestamp, tvl} pairs ascending by time.
 */
function buildTvlTimeline(
  records: VaultBalanceRecord[]
): { ts: number; tvl: number }[] {
  const central = records.filter((r) => r.vaultType === CENTRAL_VAULT_TYPE);
  const sorted = [...central].sort((a, b) =>
    a.blockTimestamp.localeCompare(b.blockTimestamp)
  );
  const running: Record<string, number> = {};
  const out: { ts: number; tvl: number }[] = [];
  for (const r of sorted) {
    const addr = r.tokenAddress.toLowerCase();
    const usd = tokenToUsd(r.tokenAddress, r.newBalance);
    running[addr] = usd;
    let total = 0;
    for (const v of Object.values(running)) total += v;
    out.push({ ts: Date.parse(r.blockTimestamp), tvl: total });
  }
  return out;
}

// ── Component ──────────────────────────────────────────────────────────

export function AnalyticsLiquidStat() {
  const { data: vaultRecords } = useVaultBalanceHistory(undefined, undefined, 100);
  const { data: enriched } = useEnrichedGlobalStats();
  const { tickers } = useRealtimePrices();

  const states: LiquidStatState[] | null = useMemo(() => {
    if (!vaultRecords || vaultRecords.length === 0) return null;
    if (!enriched) return null;
    const tickerList = Object.values(tickers);
    if (tickerList.length === 0) return null;

    // ── TVL ──
    const timeline = buildTvlTimeline(vaultRecords);
    if (timeline.length === 0) return null;
    const tvlLatest = timeline[timeline.length - 1]!.tvl;
    // Baseline = nearest timeline entry ≤ (now - 24h) when available, else oldest.
    const cutoff = timeline[timeline.length - 1]!.ts - DAY_MS;
    const baseline =
      [...timeline].reverse().find((p) => p.ts <= cutoff) ?? timeline[0]!;
    const tvlDelta =
      baseline.tvl > 0 ? ((tvlLatest - baseline.tvl) / baseline.tvl) * 100 : 0;
    const tvlSpark = normalizeSpark(
      timeline.slice(-SPARK_POINTS).map((p) => p.tvl)
    );

    // ── 24h Volume ──
    // Orderly aggregates are canonical protocol-wide volume. Source of truth.
    const orderly = enriched.orderly;
    const vol1d = orderly?.perp_volume_last_1_day ?? 0;
    const vol7d = orderly?.perp_volume_last_7_days ?? 0;
    const vol7dAvg = vol7d > 0 ? vol7d / 7 : 0;
    const volDelta =
      vol7dAvg > 0 ? ((vol1d - vol7dAvg) / vol7dAvg) * 100 : 0;
    // Spark = per-market 24h volume in USD notional (h24_amount), sorted
    // asc so the line crests toward the volume leader on the right edge.
    const volBars = tickerList
      .map((t) => (t as { "24h_amount"?: number })["24h_amount"] ?? 0)
      .filter((v) => v > 0)
      .sort((a, b) => a - b)
      .slice(-SPARK_POINTS);
    const volSpark = normalizeSpark(volBars);

    // ── Active Markets ──
    // enriched.onchain.totalMarkets is the source-of-truth count on-chain.
    const totalMarkets = enriched.onchain.totalMarkets;
    // No temporal history for totalMarkets; honest "flat" spark at the
    // current value. When markets get added this will step up organically.
    const marketsSpark = normalizeSpark(
      new Array(SPARK_POINTS).fill(totalMarkets)
    );
    // Delta intentionally 0 — real data, real constraint. When snapshot
    // backfill lands, replace with temporal diff.
    const marketsDelta = 0;

    const out: LiquidStatState[] = [
      {
        label: "TVL",
        value: tvlLatest,
        delta: tvlDelta,
        sym: "$",
        data: tvlSpark,
      },
      {
        label: "24h Volume",
        value: vol1d,
        delta: volDelta,
        sym: "$",
        data: volSpark,
      },
      {
        label: "Active Markets",
        value: totalMarkets,
        delta: marketsDelta,
        data: marketsSpark,
      },
    ];
    return out;
  }, [vaultRecords, enriched, tickers]);

  if (!states) {
    return (
      <div
        aria-hidden
        style={{
          width: 360,
          height: 260,
          borderRadius: 18,
          background:
            "linear-gradient(165deg, rgba(0,30,92,0.35) 0%, rgba(0,76,237,0.25) 100%)",
          border: "1px solid rgba(143,168,255,0.18)",
        }}
        className="animate-pulse"
      />
    );
  }

  return <LiquidStat states={states} />;
}

export default AnalyticsLiquidStat;
