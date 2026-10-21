"use client";

import { useState, useCallback } from "react";
import { Lock, Trash2, Bell, Loader2 } from "lucide-react";
import { useSignMessage } from "wagmi";
import { cn } from "@/ui/utils/cn";
import { Button } from "@/ui/atoms/button";
import {
  usePerfdbAlerts,
  type AlertType,
  type PerfdbAlert,
} from "@/hooks/perps/usePerfdbAlerts";
import { usePerfdbAuth } from "@/hooks/perps/usePerfdbAuth";
import { PERPS_MARKETS } from "@/core/perps/config";
import { classifyError } from "@/core/errors/classify";
import { reportError } from "@/core/errors/report";

interface AlertsPanelProps {
  address: string | undefined;
}

const ALERT_TYPE_OPTIONS: { value: AlertType; label: string }[] = [
  { value: "price_above", label: "Price Above" },
  { value: "price_below", label: "Price Below" },
  { value: "liquidation_warning", label: "Liquidation Warning" },
  { value: "funding_threshold", label: "Funding Threshold" },
];

function marketLabel(marketId: number | null): string {
  if (marketId === null || marketId === undefined) return "All markets";
  const m = PERPS_MARKETS.find((mk) => mk.marketId === marketId);
  return m ? `${m.symbol}/USD` : `Market ${marketId}`;
}

function alertTypeLabel(t: AlertType): string {
  return ALERT_TYPE_OPTIONS.find((o) => o.value === t)?.label ?? t;
}

