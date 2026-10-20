import { useState, useCallback, useMemo, useEffect } from "react";
import { formatUnits } from "viem";
import { cn } from "@/ui/utils/cn";
import { Button } from "@/ui/atoms/button";
import { X, Loader2, AlertTriangle, Check, ArrowDownToLine, ArrowUpFromLine } from "lucide-react";
import { usePerpsWallet } from "@/hooks/perps/usePerpsWallet";
import { useAccount } from "@/hooks/perps/useAccount";
import type { AccountView } from "@/core/perps/accountView";
import { safeParseUnits } from "@/core/perps/amounts";
import { toast } from "@/ui/atoms/use-toast";
import { toastError } from "@/widgets/errors";
import { classifyError } from "@/core/errors/classify";
import { reportError } from "@/core/errors/report";
import { COLLATERAL_TOKENS } from "@/core/perps/config";

const USDL = COLLATERAL_TOKENS.usdl;

function fmtBal(raw: bigint): string {
  return Number(formatUnits(raw, USDL.decimals)).toLocaleString("en-US", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });
}

interface DepositWithdrawModalProps {
  isOpen: boolean;
  onClose: () => void;
}

export function DepositWithdrawModal({ isOpen, onClose }: DepositWithdrawModalProps) {
  const { isConnected, address, connect } = usePerpsWallet();
  const { getAccount, getWalletBalance, deposit, withdraw, withdrawAll } = useAccount();

  const [action, setAction] = useState<"deposit" | "withdraw">("deposit");
  const [amount, setAmount] = useState("");

  const [walletBalance, setWalletBalance] = useState<bigint>(0n);
  const [account, setAccount] = useState<AccountView | null>(null);
  const [isLoadingBalances, setIsLoadingBalances] = useState(false);

  const [isProcessing, setIsProcessing] = useState(false);
  const [txStep, setTxStep] = useState<"idle" | "approving" | "executing" | "confirming">("idle");
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  const [isWithdrawingAll, setIsWithdrawingAll] = useState(false);

  const withdrawable = account?.withdrawable ?? 0n;
  const amountUnits = useMemo(() => safeParseUnits(amount, USDL.decimals), [amount]);

  // ── Reset the form whenever the modal closes so stale amounts and
  //    success/error banners never leak into the next open.
  useEffect(() => {
    if (isOpen) return;
    setAmount("");
    setError(null);
    setSuccess(null);
    setTxStep("idle");
    setIsProcessing(false);
  }, [isOpen]);

  // Reads the wallet USDL balance and getAccount(account) at 0x1015.
  const refreshBalances = useCallback(async (isCancelled: () => boolean = () => false) => {
    if (!address) return;
    const [wb, acc] = await Promise.allSettled([getWalletBalance(address), getAccount(address)]);
    if (isCancelled()) return;
    setWalletBalance(wb.status === "fulfilled" ? wb.value : 0n);
    setAccount(acc.status === "fulfilled" ? acc.value : null);
  }, [address, getWalletBalance, getAccount]);

  useEffect(() => {
    if (!isOpen || !isConnected || !address) return;
    let cancelled = false;
    setIsLoadingBalances(true);
    void refreshBalances(() => cancelled).finally(() => {
      if (!cancelled) setIsLoadingBalances(false);
    });
    return () => { cancelled = true; };
  }, [isOpen, isConnected, address, refreshBalances]);

  const maxUnits = action === "deposit" ? walletBalance : withdrawable;

  const marginRatio = useMemo(() => {
    if (!account || account.equityUsd <= 0n) return null;
    return (Number(account.maintenanceMarginUsd) / Number(account.equityUsd)) * 100;
  }, [account]);

  const handleSubmit = useCallback(async () => {
    if (!address || amountUnits <= 0n) return;
    const shown = formatUnits(amountUnits, USDL.decimals);
    setIsProcessing(true); setError(null); setSuccess(null);
    try {
      if (action === "deposit") {
        await deposit(address, amountUnits, setTxStep);
        setSuccess(`Deposited ${shown} ${USDL.symbol}`);
      } else {
        setTxStep("executing");
        await withdraw(amountUnits);
        setSuccess(`Withdrew ${shown} ${USDL.symbol}`);
      }
      setTxStep("confirming");
      setAmount("");
      // Refresh live balances so the "Max" row and Stats update immediately.
      await refreshBalances();
    } catch (err) {
      const ufe = classifyError(err, `account/${action}`);
      // Local inline-error block — classified message + optional fix, never raw err.message.
      setError(ufe.suggestedFix ? `${ufe.message} ${ufe.suggestedFix}` : ufe.message);
      void reportError(ufe);
    } finally {
      setIsProcessing(false); setTxStep("idle");
      setTimeout(() => setSuccess(null), 4000);
    }
  }, [address, amountUnits, action, deposit, withdraw, refreshBalances]);

  const handleWithdrawAll = useCallback(async () => {
    if (!address) return;
    setIsWithdrawingAll(true); setError(null);
    try {
      await withdrawAll(address);
      toast({ title: "Withdrew all available collateral" });
      await refreshBalances();
    } catch (err) {
      toastError(err, {
        context: "account/withdraw-all",
        titleOverride: "Withdraw all failed",
      });
    } finally {
      setIsWithdrawingAll(false);
    }
  }, [address, withdrawAll, refreshBalances]);

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-end sm:items-center justify-center sm:p-4">
      <div className="absolute inset-0 bg-black/70 backdrop-blur-sm" onClick={onClose} />
      {/*
        Card is a flex column with capped max-height. The header stays pinned
        (shrink-0) while the body scrolls if content exceeds the viewport — this
        prevents the previous behavior where tall content (esp. with the
        withdraw-all block) got clipped off the top of the screen.
      */}
      <div className="relative w-full sm:max-w-md mx-0 sm:mx-0 rounded-t-2xl sm:rounded-2xl border border-[var(--brand-border)] bg-[var(--brand-surface)] shadow-2xl overflow-hidden flex flex-col max-h-[calc(100vh-2rem)] sm:max-h-[90vh]">

        {/* ── Header ── */}
        <div className="flex items-center justify-between px-5 pt-5 pb-4 shrink-0 border-b border-[var(--brand-border)]">
          <div>
            <h2 className="text-base font-semibold">
              {action === "deposit" ? "Deposit Collateral" : "Withdraw Collateral"}
            </h2>
            <p className="text-xs text-muted-foreground mt-0.5">
              {action === "deposit" ? "Move USDL from your wallet into your cross-margin account." : "Move withdrawable USDL from your account back to your wallet."}
            </p>
          </div>
          <button onClick={onClose} className="ml-3 shrink-0 rounded-full p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground transition-colors">
            <X className="h-4 w-4" />
          </button>
        </div>

        {/* ── Body ─────────────────────────────────────────────────────
           Mirrors the portfolio page's inline deposit/withdraw form:
           grid tab toggle, amount input with 25/50/75/100 preset buttons,
           and a single button whose label adapts to wallet state.
           Withdraw-all hangs off the bottom.

           IMPORTANT: `min-h-0` is required on a flex-1 child inside a
           `flex-col` parent, otherwise the child's intrinsic content
           height wins and the parent's `max-h` is silently ignored
           (which previously pushed the modal header above the viewport).
        */}
        <div className="px-5 pt-4 pb-5 space-y-4 overflow-y-auto flex-1 min-h-0">
          {/* Action toggle — grid style matching portfolio page */}
          <div className="grid grid-cols-2 gap-1 p-0.5 rounded-lg bg-muted/50">
            <button
              className={cn(
                "py-2 rounded-md text-sm font-semibold transition-all flex items-center justify-center gap-1.5",
                action === "deposit" ? "bg-background shadow text-foreground" : "text-muted-foreground"
              )}
              onClick={() => { setAction("deposit"); setAmount(""); setError(null); }}
            >
              <ArrowDownToLine className="h-4 w-4" /> Deposit
            </button>
            <button
              className={cn(
                "py-2 rounded-md text-sm font-semibold transition-all flex items-center justify-center gap-1.5",
                action === "withdraw" ? "bg-background shadow text-foreground" : "text-muted-foreground"
              )}
              onClick={() => { setAction("withdraw"); setAmount(""); setError(null); }}
            >
              <ArrowUpFromLine className="h-4 w-4" /> Withdraw
            </button>
          </div>

          {/* Amount input + preset buttons */}
          <div>
            <div className="flex items-center justify-between mb-1">
              <label className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Amount ({USDL.symbol})</label>
              <button
                className="text-[10px] text-primary hover:text-primary/80 font-medium disabled:opacity-50"
                onClick={() => setAmount(formatUnits(maxUnits, USDL.decimals))}
                disabled={isLoadingBalances || !isConnected}
              >
                {isLoadingBalances ? (
                  <span className="inline-flex items-center gap-1">
                    <Loader2 className="h-2.5 w-2.5 animate-spin" />
                    Loading…
                  </span>
                ) : (
                  <>Max: {fmtBal(maxUnits)} {USDL.symbol}</>
                )}
              </button>
            </div>
            <input
              className="w-full rounded-[var(--radius-md)] bg-surface-2 px-3 py-2 text-sm font-mono"
              placeholder="0.00"
              type="text"
              inputMode="decimal"
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              disabled={!isConnected}
            />
            <div className="grid grid-cols-4 gap-1 mt-1.5">
              {[25n, 50n, 75n, 100n].map((pct) => (
                <button
                  key={pct.toString()}
                  className="py-1 rounded text-[10px] font-medium bg-muted/50 text-muted-foreground hover:text-foreground transition-all disabled:opacity-40"
                  disabled={!isConnected || isLoadingBalances}
                  onClick={() => setAmount(formatUnits((maxUnits * pct) / 100n, USDL.decimals))}
                >
                  {pct.toString()}%
                </button>
              ))}
            </div>
          </div>

          {/* Quick account snapshot: collateral, withdrawable and margin ratio */}
          {isConnected && (
            <div className="grid grid-cols-3 gap-2 rounded-lg bg-surface-1 p-2.5">
              <div className="text-center">
                <div className="text-[9px] font-medium uppercase tracking-wider text-muted-foreground">Collateral</div>
                <div className="text-xs font-mono font-semibold mt-0.5">
                  {isLoadingBalances ? "…" : account ? fmtBal(account.collateral) : "—"}
                </div>
              </div>
              <div className="text-center border-x border-border">
                <div className="text-[9px] font-medium uppercase tracking-wider text-muted-foreground">Withdrawable</div>
                <div className="text-xs font-mono font-semibold mt-0.5">
                  {isLoadingBalances ? "…" : account ? fmtBal(account.withdrawable) : "—"}
                </div>
              </div>
              <div className="text-center">
                <div className="text-[9px] font-medium uppercase tracking-wider text-muted-foreground">Margin ratio</div>
                <div className={cn("text-xs font-mono font-semibold mt-0.5", marginRatio === null || marginRatio < 50 ? "text-emerald-500" : marginRatio < 80 ? "text-amber-500" : "text-red-500")}>
                  {isLoadingBalances ? "…" : marginRatio === null ? "—" : `${marginRatio.toFixed(1)}%`}
                </div>
              </div>
            </div>
          )}

          {/* Transaction progress */}
          {txStep !== "idle" && (
            <div className="flex items-center gap-2 text-xs text-muted-foreground bg-muted/30 rounded-lg px-3 py-2">
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
              {txStep === "approving" && "Approving token spend..."}
              {txStep === "executing" && (action === "deposit" ? "Depositing..." : "Withdrawing...")}
              {txStep === "confirming" && "Confirming on-chain..."}
            </div>
          )}

          {error && (
            <div className="flex items-start gap-2 text-xs text-red-500 bg-red-500/10 rounded-lg px-3 py-2">
              <AlertTriangle className="h-3.5 w-3.5 mt-0.5 shrink-0" />
              <span>{error}</span>
            </div>
          )}
          {success && (
            <div className="flex items-center gap-2 text-xs text-emerald-500 bg-emerald-500/10 rounded-lg px-3 py-2">
              <Check className="h-3.5 w-3.5" />
              <span>{success}</span>
            </div>
          )}

          {/* Single adaptive CTA: connect → deposit/withdraw */}
          <Button
            className={cn("w-full font-semibold", action === "deposit" && isConnected ? "bg-emerald-600 hover:bg-emerald-700" : "")}
            size="lg"
            disabled={
              isConnected && (isProcessing || amountUnits <= 0n || isLoadingBalances)
            }
            onClick={() => {
              if (!isConnected) { connect(); onClose(); return; }
              void handleSubmit();
            }}
          >
            {isProcessing ? (
              <Loader2 className="h-4 w-4 animate-spin mr-2" />
            ) : !isConnected ? null : action === "deposit" ? (
              <ArrowDownToLine className="h-4 w-4 mr-2" />
            ) : (
              <ArrowUpFromLine className="h-4 w-4 mr-2" />
            )}
            {!isConnected
              ? "Connect wallet"
              : `${action === "deposit" ? "Deposit" : "Withdraw"} ${USDL.symbol}`}
          </Button>

          <p className="text-[10px] text-muted-foreground text-center">
            {!isConnected
              ? "Connect your wallet to move funds in or out of your trading account."
              : action === "deposit"
                ? "USDL moves from your wallet into your cross-margin account; the first deposit opens it."
                : "Only the withdrawable balance can be withdrawn."}
          </p>

          {/* ── Withdraw all: withdraw(USDL, getAccount(account).withdrawable) ── */}
          {isConnected && (
            <div className="border-t border-white/[0.06] pt-3 mt-3">
              <button
                type="button"
                className={cn(
                  "w-full rounded-xl h-9 text-xs font-semibold transition-all",
                  withdrawable > 0n && !isWithdrawingAll
                    ? "bg-muted/50 text-foreground hover:bg-muted"
                    : "bg-muted/30 text-muted-foreground cursor-not-allowed opacity-50"
                )}
                disabled={withdrawable <= 0n || isWithdrawingAll || isProcessing || isLoadingBalances}
                onClick={() => { void handleWithdrawAll(); }}
              >
                {isWithdrawingAll
                  ? "Processing..."
                  : `Withdraw all (${fmtBal(withdrawable)} ${USDL.symbol})`}
              </button>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
