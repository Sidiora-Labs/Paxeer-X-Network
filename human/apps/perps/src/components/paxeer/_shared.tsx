// _shared.tsx — tokens, icons, helpers shared across Paxeer components
import * as React from "react";

export type Token = {
  sym: string;
  name: string;
  color: string;
  price: number;
  bal: number;
  change: number;
};

export const TOKENS: Token[] = [
  { sym: "PAX",  name: "Paxeer",   color: "var(--brand-blue)", price: 0.4821,   bal: 2592.40, change: 4.21 },
  { sym: "USDC", name: "USD Coin", color: "#2775CA", price: 1.0001,   bal: 840.00,  change: 0.01 },
  { sym: "ETH",  name: "Ethereum", color: "#627EEA", price: 3214.88,  bal: 0.1482,  change: -1.64 },
  { sym: "BTC",  name: "Bitcoin",  color: "#F7931A", price: 67842.10, bal: 0.0082,  change: 2.14 },
  { sym: "SOL",  name: "Solana",   color: "#9945FF", price: 138.42,   bal: 14.28,   change: 6.32 },
  { sym: "ARB",  name: "Arbitrum", color: "#28A0F0", price: 1.142,    bal: 320.12,  change: -2.10 },
  { sym: "OP",   name: "Optimism", color: "#FF0420", price: 2.384,    bal: 128.44,  change: 0.84 },
  { sym: "DAI",  name: "Dai",      color: "#F5AC37", price: 0.9998,   bal: 420.00,  change: 0.00 },
];

type IconProps = React.SVGProps<SVGSVGElement>;

export const Icon = {
  arrow: (p: IconProps) => <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" {...p}><path d="M5 12h14M12 5l7 7-7 7"/></svg>,
  chevron: (p: IconProps) => <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" {...p}><path d="M6 9l6 6 6-6"/></svg>,
  x: (p: IconProps) => <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" {...p}><path d="M18 6L6 18M6 6l12 12"/></svg>,
  check: (p: IconProps) => <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" {...p}><path d="M20 6L9 17l-5-5"/></svg>,
  up: (p: IconProps) => <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" {...p}><path d="M18 15l-6-6-6 6"/></svg>,
  down: (p: IconProps) => <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" {...p}><path d="M6 9l6 6 6-6"/></svg>,
  send: (p: IconProps) => <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" {...p}><path d="M22 2L11 13M22 2l-7 20-4-9-9-4 20-7z"/></svg>,
  receive: (p: IconProps) => <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" {...p}><path d="M12 5v14M19 12l-7 7-7-7"/></svg>,
  swap: (p: IconProps) => <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" {...p}><path d="M7 3v14M7 17l-3-3M7 17l3-3M17 21V7M17 7l-3 3M17 7l3 3"/></svg>,
  zap: (p: IconProps) => <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" {...p}><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"/></svg>,
  plus: (p: IconProps) => <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" {...p}><path d="M12 5v14M5 12h14"/></svg>,
  copy: (p: IconProps) => <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" {...p}><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/></svg>,
  qr: (p: IconProps) => <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" {...p}><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/><path d="M14 14h3v3h-3zM18 18h3v3h-3z"/></svg>,
};

export function cx(...parts: Array<string | false | null | undefined>): string {
  return parts.filter(Boolean).join(" ");
}

export function fmt(n: number, digits = 2): string {
  return Number(n).toLocaleString("en-US", { minimumFractionDigits: digits, maximumFractionDigits: digits });
}

export function shortAddr(a: string): string {
  return a.length > 12 ? a.slice(0, 6) + "…" + a.slice(-4) : a;
}

export function TokenBadge({ token, size = 28 }: { token: Token; size?: number }) {
  return (
    <div style={{
      width: size, height: size, borderRadius: 999,
      background: token.color,
      display: "grid", placeItems: "center",
      color: "#fff", fontWeight: 600,
      fontFamily: "'Paxeer Grand Sans', Inter, sans-serif",
      fontSize: Math.max(10, size * 0.42),
      letterSpacing: "-0.02em",
      flexShrink: 0,
    }}>{token.sym[0]}</div>
  );
}

export function useAnimatedNumber(target: number, duration = 600): number {
  const [val, setVal] = React.useState(target);
  const fromRef = React.useRef(target);
  const startRef = React.useRef(0);
  React.useEffect(() => {
    fromRef.current = val;
    startRef.current = performance.now();
    let raf: number;
    const tick = (t: number) => {
      const e = Math.min(1, (t - startRef.current) / duration);
      const eased = 1 - Math.pow(1 - e, 3);
      setVal(fromRef.current + (target - fromRef.current) * eased);
      if (e < 1) raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [target]);
  return val;
}
