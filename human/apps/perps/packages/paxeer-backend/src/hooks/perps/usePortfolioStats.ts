"use client";

import { useEffect, useMemo, useState } from "react";
import { usePerpsWallet } from "./usePerpsWallet";
import { useRealtimePrices } from "./useRealtimePrices";
import { subgraphQuery, POSITIONS_QUERY, POSITION_CHANGED_QUERY } from "./useSubgraph";
import { computePositionPnl, type ParsedPosition } from "@paxeer-network/paxeer-backend/core/perps/pnlHelpers";
import { getVaultAddress } from "./useVault";
import { getPublicClient } from "@paxeer-network/paxeer-backend/core/viem/clients";
import { TradingAccountABI as UserVaultAbi } from "@paxeer-network/paxeer-backend/core/network/abi/TradingAccount";
import { PERPS_MARKETS, COLLATERAL_TOKENS, INDEXER_GRAPHQL } from "@paxeer-network/paxeer-backend/core/perps/config";
import { subscribeStream } from "@paxeer-network/paxeer-backend/core/graphql/client";
import { activePoll, isPageActive, onStreamResync, type CancelTask } from "@paxeer-network/paxeer-backend/core/net/realtime";

const ZERO_ADDR = "0x0000000000000000000000000000000000000000" as `0x${string}`;
const MULTICALL3 = "0xCc4a067A6B7fD2cdeFB354A26Ff5d9b336bbE0AF" as `0x${string}`;

// Vault address is immutable once created — cache indefinitely per user address
const _vaultAddrCache = new Map<string, `0x${string}`>();

const ALL_TOKENS = Object.values(COLLATERAL_TOKENS);

export interface PortfolioStats {
  totalVaultUsd: number;
  freeCollateral: number;
  unrealizedPnl: number;
  totalLeverage: number;
  isLoading: boolean;
  positions: any[];
}

function parse18(val: string | null | undefined): number {
  if (!val) return 0;
  return parseFloat(val) / 1e18;
}

/* ══════════════════════════════════════════════════════════════════════
   Module-level singleton — one vault poller (15s) + one positions poller
   (5s) per connected address, shared by all subscribers.
   ══════════════════════════════════════════════════════════════════════ */

interface RawSnapshot {
  positions: any[];
  vaultUsd: number;
  lockedUsd: number;
  isLoading: boolean;
}

const EMPTY_RAW: RawSnapshot = {
  positions: [],
  vaultUsd: 0,
  lockedUsd: 0,
  isLoading: false,
};

let _raw: RawSnapshot = EMPTY_RAW;
const _subs = new Set<(s: RawSnapshot) => void>();
let _activeAddress: string | null = null;
let _vaultTimer: CancelTask | null = null;
let _resyncCleanup: CancelTask | null = null;
let _posUnsubscribe: (() => void) | null = null;

// ── Locally-closed position IDs ────────────────────────────────────────
// Tracks positionIds the user closed during this browser session so the
// rows stay hidden in the UI even when:
//   • the user navigates between /perps and /portfolio (component
//     unmount destroys local state — but this Set is module-level)
//   • the user hard-reloads while the indexer hasn't yet processed the
//     close (sessionStorage rehydrates the Set on next load)
//
// Self-healing: each time fresh raw positions arrive from the indexer
// we drop any ID in this Set that the indexer has already removed from
// its result. So the list shrinks naturally once the backend catches up.
const _locallyClosed = new Set<string>();
const _LOCAL_CLOSED_SS_KEY = "perps.locallyClosed.v1";

if (typeof window !== "undefined") {
  try {
    const saved = sessionStorage.getItem(_LOCAL_CLOSED_SS_KEY);
    if (saved) JSON.parse(saved).forEach((id: string) => _locallyClosed.add(String(id)));
  } catch {}
}

function _persistLocallyClosed() {
  if (typeof window === "undefined") return;
  try {
    sessionStorage.setItem(_LOCAL_CLOSED_SS_KEY, JSON.stringify([..._locallyClosed]));
  } catch {}
}

function _reconcileLocallyClosed(rawPositions: any[]) {
  if (_locallyClosed.size === 0) return;
  const live = new Set(rawPositions.map((p: any) => String(p.positionId)));
  let changed = false;
  for (const id of [..._locallyClosed]) {
    if (!live.has(id)) {
      _locallyClosed.delete(id);
      changed = true;
    }
  }
  if (changed) _persistLocallyClosed();
}

