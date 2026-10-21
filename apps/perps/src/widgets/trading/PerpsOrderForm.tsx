import React, { useState, useCallback, useMemo, useEffect, useRef } from "react";
import { cn } from "@/ui/utils/cn";
import { Slider } from "@/ui/atoms/slider";
import { Switch } from "@/ui/atoms/switch";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/ui/atoms/tooltip";
import { usePerpsWallet } from "@/hooks/perps/usePerpsWallet";
import { usePortfolioStats } from "@/hooks/perps/usePortfolioStats";
import { usePerpsOrders } from "@/hooks/perps/usePerpsOrders";
import { useQuoter } from "@/hooks/perps/useQuoter";
import type { OpenPositionQuote } from "@/hooks/perps/useQuoter";
import {
  COLLATERAL_OPTIONS,
  type PerpsMarket,
} from "@/core/perps/config";
import {
  safeParseUnits,
  getCollateralDecimals,
  deriveImpliedCollateral,
} from "@/core/perps/amounts";
import { Info, Settings2 } from "lucide-react";
import { IcoAlert, IcoExclamation } from "@/ui/atoms/custom-icon";
import { Popover, PopoverContent, PopoverTrigger } from "@/ui/atoms/popover";
import { useOrderLineStore } from "@/hooks/perps/useOrderLineStore";
import { classifyError } from "@/core/errors/classify";
import { reportError } from "@/core/errors/report";
import type { UserFacingError } from "@/core/errors/types";
import {
  buildConfirmation,
  buildOrderPlan,
  needsConfirmation,
  needsRequote,
  selectOrderOption,
  successReset,
  type OrderConfirmation,
  type OrderOptions,
  type OrderPlan,
} from "@/core/perps/orderPlan";

type OrderType = "market" | "limit" | "stop-limit" | "scaled";
type QtyDistribution = "flat" | "asc" | "desc" | "custom";

function n18(val: bigint | undefined | null): number {
  if (val == null) return 0;
  return Number(val) / 1e18;
}

