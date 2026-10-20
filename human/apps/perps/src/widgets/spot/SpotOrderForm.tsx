"use client";

import React, { useEffect, useMemo, useState } from "react";
import { formatUnits, type Address } from "viem";
import { cn } from "@/ui/utils/cn";
import { Info } from "lucide-react";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/ui/atoms/tooltip";
import { Switch } from "@/ui/atoms/switch";
import { toast } from "@/ui/atoms/use-toast";
import { usePerpsWallet } from "@/hooks/perps/usePerpsWallet";
import { useSpotOrderbook, useSpotStats } from "@/hooks/spot/useSpotMarketStream";
import { useSpotPortfolio } from "@/hooks/spot/useSpotPortfolio";
import { dStrToToken } from "@/core/spot/api";
import { erc20Abi } from "@/core/perps/config";
import { WEI_PER_UHPX } from "@/core/spot/calls";
import { toPlainDecimal } from "@/ui/utils/format";
import { readContract } from "@/hooks/perps/useContract";
import {
  type SpotMarket,
  SPOT_STABLECOINS,
} from "@/core/spot/config";
import { classifyError } from "@/core/errors/classify";
import { reportError } from "@/core/errors/report";
import {
  limitMinOut,
  parseTokenAmount,
  parsePrice,
  quoteDecimals,
  spotOrderRail,
  type SpotOrderType,
  type SpotTimeInForce as TimeInForce,
} from "@/core/spot/slippage";
import {
  selectStableForBuy,
  selectStableForSell,
  buildStableAmountMap,
} from "@/core/spot/stableSelector";
import {
  marketBuy as execMarketBuy,
  marketSell as execMarketSell,
  placeLimitBuy as execPlaceLimitBuy,
  placeLimitSell as execPlaceLimitSell,
  placeStopLimitBuy as execPlaceStopLimitBuy,
  placeStopLimitSell as execPlaceStopLimitSell,
  getPaxPointer,
} from "@/hooks/spot/useSpotOrders";
import {
  getAllLiquidityInfo,
  quoteMarketBuy,
  quoteMarketSell,
  type SpotQuoteResult,
} from "@/hooks/spot/useSpotQuoter";

/* ══════════════════════════════════════════════════════════════════════
   SpotOrderForm

   Order types:
     · market     — taker, slippage-bounded
     · limit      — maker, optional Post-Only / IOC / FOK
     · stop-limit — trigger price + limit price

   Size entry:
     · BASE     — input is in `market.symbol` (e.g. SID, ETH)
     · USD      — input is in stablecoin USD (uses live mid)
     · % of Max — 25 / 50 / 75 / 100 of available balance for the side

   Submit dispatches to the PECOR engine via `useSpotOrders`:
     · market           → PECOR.marketBuy / marketSell
     · limit + GTC      → PECOROrders.placeLimitBuy / placeLimitSell
       (post-only forces GTC)
     · limit + IOC/FOK  → PECOR.marketBuy / marketSell with `minOut`
                          from `limitMinOut` at the limit price
                          (PECOR market orders are atomic, so IOC ≡ FOK)
     · stop-limit       → PECORStopOrders.placeStopLimitBuy / Sell

   The stable side is chosen by `selectStableForBuy / Sell`:
     · BUY  → highest user balance ≥ amount
     · SELL → deepest vault liquidity ≥ expected output
   ══════════════════════════════════════════════════════════════════════ */

type SizeMode = "BASE" | "USD";

const PRICE_SCALE = 10n ** 18n;

/**
 * Exact base and quote units for an order. `amount` is in `sizeMode`'s
 * unit and `price` is quote per base ("" when there is no reference).
 * The amount is parsed at its own token's decimals and the price at the
 * 18 price decimals, both straight from their decimal strings (scientific
 * notation included); the converted side is scaled in bigint and floored,
 * so a buy never spends more than the size implies.
 */
export function spotOrderUnits(p: {
  sizeMode: SizeMode;
  amount: string;
  price: string;
  baseDecimals: number;
  quoteDecimals: number;
}): { baseUnits: bigint; quoteUnits: bigint } {
  const priceUnits = p.price.trim() === "" ? 0n : parsePrice(toPlainDecimal(p.price));
  const baseScale = 10n ** BigInt(p.baseDecimals);
  const quoteScale = 10n ** BigInt(p.quoteDecimals);
  if (p.sizeMode === "BASE") {
    const baseUnits = parseTokenAmount(toPlainDecimal(p.amount), p.baseDecimals);
    if (priceUnits <= 0n) return { baseUnits, quoteUnits: 0n };
    return { baseUnits, quoteUnits: (baseUnits * priceUnits * quoteScale) / (baseScale * PRICE_SCALE) };
  }
  const quoteUnits = parseTokenAmount(toPlainDecimal(p.amount), p.quoteDecimals);
  if (priceUnits <= 0n) return { baseUnits: 0n, quoteUnits };
  return { baseUnits: (quoteUnits * baseScale * PRICE_SCALE) / (priceUnits * quoteScale), quoteUnits };
}

