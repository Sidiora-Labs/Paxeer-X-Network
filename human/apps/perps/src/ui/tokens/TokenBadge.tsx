/**
 * Token badge — colored circle with the first letter of the symbol.
 * Ported from src/components/design/shared.jsx:36-51 (Phase 1, 2026-04-23).
 *
 * Keeps the prototype's inline-style approach because the color is per-token
 * dynamic and Tailwind arbitrary-value classes would just duplicate the same
 * style string. Font family resolves via the --font-display CSS var which is
 * aliased in globals.css (see DESIGN-AUDIT.md §5).
 */

export interface TokenSpec {
  /** Ticker symbol, e.g. "PAX", "USDC", "ETH" */
  sym: string;
  /** Brand hex color, e.g. "var(--brand-blue)" */
  color: string;
}

interface TokenBadgeProps {
  token: TokenSpec;
  /** Outer diameter in pixels. Default 28. */
  size?: number;
  /** Extra className (merged after default styles). */
  className?: string;
}

export function TokenBadge({ token, size = 28, className }: TokenBadgeProps) {
  return (
    <div
      className={className}
      style={{
        width: size,
        height: size,
        borderRadius: 999,
        background: token.color,
        display: "grid",
        placeItems: "center",
        color: "#fff",
        fontWeight: 600,
        fontFamily: "var(--font-display)",
        fontSize: Math.max(10, size * 0.42),
        letterSpacing: "-0.02em",
        flexShrink: 0,
      }}
    >
      {token.sym[0]}
    </div>
  );
}
