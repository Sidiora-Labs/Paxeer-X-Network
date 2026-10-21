"use client";

import * as React from "react";
import { usePlatform, type PlatformSetting } from "../lib/platform";
import { CodeInput, Keypad } from "./code-input";
import { cn } from "../lib/utils";

/**
 * Code / secret entry, per platform:
 * - mobile:  tap-per-box code kit — segmented display driven by the
 *            on-screen Keypad (native keyboard stays down)
 * - desktop: a single segmented input with full-code paste + auto-advance
 *
 * Includes the resend timer row and error copy from the 2FA screens.
 */
export function CodeEntry({
  length = 6,
  value,
  onChange,
  onComplete,
  error,
  errorText,
  resendIn,
  onResend,
  platform,
  className,
}: {
  length?: number;
  value: string;
  onChange: (value: string) => void;
  onComplete?: (value: string) => void;
  error?: boolean;
  errorText?: string;
  /** Seconds until resend is allowed; 0/undefined = resend available. */
  resendIn?: number;
  onResend?: () => void;
  platform?: PlatformSetting;
  className?: string;
}) {
  const resolved = usePlatform(platform);
  const mobile = resolved === "mobile";

  return (
    <div className={cn("flex flex-col gap-4", className)}>
      <CodeInput
        length={length}
        value={value}
        onChange={onChange}
        onComplete={onComplete}
        error={error}
        readOnly={mobile}
        autoFocus={!mobile}
        aria-label="Verification code"
      />

      {error && errorText && (
        <p role="alert" className="text-center text-[13px] font-medium text-destructive">
          {errorText}
        </p>
      )}

      {(onResend || (resendIn ?? 0) > 0) && (
        <p className="text-center text-sm text-muted-foreground">
          {(resendIn ?? 0) > 0 ? (
            <>
              Resend in{" "}
              <span className="font-semibold tabular-nums text-accent-strong">
                00:{String(resendIn).padStart(2, "0")}
              </span>
            </>
          ) : (
            <button
              type="button"
              onClick={onResend}
              className="font-semibold text-accent hover:underline"
            >
              Resend code
            </button>
          )}
        </p>
      )}

      {mobile && (
        <Keypad
          className="mt-2"
          onDigit={(d) => {
            if (value.length < length) {
              const next = value + d;
              onChange(next);
              if (next.length === length) onComplete?.(next);
            }
          }}
          onBackspace={() => onChange(value.slice(0, -1))}
        />
      )}
    </div>
  );
}
