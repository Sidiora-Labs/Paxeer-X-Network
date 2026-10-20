import { useState, useCallback, useMemo } from "react";
import {
  Users,
  Copy,
  Check,
  Loader2,
  Share2,
  Trophy,
  Lock,
} from "lucide-react";
import { useSignMessage } from "wagmi";
import { cn } from "@/ui/utils/cn";
import { Button } from "@/ui/atoms/button";
import { usePerpsWallet } from "@/hooks/perps/usePerpsWallet";
import { usePerfdbReferrals } from "@/hooks/perps/usePerfdbReferrals";
import { usePerfdbAuth } from "@/hooks/perps/usePerfdbAuth";
import { fmtUsdCompact, parse18 } from "@/ui/utils/format";

export default function ReferralsPage() {
  const { address, isConnected, connect } = usePerpsWallet();
  const {
    data,
    tiers,
    isLoading,
    error,
    generateCode,
    applyCode,
  } = usePerfdbReferrals(address ?? undefined);
  const auth = usePerfdbAuth();
  const { signMessageAsync } = useSignMessage();

  const [copied, setCopied] = useState<boolean>(false);
  const [applyCodeInput, setApplyCodeInput] = useState<string>("");
  const [applyStatus, setApplyStatus] = useState<
    { type: "success" | "error"; msg: string } | null
  >(null);
  const [isApplying, setIsApplying] = useState<boolean>(false);
  const [isGenerating, setIsGenerating] = useState<boolean>(false);
  const [genError, setGenError] = useState<string | null>(null);
  const [isSigningIn, setIsSigningIn] = useState<boolean>(false);
  const [signInError, setSignInError] = useState<string | null>(null);

  // ── Derived ────────────────────────────────────────────────────
  const currentTierIdx = useMemo(() => {
    if (!data) return -1;
    const idx = tiers.findIndex(
      (t) => t.tier.toLowerCase() === data.tier.toLowerCase()
    );
    return idx;
  }, [data, tiers]);

  const nextTier = useMemo(() => {
    if (currentTierIdx < 0) return null;
    return tiers[currentTierIdx + 1] ?? null;
  }, [tiers, currentTierIdx]);

  const progressPct = useMemo(() => {
    if (!data || !nextTier || nextTier.requiredReferrals <= 0) return 100;
    const pct = (data.referralCount / nextTier.requiredReferrals) * 100;
    return Math.min(100, Math.max(0, pct));
  }, [data, nextTier]);

  // ── Handlers ───────────────────────────────────────────────────
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

  const handleCopy = useCallback(async (): Promise<void> => {
    if (!data?.referralCode) return;
    try {
      await navigator.clipboard.writeText(data.referralCode);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // ignore
    }
  }, [data]);

  const handleGenerate = useCallback(async (): Promise<void> => {
    setIsGenerating(true);
    setGenError(null);
    try {
      await generateCode();
    } catch (err) {
      setGenError(
        err instanceof Error ? err.message : "Failed to generate code"
      );
    } finally {
      setIsGenerating(false);
    }
  }, [generateCode]);

  const handleApply = useCallback(
    async (e: React.FormEvent<HTMLFormElement>): Promise<void> => {
      e.preventDefault();
      setIsApplying(true);
      setApplyStatus(null);
      try {
        await applyCode(applyCodeInput);
        setApplyStatus({ type: "success", msg: "Referral code applied!" });
        setApplyCodeInput("");
      } catch (err) {
        setApplyStatus({
          type: "error",
          msg: err instanceof Error ? err.message : "Failed to apply code",
        });
      } finally {
        setIsApplying(false);
      }
    },
    [applyCode, applyCodeInput]
  );

  // ── Render ─────────────────────────────────────────────────────
  return (
    <div className="flex flex-col h-full bg-background text-foreground overflow-hidden">
      <main className="flex-1 min-h-0 overflow-y-auto max-w-6xl mx-auto w-full p-4 md:p-6">
        {/* Page header */}
        <div className="mb-6">
          <div className="flex items-center gap-2 mb-1">
            <Share2 className="h-6 w-6 text-primary" />
            <h1 className="text-2xl font-bold">Referrals</h1>
          </div>
          <p className="text-sm text-muted-foreground">
            Earn a share of trading fees when friends you refer trade on HyperPax.
          </p>
        </div>

        {/* Inline connect banner (non-blocking — cards below still render) */}
        {!isConnected && (
          <div className="rounded-[var(--radius-lg)] bg-surface-1 p-4 mb-4 flex items-center justify-between gap-3">
            <div className="flex items-center gap-3">
              <Users className="h-5 w-5 text-primary" />
              <div>
                <p className="text-sm font-semibold">Wallet not connected</p>
                <p className="text-xs text-muted-foreground">Connect to generate a code and track referral rewards.</p>
              </div>
            </div>
            <Button size="sm" onClick={connect}>Connect Wallet</Button>
          </div>
        )}

        {/* Code + Tier grid — always rendered so layout is stable */}
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4 mb-6">
            {/* Your Referral Code card */}
            <div className="rounded-[var(--radius-lg)] bg-surface-1 p-5">
              <h3 className="text-sm font-semibold mb-3">Your Referral Code</h3>
              {!isConnected ? (
                <div className="flex flex-col items-start gap-3">
                  <p className="text-xs text-muted-foreground flex items-center gap-1.5">
                    <Lock className="h-3.5 w-3.5" /> Connect your wallet to view or generate a code.
                  </p>
                  <Button size="sm" onClick={connect}>Connect Wallet</Button>
                </div>
              ) : !auth.isAuthenticated ? (
                <div className="flex flex-col items-start gap-3">
                  <p className="text-xs text-muted-foreground flex items-center gap-1.5">
                    <Lock className="h-3.5 w-3.5" /> Sign in to view / generate your code.
                  </p>
                  <Button size="sm" onClick={handleSignIn} disabled={isSigningIn}>
                    {isSigningIn ? (
                      <Loader2 className="h-4 w-4 animate-spin mr-2" />
                    ) : null}
                    Sign In
                  </Button>
                  {signInError && (
                    <p className="text-xs text-red-500">{signInError}</p>
                  )}
                </div>
              ) : data?.referralCode ? (
                <div className="flex items-center gap-2">
                  <code className="flex-1 text-lg font-mono font-semibold bg-surface-2 px-3 py-2 rounded-[var(--radius-md)] tracking-widest">
                    {data.referralCode}
                  </code>
                  <button
                    onClick={handleCopy}
                    className="h-10 w-10 flex items-center justify-center rounded-[var(--radius-md)] bg-surface-2 hover:bg-white/[0.08] transition-colors"
                    aria-label="Copy code"
                  >
                    {copied ? (
                      <Check className="h-4 w-4 text-emerald-500" />
                    ) : (
                      <Copy className="h-4 w-4" />
                    )}
                  </button>
                </div>
              ) : (
                <div className="flex flex-col items-start gap-3">
                  <p className="text-sm text-muted-foreground">
                    You don&apos;t have a code yet. Generate one to start earning.
                  </p>
                  <Button
                    size="sm"
                    onClick={handleGenerate}
                    disabled={isGenerating}
                  >
                    {isGenerating ? (
                      <Loader2 className="h-4 w-4 animate-spin mr-2" />
                    ) : null}
                    Generate Code
                  </Button>
                  {genError && (
                    <p className="text-xs text-red-500">{genError}</p>
                  )}
                </div>
              )}
            </div>

            {/* Your Tier card */}
            <div className="rounded-[var(--radius-lg)] bg-surface-1 p-5">
              <h3 className="text-sm font-semibold mb-3">Your Tier</h3>
              {isLoading && !data ? (
                <div className="h-24 rounded-md bg-primary/10 animate-pulse" />
              ) : data ? (
                <>
                  <div className="flex items-center gap-2 mb-2">
                    <Trophy className="h-4 w-4 text-amber-400" />
                    <span className="text-lg font-semibold capitalize">
                      {data.tier}
                    </span>
                    <span className="text-xs text-muted-foreground ml-auto">
                      {data.referralCount} referrals
                    </span>
                  </div>
                  {nextTier ? (
                    <>
                      <div className="h-2 bg-surface-2 rounded-full overflow-hidden mb-1.5">
                        <div
                          className="h-full bg-primary transition-all duration-300"
                          style={{ width: `${progressPct}%` }}
                        />
                      </div>
                      <p className="text-xs text-muted-foreground">
                        {data.referralCount} / {nextTier.requiredReferrals}{" "}
                        referrals to <span className="capitalize">{nextTier.name}</span>
                      </p>
                    </>
                  ) : (
                    <p className="text-xs text-emerald-400">
                      Max tier unlocked 🎉
                    </p>
                  )}
                  {data.totalEarned !== null && (
                    <div className="mt-3 pt-3 border-t border-white/[0.06]">
                      <p className="text-xs text-muted-foreground">
                        Total Earned
                      </p>
                      <p className="text-lg font-semibold font-mono">
                        {fmtUsdCompact(parse18(data.totalEarned))}
                      </p>
                    </div>
                  )}
                </>
              ) : (
                <p className="text-sm text-muted-foreground">
                  {!isConnected ? "Connect your wallet to see tier progress." : "No tier data available."}
                </p>
              )}
            </div>
          </div>

        {/* Apply a Code card — always rendered with inline disabled state */}
        <div className="rounded-[var(--radius-lg)] bg-surface-1 p-5 mb-6">
          <h3 className="text-sm font-semibold mb-3">Apply a Code</h3>
          {!isConnected ? (
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <Lock className="h-3.5 w-3.5" /> Connect your wallet to apply a code.
            </div>
          ) : !auth.isAuthenticated ? (
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <Lock className="h-3.5 w-3.5" /> Sign in to apply a code.
            </div>
          ) : (
            <form
              onSubmit={handleApply}
              className="flex flex-col sm:flex-row gap-2"
            >
              <input
                className="flex-1 rounded-[var(--radius-md)] bg-surface-2 px-3 py-2 text-sm"
                placeholder="Enter referral code"
                value={applyCodeInput}
                onChange={(e) => setApplyCodeInput(e.target.value)}
              />
              <Button type="submit" disabled={isApplying || !applyCodeInput}>
                {isApplying ? (
                  <Loader2 className="h-4 w-4 animate-spin mr-2" />
                ) : null}
                Apply
              </Button>
            </form>
          )}
          {applyStatus && (
            <p
              className={cn(
                "text-xs mt-2",
                applyStatus.type === "success"
                  ? "text-emerald-500"
                  : "text-red-500"
              )}
            >
              {applyStatus.msg}
            </p>
          )}
        </div>

        {/* All Tiers table */}
        <div className="rounded-[var(--radius-lg)] bg-surface-1 overflow-hidden">
          <div className="px-5 py-3 border-b border-white/[0.06]">
            <h3 className="text-sm font-semibold">All Tiers</h3>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-white/[0.06] text-muted-foreground">
                  <th className="text-left px-4 py-3 font-medium">Tier</th>
                  <th className="text-right px-4 py-3 font-medium">
                    Referrals Required
                  </th>
                  <th className="text-right px-4 py-3 font-medium hidden sm:table-cell">
                    Volume Required
                  </th>
                  <th className="text-right px-4 py-3 font-medium">Fee Share</th>
                  <th className="text-right px-4 py-3 font-medium hidden md:table-cell">
                    Fee Discount
                  </th>
                </tr>
              </thead>
              <tbody>
                {tiers.length === 0 ? (
                  <tr>
                    <td
                      colSpan={5}
                      className="py-12 text-center text-muted-foreground"
                    >
                      {isLoading
                        ? "Loading tiers…"
                        : error
                        ? `Failed to load tiers: ${error.message}`
                        : "No tiers available"}
                    </td>
                  </tr>
                ) : (
                  tiers.map((t) => {
                    const isCurrent =
                      data?.tier.toLowerCase() === t.tier.toLowerCase();
                    return (
                      <tr
                        key={t.tier}
                        className={cn(
                          "border-b border-white/[0.04]",
                          isCurrent && "bg-primary/5"
                        )}
                      >
                        <td className="px-4 py-3">
                          <span
                            className={cn(
                              "capitalize font-medium",
                              isCurrent && "text-primary"
                            )}
                          >
                            {t.name}
                          </span>
                          {isCurrent && (
                            <span className="ml-2 text-xs px-1.5 py-0.5 rounded-full bg-primary/20 text-primary">
                              Current
                            </span>
                          )}
                        </td>
                        <td className="px-4 py-3 text-right font-mono">
                          {t.requiredReferrals}
                        </td>
                        <td className="px-4 py-3 text-right font-mono hidden sm:table-cell">
                          {fmtUsdCompact(parse18(t.minVolume))}
                        </td>
                        <td className="px-4 py-3 text-right font-mono">
                          {(t.rewardBps / 100).toFixed(2)}%
                        </td>
                        <td className="px-4 py-3 text-right font-mono hidden md:table-cell">
                          {t.feeDiscountBps > 0
                            ? `-${(t.feeDiscountBps / 100).toFixed(2)}%`
                            : "\u2014"}
                        </td>
                      </tr>
                    );
                  })
                )}
              </tbody>
            </table>
          </div>
        </div>
      </main>
    </div>
  );
}
