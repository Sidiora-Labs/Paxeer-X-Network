import { fetchWithPolicy as fetch } from "@/core/net/fetch";
import { useRef, useState, useCallback } from "react";
import domtoimage from "dom-to-image";
import { cn } from "@/ui/utils/cn";
import { Button } from "@/ui/atoms/button";
import { X, Download, Share2, Check, Loader2 } from "lucide-react";
import { coinLogoUrl } from "@/core/perps/config";
import type { PositionPnl } from "@/core/perps/pnlHelpers";
import { fmtUsd as _fmtUsd, fmtPrice, fmtDate } from "@/ui/utils/format";
const fmtUsd = (v: number) => _fmtUsd(Math.abs(v));

export interface PnlCardData {
  symbol: string;
  isLong: boolean;
  leverage: number;
  entryPrice: number;
  size: number;
  pnl: PositionPnl;
  openedAt?: string;
}

interface PnlShareCardProps {
  data: PnlCardData;
  onClose: () => void;
}

export function PnlShareCard({ data, onClose }: PnlShareCardProps) {
  const cardRef = useRef<HTMLDivElement>(null);
  const [busy, setBusy] = useState(false);
  const [copied, setCopied] = useState(false);

  const { symbol, isLong, leverage, entryPrice, size, pnl, openedAt } = data;
  const isProfit = pnl.unrealizedPnl >= 0;

  const captureCard = useCallback(async (): Promise<Blob | null> => {
    if (!cardRef.current) return null;
    try {
      const dataUrl = await domtoimage.toPng(cardRef.current, {
        quality: 1,
        bgcolor: "#0a0a0a",
        width: 480,
        height: cardRef.current.offsetHeight,
        style: {
          transform: "scale(1)",
          transformOrigin: "top left",
        },
      });
      const res = await fetch(dataUrl);
      return await res.blob();
    } catch (err) {
      console.error("Failed to capture PnL card:", err);
      return null;
    }
  }, []);

  const handleDownload = useCallback(async () => {
    setBusy(true);
    try {
      const blob = await captureCard();
      if (!blob) return;
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `hyperpax-pnl-${symbol}-${Date.now()}.png`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
    } finally {
      setBusy(false);
    }
  }, [captureCard, symbol]);

  const handleShare = useCallback(async () => {
    setBusy(true);
    try {
      const blob = await captureCard();
      if (!blob) return;

      if (navigator.share && navigator.canShare) {
        const file = new File([blob], `hyperpax-pnl-${symbol}.png`, { type: "image/png" });
        const shareData = {
          title: `${symbol}/USD ${isLong ? "Long" : "Short"} — HyperPax`,
          text: `${isLong ? "Long" : "Short"} ${symbol}/USD ${leverage}x | PnL: ${isProfit ? "+" : "-"}${fmtUsd(pnl.unrealizedPnl)} (${pnl.roe >= 0 ? "+" : ""}${pnl.roe.toFixed(2)}%)`,
          files: [file],
        };
        if (navigator.canShare(shareData)) {
          await navigator.share(shareData);
          return;
        }
      }

      // Fallback: copy image to clipboard
      await navigator.clipboard.write([
        new ClipboardItem({ "image/png": blob }),
      ]);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch (err: any) {
      if (err?.name !== "AbortError") {
        console.error("Share failed:", err);
      }
    } finally {
      setBusy(false);
    }
  }, [captureCard, symbol, isLong, leverage, isProfit, pnl]);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/70" onClick={onClose} />
      <div className="relative flex flex-col items-center gap-4 z-10">
        {/* The card — captured as image */}
        <div
          ref={cardRef}
          className="w-[480px] rounded-2xl overflow-hidden"
          style={{
            background: isProfit
              ? "linear-gradient(135deg, #0a1a0f 0%, #0d2818 40%, #0a0a0a 100%)"
              : "linear-gradient(135deg, #1a0a0a 0%, #280d0d 40%, #0a0a0a 100%)",
            fontFamily: "'IBM Plex Mono', monospace",
          }}
        >
          {/* Top bar */}
          <div className="flex items-center justify-between px-6 pt-5 pb-3">
            <div className="flex items-center gap-2">
              <img
                src="/pax-white.svg"
                alt="HyperPax"
                className="h-5 w-5 object-contain"
                crossOrigin="anonymous"
              />
              <span
                className="text-white text-[15px] leading-none"
                style={{ fontFamily: '"Paxeer Sans Rounded", sans-serif', letterSpacing: '-0.01em' }}
              >
                <span style={{ fontWeight: 700 }}>Hyper</span>
                <span style={{ fontWeight: 400 }}>Pax</span>
              </span>
            </div>
            <span className="text-[10px] text-white/40 font-medium tracking-wider uppercase">
              hyperpax.com
            </span>
          </div>

          {/* Market + Side */}
          <div className="px-6 pb-2">
            <div className="flex items-center gap-3">
              <img
                src={coinLogoUrl(symbol)}
                alt={symbol}
                className="w-10 h-10 rounded-full"
                crossOrigin="anonymous"
              />
              <div>
                <div className="flex items-center gap-2">
                  <span className="text-white text-xl font-bold">{symbol}/USD</span>
                  <span
                    className={cn(
                      "text-xs font-bold px-2 py-0.5 rounded",
                      isLong ? "bg-emerald-500/20 text-emerald-400" : "bg-red-500/20 text-red-400"
                    )}
                  >
                    {isLong ? "LONG" : "SHORT"} {leverage.toFixed(0)}x
                  </span>
                </div>
                {openedAt && (
                  <span className="text-[11px] text-white/40">Opened {fmtDate(openedAt)}</span>
                )}
              </div>
            </div>
          </div>

          {/* PnL hero */}
          <div className="px-6 py-4">
            <div className="text-[11px] text-white/50 uppercase tracking-wider mb-1">
              Unrealized PnL
            </div>
            <div className="flex items-baseline gap-3">
              <span
                className={cn(
                  "text-4xl font-bold tracking-tight",
                  isProfit ? "text-emerald-400" : "text-red-400"
                )}
              >
                {isProfit ? "+" : "-"}{fmtUsd(pnl.unrealizedPnl)}
              </span>
              <span
                className={cn(
                  "text-lg font-semibold",
                  isProfit ? "text-emerald-400/70" : "text-red-400/70"
                )}
              >
                {pnl.roe >= 0 ? "+" : ""}{pnl.roe.toFixed(2)}%
              </span>
            </div>
          </div>

          {/* Stats grid */}
          <div className="px-6 pb-5">
            <div className="grid grid-cols-3 gap-3">
              {[
                { label: "Entry Price", value: `$${fmtPrice(entryPrice)}` },
                { label: "Mark Price", value: pnl.markPrice > 0 ? `$${fmtPrice(pnl.markPrice)}` : "\u2014" },
                { label: "Liq. Price", value: pnl.liqPrice > 0 ? `$${fmtPrice(pnl.liqPrice)}` : "\u2014" },
                { label: "Size", value: fmtUsd(size) },
                { label: "Collateral", value: fmtUsd(pnl.initialCollateral) },
                { label: "Net Value", value: fmtUsd(pnl.netValue) },
              ].map((item) => (
                <div key={item.label} className="rounded-lg bg-white/[0.04] px-3 py-2">
                  <div className="text-[9px] text-white/40 uppercase tracking-wider mb-0.5">
                    {item.label}
                  </div>
                  <div className="text-[13px] text-white font-medium">{item.value}</div>
                </div>
              ))}
            </div>
          </div>

          {/* Bottom bar */}
          <div className="flex items-center justify-between px-6 py-3 border-t border-white/[0.06]">
            <span className="text-[10px] text-white/30">
              Perpetual Futures on Paxeer Network
            </span>
            <span className="text-[10px] text-white/30 font-mono">
              {new Date().toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric", hour: "2-digit", minute: "2-digit" })}
            </span>
          </div>
        </div>

        {/* Action buttons (outside the card — not captured) */}
        <div className="flex items-center gap-3">
          <Button
            variant="outline"
            className="h-9 px-4 text-sm bg-white/10 border-white/20 text-white hover:bg-white/20"
            onClick={handleDownload}
            disabled={busy}
          >
            {busy ? <Loader2 className="h-4 w-4 mr-2 animate-spin" /> : <Download className="h-4 w-4 mr-2" />}
            Download PNG
          </Button>
          <Button
            variant="outline"
            className="h-9 px-4 text-sm bg-white/10 border-white/20 text-white hover:bg-white/20"
            onClick={handleShare}
            disabled={busy}
          >
            {copied ? (
              <><Check className="h-4 w-4 mr-2 text-emerald-400" /> Copied!</>
            ) : busy ? (
              <><Loader2 className="h-4 w-4 mr-2 animate-spin" /> Sharing...</>
            ) : (
              <><Share2 className="h-4 w-4 mr-2" /> Share</>
            )}
          </Button>
          <Button
            variant="outline"
            size="icon"
            className="h-9 w-9 bg-white/10 border-white/20 text-white hover:bg-white/20"
            onClick={onClose}
          >
            <X className="h-4 w-4" />
          </Button>
        </div>
      </div>
    </div>
  );
}