const ORDER_TYPE_LABELS: Record<SpotOrderType, string> = {
  market: "Market",
  limit: "Limit",
  "stop-limit": "Stop-Limit",
};

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
const FIELD_INPUT_COMPACT: React.CSSProperties = {
  ...FIELD_INPUT,
  fontSize: 16,
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

const SLIPPAGE_PRESETS = [0.1, 0.5, 1, 2]; // percent
const PCT_PRESETS = [25, 50, 75, 100];

function fmtUsd(v: number): string {
  if (!Number.isFinite(v)) return "—";
  if (Math.abs(v) >= 1_000_000) return `$${(v / 1_000_000).toFixed(2)}M`;
  return `$${v.toLocaleString("en-US", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })}`;
}

function fmtBase(v: number, sym: string): string {
  if (!Number.isFinite(v) || v === 0) return `0 ${sym}`;
  if (Math.abs(v) >= 1_000_000) return `${(v / 1_000_000).toFixed(4)}M ${sym}`;
  return `${v.toLocaleString("en-US", {
    maximumFractionDigits: v < 1 ? 6 : 4,
  })} ${sym}`;
}

function DetailRow({
  label,
  value,
  tooltip,
  valueClass,
}: {
  label: string;
  value: string;
  tooltip?: string;
  valueClass?: string;
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
              <TooltipTrigger>
                <Info className="w-3 h-3" />
              </TooltipTrigger>
              <TooltipContent side="left" className="max-w-[220px]">
                {tooltip}
              </TooltipContent>
            </Tooltip>
          </TooltipProvider>
        )}
      </div>
      <span
        className={cn("tabular-nums", valueClass)}
        style={{
          fontFamily: "var(--font-mono)",
          fontSize: 11,
          color: "var(--fg)",
        }}
      >
        {value}
      </span>
    </div>
  );
}

interface SpotOrderFormProps {
  market: SpotMarket;
  /** Compact density mode for the mobile trade view. */
  compact?: boolean;
}

