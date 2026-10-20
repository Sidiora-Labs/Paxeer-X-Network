import { useState, useEffect, useCallback, useMemo } from "react";
import { activePoll } from "@/core/net/realtime";
import {
  Loader2, Search, Trophy, TrendingUp, TrendingDown, Medal,
  ChevronUp, ChevronDown, Filter, Users, BarChart3,
} from "lucide-react";
import { cn } from "@/ui/utils/cn";
import { Button } from "@/ui/atoms/button";
import { perfdbRest } from "@/core/perfdb/client";
import { fmtUsdCompact as fmtUsd, parse18, shortAddr } from "@/ui/utils/format";

// ── Interfaces ───────────────────────────────────────────────────────
interface LeaderboardEntry {
  rank: number;
  userAddress: string;
  totalRealizedPnl: string;
  totalTrades: number;
  winRate: number;
  avgHoldTime?: string;
  openPositions?: number;
  totalVolume: string;
}

// PerfDB /v2/leaderboard REST response row shape (from perfdb.openapi.yaml).
interface PerfdbLeaderboardEntry {
  rank: number;
  address: string;
  pnl: string;
  volume: string;
  trades: number;
  win_rate: number; // percentage 0–100, e.g. 68.0 = 68%
}

// PerfDB occasionally returns wei values in scientific notation
// (e.g. "108310e+20"). parse18's BigInt path rejects those, so normalize to
// a plain integer string before storing.
function normalizeWeiStr(v: string | null | undefined): string {
  if (!v) return "0";
  if (!/[eE]/.test(v)) return v;
  const n = Number(v);
  if (!isFinite(n) || isNaN(n)) return "0";
  return n.toFixed(0);
}