export function AlertsPanel({ address }: AlertsPanelProps) {
  const { alerts, isLoading, error, createAlert, deleteAlert } =
    usePerfdbAlerts(address);
  const auth = usePerfdbAuth();
  const { signMessageAsync } = useSignMessage();

  // ── Form state ─────────────────────────────────────────────────
  const [selectedType, setSelectedType] = useState<AlertType>("price_above");
  const [marketIdStr, setMarketIdStr] = useState<string>("");
  const [threshold, setThreshold] = useState<string>("");
  const [isSubmitting, setIsSubmitting] = useState<boolean>(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const [isSigningIn, setIsSigningIn] = useState<boolean>(false);

  const handleSignIn = useCallback(async (): Promise<void> => {
    if (!address) return;
    setIsSigningIn(true);
    setFormError(null);
    try {
      await auth.authenticate(address, async (msg) =>
        signMessageAsync({ message: msg })
      );
    } catch (err) {
      const ufe = classifyError(err, "alerts/sign-in");
      setFormError(
        ufe.suggestedFix ? `${ufe.message} ${ufe.suggestedFix}` : ufe.message,
      );
      void reportError(ufe);
    } finally {
      setIsSigningIn(false);
    }
  }, [address, auth, signMessageAsync]);

  const handleSubmit = useCallback(
    async (e: React.FormEvent<HTMLFormElement>): Promise<void> => {
      e.preventDefault();
      setIsSubmitting(true);
      setFormError(null);
      try {
        const marketId =
          marketIdStr.trim() === "" ? undefined : parseInt(marketIdStr, 10);
        if (marketIdStr && isNaN(marketId as number)) {
          throw new Error("Invalid market ID");
        }
        await createAlert(
          selectedType,
          marketId,
          threshold.trim() === "" ? undefined : threshold.trim()
        );
        setMarketIdStr("");
        setThreshold("");
      } catch (err) {
        const ufe = classifyError(err, "alerts/create");
        setFormError(
          ufe.suggestedFix ? `${ufe.message} ${ufe.suggestedFix}` : ufe.message,
        );
        void reportError(ufe);
      } finally {
        setIsSubmitting(false);
      }
    },
    [createAlert, selectedType, marketIdStr, threshold]
  );

  const handleDelete = useCallback(
    async (id: string): Promise<void> => {
      setDeletingId(id);
      try {
        await deleteAlert(id);
      } catch (err) {
        const ufe = classifyError(err, "alerts/delete");
        setFormError(
          ufe.suggestedFix ? `${ufe.message} ${ufe.suggestedFix}` : ufe.message,
        );
        void reportError(ufe);
      } finally {
        setDeletingId(null);
      }
    },
    [deleteAlert]
  );

  // ── Auth gate ─────────────────────────────────────────────────
  if (!auth.isLoading && !auth.isAuthenticated) {
    return (
      <div className="rounded-[var(--radius-lg)] bg-surface-1 p-8 flex flex-col items-center text-center gap-3">
        <div className="w-12 h-12 rounded-full bg-primary/10 flex items-center justify-center">
          <Lock className="h-5 w-5 text-primary" />
        </div>
        <div>
          <h3 className="text-base font-semibold">Sign in to manage alerts</h3>
          <p className="text-sm text-muted-foreground mt-1 max-w-sm">
            Create price, liquidation, and funding-rate alerts. Sign a message
            with your wallet to verify ownership — no funds move.
          </p>
        </div>
        <Button
          size="default"
          onClick={handleSignIn}
          disabled={!address || isSigningIn}
        >
          {isSigningIn ? (
            <Loader2 className="h-4 w-4 animate-spin mr-2" />
          ) : null}
          {address ? "Sign In" : "Connect wallet first"}
        </Button>
        {formError && (
          <p className="text-xs text-red-500 max-w-sm">{formError}</p>
        )}
      </div>
    );
  }

  // ── Loading state ─────────────────────────────────────────────
  if (isLoading && alerts.length === 0) {
    return (
      <div className="rounded-[var(--radius-lg)] bg-surface-1 p-5 space-y-2">
        {[0, 1, 2].map((i) => (
          <div
            key={i}
            className="h-10 rounded-md bg-primary/10 animate-pulse"
          />
        ))}
      </div>
    );
  }

  // ── Main UI ───────────────────────────────────────────────────
  return (
    <div className="space-y-4">
      {/* Alerts list */}
      <div className="rounded-[var(--radius-lg)] bg-surface-1 overflow-hidden">
        <div className="px-5 py-3 border-b border-white/[0.06] flex items-center gap-2">
          <Bell className="h-4 w-4 text-muted-foreground" />
          <h3 className="text-sm font-semibold">Active Alerts</h3>
          <span className="ml-auto text-xs text-muted-foreground">
            {alerts.length}
          </span>
        </div>
        {alerts.length === 0 ? (
          <div className="py-8 text-center text-sm text-muted-foreground">
            No alerts yet. Create one below.
          </div>
        ) : (
          <div className="divide-y divide-white/[0.06]">
            {alerts.map((a: PerfdbAlert) => (
              <div
                key={a.id}
                className="flex items-center justify-between px-5 py-3"
              >
                <div className="flex items-center gap-3 min-w-0">
                  <span
                    className={cn(
                      "text-xs px-2 py-0.5 rounded-full",
                      a.isActive
                        ? "bg-emerald-500/20 text-emerald-400"
                        : "bg-muted text-muted-foreground"
                    )}
                  >
                    {alertTypeLabel(a.alertType)}
                  </span>
                  <span className="text-sm text-muted-foreground">
                    {marketLabel(a.marketId)}
                  </span>
                  {a.threshold && (
                    <span className="text-sm font-mono">
                      @ {a.threshold}
                    </span>
                  )}
                </div>
                <button
                  onClick={() => handleDelete(a.id)}
                  disabled={deletingId === a.id}
                  className="text-muted-foreground hover:text-red-500 transition-colors disabled:opacity-50 p-1"
                  aria-label="Delete alert"
                >
                  {deletingId === a.id ? (
                    <Loader2 className="h-4 w-4 animate-spin" />
                  ) : (
                    <Trash2 className="h-4 w-4" />
                  )}
                </button>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Add Alert form */}
      <form
        onSubmit={handleSubmit}
        className="rounded-[var(--radius-lg)] bg-surface-1 p-5 space-y-3"
      >
        <h3 className="text-sm font-semibold">Add Alert</h3>
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
          <div>
            <label className="text-xs text-muted-foreground block mb-1">
              Alert Type
            </label>
            <select
              className="w-full rounded-[var(--radius-md)] bg-surface-2 px-3 py-2 text-sm"
              value={selectedType}
              onChange={(e) =>
                setSelectedType(e.target.value as AlertType)
              }
            >
              {ALERT_TYPE_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label className="text-xs text-muted-foreground block mb-1">
              Market ID
            </label>
            <input
              className="w-full rounded-[var(--radius-md)] bg-surface-2 px-3 py-2 text-sm"
              value={marketIdStr}
              onChange={(e) => setMarketIdStr(e.target.value)}
              placeholder="All markets"
              inputMode="numeric"
            />
          </div>
          <div>
            <label className="text-xs text-muted-foreground block mb-1">
              Threshold
            </label>
            <input
              className="w-full rounded-[var(--radius-md)] bg-surface-2 px-3 py-2 text-sm"
              value={threshold}
              onChange={(e) => setThreshold(e.target.value)}
              placeholder="Price threshold"
              inputMode="decimal"
            />
          </div>
        </div>
        {formError && (
          <p className="text-xs text-red-500">{formError}</p>
        )}
        {error && !formError && (
          <p className="text-xs text-red-500">{error.message}</p>
        )}
        <Button type="submit" disabled={isSubmitting}>
          {isSubmitting ? (
            <Loader2 className="h-4 w-4 animate-spin mr-2" />
          ) : null}
          Create Alert
        </Button>
      </form>
    </div>
  );
}
