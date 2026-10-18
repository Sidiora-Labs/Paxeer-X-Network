"use client";
import { fetchWithPolicy as fetch } from "@paxeer-network/paxeer-backend/core/net/fetch";

// ─────────────────────────────────────────────────────────────────────
// PerfDB client — shared between Agent 1 & Agent 2.
// Agent 2 authored the initial stub; Agent 1 may later overwrite with
// a fuller version (subscriptions, batching, etc.).
// ─────────────────────────────────────────────────────────────────────

import { gqlEndpoint, type GraphQLEndpoint } from "@paxeer-network/paxeer-backend/core/graphql/client";

export const PERFDB_URL =
  process.env.NEXT_PUBLIC_PERFDB_URL ??
  "https://api-production-5335.up.railway.app";

export const PERFDB_REST_URL = PERFDB_URL;

export const PERFDB_WS_URL = PERFDB_URL.replace(/^https/, "wss").replace(
  /^http/,
  "ws"
);

const JWT_KEY = "perfdb_jwt";
const JWT_EXPIRES_KEY = "perfdb_jwt_expires";

// ── Auth helpers ────────────────────────────────────────────────────

export function getAuthToken(): string | null {
  if (typeof window === "undefined") return null;
  try {
    const token = window.localStorage.getItem(JWT_KEY);
    const expiresAtRaw = window.localStorage.getItem(JWT_EXPIRES_KEY);
    if (!token) return null;
    if (expiresAtRaw) {
      const expiresAt = parseInt(expiresAtRaw, 10);
      if (!isNaN(expiresAt) && Date.now() >= expiresAt) return null;
    }
    return token;
  } catch {
    return null;
  }
}

export function getAuthHeaders(): Record<string, string> {
  const token = getAuthToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

// ── Core fetchers ───────────────────────────────────────────────────

export async function perfdbQuery<T>(
  document: string,
  variables?: Record<string, unknown>
): Promise<T> {
  const res = await fetch(`${PERFDB_URL}/graphql`, {
    method: "POST",
    idempotent: /^\s*(query\b|\{)/.test(document),
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ query: document, variables }),
  });
  if (!res.ok) throw new Error(`PerfDB GraphQL error: ${res.status}`);
  const json = await res.json();
  if (json.errors?.length) throw new Error(json.errors[0].message);
  return json.data as T;
}

export async function perfdbQueryAuth<T>(
  document: string,
  variables?: Record<string, unknown>
): Promise<T> {
  const res = await fetch(`${PERFDB_URL}/graphql`, {
    method: "POST",
    idempotent: /^\s*(query\b|\{)/.test(document),
    headers: {
      "Content-Type": "application/json",
      ...getAuthHeaders(),
    },
    body: JSON.stringify({ query: document, variables }),
  });
  if (!res.ok) throw new Error(`PerfDB GraphQL error: ${res.status}`);
  const json = await res.json();
  if (json.errors?.length) throw new Error(json.errors[0].message);
  return json.data as T;
}

export async function perfdbRest<T>(
  path: string,
  options?: RequestInit
): Promise<T> {
  const res = await fetch(`${PERFDB_URL}${path}`, {
    headers: { "Content-Type": "application/json" },
    ...options,
  });
  if (!res.ok) throw new Error(`PerfDB REST error: ${res.status}`);
  return res.json() as Promise<T>;
}

export async function perfdbRestAuth<T>(
  path: string,
  options?: RequestInit
): Promise<T> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...getAuthHeaders(),
    ...((options?.headers as Record<string, string>) ?? {}),
  };
  const res = await fetch(`${PERFDB_URL}${path}`, { ...options, headers });
  if (!res.ok) throw new Error(`PerfDB REST error: ${res.status}`);
  return res.json() as Promise<T>;
}

// ── GraphQL endpoint singleton ──────────────────────────────────────
//
// All polled queries + WS subscriptions to PerfDB must go through
// `subscribeQuery` / `subscribeStream` from `graphqlClient.ts` using
// this object so the shared timer + multiplexed WS apply.
export const PERFDB_GRAPHQL: GraphQLEndpoint = gqlEndpoint(
  `${PERFDB_URL}/graphql`,
  {
    wsUrl: `${PERFDB_WS_URL}/graphql`,
    authHeaders: getAuthHeaders,
    name: "perfdb",
  },
);