// ── Helpers ──────────────────────────────────────────────────────────
function fmtDuration(val: string | null | undefined): string {
  if (!val) return "\u2014";
  const seconds = parseInt(val);
  if (isNaN(seconds) || seconds <= 0) return "\u2014";
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`;
  return `${Math.floor(seconds / 86400)}d ${Math.floor((seconds % 86400) / 3600)}h`;
}

function fmtWinRate(val: number | null | undefined): string {
  if (val === null || val === undefined) return "\u2014";
  return `${(val * 100).toFixed(1)}%`;
}

function rankBadgeClass(rank: number): string {
  if (rank === 1) return "bg-amber-500/20 text-amber-400 border-amber-500/30";
  if (rank === 2) return "bg-zinc-400/20 text-zinc-300 border-zinc-400/30";
  if (rank === 3) return "bg-orange-600/20 text-orange-400 border-orange-600/30";
  return "bg-muted text-muted-foreground border-border";
}

type SortKey = "rank" | "pnl" | "trades" | "winRate" | "avgHoldTime" | "volume";
type SortDir = "asc" | "desc";

const PAGE_SIZE = 25;

export default function LeaderboardPage() {
  const [entries, setEntries] = useState<LeaderboardEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState("");
  const [sortKey, setSortKey] = useState<SortKey>("rank");
  const [sortDir, setSortDir] = useState<SortDir>("asc");
  const [page, setPage] = useState(0);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const res = await perfdbRest<{ leaderboard: PerfdbLeaderboardEntry[] }>(
        "/v2/leaderboard?metric=pnl&period=all&limit=200"
      );
      const mapped: LeaderboardEntry[] = (res.leaderboard ?? []).map((e) => ({
        rank: e.rank,
        userAddress: e.address,
        totalRealizedPnl: normalizeWeiStr(e.pnl),
        totalTrades: e.trades,
        winRate: (e.win_rate ?? 0) / 100,
        totalVolume: normalizeWeiStr(e.volume),
      }));
      setEntries(mapped);
    } catch (err) {
      console.error("Failed to fetch leaderboard:", err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    return activePoll(fetchData, 30000);
  }, [fetchData]);

  // ── Filter + Sort ──
  const filtered = useMemo(() => {
    let list = [...entries];

    if (search) {
      const q = search.toLowerCase();
      list = list.filter((e) => e.userAddress.toLowerCase().includes(q));
    }

    list.sort((a, b) => {
      let cmp = 0;
      switch (sortKey) {
        case "rank": cmp = a.rank - b.rank; break;
        case "pnl": cmp = parse18(a.totalRealizedPnl) - parse18(b.totalRealizedPnl); break;
        case "trades": cmp = a.totalTrades - b.totalTrades; break;
        case "winRate": cmp = (a.winRate ?? 0) - (b.winRate ?? 0); break;
        case "avgHoldTime": cmp = parseInt(a.avgHoldTime || "0") - parseInt(b.avgHoldTime || "0"); break;
        case "volume": cmp = parse18(a.totalVolume) - parse18(b.totalVolume); break;
      }
      return sortDir === "asc" ? cmp : -cmp;
    });

    return list;
  }, [entries, search, sortKey, sortDir]);

  const paginated = useMemo(() => {
    return filtered.slice(page * PAGE_SIZE, (page + 1) * PAGE_SIZE);
  }, [filtered, page]);

  const totalPages = Math.ceil(filtered.length / PAGE_SIZE);

  const toggleSort = (key: SortKey) => {
    if (sortKey === key) setSortDir(sortDir === "asc" ? "desc" : "asc");
    else { setSortKey(key); setSortDir(key === "rank" ? "asc" : "desc"); }
  };

  const sortIcon = (key: SortKey) => {
    if (sortKey !== key) return null;
    return sortDir === "asc"
      ? <ChevronUp className="h-3 w-3 inline" />
      : <ChevronDown className="h-3 w-3 inline" />;
  };

  // ── Summary stats ──
  const stats = useMemo(() => {
    if (entries.length === 0) return null;
    const totalTraders = entries.length;
    const totalPnl = entries.reduce((sum, e) => sum + parse18(e.totalRealizedPnl), 0);
    const totalTrades = entries.reduce((sum, e) => sum + e.totalTrades, 0);
    const avgWinRate = entries.reduce((sum, e) => sum + (e.winRate ?? 0), 0) / entries.length;
    return { totalTraders, totalPnl, totalTrades, avgWinRate };
  }, [entries]);

  return (
    <div className="flex flex-col h-full bg-background text-foreground overflow-hidden">
      <main className="flex-1 min-h-0 overflow-y-auto max-w-6xl mx-auto w-full p-4 md:p-6">
        {/* Page Header */}
        <div className="mb-6">
          <div className="flex items-center gap-2 mb-1">
            <Trophy className="h-6 w-6 text-amber-400" />
            <h1 className="text-2xl font-bold">Leaderboard</h1>
          </div>
          <p className="text-sm text-muted-foreground">Top traders on HyperPax ranked by PnL</p>
        </div>

        {/* Summary stats */}
        {stats && (
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 mb-6">
            <div className="rounded-[var(--radius-lg)] bg-surface-1 p-3">
              <div className="flex items-center gap-1.5 mb-1">
                <Users className="h-3.5 w-3.5 text-muted-foreground" />
                <span className="text-[10px] text-muted-foreground uppercase tracking-wider font-medium">Traders</span>
              </div>
              <p className="text-lg font-bold font-mono">{stats.totalTraders.toLocaleString()}</p>
            </div>
            <div className="rounded-[var(--radius-lg)] bg-surface-1 p-3">
              <div className="flex items-center gap-1.5 mb-1">
                <TrendingUp className="h-3.5 w-3.5 text-muted-foreground" />
                <span className="text-[10px] text-muted-foreground uppercase tracking-wider font-medium">Total PnL</span>
              </div>
              <p className={cn("text-lg font-bold font-mono", stats.totalPnl >= 0 ? "text-emerald-500" : "text-red-500")}>
                {stats.totalPnl >= 0 ? "+" : ""}{fmtUsd(stats.totalPnl)}
              </p>
            </div>
            <div className="rounded-[var(--radius-lg)] bg-surface-1 p-3">
              <div className="flex items-center gap-1.5 mb-1">
                <BarChart3 className="h-3.5 w-3.5 text-muted-foreground" />
                <span className="text-[10px] text-muted-foreground uppercase tracking-wider font-medium">Total Trades</span>
              </div>
              <p className="text-lg font-bold font-mono">{stats.totalTrades.toLocaleString()}</p>
            </div>
            <div className="rounded-[var(--radius-lg)] bg-surface-1 p-3">
              <div className="flex items-center gap-1.5 mb-1">
                <Medal className="h-3.5 w-3.5 text-muted-foreground" />
                <span className="text-[10px] text-muted-foreground uppercase tracking-wider font-medium">Avg Win Rate</span>
              </div>
              <p className="text-lg font-bold font-mono">{fmtWinRate(stats.avgWinRate)}</p>
            </div>
          </div>
        )}

        {/* Search */}
        <div className="flex items-center gap-3 mb-4">
          <div className="relative flex-1 max-w-sm">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
            <input
              className="w-full rounded-[var(--radius-md)] bg-surface-2 pl-9 pr-3 py-1.5 text-sm"
              placeholder="Search by address..."
              value={search}
              onChange={(e) => { setSearch(e.target.value); setPage(0); }}
            />
          </div>
        </div>

        {/* Leaderboard Table */}
        {loading && entries.length === 0 ? (
          <div className="flex items-center justify-center py-20">
            <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
          </div>
        ) : entries.length === 0 ? (
          <div className="rounded-[var(--radius-lg)] bg-surface-1 py-20 text-center">
            <Trophy className="h-10 w-10 text-muted-foreground mx-auto mb-3" />
            <p className="text-sm text-muted-foreground">No leaderboard data available yet</p>
            <p className="text-xs text-muted-foreground mt-1">Start trading to appear on the leaderboard</p>
          </div>
        ) : (
          <div className="rounded-[var(--radius-lg)] bg-surface-1 overflow-hidden">
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-white/[0.06] text-muted-foreground">
                    <th
                      className="text-left px-4 py-3 font-medium cursor-pointer hover:text-foreground w-16"
                      onClick={() => toggleSort("rank")}
                    >
                      <span className="flex items-center gap-1">Rank {sortIcon("rank")}</span>
                    </th>
                    <th className="text-left px-4 py-3 font-medium">Address</th>
                    <th
                      className="text-right px-4 py-3 font-medium cursor-pointer hover:text-foreground"
                      onClick={() => toggleSort("pnl")}
                    >
                      <span className="flex items-center gap-1 justify-end">PnL {sortIcon("pnl")}</span>
                    </th>
                    <th
                      className="text-right px-4 py-3 font-medium cursor-pointer hover:text-foreground"
                      onClick={() => toggleSort("trades")}
                    >
                      <span className="flex items-center gap-1 justify-end">Total Trades {sortIcon("trades")}</span>
                    </th>
                    <th
                      className="text-right px-4 py-3 font-medium cursor-pointer hover:text-foreground"
                      onClick={() => toggleSort("winRate")}
                    >
                      <span className="flex items-center gap-1 justify-end">Win Rate {sortIcon("winRate")}</span>
                    </th>
                    <th
                      className="text-right px-4 py-3 font-medium cursor-pointer hover:text-foreground hidden md:table-cell"
                      onClick={() => toggleSort("avgHoldTime")}
                    >
                      <span className="flex items-center gap-1 justify-end">Avg Hold {sortIcon("avgHoldTime")}</span>
                    </th>
                    <th
                      className="text-right px-4 py-3 font-medium cursor-pointer hover:text-foreground hidden lg:table-cell"
                      onClick={() => toggleSort("volume")}
                    >
                      <span className="flex items-center gap-1 justify-end">Volume {sortIcon("volume")}</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {paginated.map((entry) => {
                    const pnl = parse18(entry.totalRealizedPnl);
                    const volume = parse18(entry.totalVolume);
                    return (
                      <tr
                        key={entry.userAddress}
                        className="border-b border-white/[0.04] hover:bg-white/[0.03] transition-colors"
                      >
                        <td className="px-4 py-3">
                          <span className={cn(
                            "inline-flex items-center justify-center w-7 h-7 rounded-lg text-xs font-bold border",
                            rankBadgeClass(entry.rank)
                          )}>
                            {entry.rank}
                          </span>
                        </td>
                        <td className="px-4 py-3">
                          <span className="font-mono text-sm">{shortAddr(entry.userAddress)}</span>
                        </td>
                        <td className={cn(
                          "px-4 py-3 text-right font-mono font-semibold",
                          pnl >= 0 ? "text-emerald-500" : "text-red-500"
                        )}>
                          {pnl >= 0 ? "+" : ""}{fmtUsd(pnl)}
                        </td>
                        <td className="px-4 py-3 text-right font-mono">
                          {entry.totalTrades.toLocaleString()}
                        </td>
                        <td className="px-4 py-3 text-right font-mono">
                          <span className={cn(
                            (entry.winRate ?? 0) >= 0.5 ? "text-emerald-500" : "text-muted-foreground"
                          )}>
                            {fmtWinRate(entry.winRate)}
                          </span>
                        </td>
                        <td className="px-4 py-3 text-right font-mono text-muted-foreground hidden md:table-cell">
                          {fmtDuration(entry.avgHoldTime)}
                        </td>
                        <td className="px-4 py-3 text-right font-mono text-muted-foreground hidden lg:table-cell">
                          {volume > 0 ? fmtUsd(volume) : "\u2014"}
                        </td>
                      </tr>
                    );
                  })}
                  {paginated.length === 0 && (
                    <tr>
                      <td colSpan={7} className="py-12 text-center text-muted-foreground">
                        {search ? "No traders match your search" : "No data available"}
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>

            {/* Pagination */}
            {totalPages > 1 && (
              <div className="flex items-center justify-between px-4 py-3 border-t border-white/[0.06]">
                <p className="text-xs text-muted-foreground">
                  Showing {page * PAGE_SIZE + 1}-{Math.min((page + 1) * PAGE_SIZE, filtered.length)} of {filtered.length}
                </p>
                <div className="flex items-center gap-1">
                  <Button
                    size="sm"
                    variant="outline"
                    className="h-7 text-xs px-2"
                    disabled={page === 0}
                    onClick={() => setPage(page - 1)}
                  >
                    Prev
                  </Button>
                  {Array.from({ length: Math.min(totalPages, 5) }, (_, i) => {
                    const pageNum = page < 3 ? i : page - 2 + i;
                    if (pageNum >= totalPages) return null;
                    return (
                      <Button
                        key={pageNum}
                        size="sm"
                        variant={pageNum === page ? "default" : "outline"}
                        className="h-7 text-xs px-2 min-w-[28px]"
                        onClick={() => setPage(pageNum)}
                      >
                        {pageNum + 1}
                      </Button>
                    );
                  })}
                  <Button
                    size="sm"
                    variant="outline"
                    className="h-7 text-xs px-2"
                    disabled={page >= totalPages - 1}
                    onClick={() => setPage(page + 1)}
                  >
                    Next
                  </Button>
                </div>
              </div>
            )}
          </div>
        )}
      </main>
    </div>
  );
}
