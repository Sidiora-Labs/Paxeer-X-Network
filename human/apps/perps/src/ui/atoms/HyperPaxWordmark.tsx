import Image from "next/image";
import { cn } from "@/ui/utils/cn";

/**
 * HyperPax brand wordmark.
 *
 * Renders the Pax symbol (SVG) alongside the "HyperPax" wordmark with
 * "Hyper" rendered bolder than "Pax", set in the Paxeer Sans Rounded
 * typeface for a consistent brand feel across the app.
 *
 * Sizing is controlled via the `size` prop — each size gives a matched
 * logo dimension + text scale. If you need finer control, pass a
 * `className` for the outer container.
 */
export interface HyperPaxWordmarkProps {
  /** Preset size — controls both the logo mark and the wordmark text. */
  size?: "sm" | "md" | "lg";
  /** Hide the SVG logo mark and render only the wordmark. */
  textOnly?: boolean;
  /** Hide the wordmark and render only the SVG logo mark. */
  markOnly?: boolean;
  /** Override the wordmark colour. Defaults to `currentColor`. */
  color?: string;
  /** Apply additional classes to the outer container. */
  className?: string;
}

const SIZE_CONFIG = {
  sm: { mark: 18, textPx: 14, gapPx: 6 },
  md: { mark: 24, textPx: 18, gapPx: 8 },
  lg: { mark: 32, textPx: 24, gapPx: 10 },
} as const;

export function HyperPaxWordmark({
  size = "md",
  textOnly = false,
  markOnly = false,
  color,
  className,
}: HyperPaxWordmarkProps) {
  const cfg = SIZE_CONFIG[size];

  return (
    <span
      className={cn("inline-flex items-center select-none", className)}
      style={{ gap: cfg.gapPx, color: color ?? "currentColor" }}
      aria-label="HyperPax"
    >
      {!textOnly && (
        <Image
          src="/pax-white.svg"
          alt=""
          width={cfg.mark}
          height={cfg.mark}
          priority
          style={{ height: cfg.mark, width: cfg.mark }}
        />
      )}
      {!markOnly && (
        <span
          style={{
            fontFamily: '"Paxeer Sans Rounded", sans-serif',
            fontSize: cfg.textPx,
            lineHeight: 1,
            letterSpacing: "-0.01em",
            whiteSpace: "nowrap",
          }}
        >
          <span style={{ fontWeight: 700 }}>Hyper</span>
          <span style={{ fontWeight: 400 }}>Pax</span>
        </span>
      )}
    </span>
  );
}
