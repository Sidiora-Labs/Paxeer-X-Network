"use client";
import { isPageActive, scheduleActive, watchSocket, type CancelTask } from "@paxeer-network/paxeer-backend/core/net/realtime";

import { useEffect, useRef, useState } from "react";

/* ══════════════════════════════════════════════════════════════════════
   Explorer real-time WS hook.

   Protocol (confirmed from live wscat session):

   On subscribe → server immediately pushes a snapshot frame:
     { topic: "chain@blocks", data: [...array of blocks...], snapshot: true }

   Subsequent live frames carry a single object:
     { topic: "chain@blocks", data: { block_number, time (ISO string),
                                       proposer, hash, transactions } }

   The same data/snapshot duality applies to chain@transactions.

   `time` is an ISO 8601 string ("2026-05-12T16:11:09.515Z"), not epoch ms.

   No separate REST seed is required — the WS snapshot pre-fills the list
   within ~200 ms of connection, keeping the page never empty.
   ══════════════════════════════════════════════════════════════════════ */


// The gateway permits the paxeer-network browser origin; no client secret is shipped.

const EXPLORER_WS_URL = `wss://data.feed.chainflowtrading.com/ws/explorer`;

const MAX_BLOCKS        = 50;
const MAX_TXS           = 100;
const RECONNECT_INITIAL = 500;
const RECONNECT_MAX     = 60_000;
const PING_INTERVAL     = 25_000;

// ── Public types ───────────────────────────────────────────────────────

export interface ExplorerBlock {
  block_number: number;
  /** Unix milliseconds. */
  time: number;
  proposer: string;
  hash: string;
  /** Total transaction count in this block. */
  transactions: number;
}

export interface ExplorerTx {
  hash: string;
  /** "Place Order" | "Cancel Order" | "Modify Order" | "OROB Fill" |
   *  "Oracle Updated" | "Sealed Batch Executed" */
  action: string;
  block: number;
  /** Unix milliseconds. */
  time: number;
  user: string;
  symbol?: string;
  side?: string;
  price?: number;
  size?: number;
}

export interface UseExplorerStreamResult {
  blocks: ExplorerBlock[];
  txs: ExplorerTx[];
  connected: boolean;
  /** True while the initial REST seed fetch is in-flight. */
  seedLoading: boolean;
}

// ── Normalizers ────────────────────────────────────────────────────────

/** Parse ISO 8601 or unix-ms strings/numbers to epoch milliseconds. */
function parseTime(raw: unknown): number {
  if (typeof raw === "string") {
    const ms = new Date(raw).getTime();
    return Number.isFinite(ms) ? ms : Date.now();
  }
  if (typeof raw === "number") {
    // If looks like unix-seconds (< year 3000 in seconds), convert.
    return raw < 1e12 ? raw * 1000 : raw;
  }
  return Date.now();
}

function normalizeBlock(d: any): ExplorerBlock | null {
  const hash = String(d?.hash ?? "");
  if (!hash) return null;
  return {
    block_number: Number(d.block_number ?? d.height ?? 0),
    time:         parseTime(d.time ?? d.timestamp),
    proposer:     String(d.proposer ?? ""),
    hash,
    transactions: Number(d.transactions ?? d.transactions_count ?? 0),
  };
}

function normalizeTx(d: any): ExplorerTx | null {
  const hash = String(d?.hash ?? "");
  if (!hash) return null;
  return {
    hash,
    action:  String(d.action ?? ""),
    block:   Number(d.block  ?? 0),
    time:    parseTime(d.time ?? d.timestamp),
    user:    String(d.user   ?? ""),
    symbol:  d.symbol != null ? String(d.symbol) : undefined,
    side:    d.side   != null ? String(d.side)   : undefined,
    price:   d.price  != null ? Number(d.price)  : undefined,
    size:    d.size   != null ? Number(d.size)   : undefined,
  };
}

// ── Hook ──────────────────────────────────────────────────────────────