/**
 * Mark a position as locally closed. Hides it from `usePortfolioStats().positions`
 * everywhere (perps panel, portfolio panel) until the indexer confirms the
 * close and removes it from its result. Persisted to sessionStorage so it
 * survives page reloads.
 */
export function markPositionLocallyClosed(positionId: string | bigint) {
  _locallyClosed.add(String(positionId));
  _persistLocallyClosed();
  _notify();
}

/** Roll back a `markPositionLocallyClosed` call (e.g. on close-tx failure). */
export function unmarkPositionLocallyClosed(positionId: string | bigint) {
  if (_locallyClosed.delete(String(positionId))) {
    _persistLocallyClosed();
    _notify();
  }
}

function _notify() {
  // Spread on every notify so React subscribers always see a new object
  // reference — needed because `markPositionLocallyClosed` doesn't mutate
  // `_raw` itself but still requires consumers to re-derive their filtered
  // positions list.
  const snapshot = { ..._raw };
  _subs.forEach((cb) => cb(snapshot));
}

function _setRaw(patch: Partial<RawSnapshot>) {
  _raw = { ..._raw, ...patch };
  _notify();
}

async function _fetchVault(addr: string) {
  if (!isPageActive()) return;
  try {
    // Vault address never changes — use module-level cache after first lookup
    let vault = _vaultAddrCache.get(addr);
    if (!vault) {
      vault = await getVaultAddress(addr as `0x${string}`);
      if (_activeAddress !== addr) return;
      if (vault === ZERO_ADDR) return;
      _vaultAddrCache.set(addr, vault);
    }
    if (_activeAddress !== addr) return;

    // Batch all getBalance + getLockedBalance calls into a single multicall
    const client = getPublicClient();
    const calls = ALL_TOKENS.flatMap((tok) => [
      { address: vault as `0x${string}`, abi: UserVaultAbi as any, functionName: "getBalance",       args: [tok.address] },
      { address: vault as `0x${string}`, abi: UserVaultAbi as any, functionName: "getLockedBalance", args: [tok.address] },
    ]);
    const results = await client.multicall({
      multicallAddress: MULTICALL3,
      contracts: calls as any,
      allowFailure: true,
    });

    if (_activeAddress !== addr) return;
    let totalBal = 0, totalLocked = 0;
    for (let i = 0; i < ALL_TOKENS.length; i++) {
      const tok = ALL_TOKENS[i];
      const balRes  = results[i * 2];
      const lockRes = results[i * 2 + 1];
      if (balRes.status  === "success") totalBal    += Number(balRes.result  as bigint) / 10 ** tok.decimals;
      if (lockRes.status === "success") totalLocked += Number(lockRes.result as bigint) / 10 ** tok.decimals;
    }
    _setRaw({ vaultUsd: totalBal, lockedUsd: totalLocked });
  } catch {}
}

let _ownerlessWarned = false;

async function _fetchPositionsOnce(addr: string) {
  try {
    const res = await subgraphQuery<{ positions: any[] }>(POSITIONS_QUERY, { userAddress: addr });
    if (_activeAddress !== addr) return;
    const positions = res.positions ?? [];
    _reconcileLocallyClosed(positions);
    _setRaw({ positions });
  } catch {}
}

/**
 * Force-refresh the positions list from the subgraph (HTTP) — used
 * after a close / partial-close / margin / size action when the UI
 * shouldn't have to wait for the WS `positionChanged` push to land.
 *
 * Issues an immediate HTTP fetch and schedules two backoff retries
 * (2 s and 6 s) so we still catch the indexer's state once it has
 * processed the on-chain event, even if it lagged behind our first
 * fetch. Silent: no spinner, no UI signal.
 *
 * Reads from `_activeAddress` snapshot at call time so unmounting or
 * switching accounts mid-flight cancels remaining retries cleanly.
 */
export function refreshPositions() {
  const addr = _activeAddress;
  if (!addr) return;
  _fetchPositionsOnce(addr);
  for (const delay of [2_000, 6_000]) {
    setTimeout(() => {
      if (_activeAddress === addr) _fetchPositionsOnce(addr);
    }, delay);
  }
}

function _stopPolling() {
  if (_vaultTimer)    { _vaultTimer(); _vaultTimer = null; }
  _resyncCleanup?.(); _resyncCleanup = null;
  if (_posUnsubscribe) { _posUnsubscribe(); _posUnsubscribe = null; }
}

