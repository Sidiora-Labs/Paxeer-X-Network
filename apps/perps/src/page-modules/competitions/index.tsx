import { useState, useCallback, useMemo } from "react";
import {
  Trophy,
  Calendar,
  Users,
  ArrowLeft,
  Loader2,
  Lock,
} from "lucide-react";
import { useSignMessage } from "wagmi";
import { cn } from "@/ui/utils/cn";
import { Button } from "@/ui/atoms/button";
import { usePerpsWallet } from "@/hooks/perps/usePerpsWallet";
import {
  usePerfdbCompetitions,
  usePerfdbCompetitionDetail,
  type Competition,
  type CompetitionStatus,
} from "@/hooks/perps/usePerfdbCompetitions";
import { usePerfdbAuth } from "@/hooks/perps/usePerfdbAuth";
import { shortAddr, fmtDate, fmtUsdCompact } from "@/ui/utils/format";

function statusBadgeClass(status: CompetitionStatus): string {
  switch (status) {
    case "active":
      return "bg-emerald-500/20 text-emerald-400";
    case "upcoming":
      return "bg-amber-500/20 text-amber-400";
    case "ended":
    default:
      return "bg-muted text-muted-foreground";
  }
}

function metricBadgeClass(metric: string): string {
  switch (metric.toLowerCase()) {
    case "pnl":
      return "bg-emerald-500/15 text-emerald-300";
    case "volume":
      return "bg-blue-500/15 text-blue-300";
    case "trades":
      return "bg-purple-500/15 text-purple-300";
    case "roi":
      return "bg-amber-500/15 text-amber-300";
    default:
      return "bg-muted text-muted-foreground";
  }
}

