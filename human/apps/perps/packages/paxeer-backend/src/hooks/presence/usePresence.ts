"use client";
import { fetchWithPolicy as fetch } from "@paxeer-network/paxeer-backend/core/net/fetch";

import { useEffect, useRef, useState } from "react";

const SESSION_KEY = "pax-presence-session";
const HEARTBEAT_INTERVAL_MS = 5_000;
const SNAPSHOT_INTERVAL_MS = 4_000;

export interface PresenceSnapshot {
  total: number;
  byMarket: Record<string, number>;
  top: Array<{ market: string; count: number; delta5m: number }>;
  rising: Array<{ market: string; count: number; deltaPct: number }>;
  history: Array<{ at: number; total: number }>;
  lastUpdate: string;
  window: { ttlSeconds: number; historyMinutes: number };
}

function getOrCreateSessionId(): string {
  if (typeof window === "undefined") return "";
  try {
    const existing = window.localStorage.getItem(SESSION_KEY);
    if (existing && /^[A-Za-z0-9_-]{8,64}$/.test(existing)) return existing;
  } catch {}
  const rand = crypto.randomUUID ? crypto.randomUUID() : fallbackId();
  const id = rand.replace(/-/g, "").slice(0, 24);
  try {
    window.localStorage.setItem(SESSION_KEY, id);
  } catch {}
  return id;
}

function fallbackId(): string {
  return (
    Math.random().toString(36).slice(2) +
    Date.now().toString(36) +
    Math.random().toString(36).slice(2)
  );
}

/**
 * Fires a heartbeat every 5s tagging the caller with the market they're
 * viewing. Server-side TTL is 15s so missing two heartbeats drops the user.
 * Pass `"home"` (or nothing) when on the landing page.
 */
export function usePresenceHeartbeat(market: string = "home") {
  const marketRef = useRef(market);
  useEffect(() => {
    marketRef.current = market;
  }, [market]);

  useEffect(() => {
    const sessionId = getOrCreateSessionId();
    if (!sessionId) return;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;

    const send = async () => {
      if (cancelled) return;
      try {
        await fetch("/api/presence", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ sessionId, market: marketRef.current }),
          keepalive: true,
        });
      } catch {}
      if (!cancelled) timer = setTimeout(send, HEARTBEAT_INTERVAL_MS);
    };

    send();

    const onVisibility = () => {
      if (document.visibilityState === "visible" && !cancelled) {
        if (timer) clearTimeout(timer);
        send();
      }
    };
    document.addEventListener("visibilitychange", onVisibility);

    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, []);
}

/**
 * Polls the presence snapshot endpoint. Returns `null` until first successful
 * response, then the latest structured snapshot.
 */
export function usePresenceSnapshot(intervalMs: number = SNAPSHOT_INTERVAL_MS) {
  const [snapshot, setSnapshot] = useState<PresenceSnapshot | null>(null);

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;

    const pull = async () => {
      if (cancelled) return;
      try {
        const res = await fetch("/api/presence", { cache: "no-store" });
        if (res.ok) {
          const json = (await res.json()) as PresenceSnapshot;
          if (!cancelled) setSnapshot(json);
        }
      } catch {}
      if (!cancelled) timer = setTimeout(pull, intervalMs);
    };

    pull();

    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, [intervalMs]);

  return snapshot;
}