function _startPolling(addr: string) {
  _stopPolling();
  _activeAddress = addr;
  _setRaw({ isLoading: true });
  Promise.all([_fetchVault(addr), _fetchPositionsOnce(addr)]).finally(() => {
    if (_activeAddress === addr) _setRaw({ isLoading: false });
  });
  _vaultTimer = activePoll(() => _fetchVault(addr), 15_000, false);
  _resyncCleanup = onStreamResync(url => {
    if (url === INDEXER_GRAPHQL.wsUrl && _activeAddress === addr) { void _fetchPositionsOnce(addr); void _fetchVault(addr); }
  });
  // Subscribe to positionChanged — server pushes full position list on each change.
  // The initial query above fills the gap while the WS handshake completes.
  _posUnsubscribe = subscribeStream<{ positionChanged: any[] }>({
    endpoint: INDEXER_GRAPHQL,
    query: POSITION_CHANGED_QUERY,
    variables: { userAddress: addr },
    onData: (data) => {
      if (_activeAddress !== addr) return;
      const positions = data.positionChanged ?? [];
      if (positions.some((p: any) => !p?.userAddress)) {
        if (!_ownerlessWarned) {
          _ownerlessWarned = true;
          console.warn("positionChanged rows without userAddress ignored; keeping previous positions");
        }
        return;
      }
      _reconcileLocallyClosed(positions);
      _setRaw({ positions });
    },
  });
}

function _reset() {
  _stopPolling();
  _activeAddress = null;
  _raw = EMPTY_RAW;
  _notify();
}

/**
 * Lightweight portfolio stats hook — shared singleton pollers.
 * - Positions fetched from subgraph (no RPC), 5s interval, shared
 * - Vault balance: 2 RPC reads every 15s, shared
 * - Mark prices from REST tickers via useRealtimePrices (also singleton)
 */
export function usePortfolioStats(): PortfolioStats {
  const { isConnected, address } = usePerpsWallet();
  const [raw, setRaw] = useState<RawSnapshot>(_raw);
  const { tickers } = useRealtimePrices({ enabled: isConnected && raw.positions.length > 0 });

  const wantedAddress = isConnected && address ? address : null;

  useEffect(() => {
    _subs.add(setRaw);
    // Sync in case snapshot changed between render and subscribe
    if (raw !== _raw) setRaw(_raw);

    if (wantedAddress) {
      if (_activeAddress !== wantedAddress) _startPolling(wantedAddress);
    } else {
      if (_activeAddress !== null) _reset();
    }

    return () => {
      _subs.delete(setRaw);
      if (_subs.size === 0) _reset();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [wantedAddress]);

  // Hide IDs the user closed locally until the indexer drops them. The
  // filtering happens here (not in `_setRaw`) so we always keep the raw
  // indexer response intact for reconciliation, while consumers see only
  // positions that should still be open from the user's perspective.
  const visiblePositions = useMemo(
    () => raw.positions.filter((p: any) => !_locallyClosed.has(String(p.positionId))),
    [raw],
  );

  const { unrealizedPnl, totalLeverage } = useMemo(() => {
    if (visiblePositions.length === 0) return { unrealizedPnl: 0, totalLeverage: 0 };
    let totalPnl = 0;
    let weightedLev = 0;
    let totalSize = 0;
    for (const pos of visiblePositions) {
      const sym = PERPS_MARKETS.find((m) => m.marketId === Number(pos.marketId))?.symbol ?? "";
      const markPrice = tickers[sym]?.mark_price ?? 0;
      const parsed: ParsedPosition = {
        positionId: BigInt(pos.positionId),
        marketId: Number(pos.marketId),
        symbol: sym,
        isLong: pos.isLong,
        size: parse18(pos.sizeUsd),
        collateral: parse18(pos.collateralUsd),
        leverage: parse18(pos.leverage),
        entryPrice: parse18(pos.entryPrice),
        collateralToken: pos.collateralToken ?? "",
        collateralAmount: parse18(pos.collateralAmount),
        openedAt: pos.openedAt,
        openTxHash: pos.openTxHash,
      };
      if (markPrice > 0) {
        const pnlData = computePositionPnl(parsed, markPrice);
        totalPnl += pnlData.unrealizedPnl;
      }
      weightedLev += parsed.leverage * parsed.size;
      totalSize += parsed.size;
    }
    return {
      unrealizedPnl: totalPnl,
      totalLeverage: totalSize > 0 ? weightedLev / totalSize : 0,
    };
  }, [visiblePositions, tickers]);

  return {
    totalVaultUsd: raw.vaultUsd,
    freeCollateral: Math.max(0, raw.vaultUsd - raw.lockedUsd),
    unrealizedPnl,
    totalLeverage,
    isLoading: raw.isLoading,
    positions: visiblePositions,
  };
}