// ── Competition Card ────────────────────────────────────────────────
function CompetitionCard({
  comp,
  onView,
  onJoin,
  isJoining,
  canJoin,
}: {
  comp: Competition;
  onView: (id: string) => void;
  onJoin: (id: string) => void;
  isJoining: boolean;
  canJoin: boolean;
}) {
  return (
    <div className="rounded-[var(--radius-lg)] bg-surface-1 p-5 flex flex-col gap-3">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <h3 className="text-base font-semibold truncate">{comp.name}</h3>
          {comp.description && (
            <p className="text-xs text-muted-foreground mt-0.5 line-clamp-2">
              {comp.description}
            </p>
          )}
        </div>
        <span
          className={cn(
            "text-xs px-2 py-0.5 rounded-full capitalize shrink-0",
            statusBadgeClass(comp.status)
          )}
        >
          {comp.status}
        </span>
      </div>

      <div className="flex items-center gap-2 flex-wrap">
        <span
          className={cn(
            "text-xs px-2 py-0.5 rounded-full capitalize",
            metricBadgeClass(comp.metric)
          )}
        >
          {comp.metric}
        </span>
        <span className="text-xs text-muted-foreground flex items-center gap-1">
          <Calendar className="h-3 w-3" />
          {fmtDate(comp.startDate)} – {fmtDate(comp.endDate)}
        </span>
        <span className="text-xs text-muted-foreground flex items-center gap-1">
          <Users className="h-3 w-3" />
          {comp.participantCount}
        </span>
      </div>

      <div className="flex items-center justify-between gap-2 pt-2 border-t border-white/[0.06]">
        <div>
          <p className="text-[10px] text-muted-foreground uppercase tracking-wider">
            Prize Pool
          </p>
          <p className="text-lg font-bold font-mono">
            {fmtUsdCompact(comp.prizePool)}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            size="sm"
            variant="outline"
            onClick={() => onView(comp.id)}
          >
            View
          </Button>
          {comp.status === "active" && (
            <Button
              size="sm"
              onClick={() => onJoin(comp.id)}
              disabled={!canJoin || isJoining}
            >
              {isJoining ? (
                <Loader2 className="h-3 w-3 animate-spin mr-1.5" />
              ) : null}
              Join
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}

// ── Competition Detail Panel ────────────────────────────────────────
function CompetitionDetailPanel({
  id,
  onBack,
  userAddress,
  canJoin,
  onRequireSignIn,
  authIsAuthenticated,
}: {
  id: string;
  onBack: () => void;
  userAddress: string | null;
  canJoin: boolean;
  onRequireSignIn: () => void;
  authIsAuthenticated: boolean;
}) {
  const { detail, isLoading, error, join } = usePerfdbCompetitionDetail(id);
  const [isJoining, setIsJoining] = useState<boolean>(false);
  const [joinError, setJoinError] = useState<string | null>(null);

  const handleJoin = useCallback(async (): Promise<void> => {
    if (!userAddress) return;
    if (!authIsAuthenticated) {
      onRequireSignIn();
      return;
    }
    setIsJoining(true);
    setJoinError(null);
    try {
      await join(userAddress);
    } catch (err) {
      setJoinError(err instanceof Error ? err.message : "Failed to join");
    } finally {
      setIsJoining(false);
    }
  }, [join, userAddress, authIsAuthenticated, onRequireSignIn]);

  return (
    <div className="space-y-4">
      <Button
        size="sm"
        variant="outline"
        onClick={onBack}
        className="gap-1.5"
      >
        <ArrowLeft className="h-3.5 w-3.5" /> Back to list
      </Button>

      {isLoading ? (
        <div className="rounded-[var(--radius-lg)] bg-surface-1 p-8 flex items-center justify-center">
          <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
        </div>
      ) : error ? (
        <div className="rounded-[var(--radius-lg)] bg-surface-1 p-8 text-center text-sm text-red-500">
          {error.message}
        </div>
      ) : !detail ? (
        <div className="rounded-[var(--radius-lg)] bg-surface-1 p-8 text-center text-sm text-muted-foreground">
          Competition not found.
        </div>
      ) : (
        <>
          {/* Header card */}
          <div className="rounded-[var(--radius-lg)] bg-surface-1 p-5">
            <div className="flex items-start justify-between gap-3 mb-3">
              <div>
                <h2 className="text-xl font-bold">{detail.name}</h2>
                {detail.description && (
                  <p className="text-sm text-muted-foreground mt-1">
                    {detail.description}
                  </p>
                )}
              </div>
              <span
                className={cn(
                  "text-xs px-2 py-0.5 rounded-full capitalize shrink-0",
                  statusBadgeClass(detail.status)
                )}
              >
                {detail.status}
              </span>
            </div>
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 mt-4">
              <div>
                <p className="text-[10px] text-muted-foreground uppercase tracking-wider">
                  Prize Pool
                </p>
                <p className="text-lg font-bold font-mono">
                  {fmtUsdCompact(detail.prizePool)}
                </p>
              </div>
              <div>
                <p className="text-[10px] text-muted-foreground uppercase tracking-wider">
                  Metric
                </p>
                <p className="text-base font-semibold capitalize">
                  {detail.metric}
                </p>
              </div>
              <div>
                <p className="text-[10px] text-muted-foreground uppercase tracking-wider">
                  Starts
                </p>
                <p className="text-sm font-mono">
                  {fmtDate(detail.startDate)}
                </p>
              </div>
              <div>
                <p className="text-[10px] text-muted-foreground uppercase tracking-wider">
                  Ends
                </p>
                <p className="text-sm font-mono">{fmtDate(detail.endDate)}</p>
              </div>
            </div>
            {detail.status === "active" && (
              <div className="mt-4 pt-4 border-t border-white/[0.06] flex items-center gap-2">
                <Button
                  size="sm"
                  onClick={handleJoin}
                  disabled={!canJoin || isJoining}
                >
                  {isJoining ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin mr-1.5" />
                  ) : null}
                  Join Competition
                </Button>
                {!canJoin && !authIsAuthenticated && (
                  <span className="text-xs text-muted-foreground flex items-center gap-1">
                    <Lock className="h-3 w-3" /> Sign in required
                  </span>
                )}
                {joinError && (
                  <span className="text-xs text-red-500">{joinError}</span>
                )}
              </div>
            )}
          </div>

          {/* Rankings */}
          <div className="rounded-[var(--radius-lg)] bg-surface-1 overflow-hidden">
            <div className="px-5 py-3 border-b border-white/[0.06] flex items-center gap-2">
              <Trophy className="h-4 w-4 text-amber-400" />
              <h3 className="text-sm font-semibold">Leaderboard</h3>
              <span className="ml-auto text-xs text-muted-foreground">
                {detail.rankings.length} entries
              </span>
            </div>
            {detail.rankings.length === 0 ? (
              <div className="py-10 text-center text-sm text-muted-foreground">
                No rankings yet.
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-white/[0.06] text-muted-foreground">
                      <th className="text-left px-4 py-2.5 font-medium w-16">
                        Rank
                      </th>
                      <th className="text-left px-4 py-2.5 font-medium">
                        Address
                      </th>
                      <th className="text-right px-4 py-2.5 font-medium">
                        Value
                      </th>
                      <th className="text-right px-4 py-2.5 font-medium hidden sm:table-cell">
                        Prize
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {detail.rankings.map((r) => (
                      <tr
                        key={`${r.rank}-${r.address}`}
                        className={cn(
                          "border-b border-white/[0.04]",
                          userAddress &&
                            r.address.toLowerCase() ===
                              userAddress.toLowerCase() &&
                            "bg-primary/5"
                        )}
                      >
                        <td className="px-4 py-3 font-mono font-semibold">
                          #{r.rank}
                        </td>
                        <td className="px-4 py-3 font-mono text-xs">
                          {shortAddr(r.address)}
                        </td>
                        <td className="px-4 py-3 text-right font-mono">
                          {r.value}
                        </td>
                        <td className="px-4 py-3 text-right font-mono hidden sm:table-cell text-muted-foreground">
                          {r.prize ? fmtUsdCompact(r.prize) : "\u2014"}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </>
      )}
    </div>
  );
}

// ── Page ──────────────────────────────────────────────────────────────────
export default function CompetitionsPage() {
  const { address, isConnected, connect } = usePerpsWallet();
  const { competitions, isLoading, error } = usePerfdbCompetitions();
  const auth = usePerfdbAuth();
  const { signMessageAsync } = useSignMessage();
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [isSigningIn, setIsSigningIn] = useState<boolean>(false);
  const [signInError, setSignInError] = useState<string | null>(null);

  // ── Grouped sections ────────────────────────────────────────────
  const sections = useMemo(() => {
    const active = competitions.filter((c) => c.status === "active");
    const upcoming = competitions.filter((c) => c.status === "upcoming");
    const ended = competitions.filter((c) => c.status === "ended");
    return { active, upcoming, ended };
  }, [competitions]);

  const handleSignIn = useCallback(async (): Promise<void> => {
    if (!address) return;
    setIsSigningIn(true);
    setSignInError(null);
    try {
      await auth.authenticate(address, async (msg) =>
        signMessageAsync({ message: msg })
      );
    } catch (err) {
      setSignInError(
        err instanceof Error ? err.message : "Failed to sign in"
      );
    } finally {
      setIsSigningIn(false);
    }
  }, [address, auth, signMessageAsync]);

  const handleJoinFromList = useCallback(
    (id: string): void => {
      if (!auth.isAuthenticated) {
        handleSignIn();
        return;
      }
      setSelectedId(id);
    },
    [auth.isAuthenticated, handleSignIn]
  );

  const canJoin = Boolean(isConnected && address);

  return (
    <div className="flex flex-col h-full bg-background text-foreground overflow-hidden">
      <main className="flex-1 min-h-0 overflow-y-auto max-w-6xl mx-auto w-full p-4 md:p-6">
        {/* Page header */}
        <div className="mb-6">
          <div className="flex items-center gap-2 mb-1">
            <Trophy className="h-6 w-6 text-amber-400" />
            <h1 className="text-2xl font-bold">Competitions</h1>
          </div>
          <p className="text-sm text-muted-foreground">
            Trading tournaments with prize pools. Climb the leaderboard to win.
          </p>
        </div>

        {/* Sign-in banner (only when connected and not authenticated) */}
        {isConnected && !auth.isAuthenticated && !selectedId && (
          <div className="rounded-[var(--radius-lg)] bg-surface-1 p-4 mb-4 flex items-center justify-between gap-3">
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              <Lock className="h-4 w-4" />
              Sign in to join competitions.
            </div>
            <Button
              size="sm"
              onClick={handleSignIn}
              disabled={isSigningIn}
            >
              {isSigningIn ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin mr-1.5" />
              ) : null}
              Sign In
            </Button>
          </div>
        )}
        {signInError && !selectedId && (
          <p className="text-xs text-red-500 mb-4">{signInError}</p>
        )}

        {/* Detail view vs list */}
        {selectedId ? (
          <CompetitionDetailPanel
            id={selectedId}
            onBack={() => setSelectedId(null)}
            userAddress={address}
            canJoin={canJoin}
            authIsAuthenticated={auth.isAuthenticated}
            onRequireSignIn={handleSignIn}
          />
        ) : isLoading && competitions.length === 0 ? (
          <div className="flex items-center justify-center py-20">
            <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
          </div>
        ) : error ? (
          <div className="rounded-[var(--radius-lg)] bg-surface-1 p-10 text-center text-sm text-red-500">
            Failed to load competitions: {error.message}
          </div>
        ) : competitions.length === 0 ? (
          <div className="rounded-[var(--radius-lg)] bg-surface-1 py-20 text-center">
            <Trophy className="h-10 w-10 text-muted-foreground mx-auto mb-3" />
            <p className="text-sm text-muted-foreground">
              No competitions scheduled
            </p>
            <p className="text-xs text-muted-foreground mt-1">
              Check back soon for tournaments.
            </p>
          </div>
        ) : (
          <div className="space-y-8">
            {sections.active.length > 0 && (
              <section>
                <h2 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-3">
                  Active
                </h2>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                  {sections.active.map((c) => (
                    <CompetitionCard
                      key={c.id}
                      comp={c}
                      onView={(id) => setSelectedId(id)}
                      onJoin={handleJoinFromList}
                      isJoining={false}
                      canJoin={canJoin}
                    />
                  ))}
                </div>
              </section>
            )}

            {sections.upcoming.length > 0 && (
              <section>
                <h2 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-3">
                  Upcoming
                </h2>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                  {sections.upcoming.map((c) => (
                    <CompetitionCard
                      key={c.id}
                      comp={c}
                      onView={(id) => setSelectedId(id)}
                      onJoin={handleJoinFromList}
                      isJoining={false}
                      canJoin={canJoin}
                    />
                  ))}
                </div>
              </section>
            )}

            {sections.ended.length > 0 && (
              <section>
                <h2 className="text-sm font-semibold text-muted-foreground uppercase tracking-wider mb-3">
                  Ended
                </h2>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                  {sections.ended.map((c) => (
                    <CompetitionCard
                      key={c.id}
                      comp={c}
                      onView={(id) => setSelectedId(id)}
                      onJoin={handleJoinFromList}
                      isJoining={false}
                      canJoin={canJoin}
                    />
                  ))}
                </div>
              </section>
            )}
          </div>
        )}

        {/* Connect prompt */}
        {!isConnected && !selectedId && (
          <div className="rounded-[var(--radius-lg)] bg-surface-1 p-4 mt-4 flex items-center justify-between gap-3">
            <p className="text-sm text-muted-foreground">
              Connect your wallet to join competitions and track your rank.
            </p>
            <Button size="sm" onClick={connect}>
              Connect Wallet
            </Button>
          </div>
        )}
      </main>
    </div>
  );
}
