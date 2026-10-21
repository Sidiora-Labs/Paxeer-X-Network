import type { NextApiRequest, NextApiResponse } from "next";

const TTL_MS = 15_000;
const HISTORY_SLOTS = 12;
const HISTORY_INTERVAL_MS = 60_000;
const MAX_SESSIONS = 50_000;

interface Session {
  market: string;
  expiresAt: number;
  joinedAt: number;
}

interface HistorySlot {
  at: number;
  total: number;
  byMarket: Record<string, number>;
}

interface PresenceSnapshot {
  total: number;
  byMarket: Record<string, number>;
  top: Array<{ market: string; count: number; delta5m: number }>;
  rising: Array<{ market: string; count: number; deltaPct: number }>;
  history: Array<{ at: number; total: number }>;
  lastUpdate: string;
  window: { ttlSeconds: number; historyMinutes: number };
}

type GlobalBag = typeof globalThis & {
  __paxPresence?: {
    sessions: Map<string, Session>;
    history: HistorySlot[];
    lastHistoryAt: number;
  };
};

function getStore() {
  const g = globalThis as GlobalBag;
  if (!g.__paxPresence) {
    g.__paxPresence = {
      sessions: new Map(),
      history: [],
      lastHistoryAt: 0,
    };
  }
  return g.__paxPresence;
}

function normalizeMarket(raw: unknown): string {
  if (typeof raw !== "string") return "__home__";
  const trimmed = raw.trim().toUpperCase();
  if (!trimmed) return "__home__";
  if (trimmed === "HOME") return "__home__";
  if (!/^[A-Z0-9_/\-]{1,16}$/.test(trimmed)) return "__home__";
  return trimmed;
}

function purge(now: number) {
  const { sessions } = getStore();
  for (const [id, s] of sessions) {
    if (s.expiresAt <= now) sessions.delete(id);
  }
}

function snapshotCounts(now: number) {
  const { sessions } = getStore();
  let total = 0;
  const byMarket: Record<string, number> = {};
  for (const [, s] of sessions) {
    if (s.expiresAt <= now) continue;
    total += 1;
    byMarket[s.market] = (byMarket[s.market] ?? 0) + 1;
  }
  return { total, byMarket };
}

function maybeRecordHistory(now: number) {
  const store = getStore();
  if (now - store.lastHistoryAt < HISTORY_INTERVAL_MS) return;
  const counts = snapshotCounts(now);
  store.history.push({ at: now, total: counts.total, byMarket: counts.byMarket });
  while (store.history.length > HISTORY_SLOTS) store.history.shift();
  store.lastHistoryAt = now;
}

function computeSnapshot(now: number): PresenceSnapshot {
  const store = getStore();
  const { total, byMarket } = snapshotCounts(now);

  const past5m = store.history.find((slot) => now - slot.at >= 5 * 60_000);

  const entries = Object.entries(byMarket).filter(([m]) => m !== "__home__");

  const top = entries
    .map(([market, count]) => {
      const prev = past5m?.byMarket[market] ?? 0;
      return { market, count, delta5m: count - prev };
    })
    .sort((a, b) => b.count - a.count)
    .slice(0, 8);

  const rising = entries
    .map(([market, count]) => {
      const prev = past5m?.byMarket[market] ?? 0;
      const deltaPct = prev > 0 ? ((count - prev) / prev) * 100 : count > 0 ? 100 : 0;
      return { market, count, deltaPct };
    })
    .filter((r) => r.count >= 2 && r.deltaPct > 0)
    .sort((a, b) => b.deltaPct - a.deltaPct)
    .slice(0, 5);

  return {
    total,
    byMarket,
    top,
    rising,
    history: store.history.map((slot) => ({ at: slot.at, total: slot.total })),
    lastUpdate: new Date(now).toISOString(),
    window: {
      ttlSeconds: Math.round(TTL_MS / 1000),
      historyMinutes: Math.round((HISTORY_SLOTS * HISTORY_INTERVAL_MS) / 60_000),
    },
  };
}

export default function handler(req: NextApiRequest, res: NextApiResponse) {
  res.setHeader("Cache-Control", "no-store, max-age=0");

  const now = Date.now();
  purge(now);

  if (req.method === "POST") {
    const body = (typeof req.body === "string" ? safeJson(req.body) : req.body) ?? {};
    const sessionId = typeof body.sessionId === "string" ? body.sessionId.slice(0, 64) : null;
    const market = normalizeMarket(body.market);

    if (!sessionId || !/^[A-Za-z0-9_-]{8,64}$/.test(sessionId)) {
      return res.status(400).json({ error: "invalid sessionId" });
    }

    const store = getStore();
    if (!store.sessions.has(sessionId) && store.sessions.size >= MAX_SESSIONS) {
      let oldest: string | null = null;
      let oldestAt = Infinity;
      for (const [id, s] of store.sessions) {
        if (s.expiresAt < oldestAt) {
          oldestAt = s.expiresAt;
          oldest = id;
        }
      }
      if (oldest) store.sessions.delete(oldest);
    }

    const existing = store.sessions.get(sessionId);
    store.sessions.set(sessionId, {
      market,
      expiresAt: now + TTL_MS,
      joinedAt: existing?.joinedAt ?? now,
    });

    maybeRecordHistory(now);
    return res.status(200).json({ ok: true });
  }

  if (req.method === "GET") {
    maybeRecordHistory(now);
    return res.status(200).json(computeSnapshot(now));
  }

  res.setHeader("Allow", "GET, POST");
  return res.status(405).json({ error: "method not allowed" });
}

function safeJson(raw: string): Record<string, unknown> | null {
  try {
    const v = JSON.parse(raw);
    return v && typeof v === "object" && !Array.isArray(v) ? v : null;
  } catch {
    return null;
  }
}
