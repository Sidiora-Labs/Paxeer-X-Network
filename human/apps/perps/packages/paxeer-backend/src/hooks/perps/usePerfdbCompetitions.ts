"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { perfdbQuery, perfdbRestAuth, getAuthToken } from "@paxeer-network/paxeer-backend/core/perfdb/client";

// ── Types ────────────────────────────────────────────────────────────
// Schema: GqlCompetition = { id, name, description, startAt, endAt, metric, prizePool, active, rankings }
// Schema: GqlCompetitionEntry = { rank, address, score }
// Spec called for: { startDate, endDate, status, participantCount }
//                  { value, prize }
// We expose BOTH — server-sourced fields + derived spec-compat fields.

export type CompetitionMetric = "pnl" | "volume" | "trades" | "roi" | string;
export type CompetitionStatus = "upcoming" | "active" | "ended";

export interface Competition {
  id: string;
  name: string;
  description: string | null;
  metric: CompetitionMetric;
  prizePool: string | null;
  // Spec-compat aliases
  startDate: string;
  endDate: string;
  status: CompetitionStatus;
  participantCount: number;
  // Schema-native
  startAt: string;
  endAt: string;
  active: boolean;
}

export interface CompetitionRanking {
  rank: number;
  address: string;
  value: string; // schema `score`
  prize: string | null; // not in schema; null
}

export interface CompetitionDetail extends Competition {
  rankings: CompetitionRanking[];
}

interface RawRanking {
  rank: number;
  address: string;
  score: string;
}

interface RawCompetition {
  id: string;
  name: string;
  description: string | null;
  startAt: string;
  endAt: string;
  metric: string;
  prizePool: string | null;
  active: boolean;
  rankings?: RawRanking[];
}

// ── Queries ──────────────────────────────────────────────────────────
const COMPETITIONS_LIST_QUERY = /* GraphQL */ `
  query Competitions {
    competitions {
      id
      name
      description
      startAt
      endAt
      metric
      prizePool
      active
    }
  }
`;

const COMPETITION_DETAIL_QUERY = /* GraphQL */ `
  query Competition($id: String!) {
    competition(id: $id) {
      id
      name
      description
      startAt
      endAt
      metric
      prizePool
      active
      rankings {
        rank
        address
        score
      }
    }
  }
`;

// ── Helpers ──────────────────────────────────────────────────────────
function deriveStatus(raw: RawCompetition): CompetitionStatus {
  if (raw.active) return "active";
  const now = Date.now();
  const start = Date.parse(raw.startAt);
  const end = Date.parse(raw.endAt);
  if (!isNaN(end) && now > end) return "ended";
  if (!isNaN(start) && now < start) return "upcoming";
  return "ended";
}

function normalize(raw: RawCompetition): Competition {
  const participantCount = raw.rankings ? raw.rankings.length : 0;
  return {
    id: raw.id,
    name: raw.name,
    description: raw.description,
    metric: raw.metric,
    prizePool: raw.prizePool,
    startDate: raw.startAt,
    endDate: raw.endAt,
    status: deriveStatus(raw),
    participantCount,
    startAt: raw.startAt,
    endAt: raw.endAt,
    active: raw.active,
  };
}

function normalizeDetail(raw: RawCompetition): CompetitionDetail {
  const base = normalize(raw);
  const rankings: CompetitionRanking[] = (raw.rankings ?? []).map((r) => ({
    rank: r.rank,
    address: r.address,
    value: r.score,
    prize: null,
  }));
  return { ...base, participantCount: rankings.length, rankings };
}

// ── List hook ────────────────────────────────────────────────────────
interface CompetitionsListResponse {
  competitions: RawCompetition[];
}

export function usePerfdbCompetitions(): {
  competitions: Competition[];
  isLoading: boolean;
  error: Error | null;
} {
  const [competitions, setCompetitions] = useState<Competition[]>([]);
  const [isLoading, setIsLoading] = useState<boolean>(true);
  const [error, setError] = useState<Error | null>(null);
  const mountedRef = useRef<boolean>(true);

  const fetchList = useCallback(async (): Promise<void> => {
    try {
      const res = await perfdbQuery<CompetitionsListResponse>(
        COMPETITIONS_LIST_QUERY
      );
      if (!mountedRef.current) return;
      setCompetitions((res.competitions ?? []).map(normalize));
      setError(null);
    } catch (err) {
      if (!mountedRef.current) return;
      setError(err instanceof Error ? err : new Error(String(err)));
      setCompetitions([]);
    } finally {
      if (mountedRef.current) setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    fetchList();
    return () => {
      mountedRef.current = false;
    };
  }, [fetchList]);

  return { competitions, isLoading, error };
}

// ── Detail hook ──────────────────────────────────────────────────────
interface CompetitionDetailResponse {
  competition: RawCompetition | null;
}

export function usePerfdbCompetitionDetail(id: string | null): {
  detail: CompetitionDetail | null;
  isLoading: boolean;
  error: Error | null;
  join: (address: string) => Promise<void>;
} {
  const [detail, setDetail] = useState<CompetitionDetail | null>(null);
  const [isLoading, setIsLoading] = useState<boolean>(Boolean(id));
  const [error, setError] = useState<Error | null>(null);
  const mountedRef = useRef<boolean>(true);
  const idRef = useRef<string | null>(id);

  useEffect(() => {
    idRef.current = id;
  }, [id]);

  const fetchDetail = useCallback(async (compId: string): Promise<void> => {
    try {
      const res = await perfdbQuery<CompetitionDetailResponse>(
        COMPETITION_DETAIL_QUERY,
        { id: compId }
      );
      if (!mountedRef.current) return;
      setDetail(res.competition ? normalizeDetail(res.competition) : null);
      setError(null);
    } catch (err) {
      if (!mountedRef.current) return;
      setError(err instanceof Error ? err : new Error(String(err)));
      setDetail(null);
    } finally {
      if (mountedRef.current) setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    if (!id) {
      setDetail(null);
      setIsLoading(false);
      setError(null);
      return () => {
        mountedRef.current = false;
      };
    }
    setIsLoading(true);
    fetchDetail(id);
    return () => {
      mountedRef.current = false;
    };
  }, [id, fetchDetail]);

  const join = useCallback(
    async (address: string): Promise<void> => {
      const token = getAuthToken();
      if (!token) {
        throw new Error("Not authenticated. Sign in to join competitions.");
      }
      const compId = idRef.current;
      if (!compId) throw new Error("No competition selected.");
      await perfdbRestAuth<{ ok: boolean }>(
        `/v2/competitions/${encodeURIComponent(compId)}/join`,
        {
          method: "POST",
          body: JSON.stringify({ address }),
        }
      );
      await fetchDetail(compId);
    },
    [fetchDetail]
  );

  return { detail, isLoading, error, join };
}
