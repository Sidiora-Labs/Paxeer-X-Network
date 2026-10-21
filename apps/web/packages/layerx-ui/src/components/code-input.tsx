"use client";

import * as React from "react";
import { Delete } from "lucide-react";
import { cn } from "../lib/utils";

export interface CodeInputProps {
  length?: number;
  value: string;
  onChange: (value: string) => void;
  onComplete?: (value: string) => void;
  error?: boolean;
  disabled?: boolean;
  autoFocus?: boolean;
  /** Hide the native input — pair with the on-screen Keypad. */
  readOnly?: boolean;
  className?: string;
  "aria-label"?: string;
}

/**
 * Segmented code entry: per-box display, full-code paste, auto-advance,
 * red error state — the 2FA/PIN kit from the design set.
 * A single hidden input drives everything, so paste and IME work everywhere.
 */
export function CodeInput({
  length = 6,
  value,
  onChange,
  onComplete,
  error,
  disabled,
  autoFocus,
  readOnly,
  className,
  ...aria
}: CodeInputProps) {
  const inputRef = React.useRef<HTMLInputElement>(null);
  const [focused, setFocused] = React.useState(false);

  // Focus via effect with preventScroll — the HTML autofocus attribute
  // scrolls the page to the input on load, which hijacks docs pages.
  React.useEffect(() => {
    if (autoFocus && !readOnly && !disabled) {
      inputRef.current?.focus({ preventScroll: true });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const commit = (next: string) => {
    const clean = next.replace(/\D/g, "").slice(0, length);
    onChange(clean);
    if (clean.length === length) onComplete?.(clean);
  };

  const activeIndex = Math.min(value.length, length - 1);

  return (
    <div
      className={cn("relative", className)}
      onClick={() => !readOnly && inputRef.current?.focus()}
    >
      <input
        ref={inputRef}
        type="text"
        inputMode="numeric"
        autoComplete="one-time-code"
        aria-invalid={error || undefined}
        aria-label={aria["aria-label"] ?? "Verification code"}
        className={cn(
          "absolute inset-0 h-full w-full opacity-0",
          readOnly ? "pointer-events-none" : "cursor-text",
        )}
        value={value}
        disabled={disabled}
        readOnly={readOnly}
        onFocus={() => setFocused(true)}
        onBlur={() => setFocused(false)}
        onChange={(e) => commit(e.target.value)}
        onPaste={(e) => {
          e.preventDefault();
          commit(e.clipboardData.getData("text"));
        }}
      />
      <div className="flex items-center justify-center gap-2.5" aria-hidden>
        {Array.from({ length }).map((_, i) => {
          const char = value[i];
          const isActive = focused && !disabled && i === activeIndex;
          return (
            <span
              key={i}
              className={cn(
                "flex size-12 items-center justify-center rounded-md border bg-surface text-xl font-semibold tabular-nums transition-colors",
                error
                  ? "border-destructive text-destructive"
                  : isActive
                    ? "border-accent ring-2 ring-accent/20 text-foreground"
                    : "border-border text-foreground",
                disabled && "opacity-50",
              )}
            >
              {char ?? ""}
            </span>
          );
        })}
      </div>
    </div>
  );
}

/* ---------------------------------------------------------------- Keypad */

/**
 * On-screen numeric keypad (1–9 with T9 letters, "+*#", 0, backspace) —
 * the mobile half of the code-entry kit.
 */
export function Keypad({
  onDigit,
  onBackspace,
  className,
}: {
  onDigit: (digit: string) => void;
  onBackspace: () => void;
  className?: string;
}) {
  const keys: { main: string; sub?: string }[] = [
    { main: "1" },
    { main: "2", sub: "ABC" },
    { main: "3", sub: "DEF" },
    { main: "4", sub: "GHI" },
    { main: "5", sub: "JKL" },
    { main: "6", sub: "MNO" },
    { main: "7", sub: "PQRS" },
    { main: "8", sub: "TUV" },
    { main: "9", sub: "WXYZ" },
    { main: "+*#" },
    { main: "0" },
    { main: "back" },
  ];
  return (
    <div className={cn("grid grid-cols-3 gap-px overflow-hidden rounded-lg bg-border", className)}>
      {keys.map((k) =>
        k.main === "back" ? (
          <button
            key="back"
            type="button"
            aria-label="Backspace"
            onClick={onBackspace}
            className="flex h-14 items-center justify-center bg-surface text-foreground transition-colors active:bg-surface-sunken"
          >
            <Delete className="size-5" />
          </button>
        ) : (
          <button
            key={k.main}
            type="button"
            onClick={() => /^\d$/.test(k.main) && onDigit(k.main)}
            className="flex h-14 flex-col items-center justify-center gap-0 bg-surface text-foreground transition-colors active:bg-surface-sunken"
          >
            <span className="text-xl font-semibold leading-tight">{k.main}</span>
            {k.sub && (
              <span className="text-[9px] font-semibold tracking-[0.18em] text-muted-foreground">
                {k.sub}
              </span>
            )}
          </button>
        ),
      )}
    </div>
  );
}