export function SpotOrderForm({ market, compact = false }: SpotOrderFormProps) {
  const { isConnected, connect, isCorrectChain, switchToPaxeer, address } =
    usePerpsWallet();

  // ── Order config state ──
  const [orderType, setOrderType] = useState<SpotOrderType>("market");
  const [side, setSide] = useState<"BUY" | "SELL">("BUY");
  const [sizeMode, setSizeMode] = useState<SizeMode>("BASE");
  const [amount, setAmount] = useState(""); // input value tied to `sizeMode`
  const [limitPrice, setLimitPrice] = useState("");
  const [stopPrice, setStopPrice] = useState("");
  const [tif, setTif] = useState<TimeInForce>("GTC");
  const [postOnly, setPostOnly] = useState(false);
  const [slippagePct, setSlippagePct] = useState(0.5); // % for market orders

  // ── Live data ──
  const orderbook = useSpotOrderbook(market.symbol);
  const stats = useSpotStats(market.symbol);
  const portfolio = useSpotPortfolio(address);

  const bestBid = orderbook?.bids[0]?.price ?? 0;
  const bestAsk = orderbook?.asks[0]?.price ?? 0;
  const midPrice = bestBid > 0 && bestAsk > 0 ? (bestBid + bestAsk) / 2 : 0;
  const lastPrice = stats?.last_price ?? 0;
  const refPrice = midPrice || lastPrice;

  // ── Balances ──
  // PAX is the uhpx bank coin: its balance is `balanceOf` on the uhpx
  // pointer, at the 6 decimals of the token entry.
  const isNativeBase = market.baseTokenMeta.isNative;
  const [paxPointerBalance, setPaxPointerBalance] = useState<bigint | null>(null);
  useEffect(() => {
    if (!isNativeBase || !address) {
      setPaxPointerBalance(null);
      return;
    }
    let cancelled = false;
    (async () => {
      const pointer = await getPaxPointer();
      const balance = await readContract({
        address: pointer,
        abi: erc20Abi,
        functionName: "balanceOf",
        args: [address as Address],
      });
      if (typeof balance !== "bigint") {
        throw new Error("balanceOf on the uhpx pointer returned no uint256");
      }
      if (!cancelled) setPaxPointerBalance(balance);
    })().catch((err: unknown) => {
      if (cancelled) return;
      setPaxPointerBalance(null);
      void reportError(classifyError(err, "spot-order/pax-balance"));
    });
    return () => {
      cancelled = true;
    };
  }, [isNativeBase, address, portfolio.holdings]);

  const baseBalance = useMemo(() => {
    if (isNativeBase) {
      return paxPointerBalance === null
        ? 0
        : Number(formatUnits(paxPointerBalance, market.baseDecimals));
    }
    if (!portfolio.holdings) return 0;
    const lcBase = market.baseToken.toLowerCase();
    // Match by symbol primarily; fall back to a substring on the token address
    const row = portfolio.holdings.erc20.find((e) => {
      if (e.symbol && e.symbol.toUpperCase() === market.baseToken.toUpperCase())
        return true;
      return false;
    });
    if (row) {
      const dec =
        typeof row.decimals === "number"
          ? row.decimals
          : typeof row.decimals === "string"
            ? parseInt(row.decimals, 10)
            : market.baseDecimals;
      return dStrToToken(row.balance, dec);
    }
    void lcBase;
    return 0;
  }, [portfolio.holdings, market, isNativeBase, paxPointerBalance]);

  const quoteBalance = useMemo(() => {
    if (!portfolio.holdings) return 0;
    let total = 0;
    for (const stable of SPOT_STABLECOINS) {
      const row = portfolio.holdings.erc20.find(
        (e) => e.token.toLowerCase() === stable.address.toLowerCase(),
      );
      if (row) {
        total += dStrToToken(row.balance, stable.decimals);
      }
    }
    return total;
  }, [portfolio.holdings]);

  const availableBalance = side === "BUY" ? quoteBalance : baseBalance;

  // ── Derived order amounts ──
  const amountNum = parseFloat(amount) || 0;
  const limitNum = parseFloat(limitPrice) || 0;
  const stopNum = parseFloat(stopPrice) || 0;

  // Convert input to canonical (base, quote) pair given size mode + ref price.
  const { baseAmount, quoteAmount } = useMemo(() => {
    if (amountNum <= 0) return { baseAmount: 0, quoteAmount: 0 };
    const priceForConversion =
      orderType === "limit"
        ? limitNum > 0
          ? limitNum
          : refPrice
        : refPrice;
    if (priceForConversion <= 0) {
      // No price reference — treat input literally.
      return sizeMode === "BASE"
        ? { baseAmount: amountNum, quoteAmount: 0 }
        : { baseAmount: 0, quoteAmount: amountNum };
    }
    if (sizeMode === "BASE") {
      return {
        baseAmount: amountNum,
        quoteAmount: amountNum * priceForConversion,
      };
    }
    return {
      baseAmount: amountNum / priceForConversion,
      quoteAmount: amountNum,
    };
  }, [amountNum, sizeMode, orderType, limitNum, refPrice]);

  // ── Slippage / min received (market orders) ──
  const slippageBps = Math.round(slippagePct * 100);
  const minReceived = useMemo(() => {
    if (orderType !== "market") return 0;
    if (refPrice <= 0 || baseAmount <= 0) return 0;
    if (side === "BUY") {
      // Receiving base. Worst-case fill at best ask × (1 + slip).
      const worstPrice = bestAsk > 0 ? bestAsk * (1 + slippagePct / 100) : 0;
      if (worstPrice <= 0) return 0;
      return quoteAmount / worstPrice;
    }
    // SELL — receiving quote. Worst-case fill at best bid × (1 - slip).
    const worstPrice = bestBid > 0 ? bestBid * (1 - slippagePct / 100) : 0;
    return baseAmount * worstPrice;
  }, [orderType, refPrice, baseAmount, quoteAmount, side, bestAsk, bestBid, slippagePct]);

  // ── Validation ──
  const insufficientBalance =
    side === "BUY" ? quoteAmount > quoteBalance + 1e-9 : baseAmount > baseBalance + 1e-9;
  const limitInvalid =
    (orderType === "limit" || orderType === "stop-limit") && limitNum <= 0;
  const stopInvalid = orderType === "stop-limit" && stopNum <= 0;
  const amountInvalid = baseAmount <= 0 && quoteAmount <= 0;

  // ── Submit state ──
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [submitSuccess, setSubmitSuccess] = useState<string | null>(null);

  const submitDisabled =
    !isConnected ||
    !isCorrectChain ||
    amountInvalid ||
    limitInvalid ||
    stopInvalid ||
    insufficientBalance ||
    portfolio.isLoading ||
    isSubmitting;

  const hasFundsLoaded = portfolio.hasLoadedOnce && isConnected;

  // ── % shortcuts ──
  const handlePctClick = (pct: number) => {
    const max = side === "BUY" ? quoteBalance : baseBalance;
    if (max <= 0) return;
    const target = (max * pct) / 100;
    if (sizeMode === (side === "BUY" ? "USD" : "BASE")) {
      setAmount(String(target));
    } else if (refPrice > 0) {
      // We need to convert from the user's max in the natural unit to
      // the displayed unit.
      if (sizeMode === "BASE") {
        // BUY: target was in quote; convert quote → base via price.
        // SELL: target was already in base.
        setAmount(side === "BUY" ? String(target / refPrice) : String(target));
      } else {
        // sizeMode === USD
        setAmount(side === "BUY" ? String(target) : String(target * refPrice));
      }
    } else {
      setAmount(String(target));
    }
  };

  // Auto-tune Post-Only flag — only legal on limit + GTC.
  useEffect(() => {
    if (orderType !== "limit") {
      setPostOnly(false);
    }
  }, [orderType]);
  useEffect(() => {
    if (postOnly && tif !== "GTC") setTif("GTC");
  }, [postOnly, tif]);

  // ── Submit ──

  /**
   * Compute the user's stablecoin balances as a lowercased-address →
   * bigint map, ready to pass into `selectStableForBuy`.
   */
  const stableBalanceMap = useMemo(() => {
    if (!portfolio.holdings) return {};
    return buildStableAmountMap(
      SPOT_STABLECOINS.map((s) => {
        const row = portfolio.holdings!.erc20.find(
          (e) => e.token.toLowerCase() === s.address.toLowerCase(),
        );
        return {
          address: s.address,
          amount: row ? BigInt(row.balance) : 0n,
        };
      }),
    );
  }, [portfolio.holdings]);

  const submit = async () => {
    if (submitDisabled) return;
    if (!address) return;

    setIsSubmitting(true);
    setSubmitError(null);
    setSubmitSuccess(null);

    const decimalsBase = market.baseDecimals;
    const decimalsStable = quoteDecimals(SPOT_STABLECOINS);
    const rail = spotOrderRail(orderType, tif, postOnly);
    const atLimit = orderType === "limit";
    const conversionPrice =
      orderType === "limit" && limitNum > 0 ? limitPrice : refPrice > 0 ? String(refPrice) : "";

    try {
      const { baseUnits, quoteUnits } = spotOrderUnits({
        sizeMode,
        amount,
        price: conversionPrice,
        baseDecimals: decimalsBase,
        quoteDecimals: decimalsStable,
      });

      // PAX trades as the uhpx pointer the market rail reports.
      const tokenAddr = isNativeBase ? await getPaxPointer() : market.baseAddress;
      if (rail === "market") {
        // ── Market rail. An IOC / FOK limit sends the live quote with its
        //    output set to `limitMinOut` and zero slippage, so the call's
        //    minimum output is the limit bound. ──
        if (side === "BUY") {
          const stablecoinAmount = quoteUnits;
          const selection = selectStableForBuy({
            amountUsd: stablecoinAmount,
            balances: stableBalanceMap,
          });
          if (!selection.sufficient) {
            throw new Error(
              `Insufficient USD balance — best stable (${selection.stable.symbol}) has ${selection.available}, need ${stablecoinAmount}`,
            );
          }
          let precomputedQuote: SpotQuoteResult | undefined;
          if (atLimit) {
            const minOut = limitMinOut(
              stablecoinAmount,
              parsePrice(limitNum.toString()),
              true,
              decimalsStable,
              decimalsBase,
            );
            const quote = await quoteMarketBuy({
              stablecoin: selection.stable.address,
              token: tokenAddr,
              stablecoinAmount,
            });
            if (quote.amountOut < minOut) {
              throw new Error(
                `${tif} buy not filled: ${quote.amountOut} ${market.symbol} units quoted, limit needs ${minOut}`,
              );
            }
            precomputedQuote = { ...quote, amountOut: minOut };
          }
          await execMarketBuy({
            stablecoin: selection.stable.address,
            token: tokenAddr,
            stablecoinAmount,
            slippageBps: atLimit ? 0 : slippageBps,
            user: address as `0x${string}`,
            precomputedQuote,
          });
          setSubmitSuccess(`Bought ${baseAmount.toFixed(6)} ${market.symbol}`);
        } else {
          // SELL — pick the stable with the deepest vault liquidity.
          const tokenAmount = baseUnits;
          const liquidity = await getAllLiquidityInfo();
          const vaultLiquidity = buildStableAmountMap(
            liquidity.map((r) => ({ address: r.token, amount: r.reserve })),
          );
          const selection = selectStableForSell({
            expectedUsd: quoteUnits,
            vaultLiquidity,
          });
          let precomputedQuote: SpotQuoteResult | undefined;
          if (atLimit) {
            const minOut = limitMinOut(
              tokenAmount,
              parsePrice(limitNum.toString()),
              false,
              decimalsBase,
              decimalsStable,
            );
            const quote = await quoteMarketSell({
              token: tokenAddr,
              stablecoin: selection.stable.address,
              tokenAmount,
            });
            if (quote.amountOut < minOut) {
              throw new Error(
                `${tif} sell not filled: ${quote.amountOut} ${selection.stable.symbol} units quoted, limit needs ${minOut}`,
              );
            }
            precomputedQuote = { ...quote, amountOut: minOut };
          }
          await execMarketSell({
            token: tokenAddr,
            stablecoin: selection.stable.address,
            tokenAmount,
            slippageBps: atLimit ? 0 : slippageBps,
            user: address as `0x${string}`,
            precomputedQuote,
            native: isNativeBase
              ? { nativeBalance: (paxPointerBalance ?? 0n) * WEI_PER_UHPX }
              : undefined,
          });
          setSubmitSuccess(
            `Sold ${baseAmount.toFixed(6)} ${market.symbol} for ${selection.stable.symbol}`,
          );
        }
      } else if (rail === "limit") {
        // ── Limit rail (GTC and post-only; IOC/FOK handled above) ──
        const targetPrice = parsePrice(toPlainDecimal(limitPrice));
        if (side === "BUY") {
          const stablecoinAmount = quoteUnits;
          const selection = selectStableForBuy({
            amountUsd: stablecoinAmount,
            balances: stableBalanceMap,
          });
          if (!selection.sufficient) {
            throw new Error(
              `Insufficient USD balance — best stable (${selection.stable.symbol}) has ${selection.available}, need ${stablecoinAmount}`,
            );
          }
          await execPlaceLimitBuy({
            stablecoin: selection.stable.address,
            token: tokenAddr,
            stablecoinAmount,
            targetPrice,
            user: address as `0x${string}`,
          });
        } else {
          const tokenAmount = baseUnits;
          const liquidity = await getAllLiquidityInfo();
          const vaultLiquidity = buildStableAmountMap(
            liquidity.map((r) => ({ address: r.token, amount: r.reserve })),
          );
          const selection = selectStableForSell({
            expectedUsd: quoteUnits,
            vaultLiquidity,
          });
          await execPlaceLimitSell({
            token: tokenAddr,
            stablecoin: selection.stable.address,
            tokenAmount,
            targetPrice,
            user: address as `0x${string}`,
          });
        }
        setSubmitSuccess(`Limit ${side === "BUY" ? "buy" : "sell"} placed @ $${limitNum}`);
      } else {
        const stopPriceBig = parsePrice(toPlainDecimal(stopPrice));
        const limitPriceBig = parsePrice(toPlainDecimal(limitPrice));
        if (side === "BUY") {
          const stablecoinAmount = quoteUnits;
          const selection = selectStableForBuy({
            amountUsd: stablecoinAmount,
            balances: stableBalanceMap,
          });
          if (!selection.sufficient) {
            throw new Error(
              `Insufficient USD balance — best stable (${selection.stable.symbol}) has ${selection.available}, need ${stablecoinAmount}`,
            );
          }
          await execPlaceStopLimitBuy({
            stablecoin: selection.stable.address,
            token: tokenAddr,
            stablecoinAmount,
            stopPrice: stopPriceBig,
            limitPrice: limitPriceBig,
            user: address as `0x${string}`,
          });
        } else {
          const tokenAmount = baseUnits;
          const liquidity = await getAllLiquidityInfo();
          const vaultLiquidity = buildStableAmountMap(
            liquidity.map((r) => ({ address: r.token, amount: r.reserve })),
          );
          const selection = selectStableForSell({
            expectedUsd: quoteUnits,
            vaultLiquidity,
          });
          await execPlaceStopLimitSell({
            token: tokenAddr,
            stablecoin: selection.stable.address,
            tokenAmount,
            stopPrice: stopPriceBig,
            limitPrice: limitPriceBig,
            user: address as `0x${string}`,
          });
        }
        setSubmitSuccess(`Stop-limit ${side === "BUY" ? "buy" : "sell"} placed`);
      }

      // Form reset on success.
      setAmount("");
      if (orderType !== "market") setLimitPrice("");
      if (orderType === "stop-limit") setStopPrice("");

      // Bump the portfolio so holdings + open orders reflect the trade.
      portfolio.refresh();

      // Mirror to the global toast for visibility outside the form.
      toast({
        title: `${side} ${market.pair}`,
        description: `${ORDER_TYPE_LABELS[orderType]} · ${baseAmount.toFixed(6)} ${market.symbol} · est. ${fmtUsd(quoteAmount)}`,
      });
    } catch (err) {
      // Never surface raw err.message — always go through the classifier
      // so the inline error gets a friendly, actionable string.
      const ufe = classifyError(err, `spot-order/${orderType}/${side.toLowerCase()}`);
      setSubmitError(
        ufe.suggestedFix ? `${ufe.message} ${ufe.suggestedFix}` : ufe.message,
      );
      void reportError(ufe);
    } finally {
      setIsSubmitting(false);
      window.setTimeout(() => setSubmitSuccess(null), 3000);
    }
  };

  const accentBg = side === "BUY"
    ? "linear-gradient(135deg, var(--brand-green), #04A859)"
    : "linear-gradient(135deg, var(--brand-red), #E04E58)";

  const inputStyle = compact ? FIELD_INPUT_COMPACT : FIELD_INPUT;
  const padOuter = compact ? "p-3" : "p-4";
  const gapOuter = compact ? "gap-2" : "gap-3";

  return (
    <div
      className={cn("flex flex-col h-full overflow-y-auto scrollbar-custom", padOuter, gapOuter)}
      style={{ background: "var(--bg-subtle)", borderRadius: compact ? 8 : 12 }}
    >
      {/* SPOT badge */}
      <div className="flex items-center gap-2 mb-1">
        <span className="text-[10px] font-mono uppercase tracking-wider text-primary/70 bg-primary/10 px-2 py-1 rounded-full font-semibold">
          SPOT
        </span>
        <span className="text-xs text-muted-foreground">{market.pair}</span>
      </div>

      {/* Buy / Sell */}
      <div className="grid grid-cols-2 gap-1 p-1 rounded-[var(--radius-md)] bg-surface-0">
        <button
          type="button"
          onClick={() => setSide("BUY")}
          className={cn(
            "py-2 text-sm font-semibold rounded-[var(--radius-md)] transition-all",
            side === "BUY"
              ? "bg-emerald-500/20 text-emerald-400 shadow-sm"
              : "text-muted-foreground hover:text-foreground",
          )}
        >
          Buy
        </button>
        <button
          type="button"
          onClick={() => setSide("SELL")}
          className={cn(
            "py-2 text-sm font-semibold rounded-[var(--radius-md)] transition-all",
            side === "SELL"
              ? "bg-red-500/20 text-red-400 shadow-sm"
              : "text-muted-foreground hover:text-foreground",
          )}
        >
          Sell
        </button>
      </div>

      {/* Order type tabs */}
      <div className="flex items-center gap-1">
        {(Object.keys(ORDER_TYPE_LABELS) as SpotOrderType[]).map((ot) => (
          <button
            key={ot}
            type="button"
            onClick={() => setOrderType(ot)}
            className={cn(
              "px-3 py-1.5 text-xs font-medium rounded-[var(--radius-md)] transition-colors",
              orderType === ot
                ? "bg-surface-2 text-foreground"
                : "text-muted-foreground hover:text-foreground hover:bg-surface-1",
            )}
          >
            {ORDER_TYPE_LABELS[ot]}
          </button>
        ))}
      </div>

      {/* Stop-price input */}
      {orderType === "stop-limit" && (
        <div style={FIELD_BOX}>
          <div className="flex items-center justify-between">
            <span style={FIELD_LABEL}>Stop / Trigger</span>
            <span style={FIELD_UNIT}>USD</span>
          </div>
          <input
            type="text"
            inputMode="decimal"
            placeholder="0.00"
            value={stopPrice}
            onChange={(e) => setStopPrice(e.target.value)}
            style={inputStyle}
          />
        </div>
      )}

      {/* Limit price */}
      {(orderType === "limit" || orderType === "stop-limit") && (
        <div style={FIELD_BOX}>
          <div className="flex items-center justify-between">
            <span style={FIELD_LABEL}>Limit Price</span>
            <span style={FIELD_UNIT}>USD</span>
          </div>
          <input
            type="text"
            inputMode="decimal"
            placeholder="0.00"
            value={limitPrice}
            onChange={(e) => setLimitPrice(e.target.value)}
            style={inputStyle}
          />
        </div>
      )}

      {/* Size mode + Amount */}
      <div style={FIELD_BOX}>
        <div className="flex items-center justify-between">
          <span style={FIELD_LABEL}>Amount</span>
          <div className="flex items-center gap-1">
            {(["BASE", "USD"] as SizeMode[]).map((mode) => (
              <button
                key={mode}
                type="button"
                onClick={() => setSizeMode(mode)}
                className={cn(
                  "px-2 py-0.5 text-[9px] font-mono uppercase tracking-wider rounded-full transition-colors",
                  sizeMode === mode
                    ? "bg-primary/20 text-primary"
                    : "text-muted-foreground hover:text-foreground hover:bg-white/[0.04]",
                )}
              >
                {mode === "BASE" ? market.symbol : "USD"}
              </button>
            ))}
          </div>
        </div>
        <input
          type="text"
          inputMode="decimal"
          placeholder="0.00"
          value={amount}
          onChange={(e) => setAmount(e.target.value)}
          style={inputStyle}
        />
        {/* Pct buttons */}
        <div className="grid grid-cols-4 gap-1 mt-2">
          {PCT_PRESETS.map((p) => (
            <button
              key={p}
              type="button"
              onClick={() => handlePctClick(p)}
              disabled={availableBalance <= 0}
              className={cn(
                "py-1 text-[10px] font-mono uppercase tracking-wider rounded-[var(--radius-md)] transition-colors",
                "bg-surface-0 text-muted-foreground hover:text-foreground hover:bg-white/[0.04]",
                "disabled:opacity-40 disabled:cursor-not-allowed",
              )}
            >
              {p === 100 ? "Max" : `${p}%`}
            </button>
          ))}
        </div>
      </div>

      {/* Slippage (market only) */}
      {orderType === "market" && (
        <div style={FIELD_BOX}>
          <div className="flex items-center justify-between">
            <span style={FIELD_LABEL}>Max Slippage</span>
            <span style={FIELD_UNIT}>{slippagePct.toFixed(2)}%</span>
          </div>
          <div className="grid grid-cols-4 gap-1 mt-1.5">
            {SLIPPAGE_PRESETS.map((p) => (
              <button
                key={p}
                type="button"
                onClick={() => setSlippagePct(p)}
                className={cn(
                  "py-1 text-[10px] font-mono rounded-[var(--radius-md)] transition-colors",
                  slippagePct === p
                    ? "bg-primary/20 text-primary"
                    : "bg-surface-0 text-muted-foreground hover:text-foreground hover:bg-white/[0.04]",
                )}
              >
                {p}%
              </button>
            ))}
          </div>
        </div>
      )}

      {/* TIF / Post-Only — limit + stop-limit */}
      {orderType !== "market" && (
        <div className="grid grid-cols-1 gap-1.5">
          <div className="flex items-center justify-between px-1">
            <span style={META_LABEL}>Time in Force</span>
            <div className="flex items-center gap-1">
              {(["GTC", "IOC", "FOK"] as TimeInForce[]).map((opt) => (
                <button
                  key={opt}
                  type="button"
                  onClick={() => setTif(opt)}
                  className={cn(
                    "px-2 py-0.5 text-[10px] font-mono rounded-[var(--radius-md)] transition-colors",
                    tif === opt
                      ? "bg-primary/20 text-primary"
                      : "bg-surface-0 text-muted-foreground hover:text-foreground hover:bg-white/[0.04]",
                  )}
                >
                  {opt}
                </button>
              ))}
            </div>
          </div>
          <label className="flex items-center justify-between px-1 py-1 cursor-pointer">
            <span style={META_LABEL}>Post-Only</span>
            <Switch
              checked={postOnly}
              onCheckedChange={setPostOnly}
              disabled={tif !== "GTC" || orderType !== "limit"}
              className="h-4 w-7"
            />
          </label>
        </div>
      )}

      {/* Balance preview */}
      <div className="px-1 py-2 grid grid-cols-2 gap-2 border-t border-white/[0.04]">
        <div className="flex flex-col">
          <span style={META_LABEL}>Wallet ({market.symbol})</span>
          <span
            className="tabular-nums text-foreground"
            style={{ fontFamily: "var(--font-mono)", fontSize: 11 }}
          >
            {hasFundsLoaded ? fmtBase(baseBalance, market.symbol) : "—"}
          </span>
        </div>
        <div className="flex flex-col items-end">
          <span style={{ ...META_LABEL, textAlign: "right" }}>Wallet (USD)</span>
          <span
            className="tabular-nums text-foreground"
            style={{ fontFamily: "var(--font-mono)", fontSize: 11 }}
          >
            {hasFundsLoaded ? fmtUsd(quoteBalance) : "—"}
          </span>
        </div>
      </div>

      {/* Stablecoin info */}
      <div className="px-1 py-1 text-[10px] text-muted-foreground font-mono">
        <div className="flex items-start gap-1.5">
          <Info className="w-3 h-3 shrink-0 mt-0.5" />
          <span>
            {side === "BUY"
              ? "Pooled stablecoin balance (USDL + USDC + USDT) is used to fill spot buys."
              : `${market.symbol} sells settle to a unified USD stablecoin balance.`}
          </span>
        </div>
      </div>

      {/* Order details */}
      <div className="space-y-0.5 px-1">
        <DetailRow
          label="Est. Price"
          value={
            orderType === "market"
              ? refPrice > 0 ? `$${refPrice.toLocaleString("en-US", { maximumFractionDigits: 4 })}` : "—"
              : limitNum > 0 ? `$${limitNum.toLocaleString("en-US", { maximumFractionDigits: 4 })}` : "—"
          }
        />
        <DetailRow
          label="Base Amount"
          value={baseAmount > 0 ? fmtBase(baseAmount, market.symbol) : "—"}
        />
        <DetailRow
          label="Quote Total"
          value={quoteAmount > 0 ? fmtUsd(quoteAmount) : "—"}
          tooltip="USD-equivalent total drawn from the pooled stablecoin balance."
        />
        {orderType === "market" && minReceived > 0 ? (
          <DetailRow
            label={side === "BUY" ? "Min Received" : "Min Proceeds"}
            value={
              side === "BUY"
                ? fmtBase(minReceived, market.symbol)
                : fmtUsd(minReceived)
            }
            tooltip={`Computed from your ${slippagePct.toFixed(2)}% slippage tolerance.`}
          />
        ) : null}
      </div>

      {/* Submit / Connect / Switch */}
      <div className="mt-auto pt-2">
        {!isConnected ? (
          <button
            type="button"
            onClick={connect}
            className="w-full py-3 rounded-[var(--radius-md)] text-sm font-semibold text-white transition-all hover:brightness-110"
            style={{ background: "linear-gradient(135deg, var(--brand-blue), var(--brand-blue-dark, var(--brand-blue)))" }}
          >
            Connect Wallet
          </button>
        ) : !isCorrectChain ? (
          <button
            type="button"
            onClick={switchToPaxeer}
            className="w-full py-3 rounded-[var(--radius-md)] text-sm font-semibold text-white bg-amber-500 transition-all hover:brightness-110"
          >
            Switch to Paxeer
          </button>
        ) : insufficientBalance && amountNum > 0 ? (
          <button
            type="button"
            disabled
            className="w-full py-3 rounded-[var(--radius-md)] text-sm font-semibold text-white bg-muted/40 cursor-not-allowed"
          >
            Insufficient {side === "BUY" ? "USD" : market.symbol} Balance
          </button>
        ) : (
          <button
            type="button"
            onClick={submit}
            disabled={submitDisabled}
            className={cn(
              "w-full py-3 rounded-[var(--radius-md)] text-sm font-semibold text-white transition-all",
              "disabled:opacity-40 disabled:cursor-not-allowed",
              "enabled:hover:brightness-110",
            )}
            style={{ background: accentBg }}
          >
            {isSubmitting
              ? "Submitting…"
              : side === "BUY"
                ? `Buy ${market.symbol}`
                : `Sell ${market.symbol}`}
          </button>
        )}
        {/* Error / success surface */}
        {submitError ? (
          <p
            className="mt-2 text-[10px] text-red-400 text-center font-mono"
            role="alert"
          >
            {submitError}
          </p>
        ) : submitSuccess ? (
          <p
            className="mt-2 text-[10px] text-emerald-400 text-center font-mono"
            role="status"
          >
            {submitSuccess}
          </p>
        ) : null}
      </div>
    </div>
  );
}