export function useExplorerStream(): UseExplorerStreamResult {
  const [blocks, setBlocks]           = useState<ExplorerBlock[]>([]);
  const [txs, setTxs]                 = useState<ExplorerTx[]>([]);
  const [connected, setConnected]     = useState(false);
  const [seedLoading, setSeedLoading] = useState(true);

  const wsRef             = useRef<WebSocket | null>(null);
  const reconnectTimerRef = useRef<CancelTask | null>(null);
  const pingTimerRef      = useRef<ReturnType<typeof setInterval> | null>(null);
  const reconnectDelayRef = useRef(RECONNECT_INITIAL);
  const mountedRef        = useRef(true);

  useEffect(() => {
    mountedRef.current = true;

    function cleanup() {
      if (pingTimerRef.current)      { clearInterval(pingTimerRef.current);  pingTimerRef.current = null; }
      if (reconnectTimerRef.current) { reconnectTimerRef.current(); reconnectTimerRef.current = null; }
      if (wsRef.current)             { wsRef.current.close(); wsRef.current = null; }
    }

    function scheduleReconnect() {
      if (reconnectTimerRef.current || !mountedRef.current) return;
      const delay = reconnectDelayRef.current;
      reconnectTimerRef.current = scheduleActive(() => {
        reconnectTimerRef.current = null;
        reconnectDelayRef.current = Math.min(RECONNECT_MAX, reconnectDelayRef.current * 2);
        connect();
      }, delay, EXPLORER_WS_URL);
    }

    function connect() {
      if (!mountedRef.current) return;
      if (!isPageActive()) { scheduleReconnect(); return; }
      let ws: WebSocket;
      try {
        ws = new WebSocket(EXPLORER_WS_URL);
      } catch {
        scheduleReconnect();
        return;
      }
      wsRef.current = ws;
      watchSocket(ws, () => { reconnectDelayRef.current = RECONNECT_INITIAL; });

      ws.addEventListener("open", () => {
        if (!mountedRef.current) { ws.close(); return; }
        setConnected(true);
        try {
          ws.send(JSON.stringify({ event: "subscribe", topic: "chain@blocks" }));
          ws.send(JSON.stringify({ event: "subscribe", topic: "chain@transactions" }));
        } catch { /* socket may have closed between open and first send */ }

        pingTimerRef.current = setInterval(() => {
          if (ws.readyState === WebSocket.OPEN) {
            try { ws.send(JSON.stringify({ event: "ping" })); } catch { /* ignore */ }
          }
        }, PING_INTERVAL);
      });

      ws.addEventListener("message", (ev) => {
        if (!mountedRef.current) return;
        let msg: any;
        try { msg = JSON.parse(ev.data as string); } catch { return; }
        if (!msg || !msg.topic || !msg.data) return;

        if (msg.topic === "chain@blocks") {
          if (Array.isArray(msg.data)) {
            // Snapshot frame — bulk-insert; sort descending by block_number.
            const items = (msg.data as any[]).map(normalizeBlock).filter(Boolean) as ExplorerBlock[];
            items.sort((a, b) => b.block_number - a.block_number);
            setBlocks(items.slice(0, MAX_BLOCKS));
            if (mountedRef.current) setSeedLoading(false);
          } else {
            // Live single-block frame.
            const block = normalizeBlock(msg.data);
            if (!block) return;
            setBlocks((prev) => {
              if (prev.some((b) => b.hash === block.hash || b.block_number === block.block_number)) return prev;
              return [block, ...prev].slice(0, MAX_BLOCKS);
            });
            if (mountedRef.current) setSeedLoading(false);
          }
        }

        if (msg.topic === "chain@transactions") {
          if (Array.isArray(msg.data)) {
            // Snapshot frame.
            const items = (msg.data as any[]).map(normalizeTx).filter(Boolean) as ExplorerTx[];
            setTxs(items.slice(0, MAX_TXS));
          } else {
            // Live single-tx frame.
            const tx = normalizeTx(msg.data);
            if (!tx) return;
            setTxs((prev) => {
              if (prev.some((t) => t.hash === tx.hash)) return prev;
              return [tx, ...prev].slice(0, MAX_TXS);
            });
          }
        }
      });

      const onDisconnect = () => {
        if (pingTimerRef.current) { clearInterval(pingTimerRef.current); pingTimerRef.current = null; }
        setConnected(false);
        if (wsRef.current === ws) wsRef.current = null;
        scheduleReconnect();
      };
      ws.addEventListener("close", onDisconnect);
      ws.addEventListener("error", () => { /* close will fire too */ });
    }

    connect();

    return () => {
      mountedRef.current = false;
      cleanup();
      setConnected(false);
    };
  }, []);

  return { blocks, txs, connected, seedLoading };
}
