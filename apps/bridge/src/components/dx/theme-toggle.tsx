import * as React from "react";
import { flushSync } from "react-dom";
import { motion } from "motion/react";

import { cn } from "@/lib/utils";
import { useTheme } from "@/components/theme-provider";

/**
 * ThemeTogglefive morphing sun/moon icons wired to the DX theme, with an
 * optional View Transitions reveal (circle, blur, wipe, polygon or gif mask).
 * Icons inspired by toggles.dev; transitions by rudrodip/theme-toggle-effect.
 *
 */

/* ───────────────────────── transitions ───────────────────────── */

type ThemeTransitionVariant = "none" | "circle" | "circle-blur" | "rectangle" | "polygon" | "gif";
type ThemeTransitionStart =
  | "pointer"
  | "center"
  | "top-left"
  | "top-right"
  | "bottom-left"
  | "bottom-right"
  | "top-center"
  | "bottom-center"
  | "bottom-up"
  | "top-down"
  | "left-right"
  | "right-left";

type ThemeTransitionOptions = {
  variant?: ThemeTransitionVariant;
  /** Where the reveal starts. "pointer" uses the click position. */
  start?: ThemeTransitionStart;
  blur?: boolean;
  /** Seconds. */
  duration?: number;
  /** Mask image for the "gif" variant. */
  gifUrl?: string;
};

const EXPO_OUT = "cubic-bezier(0.16, 1, 0.3, 1)";
const FULL = "polygon(0% 0%, 100% 0%, 100% 100%, 0% 100%)";

const anchor: Record<string, string> = {
  center: "50% 50%",
  "top-left": "0% 0%",
  "top-right": "100% 0%",
  "bottom-left": "0% 100%",
  "bottom-right": "100% 100%",
  "top-center": "50% 0%",
  "bottom-center": "50% 100%",
};

const wipeFrom: Record<string, string> = {
  "bottom-up": "polygon(0% 100%, 100% 100%, 100% 100%, 0% 100%)",
  "top-down": "polygon(0% 0%, 100% 0%, 100% 0%, 0% 0%)",
  "left-right": "polygon(0% 0%, 0% 0%, 0% 100%, 0% 100%)",
  "right-left": "polygon(100% 0%, 100% 0%, 100% 100%, 100% 100%)",
  "top-left": "polygon(0% 0%, 0% 0%, 0% 0%, 0% 0%)",
  "top-right": "polygon(100% 0%, 100% 0%, 100% 0%, 100% 0%)",
  "bottom-left": "polygon(0% 100%, 0% 100%, 0% 100%, 0% 100%)",
  "bottom-right": "polygon(100% 100%, 100% 100%, 100% 100%, 100% 100%)",
};

/** Builds the ::view-transition CSS for one reveal. `origin` is a CSS position like "30% 10%". */
function createThemeTransition({ variant = "circle", start = "pointer", blur = false, duration, gifUrl }: ThemeTransitionOptions, origin = "50% 50%") {
  const at = start === "pointer" ? origin : (anchor[start] ?? "50% 50%");
  const blurFrom = blur ? "filter: blur(8px);" : "";
  const blurTo = blur ? "filter: blur(0px);" : "";
  const blurMid = blur ? "50% { filter: blur(4px); }" : "";
  const base = `::view-transition-old(root),::view-transition-new(root){animation:none;mix-blend-mode:normal;}
::view-transition-old(root){z-index:-1;}`;
  const clip = (from: string, to: string, d = duration ?? 0.7) => `${base}
::view-transition-new(root){animation:dx-theme-reveal ${d}s ${EXPO_OUT} both;}
@keyframes dx-theme-reveal{from{clip-path:${from};${blurFrom}}${blurMid}to{clip-path:${to};${blurTo}}}`;

  switch (variant) {
    case "rectangle":
      return clip(wipeFrom[start] ?? wipeFrom["bottom-up"], FULL);
    case "polygon": {
      const right = start === "top-right";
      return clip(
        right ? "polygon(150% -71%, 250% 71%, 250% 71%, 150% -71%)" : "polygon(50% -71%, -50% 71%, -50% 71%, 50% -71%)",
        right ? "polygon(150% -71%, 250% 71%, 50% 171%, -71% 50%)" : "polygon(50% -71%, -50% 71%, 50% 171%, 171% 50%)",
      );
    }
    case "circle-blur": {
      const svg = `data:image/svg+xml,<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 40"><defs><filter id="b"><feGaussianBlur stdDeviation="2"/></filter></defs><circle cx="20" cy="20" r="18" fill="white" filter="url(%23b)"/></svg>`;
      return `${base}
::view-transition-new(root){-webkit-mask:url('${svg}') no-repeat;mask:url('${svg}') no-repeat;mask-position:${at};-webkit-mask-position:${at};mask-size:0;-webkit-mask-size:0;animation:dx-theme-mask ${duration ?? 1}s ${EXPO_OUT} both;}
@keyframes dx-theme-mask{from{-webkit-mask-size:0;mask-size:0;}to{-webkit-mask-size:350vmax;mask-size:350vmax;}}`;
    }
    case "gif":
      return `${base}
::view-transition-new(root){-webkit-mask:url('${gifUrl ?? ""}') center / 0 no-repeat;mask:url('${gifUrl ?? ""}') center / 0 no-repeat;animation:dx-theme-gif ${duration ?? 2.5}s both;}
@keyframes dx-theme-gif{0%{mask-size:0;-webkit-mask-size:0}10%{mask-size:50vmax;-webkit-mask-size:50vmax}90%{mask-size:50vmax;-webkit-mask-size:50vmax}100%{mask-size:2000vmax;-webkit-mask-size:2000vmax}}`;
    case "circle":
    default:
      return clip(`circle(0% at ${at})`, `circle(150% at ${at})`, duration ?? (start === "center" ? 0.7 : 1));
  }
}