function formatUsd(val: number | undefined | null, fallback = "—"): string {
  if (val === undefined || val === null || isNaN(val)) return fallback;
  if (val >= 1_000_000) return `$${(val / 1_000_000).toFixed(2)}M`;
  return `$${val.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
}

// ── Paxeer field-box styles (shared across inputs) ───────────────────
const FIELD_BOX: React.CSSProperties = {
  padding: "10px 12px",
  background: "var(--bg-subtle)",
  border: "1px solid var(--border-soft)",
  borderRadius: 10,
  display: "flex",
  flexDirection: "column",
  gap: 4,
};
const FIELD_LABEL: React.CSSProperties = {
  fontFamily: "var(--font-mono)",
  fontSize: 9,
  letterSpacing: "0.14em",
  textTransform: "uppercase",
  color: "var(--fg-subtle)",
};
const FIELD_INPUT: React.CSSProperties = {
  flex: 1,
  background: "none",
  border: "none",
  outline: "none",
  color: "var(--fg)",
  fontFamily: "var(--font-display)",
  fontSize: 20,
  fontWeight: 400,
  letterSpacing: "-0.01em",
  width: "100%",
  fontVariantNumeric: "tabular-nums",
};
const FIELD_UNIT: React.CSSProperties = {
  fontFamily: "var(--font-mono)",
  fontSize: 10,
  color: "var(--fg-muted)",
  letterSpacing: "0.08em",
};
const META_LABEL: React.CSSProperties = {
  fontFamily: "var(--font-mono)",
  fontSize: 9,
  letterSpacing: "0.12em",
  textTransform: "uppercase",
  color: "var(--fg-subtle)",
};

// ── Detail row with optional tooltip ─────────────────────────────────
function DetailRow({ label, value, tooltip, valueClass }: {
  label: string; value: string; tooltip?: string; valueClass?: string;
}) {
  return (
    <div className="flex items-center justify-between py-0.5">
      <div
        className="flex items-center gap-1"
        style={{ ...META_LABEL, color: "var(--fg-muted)" }}
      >
        {label}
        {tooltip && (
          <TooltipProvider>
            <Tooltip delayDuration={0}>
              <TooltipTrigger><Info className="w-3 h-3" /></TooltipTrigger>
              <TooltipContent side="left" className="max-w-[200px]">{tooltip}</TooltipContent>
            </Tooltip>
          </TooltipProvider>
        )}
      </div>
      <span
        className={cn("tabular-nums", valueClass)}
        style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg)" }}
      >
        {value}
      </span>
    </div>
  );
}

const LEVERAGE_PRESETS = ["5", "10", "20", "50", "100"];

// ── Risk rate bar ────────────────────────────────────────────────────
function RiskRateBar({ leverage, maxLev }: { leverage: number; maxLev: number }) {
  const pct = Math.min((leverage / maxLev) * 100, 100);
  const barColor =
    pct < 30 ? "var(--success)" : pct < 60 ? "var(--warning)" : "var(--danger)";
  const label = pct < 30 ? "Low" : pct < 60 ? "Medium" : "High";
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
      <div className="flex items-center justify-between">
        <span style={META_LABEL}>Risk rate</span>
        <span
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 10,
            letterSpacing: "0.1em",
            textTransform: "uppercase",
            color: barColor,
          }}
        >
          {label}
        </span>
      </div>
      <div
        style={{
          position: "relative",
          height: 4,
          borderRadius: 999,
          background: "var(--bg-subtle)",
          overflow: "hidden",
        }}
      >
        <div
          style={{
            position: "absolute",
            inset: 0,
            width: `${pct}%`,
            background: barColor,
            transition: "width 300ms, background 300ms",
            borderRadius: 999,
          }}
        />
      </div>
    </div>
  );
}

// ── Toggle row helper ────────────────────────────────────────────────
function ToggleRow({ label, checked, onChange, disabled }: { label: string; checked: boolean; onChange: (v: boolean) => void; disabled?: boolean }) {
  return (
    <label className={cn("flex items-center justify-between py-0.5 cursor-pointer", disabled && "opacity-40 cursor-not-allowed")}>
      <span style={{ ...META_LABEL, fontSize: 10 }}>{label}</span>
      <Switch checked={checked} onCheckedChange={onChange} disabled={disabled} className="h-4 w-7" />
    </label>
  );
}

const MAX_LADDER_ID = 1n << 96n;

/** A fresh non-zero 96-bit ladder id, so every submitted order carries an unused cloid. */
function newLadderId(): bigint {
  const bytes = crypto.getRandomValues(new Uint8Array(12));
  const id = bytes.reduce((acc, b) => (acc << 8n) | BigInt(b), 0n);
  return id === 0n || id >= MAX_LADDER_ID ? 1n : id;
}

function errorText(ufe: UserFacingError): string {
  return ufe.suggestedFix ? `${ufe.message} ${ufe.suggestedFix}` : ufe.message;
}

interface PendingOrder {
  plan: OrderPlan;
  confirmation: OrderConfirmation;
}

function quoteInputHash(params: {
  marketId: bigint;
  collateralToken: string;
  collateralBigInt: bigint;
  leverageBigInt: bigint;
  isLong: boolean;
}): string {
  return [
    params.marketId.toString(),
    params.collateralToken.toLowerCase(),
    params.collateralBigInt.toString(),
    params.leverageBigInt.toString(),
    params.isLong ? "L" : "S",
  ].join("|");
}

interface BoundQuote {
  quote: OpenPositionQuote;
  inputHash: string;
  takenAt: number;
}

export function PerpsOrderForm({ market, compact = false }: { market: PerpsMarket; compact?: boolean }) {
  const { isConnected, connect, address, isCorrectChain, switchToPaxeer } = usePerpsWallet();
  const portfolioStats = usePortfolioStats();

  const ordersHook = usePerpsOrders();
  const quoterHook = useQuoter();
  const ordersRef = useRef(ordersHook);
  ordersRef.current = ordersHook;
  const quoterRef = useRef(quoterHook);
  quoterRef.current = quoterHook;

  // ── Core state ──
  const [orderType, setOrderType] = useState<OrderType>("market");
  const [isLong, setIsLong] = useState(true);
  const [collateralToken, setCollateralToken] = useState<string>(COLLATERAL_OPTIONS[0]?.value ?? "");
  const [collateralAmount, setCollateralAmount] = useState("");
  const [leverage, setLeverage] = useState("2");
  const [triggerPrice, setTriggerPrice] = useState("");
  const [limitPrice, setLimitPrice] = useState("");
  const [sizeUsd, setSizeUsd] = useState("");

  // ── Scaled order state ──
  const [scaledStartPrice, setScaledStartPrice] = useState("");
  const [scaledEndPrice, setScaledEndPrice] = useState("");
  const [scaledTotalOrders, setScaledTotalOrders] = useState("5");
  const [scaledDistribution, setScaledDistribution] = useState<QtyDistribution>("flat");

  // ── Toggles ──
  const [tpslEnabled, setTpslEnabled] = useState(false);
  const [tpPrice, setTpPrice] = useState("");
  const [slPrice, setSlPrice] = useState("");

  // ── Sync order form → chart order lines store ──
  const olStore = useOrderLineStore;
  const chartDragSeq = useOrderLineStore((s) => s._chartDragSeq);

  // Push form values to store whenever they change
  useEffect(() => {
    olStore.getState().setOrderType(orderType as any);
  }, [orderType]);
  useEffect(() => {
    olStore.getState().setIsLong(isLong);
  }, [isLong]);
  useEffect(() => {
    const p = parseFloat(triggerPrice);
    olStore.getState().setEntryPrice(isNaN(p) ? 0 : p);
  }, [triggerPrice]);
  useEffect(() => {
    const p = parseFloat(tpPrice);
    olStore.getState().setTpPrice(isNaN(p) ? 0 : p);
  }, [tpPrice]);
  useEffect(() => {
    const p = parseFloat(slPrice);
    olStore.getState().setSlPrice(isNaN(p) ? 0 : p);
  }, [slPrice]);
  useEffect(() => {
    olStore.getState().setTpslEnabled(tpslEnabled);
  }, [tpslEnabled]);

  // Pull chart drag updates back into form inputs.
  // Use refs to read the latest input values without retriggering this effect
  // (adding them to deps would cause a loop since typing updates the store → drag seq).
  const triggerPriceRef = useRef(triggerPrice);
  const tpPriceRef = useRef(tpPrice);
  const slPriceRef = useRef(slPrice);
  triggerPriceRef.current = triggerPrice;
  tpPriceRef.current = tpPrice;
  slPriceRef.current = slPrice;
  useEffect(() => {
    if (chartDragSeq === 0) return;
    const s = olStore.getState();
    if (s.entryPrice > 0) {
      const ep = s.entryPrice.toFixed(2);
      if (ep !== triggerPriceRef.current) setTriggerPrice(ep);
    }
    if (s.tpPrice > 0) {
      const tp = s.tpPrice.toFixed(2);
      if (tp !== tpPriceRef.current) setTpPrice(tp);
    }
    if (s.slPrice > 0) {
      const sl = s.slPrice.toFixed(2);
      if (sl !== slPriceRef.current) setSlPrice(sl);
    }
  }, [chartDragSeq, olStore]);
  const [reduceOnly, setReduceOnly] = useState(false);
  const [orderOptions, setOrderOptions] = useState<OrderOptions>({ postOnly: false, ioc: false, fok: false });
  const { postOnly, ioc, fok } = orderOptions;
  const setOrderOption = useCallback(
    (option: keyof OrderOptions, on: boolean) => setOrderOptions((o) => selectOrderOption(o, option, on)),
    [],
  );
  const [orderConfirm, setOrderConfirm] = useState(true);
  const [pending, setPending] = useState<PendingOrder | null>(null);

  // ── TX state ──
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [isQuoting, setIsQuoting] = useState(false);
  const [quote, setQuote] = useState<OpenPositionQuote | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  const maxLev = market.maxLeverage ?? 1000;

  const leveragePresets = useMemo(() => {
    if (maxLev >= 1000) return [2, 10, 50, 200, 500];
    if (maxLev >= 500) return [2, 10, 50, 100, 250];
    return [2, 5, 10, 50, 100];
  }, [maxLev]);

  // ── BigInt conversions ──
  // All on-chain inputs go through viem's `parseUnits` via
  // `safeParseUnits`. Never use `parseFloat(x) * 10**decimals` — JS
  // floats can't represent decimal fractions exactly and we'd silently
  // send the wrong wei to the matching engine at high precision.
  const collateralDecimals = useMemo(
    () => getCollateralDecimals(collateralToken),
    [collateralToken],
  );

  const collateralBigInt = useMemo(
    () => safeParseUnits(collateralAmount, collateralDecimals),
    [collateralAmount, collateralDecimals],
  );

  const leverageBigInt = useMemo(() => {
    const parsed = safeParseUnits(leverage, 18);
    // Reject sub-1x leverage at the conversion layer — the rest of the
    // form treats leverageBigInt === 0n as "not ready to trade".
    if (parsed < 10n ** 18n) return 0n;
    return parsed;
  }, [leverage]);

  const triggerPriceBigInt = useMemo(
    () => safeParseUnits(triggerPrice, 18),
    [triggerPrice],
  );

  const limitPriceBigInt = useMemo(
    () => safeParseUnits(limitPrice, 18),
    [limitPrice],
  );

  const sizeUsdBigInt = useMemo(
    () => safeParseUnits(sizeUsd, 18),
    [sizeUsd],
  );

  // ── Derived values ──
  const leverageNum = parseFloat(leverage) || 1;
  const collateralNum = parseFloat(collateralAmount) || 0;

  // For LIMIT / STOP-LIMIT orders the form does not expose a dedicated collateral
  // input — the user specifies the notional SIZE and LEVERAGE directly. Collateral
  // is therefore implied: collateral = sizeUsd / leverage. Integer-only math —
  // sizeUsd and leverage are both 1e18-scaled bigints, and we rescale into the
  // collateral token's native decimals via `deriveImpliedCollateral`.
  const effectiveCollateralBigInt = useMemo(() => {
    if (orderType === "market") return collateralBigInt;
    return deriveImpliedCollateral(sizeUsdBigInt, leverageBigInt, collateralDecimals);
  }, [orderType, collateralBigInt, sizeUsdBigInt, leverageBigInt, collateralDecimals]);

  const estimatedSize = useMemo(() => {
    if (orderType === "market") {
      return collateralNum > 0 && leverageNum >= 1 ? collateralNum * leverageNum : null;
    }
    const s = parseFloat(sizeUsd);
    return isNaN(s) || s <= 0 ? null : s;
  }, [orderType, collateralNum, leverageNum, sizeUsd]);

  const orderSizeDisplay = estimatedSize ? formatUsd(estimatedSize) : "\u2014";
  const maxBuy = useMemo(() => {
    if (orderType === "market") {
      return collateralNum > 0 ? formatUsd(collateralNum * maxLev) : "\u2014";
    }
    const avail = portfolioStats.freeCollateral || 0;
    return avail > 0 ? formatUsd(avail * maxLev) : "\u2014";
  }, [orderType, collateralNum, maxLev, portfolioStats.freeCollateral]);

  const canTrade = useMemo(() => {
    if (!isConnected) return false;
    // F-03: never allow submit while the wallet is on a different chain.
    // Trying to send a Paxeer-bound write through a wrong-chain provider
    // either errors at the wallet layer or, worse, broadcasts on a chain
    // where our contracts don't exist.
    if (!isCorrectChain) return false;
    if (effectiveCollateralBigInt === BigInt(0) || leverageBigInt === BigInt(0)) return false;
    if (orderType === "limit" && triggerPriceBigInt === BigInt(0)) return false;
    if (orderType === "stop-limit" && (triggerPriceBigInt === BigInt(0) || limitPriceBigInt === BigInt(0))) return false;
    if (orderType !== "market" && sizeUsdBigInt === BigInt(0)) return false;
    if (
      orderType === "scaled" &&
      (scaledDistribution === "custom" ||
        safeParseUnits(scaledStartPrice, 18) === BigInt(0) ||
        safeParseUnits(scaledEndPrice, 18) === BigInt(0))
    ) {
      return false;
    }
    return true;
  }, [isConnected, isCorrectChain, effectiveCollateralBigInt, leverageBigInt, orderType, triggerPriceBigInt, limitPriceBigInt, sizeUsdBigInt, scaledDistribution, scaledStartPrice, scaledEndPrice]);

  // ── Auto-quote for market orders ──
  // F-11: the quote that comes back from the indexer is bound to the
  // exact (marketId, collateralToken, collateralAmount, leverage, side)
  // tuple that was sent. We track that tuple + a timestamp so the
  // submit path can refuse to forward a quote that no longer matches
  // the live inputs (user edited mid-flight) or has gone stale.
  const boundQuoteRef = useRef<BoundQuote | null>(null);
  useEffect(() => {
    const timer = setTimeout(async () => {
      if (orderType !== "market") {
        setQuote(null);
        boundQuoteRef.current = null;
        return;
      }
      if (collateralBigInt === BigInt(0) || leverageBigInt === BigInt(0)) {
        setQuote(null);
        boundQuoteRef.current = null;
        return;
      }
      const inputs = {
        marketId: BigInt(market.marketId),
        collateralToken: collateralToken as `0x${string}`,
        collateralAmount: collateralBigInt,
        leverage: leverageBigInt,
        isLong,
      };
      const inputHash = quoteInputHash({
        marketId: inputs.marketId,
        collateralToken: inputs.collateralToken,
        collateralBigInt: inputs.collateralAmount,
        leverageBigInt: inputs.leverage,
        isLong: inputs.isLong,
      });
      setIsQuoting(true);
      try {
        const result = await quoterRef.current.quoteOpenPosition(inputs);
        setQuote(result ?? null);
        boundQuoteRef.current = result
          ? { quote: result, inputHash, takenAt: Date.now() }
          : null;
      } catch {
        setQuote(null);
        boundQuoteRef.current = null;
      } finally { setIsQuoting(false); }
    }, 500);
    return () => clearTimeout(timer);
  }, [collateralAmount, leverage, isLong, collateralToken, orderType, market.marketId, collateralBigInt, leverageBigInt]);

  // ── Submit ──
  const resetAfterSuccess = useCallback(() => {
    const reset = successReset("success");
    if (!reset) return;
    setCollateralAmount(reset.inputs.collateralAmount);
    setTriggerPrice(reset.inputs.triggerPrice);
    setLimitPrice(reset.inputs.limitPrice);
    setSizeUsd(reset.inputs.sizeUsd);
    setTpPrice(reset.inputs.tpPrice);
    setSlPrice(reset.inputs.slPrice);
    setScaledStartPrice(reset.inputs.scaledStartPrice);
    setScaledEndPrice(reset.inputs.scaledEndPrice);
    const lines = olStore.getState();
    lines.setEntryPrice(reset.lines.entryPrice);
    lines.setTpPrice(reset.lines.tpPrice);
    lines.setSlPrice(reset.lines.slPrice);
  }, [olStore]);

  const sendOrder = useCallback(async (plan: OrderPlan) => {
    setIsSubmitting(true); setError(null); setSuccess(null);
    try {
      await ordersRef.current.submitOrderPlan(plan.calls);
      const placed = plan.calls.length > 1 ? `${plan.calls.length} scaled orders placed` : "Order placed";
      setSuccess(orderType === "market" ? `${isLong ? "Long" : "Short"} position opened on ${market.symbol}/USD` : placed);
      resetAfterSuccess();
    } catch (err) {
      // Never surface raw err.message — always go through the classifier
      // so the inline error block gets a friendly, actionable string.
      const ufe = classifyError(err, `perps-order/${orderType}`);
      setError(errorText(ufe));
      void reportError(ufe);
    } finally {
      setIsSubmitting(false);
      setTimeout(() => setSuccess(null), 3000);
    }
  }, [orderType, isLong, market.symbol, resetAfterSuccess]);

  /** The current quote of the market inputs; an out-of-sync or aged quote is re-fetched. */
  const currentQuote = useCallback(async (): Promise<OpenPositionQuote> => {
    const inputs = {
      marketId: BigInt(market.marketId),
      collateralToken: collateralToken as `0x${string}`,
      collateralAmount: collateralBigInt,
      leverage: leverageBigInt,
      isLong,
    };
    const inputHash = quoteInputHash({
      marketId: inputs.marketId,
      collateralToken: inputs.collateralToken,
      collateralBigInt: inputs.collateralAmount,
      leverageBigInt: inputs.leverage,
      isLong: inputs.isLong,
    });
    const bound = boundQuoteRef.current;
    if (bound && bound.inputHash === inputHash && !needsRequote(Date.now() - bound.takenAt)) return bound.quote;
    const fresh = await quoterRef.current.quoteOpenPosition(inputs);
    boundQuoteRef.current = { quote: fresh, inputHash, takenAt: Date.now() };
    setQuote(fresh);
    return fresh;
  }, [market.marketId, collateralToken, collateralBigInt, leverageBigInt, isLong]);

  const handleSubmit = useCallback(async () => {
    if (!canTrade) return;
    // F-03 (defense in depth): the canTrade memo already covers this,
    // but `canTrade` is reactive UI state — re-check at submit time so
    // the wallet can't slip across chains between click and dispatch.
    if (!isCorrectChain) {
      setError("Wrong network — switch to Paxeer Mainnet before placing this order.");
      return;
    }
    setError(null); setSuccess(null); setPending(null);
    let marketQuote: OpenPositionQuote | null = null;
    if (orderType === "market") {
      setIsQuoting(true);
      try {
        marketQuote = await currentQuote();
      } catch (err) {
        const ufe = classifyError(err, "perps-order/quote");
        setError(errorText(ufe));
        void reportError(ufe);
        return;
      } finally {
        setIsQuoting(false);
      }
    }
    const plan = buildOrderPlan(
      {
        orderType,
        marketId: BigInt(market.marketId),
        isLong,
        collateralAmount,
        leverage,
        triggerPrice,
        limitPrice,
        sizeUsd,
        scaledStartPrice,
        scaledEndPrice,
        scaledTotalOrders,
        scaledDistribution,
        tpslEnabled,
        tpPrice,
        slPrice,
        reduceOnly,
        postOnly,
        ioc,
        fok,
        ladderId: newLadderId(),
      },
      marketQuote,
    );
    if (plan.error) {
      setError(errorText(plan.error));
      return;
    }
    const confirmation = buildConfirmation(marketQuote, plan);
    if (needsConfirmation(orderConfirm) && confirmation) {
      setPending({ plan, confirmation });
      return;
    }
    await sendOrder(plan);
  }, [canTrade, isCorrectChain, orderType, currentQuote, market.marketId, isLong, collateralAmount, leverage, triggerPrice, limitPrice, sizeUsd, scaledStartPrice, scaledEndPrice, scaledTotalOrders, scaledDistribution, tpslEnabled, tpPrice, slPrice, reduceOnly, postOnly, ioc, fok, orderConfirm, sendOrder]);

  const acceptPending = useCallback(async () => {
    if (!pending) return;
    const { plan } = pending;
    setPending(null);
    await sendOrder(plan);
  }, [pending, sendOrder]);

  // A confirmation shows the plan of the inputs it was built from; any edit discards it.
  useEffect(() => {
    setPending(null);
  }, [orderType, isLong, collateralAmount, leverage, triggerPrice, limitPrice, sizeUsd, scaledStartPrice, scaledEndPrice, scaledTotalOrders, scaledDistribution, tpslEnabled, tpPrice, slPrice, reduceOnly, orderOptions]);

  const submitLabel = useMemo(() => {
    const action = isLong ? "Buy / Long" : "Sell / Short";
    return `${action}`;
  }, [isLong]);

  // ── Paxeer visual tokens for this render ──
  const accent = isLong ? "var(--success)" : "var(--danger)";
  const accentShadow = isLong
    ? "0 8px 20px -8px rgba(5, 193, 104, 0.5)"
    : "0 8px 20px -8px rgba(255, 90, 101, 0.5)";
  const previewBg = isLong ? "rgba(5, 193, 104, 0.14)" : "rgba(255, 90, 101, 0.14)";
  const previewBorder = isLong
    ? "1px solid rgba(5, 193, 104, 0.3)"
    : "1px solid rgba(255, 90, 101, 0.3)";

  const estLiqPrice = quote ? n18(quote.liquidationPrice) : null;
  const estFees = quote ? n18(quote.tradingFeeUsd) : null;
  const estSlippagePct = quote ? n18(quote.priceImpact) * 100 : null;

  const orderTypeLabels: Record<OrderType, string> = {
    market: "market",
    limit: "limit",
    "stop-limit": "stop",
    scaled: "scaled",
  };

  return (
    <div className={cn("flex flex-col", !compact && "h-full")}>
      <div
        className={cn(
          !compact
            ? "flex-1 overflow-y-auto scrollbar-custom"
            : ""
        )}
        style={{ display: "flex", flexDirection: "column", gap: 10, padding: compact ? 10 : 14 }}
      >
        {/* ═══ 1. Long/Short fat toggle ═══ */}
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "1fr 1fr",
            gap: 4,
            padding: 4,
            background: "var(--bg-subtle)",
            border: "1px solid var(--border-soft)",
            borderRadius: 12,
          }}
        >
          <button
            onClick={() => setIsLong(true)}
            style={{
              padding: "10px",
              borderRadius: 8,
              border: "none",
              background: isLong ? "var(--success)" : "transparent",
              color: isLong ? "#fff" : "var(--fg-muted)",
              fontFamily: "var(--font-body)",
              fontSize: 13,
              fontWeight: 600,
              cursor: "pointer",
              transition: "all 150ms",
              letterSpacing: "0.04em",
              textTransform: "uppercase",
            }}
          >
            ▲ Long
          </button>
          <button
            onClick={() => setIsLong(false)}
            style={{
              padding: "10px",
              borderRadius: 8,
              border: "none",
              background: !isLong ? "var(--danger)" : "transparent",
              color: !isLong ? "#fff" : "var(--fg-muted)",
              fontFamily: "var(--font-body)",
              fontSize: 13,
              fontWeight: 600,
              cursor: "pointer",
              transition: "all 150ms",
              letterSpacing: "0.04em",
              textTransform: "uppercase",
            }}
          >
            ▼ Short
          </button>
        </div>

        {/* ═══ 2. Available / Max row ═══ */}
        <div className="flex items-center justify-between" style={META_LABEL}>
          <span>
            AVAIL{" "}
            <span style={{ color: "var(--fg)" }}>
              {isConnected
                ? portfolioStats.isLoading
                  ? "…"
                  : `$${portfolioStats.freeCollateral.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`
                : "—"}
            </span>
          </span>
          <span>
            MAX <span style={{ color: "var(--fg)" }}>{maxBuy}</span>
          </span>
        </div>

        {/* ═══ 3. Order type + collateral (compact: two pill selects) ═══ */}
        {compact ? (
          <div style={{ display: "flex", gap: 6 }}>
            {/* Order type compact select */}
            <div style={{ flex: 1, position: "relative" }}>
              <select
                value={orderType}
                onChange={(e) => setOrderType(e.target.value as OrderType)}
                style={{
                  width: "100%",
                  padding: "7px 22px 7px 10px",
                  borderRadius: 999,
                  border: "1px solid var(--border-soft)",
                  background: "var(--bg-subtle)",
                  color: "var(--fg)",
                  fontFamily: "var(--font-mono)",
                  fontSize: 10,
                  letterSpacing: "0.1em",
                  textTransform: "uppercase",
                  cursor: "pointer",
                  appearance: "none" as const,
                  WebkitAppearance: "none" as const,
                  outline: "none",
                }}
              >
                {(["market", "limit", "stop-limit", "scaled"] as OrderType[]).map((t) => (
                  <option key={t} value={t}>{orderTypeLabels[t]}</option>
                ))}
              </select>
              <span aria-hidden style={{ position: "absolute", right: 8, top: "50%", transform: "translateY(-50%)", pointerEvents: "none", color: "var(--fg-muted)", fontSize: 8, lineHeight: 1 }}>▾</span>
            </div>
            {/* Collateral compact select */}
            {orderType !== "scaled" && (
              <div style={{ flex: 1, position: "relative" }}>
                <select
                  value={collateralToken}
                  onChange={(e) => setCollateralToken(e.target.value)}
                  style={{
                    width: "100%",
                    padding: "7px 22px 7px 10px",
                    borderRadius: 999,
                    border: "1px solid var(--border-soft)",
                    background: "var(--bg-subtle)",
                    color: "var(--fg)",
                    fontFamily: "var(--font-mono)",
                    fontSize: 10,
                    letterSpacing: "0.08em",
                    cursor: "pointer",
                    appearance: "none" as const,
                    WebkitAppearance: "none" as const,
                    outline: "none",
                  }}
                >
                  {COLLATERAL_OPTIONS.map((opt) => (
                    <option key={opt.value} value={opt.value}>{opt.label}</option>
                  ))}
                </select>
                <span aria-hidden style={{ position: "absolute", right: 8, top: "50%", transform: "translateY(-50%)", pointerEvents: "none", color: "var(--fg-muted)", fontSize: 8, lineHeight: 1 }}>▾</span>
              </div>
            )}
          </div>
        ) : (
          /* Desktop: full-width pill row */
          <div
            style={{
              display: "flex",
              gap: 2,
              padding: 2,
              background: "var(--bg-subtle)",
              border: "1px solid var(--border-soft)",
              borderRadius: 999,
              alignSelf: "start",
            }}
          >
            {(["market", "limit", "stop-limit", "scaled"] as OrderType[]).map((t) => {
              const active = orderType === t;
              return (
                <button
                  key={t}
                  onClick={() => setOrderType(t)}
                  style={{
                    padding: "5px 12px",
                    borderRadius: 999,
                    border: "none",
                    background: active ? "var(--bg)" : "transparent",
                    color: active ? "var(--fg)" : "var(--fg-muted)",
                    fontFamily: "var(--font-mono)",
                    fontSize: 10,
                    letterSpacing: "0.12em",
                    textTransform: "uppercase",
                    cursor: "pointer",
                    transition: "all 150ms",
                    flexShrink: 0,
                    whiteSpace: "nowrap",
                  }}
                >
                  {orderTypeLabels[t]}
                </button>
              );
            })}
          </div>
        )}

        {/* ═══ 4. SCALED order fields ═══ */}
        {orderType === "scaled" && (
          <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
              <div style={FIELD_BOX}>
                <span style={FIELD_LABEL}>Start</span>
                <div style={{ display: "flex", alignItems: "baseline", gap: 6 }}>
                  <input
                    type="number"
                    placeholder="0.00"
                    value={scaledStartPrice}
                    onChange={(e) => setScaledStartPrice(e.target.value)}
                    className="no-spinners"
                    style={FIELD_INPUT}
                  />
                  <span style={FIELD_UNIT}>USD</span>
                </div>
              </div>
              <div style={FIELD_BOX}>
                <span style={FIELD_LABEL}>End</span>
                <div style={{ display: "flex", alignItems: "baseline", gap: 6 }}>
                  <input
                    type="number"
                    placeholder="0.00"
                    value={scaledEndPrice}
                    onChange={(e) => setScaledEndPrice(e.target.value)}
                    className="no-spinners"
                    style={FIELD_INPUT}
                  />
                  <span style={FIELD_UNIT}>USD</span>
                </div>
              </div>
            </div>
            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
              <div style={FIELD_BOX}>
                <span style={FIELD_LABEL}>Size</span>
                <div style={{ display: "flex", alignItems: "baseline", gap: 6 }}>
                  <input
                    type="number"
                    placeholder="0.00"
                    value={sizeUsd}
                    onChange={(e) => setSizeUsd(e.target.value)}
                    className="no-spinners"
                    style={FIELD_INPUT}
                  />
                  <span style={FIELD_UNIT}>USD</span>
                </div>
              </div>
              <div style={FIELD_BOX}>
                <span style={FIELD_LABEL}>Orders</span>
                <input
                  type="number"
                  placeholder="5"
                  value={scaledTotalOrders}
                  onChange={(e) => setScaledTotalOrders(e.target.value)}
                  className="no-spinners"
                  style={FIELD_INPUT}
                />
              </div>
            </div>
            <div>
              <span style={{ ...FIELD_LABEL, display: "block", marginBottom: 6 }}>Distribution</span>
              <div style={{ display: "grid", gridTemplateColumns: "repeat(4, 1fr)", gap: 4 }}>
                {(["flat", "asc", "desc", "custom"] as QtyDistribution[]).map((d) => {
                  const active = scaledDistribution === d;
                  return (
                    <button
                      key={d}
                      onClick={() => setScaledDistribution(d)}
                      style={{
                        padding: "7px 0",
                        borderRadius: 6,
                        border: "1px solid var(--border-soft)",
                        background: active ? "var(--accent-solid)" : "var(--bg-subtle)",
                        color: active ? "#fff" : "var(--fg-muted)",
                        fontFamily: "var(--font-mono)",
                        fontSize: 10,
                        letterSpacing: "0.1em",
                        textTransform: "uppercase",
                        cursor: "pointer",
                        transition: "all 150ms",
                      }}
                    >
                      {d === "asc" ? "Asc" : d === "desc" ? "Desc" : d}
                    </button>
                  );
                })}
              </div>
            </div>
          </div>
        )}

        {/* ═══ 5. MARKET order fields ═══ */}
        {orderType === "market" && (
          <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
            <div style={FIELD_BOX}>
              <span style={FIELD_LABEL}>Size</span>
              <div style={{ display: "flex", alignItems: "baseline", gap: 6 }}>
                <input
                  type="number"
                  placeholder="0.00"
                  value={collateralAmount}
                  onChange={(e) => setCollateralAmount(e.target.value)}
                  className="no-spinners"
                  style={FIELD_INPUT}
                />
                <span style={FIELD_UNIT}>USD</span>
              </div>
            </div>
            <div className="flex items-center justify-between" style={META_LABEL}>
              <span>Order size</span>
              <span
                style={{
                  fontFamily: "var(--font-mono)",
                  fontSize: 11,
                  color: "var(--fg)",
                  fontVariantNumeric: "tabular-nums",
                }}
              >
                {orderSizeDisplay}
              </span>
            </div>
          </div>
        )}

        {/* ═══ 6. LIMIT order fields ═══ */}
        {orderType === "limit" && (
          <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
              <div style={FIELD_BOX}>
                <span style={FIELD_LABEL}>Price</span>
                <div style={{ display: "flex", alignItems: "baseline", gap: 6 }}>
                  <input
                    type="number"
                    placeholder="0.00"
                    value={triggerPrice}
                    onChange={(e) => setTriggerPrice(e.target.value)}
                    className="no-spinners"
                    style={FIELD_INPUT}
                  />
                  <span style={FIELD_UNIT}>USD</span>
                </div>
              </div>
              <div style={FIELD_BOX}>
                <span style={FIELD_LABEL}>Size</span>
                <div style={{ display: "flex", alignItems: "baseline", gap: 6 }}>
                  <input
                    type="number"
                    placeholder="0.00"
                    value={sizeUsd}
                    onChange={(e) => setSizeUsd(e.target.value)}
                    className="no-spinners"
                    style={FIELD_INPUT}
                  />
                  <span style={FIELD_UNIT}>USD</span>
                </div>
              </div>
            </div>
            {/* percent shortcuts */}
            <div
              className="flex justify-between"
              style={{ ...META_LABEL, fontSize: 10 }}
            >
              {[0, 25, 50, 75, 100].map((pct) => (
                <button
                  key={pct}
                  onClick={() => {
                    if (pct === 0) {
                      setSizeUsd("");
                      return;
                    }
                    const avail = portfolioStats.freeCollateral || 0;
                    const lev = parseFloat(leverage) || 1;
                    const maxVal = avail * lev;
                    if (maxVal > 0) setSizeUsd(((maxVal * pct) / 100).toFixed(2));
                  }}
                  className="hover:text-foreground transition-colors"
                  style={{
                    background: "transparent",
                    border: "none",
                    color: "var(--fg-subtle)",
                    fontFamily: "var(--font-mono)",
                    fontSize: 10,
                    letterSpacing: "0.1em",
                    cursor: "pointer",
                    padding: "2px 4px",
                  }}
                >
                  {pct}%
                </button>
              ))}
            </div>
          </div>
        )}

        {/* ═══ 7. STOP-LIMIT order fields ═══ */}
        {orderType === "stop-limit" && (
          <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
            <div style={FIELD_BOX}>
              <span style={FIELD_LABEL}>Trigger</span>
              <div style={{ display: "flex", alignItems: "baseline", gap: 6 }}>
                <input
                  type="number"
                  placeholder="0.00"
                  value={triggerPrice}
                  onChange={(e) => setTriggerPrice(e.target.value)}
                  className="no-spinners"
                  style={FIELD_INPUT}
                />
                <span style={FIELD_UNIT}>USD</span>
              </div>
            </div>
            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
              <div style={FIELD_BOX}>
                <span style={FIELD_LABEL}>Price</span>
                <div style={{ display: "flex", alignItems: "baseline", gap: 6 }}>
                  <input
                    type="number"
                    placeholder="0.00"
                    value={limitPrice}
                    onChange={(e) => setLimitPrice(e.target.value)}
                    className="no-spinners"
                    style={FIELD_INPUT}
                  />
                  <span style={FIELD_UNIT}>USD</span>
                </div>
              </div>
              <div style={FIELD_BOX}>
                <span style={FIELD_LABEL}>Size</span>
                <div style={{ display: "flex", alignItems: "baseline", gap: 6 }}>
                  <input
                    type="number"
                    placeholder="0.00"
                    value={sizeUsd}
                    onChange={(e) => setSizeUsd(e.target.value)}
                    className="no-spinners"
                    style={FIELD_INPUT}
                  />
                  <span style={FIELD_UNIT}>USD</span>
                </div>
              </div>
            </div>
          </div>
        )}

        {/* ═══ 8. Collateral token selector ═══ */}
        {orderType !== "scaled" && !compact && (
          <div style={FIELD_BOX}>
            <span style={FIELD_LABEL}>Collateral</span>
            <select
              value={collateralToken}
              onChange={(e) => setCollateralToken(e.target.value)}
              style={{
                ...FIELD_INPUT,
                fontSize: 14,
                fontFamily: "var(--font-body)",
                appearance: "none",
                cursor: "pointer",
              }}
            >
              {COLLATERAL_OPTIONS.map((opt) => (
                <option key={opt.value} value={opt.value}>
                  {opt.label}
                </option>
              ))}
            </select>
          </div>
        )}

        {/* ═══ 9. Leverage ═══ */}
        <div
          style={{
            ...FIELD_BOX,
            gap: 8,
          }}
        >
          <div className="flex items-center justify-between">
            <span style={FIELD_LABEL}>Leverage</span>
            <span
              style={{
                fontFamily: "var(--font-mono)",
                fontSize: 13,
                fontWeight: 600,
                color: accent,
                fontVariantNumeric: "tabular-nums",
              }}
            >
              {leverage}×
            </span>
          </div>
          <Slider
            min={1}
            max={maxLev}
            step={maxLev >= 500 ? 1 : 0.5}
            value={[parseFloat(leverage) || 1]}
            onValueChange={([v]) => setLeverage(String(v))}
            className="my-0"
          />
          <div className="flex gap-1" style={{ marginTop: 2 }}>
            {leveragePresets.map((preset) => {
              const active = parseFloat(leverage) === preset;
              return (
                <button
                  key={preset}
                  onClick={() => setLeverage(String(preset))}
                  style={{
                    flex: 1,
                    padding: "4px 0",
                    borderRadius: 6,
                    border: `1px solid ${active ? "var(--accent-solid)" : "var(--border-soft)"}`,
                    background: active ? "rgba(var(--accent-solid-rgb), 0.14)" : "transparent",
                    color: active ? "var(--color-blue-200)" : "var(--fg-muted)",
                    fontFamily: "var(--font-mono)",
                    fontSize: 10,
                    letterSpacing: "0.08em",
                    cursor: "pointer",
                    transition: "all 150ms",
                  }}
                >
                  {preset}×
                </button>
              );
            })}
          </div>
        </div>

        {/* ═══ 10. Risk rate ═══ */}
        <RiskRateBar leverage={leverageNum} maxLev={maxLev} />

        {/* ═══ 11. TP/SL + order options ═══ */}
        {(orderType === "market" || orderType === "limit") && (
          <div>
            <div className="flex items-center justify-between">
              <label className="flex items-center cursor-pointer" style={{ gap: 8 }}>
                <input
                  type="checkbox"
                  checked={tpslEnabled}
                  onChange={() => setTpslEnabled(!tpslEnabled)}
                  style={{ width: 14, height: 14, accentColor: "var(--accent-solid)" }}
                />
                <span style={META_LABEL}>TP / SL</span>
              </label>
              <Popover>
                <PopoverTrigger asChild>
                  <button
                    type="button"
                    aria-label="Order options"
                    title="Order options"
                    style={{
                      display: "grid",
                      placeItems: "center",
                      width: 24,
                      height: 24,
                      borderRadius: 6,
                      border: "1px solid var(--border-soft)",
                      background: "var(--bg-subtle)",
                      color: "var(--fg-muted)",
                      cursor: "pointer",
                    }}
                  >
                    <Settings2 className="w-3.5 h-3.5" />
                  </button>
                </PopoverTrigger>
                <PopoverContent align="end" sideOffset={6} className="w-56 p-3">
                  <div style={{ ...META_LABEL, marginBottom: 8 }}>Order options</div>
                  <div className="space-y-0.5">
                    <ToggleRow label="Reduce only" checked={reduceOnly} onChange={setReduceOnly} />
                    <ToggleRow label="Post only" checked={postOnly} onChange={(v) => setOrderOption("postOnly", v)} disabled={orderType === "market"} />
                    <ToggleRow label="IOC" checked={ioc} onChange={(v) => setOrderOption("ioc", v)} />
                    <ToggleRow label="FOK" checked={fok} onChange={(v) => setOrderOption("fok", v)} />
                    <div style={{ height: 1, background: "var(--border-soft)", margin: "6px 0" }} />
                    <ToggleRow label="Order confirm" checked={orderConfirm} onChange={setOrderConfirm} />
                  </div>
                </PopoverContent>
              </Popover>
            </div>
            {tpslEnabled && (
              <div style={{ marginTop: 8, display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
                <div style={FIELD_BOX}>
                  <span style={FIELD_LABEL}>Take profit</span>
                  <div style={{ display: "flex", alignItems: "baseline", gap: 6 }}>
                    <input
                      type="number"
                      placeholder="0.00"
                      value={tpPrice}
                      onChange={(e) => setTpPrice(e.target.value)}
                      className="no-spinners"
                      style={FIELD_INPUT}
                    />
                    <span style={FIELD_UNIT}>USD</span>
                  </div>
                </div>
                <div style={FIELD_BOX}>
                  <span style={FIELD_LABEL}>Stop loss</span>
                  <div style={{ display: "flex", alignItems: "baseline", gap: 6 }}>
                    <input
                      type="number"
                      placeholder="0.00"
                      value={slPrice}
                      onChange={(e) => setSlPrice(e.target.value)}
                      className="no-spinners"
                      style={FIELD_INPUT}
                    />
                    <span style={FIELD_UNIT}>USD</span>
                  </div>
                </div>
              </div>
            )}
          </div>
        )}

        {/* ═══ 12. Order options (stop-limit / scaled — no TP/SL row) ═══ */}
        {(orderType === "stop-limit" || orderType === "scaled") && (
          <div className="flex items-center justify-end">
            <Popover>
              <PopoverTrigger asChild>
                <button
                  type="button"
                  aria-label="Order options"
                  title="Order options"
                  style={{
                    display: "grid",
                    placeItems: "center",
                    width: 24,
                    height: 24,
                    borderRadius: 6,
                    border: "1px solid var(--border-soft)",
                    background: "var(--bg-subtle)",
                    color: "var(--fg-muted)",
                    cursor: "pointer",
                  }}
                >
                  <Settings2 className="w-3.5 h-3.5" />
                </button>
              </PopoverTrigger>
              <PopoverContent align="end" sideOffset={6} className="w-56 p-3">
                <div style={{ ...META_LABEL, marginBottom: 8 }}>Order options</div>
                <div className="space-y-0.5">
                  <ToggleRow label="Reduce only" checked={reduceOnly} onChange={setReduceOnly} />
                  <ToggleRow label="Post only" checked={postOnly} onChange={(v) => setOrderOption("postOnly", v)} />
                  <ToggleRow label="IOC" checked={ioc} onChange={(v) => setOrderOption("ioc", v)} />
                  <ToggleRow label="FOK" checked={fok} onChange={(v) => setOrderOption("fok", v)} />
                  <div style={{ height: 1, background: "var(--border-soft)", margin: "6px 0" }} />
                  <ToggleRow label="Order confirm" checked={orderConfirm} onChange={setOrderConfirm} />
                </div>
              </PopoverContent>
            </Popover>
          </div>
        )}

        {/* ═══ 13. Live preview card (notional / margin / liq) ═══
            Hidden until the user enters an amount that resolves to a non-zero
            estimated size — keeps the form lean when nothing is being placed.
            Two columns per row on both mobile and desktop. */}
        {estimatedSize !== null && estimatedSize > 0 && (
          <div
            style={{
              padding: "10px 12px",
              borderRadius: 10,
              background: previewBg,
              border: previewBorder,
              display: "grid",
              gridTemplateColumns: "1fr 1fr",
              columnGap: 16,
              rowGap: 4,
            }}
          >
            {/* Row 1: Notional · Margin */}
            <div className="flex items-center justify-between" style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-muted)" }}>
              <span>Notional</span>
              <span style={{ color: "var(--fg)", fontVariantNumeric: "tabular-nums" }}>
                {formatUsd(estimatedSize)}
              </span>
            </div>
            <div className="flex items-center justify-between" style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-muted)" }}>
              <span>Margin</span>
              <span style={{ color: "var(--fg)", fontVariantNumeric: "tabular-nums" }}>
                {leverageNum > 0 ? formatUsd(estimatedSize / leverageNum) : "—"}
              </span>
            </div>

            {/* Row 2: Est. liquidation · Slippage (slippage placeholder for limit) */}
            <div className="flex items-center justify-between" style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-muted)" }}>
              <span>Est. liquidation</span>
              <span style={{ color: accent, fontVariantNumeric: "tabular-nums" }}>
                {estLiqPrice !== null ? formatUsd(estLiqPrice) : "—"}
              </span>
            </div>
            <div className="flex items-center justify-between" style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-muted)" }}>
              <span>Slippage</span>
              <span style={{ color: "var(--fg)", fontVariantNumeric: "tabular-nums" }}>
                {orderType === "market"
                  ? estSlippagePct !== null ? `${estSlippagePct.toFixed(2)}%` : "0.00%"
                  : "—"}
              </span>
            </div>

            {/* Row 3: Fees · Taker / Maker */}
            <div className="flex items-center justify-between" style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--fg-muted)" }}>
              <span>Fees</span>
              <span style={{ color: "var(--warning)", fontVariantNumeric: "tabular-nums" }}>
                {estFees !== null ? formatUsd(estFees) : "—"}
              </span>
            </div>
            <div className="flex items-center justify-between" style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-subtle)" }}>
              <span>Taker / Maker</span>
              <span style={{ fontVariantNumeric: "tabular-nums" }}>0.06% / 0.03%</span>
            </div>
          </div>
        )}

        {/* ═══ 14. Submit (big rounded CTA) ═══ */}
        {/* F-03: when the wallet is connected but pointed at the wrong */}
        {/* chain, switch the CTA from "Open long" → "Switch to Paxeer" */}
        {/* and route to wagmi's switchChain instead of submit. */}
        {(() => {
          const wrongChain = isConnected && !isCorrectChain;
          const ctaDisabled = !isConnected
            ? false
            : wrongChain
              ? false
              : (!canTrade || isSubmitting);
          const onCtaClick = !isConnected
            ? connect
            : wrongChain
              ? switchToPaxeer
              : handleSubmit;
          const ctaBg = !isConnected
            ? "var(--accent-solid)"
            : wrongChain
              ? "var(--warning, #f5a623)"
              : accent;
          const ctaShadow = !isConnected
            ? "0 8px 20px -8px rgba(0, 76, 237, 0.5)"
            : wrongChain
              ? "0 8px 20px -8px rgba(245, 166, 35, 0.5)"
              : accentShadow;
          const label = !isConnected
            ? "Connect Wallet"
            : wrongChain
              ? "Switch to Paxeer"
              : isSubmitting
                ? "Placing…"
                : `${isLong ? "Open long" : "Open short"} · ${leverage}×${estimatedSize ? ` · ${formatUsd(estimatedSize)}` : ""}`;
          return (
            <button
              onClick={onCtaClick}
              disabled={ctaDisabled}
              style={{
                padding: "14px",
                borderRadius: 999,
                border: "none",
                background: ctaBg,
                color: "#fff",
                fontFamily: "var(--font-body)",
                fontSize: 14,
                fontWeight: 600,
                cursor: ctaDisabled ? "not-allowed" : "pointer",
                letterSpacing: "0.02em",
                opacity: ctaDisabled ? 0.5 : 1,
                boxShadow: ctaShadow,
                transition: "all 150ms",
              }}
            >
              {label}
            </button>
          );
        })()}

        {/* ═══ 15. Order confirmation (orderConfirm) — nothing is signed until accepted ═══ */}
        {pending && (
          <div
            role="dialog"
            aria-label="Confirm order"
            style={{
              padding: "10px 12px",
              borderRadius: 10,
              background: "var(--bg-subtle)",
              border: "1px solid var(--border-soft)",
              display: "flex",
              flexDirection: "column",
              gap: 6,
            }}
          >
            <span style={META_LABEL}>Confirm order</span>
            <DetailRow label="Side" value={pending.confirmation.display.side} />
            <DetailRow label="Size" value={pending.confirmation.display.size} />
            <DetailRow
              label={orderType === "market" ? "Acceptable price" : "Limit price"}
              value={pending.confirmation.display.acceptablePrice}
            />
            <DetailRow label="Trading fee" value={pending.confirmation.display.fee ?? "—"} />
            <DetailRow label="Liquidation price" value={pending.confirmation.display.liquidationPrice ?? "—"} />
            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8, marginTop: 4 }}>
              <button
                type="button"
                onClick={() => setPending(null)}
                style={{
                  padding: "8px",
                  borderRadius: 999,
                  border: "1px solid var(--border-soft)",
                  background: "transparent",
                  color: "var(--fg-muted)",
                  fontFamily: "var(--font-body)",
                  fontSize: 12,
                  cursor: "pointer",
                }}
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={acceptPending}
                disabled={isSubmitting}
                style={{
                  padding: "8px",
                  borderRadius: 999,
                  border: "none",
                  background: accent,
                  color: "#fff",
                  fontFamily: "var(--font-body)",
                  fontSize: 12,
                  fontWeight: 600,
                  cursor: isSubmitting ? "not-allowed" : "pointer",
                  opacity: isSubmitting ? 0.5 : 1,
                }}
              >
                Confirm
              </button>
            </div>
          </div>
        )}

        {/* ═══ 16. Error / Success ═══ */}
        {error && (
          <div
            className="flex items-start gap-2"
            style={{
              padding: "8px 12px",
              borderRadius: 8,
              background: "rgba(255, 90, 101, 0.08)",
              border: "1px solid rgba(255, 90, 101, 0.2)",
              fontFamily: "var(--font-mono)",
              fontSize: 11,
              color: "var(--danger)",
            }}
          >
            <IcoAlert size={12} className="mt-0.5 shrink-0" />
            <span>{error}</span>
          </div>
        )}
        {success && (
          <div
            style={{
              padding: "8px 12px",
              borderRadius: 8,
              background: "rgba(5, 193, 104, 0.08)",
              border: "1px solid rgba(5, 193, 104, 0.2)",
              fontFamily: "var(--font-mono)",
              fontSize: 11,
              color: "var(--success)",
            }}
          >
            {success}
          </div>
        )}
      </div>
    </div>
  );
}