const STYLE_ID = "dx-theme-transition";

/**
 * Switch the DX theme with a View Transition. Falls back to an instant switch
 * when the API is unavailable or the user prefers reduced motion.
 */
function useThemeTransition(options: ThemeTransitionOptions = {}) {
  const { resolvedTheme, setTheme } = useTheme();
  const isDark = resolvedTheme === "dark";

  const apply = React.useCallback(
    (next: "light" | "dark" | "system", event?: { clientX: number; clientY: number }) => {
      const resolved = next === "system" ? (window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light") : next;
      const commit = () => {
        document.documentElement.classList.toggle("dark", resolved === "dark");
        flushSync(() => setTheme(next));
      };
      const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
      if (!document.startViewTransition || reduce || options.variant === "none" || resolved === resolvedTheme) {
        commit();
        return;
      }
      let style = document.getElementById(STYLE_ID) as HTMLStyleElement | null;
      if (!style) {
        style = document.createElement("style");
        style.id = STYLE_ID;
        document.head.appendChild(style);
      }
      const origin = event ? `${(event.clientX / window.innerWidth) * 100}% ${(event.clientY / window.innerHeight) * 100}%` : "50% 50%";
      style.textContent = createThemeTransition(options, origin);
      const t = document.startViewTransition(commit);
      t.finished.finally(() => style?.remove());
    },
    [options, resolvedTheme, setTheme],
  );

  const toggle = React.useCallback((event?: { clientX: number; clientY: number }) => apply(isDark ? "light" : "dark", event), [apply, isDark]);

  return { isDark, toggle, setTheme: apply };
}

/* ───────────────────────── icons ───────────────────────── */

const T = { ease: "easeInOut", duration: 0.35 } as const;

function HalfIcon({ dark }: { dark: boolean }) {
  return (
    <svg viewBox="0 0 240 240" fill="none" aria-hidden>
      <motion.g initial={false} animate={{ rotate: dark ? -180 : 0 }} transition={{ ...T, duration: 0.5 }} style={{ originX: "50%", originY: "50%" }}>
        <path d="M120 67.5C149.25 67.5 172.5 90.75 172.5 120C172.5 149.25 149.25 172.5 120 172.5" fill="currentColor" />
        <path d="M120 67.5C90.75 67.5 67.5 90.75 67.5 120C67.5 149.25 90.75 172.5 120 172.5" fill="transparent" />
      </motion.g>
      <motion.path
        initial={false}
        animate={{ rotate: dark ? 180 : 0 }}
        transition={{ ...T, duration: 0.5 }}
        style={{ originX: "50%", originY: "50%" }}
        d="M120 3.75C55.5 3.75 3.75 55.5 3.75 120C3.75 184.5 55.5 236.25 120 236.25C184.5 236.25 236.25 184.5 236.25 120C236.25 55.5 184.5 3.75 120 3.75ZM120 214.5V172.5C90.75 172.5 67.5 149.25 67.5 120C67.5 90.75 90.75 67.5 120 67.5V25.5C172.5 25.5 214.5 67.5 214.5 120C214.5 172.5 172.5 214.5 120 214.5Z"
        fill="currentColor"
      />
    </svg>
  );
}

function SunIcon({ dark, id }: { dark: boolean; id: string }) {
  return (
    <svg viewBox="0 0 32 32" fill="currentColor" strokeLinecap="round" aria-hidden>
      <clipPath id={id}>
        <motion.path initial={false} animate={{ y: dark ? 10 : 0, x: dark ? -12 : 0 }} transition={T} d="M0-5h30a1 1 0 0 0 9 13v24H0Z" />
      </clipPath>
      <g clipPath={`url(#${id})`}>
        <motion.circle initial={false} animate={{ r: dark ? 10 : 8 }} transition={T} cx="16" cy="16" />
        <motion.g
          initial={false}
          animate={{ rotate: dark ? -100 : 0, scale: dark ? 0.5 : 1, opacity: dark ? 0 : 1 }}
          transition={T}
          style={{ originX: "50%", originY: "50%" }}
          stroke="currentColor"
          strokeWidth="1.5"
        >
          <path d="M16 5.5v-4M16 30.5v-4M1.5 16h4M26.5 16h4m-3.1-7.4 2.8-2.8M5.7 26.3l2.9-2.9M5.8 5.8l2.8 2.8m14.8 14.8 2.9 2.9" />
        </motion.g>
      </g>
    </svg>
  );
}

function DotsIcon({ dark, id }: { dark: boolean; id: string }) {
  return (
    <svg viewBox="0 0 32 32" fill="currentColor" aria-hidden>
      <clipPath id={id}>
        <motion.path initial={false} animate={{ y: dark ? 14 : 0, x: dark ? -11 : 0 }} transition={T} d="M0-11h25a1 1 0 0017 13v30H0Z" />
      </clipPath>
      <g clipPath={`url(#${id})`}>
        <motion.circle initial={false} animate={{ r: dark ? 10 : 8 }} transition={T} cx="16" cy="16" />
        <motion.g initial={false} animate={{ scale: dark ? 0.5 : 1, opacity: dark ? 0 : 1 }} transition={T} style={{ originX: "50%", originY: "50%" }}>
          <path d="M18.3 3.2c0 1.3-1 2.3-2.3 2.3s-2.3-1-2.3-2.3S14.7.9 16 .9s2.3 1 2.3 2.3zm-4.6 25.6c0-1.3 1-2.3 2.3-2.3s2.3 1 2.3 2.3-1 2.3-2.3 2.3-2.3-1-2.3-2.3zm15.1-10.5c-1.3 0-2.3-1-2.3-2.3s1-2.3 2.3-2.3 2.3 1 2.3 2.3-1 2.3-2.3 2.3zM3.2 13.7c1.3 0 2.3 1 2.3 2.3s-1 2.3-2.3 2.3S.9 17.3.9 16s1-2.3 2.3-2.3zm5.8-7C9 7.9 7.9 9 6.7 9S4.4 8 4.4 6.7s1-2.3 2.3-2.3S9 5.4 9 6.7zm16.3 21c-1.3 0-2.3-1-2.3-2.3s1-2.3 2.3-2.3 2.3 1 2.3 2.3-1 2.3-2.3 2.3zm2.4-21c0 1.3-1 2.3-2.3 2.3S23 7.9 23 6.7s1-2.3 2.3-2.3 2.4 1 2.4 2.3zM6.7 23C8 23 9 24 9 25.3s-1 2.3-2.3 2.3-2.3-1-2.3-2.3 1-2.3 2.3-2.3z" />
        </motion.g>
      </g>
    </svg>
  );
}

function BulbIcon({ dark }: { dark: boolean }) {
  return (
    <svg viewBox="0 0 32 32" strokeWidth="0.7" stroke="currentColor" fill="currentColor" strokeLinecap="round" aria-hidden>
      <path
        strokeWidth="0"
        d="M9.4 9.9c1.8-1.8 4.1-2.7 6.6-2.7 5.1 0 9.3 4.2 9.3 9.3 0 2.3-.8 4.4-2.3 6.1-.7.8-2 2.8-2.5 4.4 0 .2-.2.4-.5.4-.2 0-.4-.2-.4-.5v-.1c.5-1.8 2-3.9 2.7-4.8 1.4-1.5 2.1-3.5 2.1-5.6 0-4.7-3.7-8.5-8.4-8.5-2.3 0-4.4.9-5.9 2.5-1.6 1.6-2.5 3.7-2.5 6 0 2.1.7 4 2.1 5.6.8.9 2.2 2.9 2.7 4.9 0 .2-.1.5-.4.5h-.1c-.2 0-.4-.1-.4-.4-.5-1.7-1.8-3.7-2.5-4.5-1.5-1.7-2.3-3.9-2.3-6.1 0-2.3 1-4.7 2.7-6.5z"
      />
      <path d="M19.8 28.3h-7.6M19.8 29.5h-7.6M19.8 30.7h-7.6" />
      <motion.path
        initial={false}
        animate={{ pathLength: dark ? 0 : 1, opacity: dark ? 0 : 1 }}
        transition={T}
        fill="none"
        d="M14.6 27.1c0-3.4 0-6.8-.1-10.2-.2-1-1.1-1.7-2-1.7-1.2-.1-2.3 1-2.2 2.3.1 1 .9 1.9 2.1 2h7.2c1.1-.1 2-1 2.1-2 .1-1.2-1-2.3-2.2-2.3-.9 0-1.7.7-2 1.7 0 3.4 0 6.8-.1 10.2"
      />
      <motion.g initial={false} animate={{ scale: dark ? 0.5 : 1, opacity: dark ? 0 : 1 }} transition={T} style={{ originX: "50%", originY: "50%" }}>
        <path d="M16 6.4V1.3M26.3 15.8h5.1m-8.8-6.8 3.7-3.6M9.4 9 5.7 5.4M5.7 15.8H.6" />
      </motion.g>
    </svg>
  );
}

function EclipseIcon({ dark, id }: { dark: boolean; id: string }) {
  return (
    <svg viewBox="0 0 32 32" fill="currentColor" aria-hidden>
      <clipPath id={id}>
        <motion.path initial={false} animate={{ y: dark ? 5 : 0, x: dark ? -20 : 0 }} transition={T} d="M0-5h55v37h-55zm32 12a1 1 0 0025 0 1 1 0 00-25 0" />
      </clipPath>
      <g clipPath={`url(#${id})`}>
        <circle cx="16" cy="16" r="15" />
      </g>
    </svg>
  );
}

const icons = { half: HalfIcon, sun: SunIcon, dots: DotsIcon, bulb: BulbIcon, eclipse: EclipseIcon } as const;
type ThemeToggleIcon = keyof typeof icons;

/* ───────────────────────── button ───────────────────────── */

type ThemeToggleProps = Omit<React.ComponentProps<"button">, "children"> &
  ThemeTransitionOptions & {
    icon?: ThemeToggleIcon;
    /** "ghost" sits on any surface; "solid" inverts with the theme. */
    appearance?: "ghost" | "solid";
    label?: string;
  };

function ThemeToggle({
  icon = "sun",
  appearance = "ghost",
  variant = "circle",
  start = "pointer",
  blur,
  duration,
  gifUrl,
  label = "Toggle theme",
  className,
  onClick,
  ...props
}: ThemeToggleProps) {
  const options = React.useMemo(() => ({ variant, start, blur, duration, gifUrl }), [variant, start, blur, duration, gifUrl]);
  const { isDark, toggle } = useThemeTransition(options);
  const id = `dx-tt-${React.useId().replace(/:/g, "")}`;
  const Icon = icons[icon];
  return (
    <button
      type="button"
      data-slot="theme-toggle"
      aria-label={label}
      aria-pressed={isDark}
      onClick={(e) => {
        onClick?.(e);
        if (!e.defaultPrevented) toggle(e);
      }}
      className={cn(
        "focus-ring relative inline-flex size-10 shrink-0 cursor-pointer items-center justify-center rounded-full p-2.5 transition-transform duration-300 active:scale-95 [&_svg]:size-full",
        appearance === "ghost" ? "state-layer text-foreground" : "bg-foreground text-background",
        className,
      )}
      {...props}
    >
      <Icon dark={isDark} id={id} />
    </button>
  );
}

export { ThemeToggle, useThemeTransition, createThemeTransition };
export type { ThemeToggleProps, ThemeToggleIcon, ThemeTransitionOptions, ThemeTransitionVariant, ThemeTransitionStart };
