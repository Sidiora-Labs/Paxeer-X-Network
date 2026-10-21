"use client";

import * as React from "react";
import { ArrowLeft, Check } from "lucide-react";
import { cn } from "../lib/utils";
import { usePlatform, type PlatformSetting } from "../lib/platform";
import { Button, IconButton } from "./button";
import { PrimaryAction } from "./primary-action";

export interface WizardStep {
  id: string;
  /** Short label for the progress rail / summary. */
  label: string;
  /** The one decision this screen asks for. */
  title: string;
  description?: string;
  render: () => React.ReactNode;
  /** Gate Continue until the step is valid. */
  canContinue?: () => boolean;
}

export interface WizardSummaryItem {
  label: string;
  value: React.ReactNode;
}

/**
 * Multi-step journeys, per platform:
 * - mobile:  full-screen wizard — one decision per screen, progress bar,
 *            back button, pinned Continue at thumb reach
 * - desktop: split pane — form on the left, a live "what will happen"
 *            summary pinned on the right
 */
export function Wizard({
  steps,
  summary,
  onComplete,
  onCancel,
  completeLabel = "Confirm",
  summaryTitle = "What will happen",
  platform,
  className,
}: {
  steps: WizardStep[];
  /** Live summary of the choices so far (desktop right rail). */
  summary?: WizardSummaryItem[];
  onComplete?: () => void;
  onCancel?: () => void;
  completeLabel?: string;
  summaryTitle?: string;
  platform?: PlatformSetting;
  className?: string;
}) {
  const resolved = usePlatform(platform);
  const [index, setIndex] = React.useState(0);
  const step = steps[index];
  const isLast = index === steps.length - 1;
  const canContinue = step.canContinue ? step.canContinue() : true;

  const next = () => {
    if (isLast) onComplete?.();
    else setIndex((i) => Math.min(i + 1, steps.length - 1));
  };
  const back = () => {
    if (index === 0) onCancel?.();
    else setIndex((i) => Math.max(0, i - 1));
  };

  const stepBody = (
    <div className="flex flex-col gap-2">
      <h2 className="text-xl font-bold text-foreground">{step.title}</h2>
      {step.description && (
        <p className="text-[15px] leading-relaxed text-muted-foreground">{step.description}</p>
      )}
      <div className="pt-4">{step.render()}</div>
    </div>
  );

  if (resolved === "mobile") {
    return (
      <div className={cn("flex h-full min-h-0 flex-1 flex-col", className)}>
        {/* progress + back */}
        <div className="flex items-center gap-3 px-4 pt-2 pb-4">
          <IconButton variant="outline" size="sm" onClick={back} aria-label="Back">
            <ArrowLeft />
          </IconButton>
          <div className="flex flex-1 items-center gap-1.5" aria-hidden>
            {steps.map((s, i) => (
              <span
                key={s.id}
                className={cn(
                  "h-1 flex-1 rounded-full transition-colors",
                  i <= index ? "bg-foreground" : "bg-border",
                )}
              />
            ))}
          </div>
          <span className="text-xs font-semibold text-muted-foreground tabular-nums">
            {index + 1}/{steps.length}
          </span>
        </div>

        <div className="lx-scroll flex min-h-0 flex-1 flex-col overflow-y-auto px-4">
          {stepBody}
          <PrimaryAction onClick={next} disabled={!canContinue} platform="mobile">
            {isLast ? completeLabel : "Continue"}
          </PrimaryAction>
        </div>
      </div>
    );
  }

  // Desktop split pane
  return (
    <div className={cn("grid min-h-0 flex-1 grid-cols-[1fr_340px] gap-8", className)}>
      <div className="flex min-h-0 flex-col">
        {/* step rail */}
        <ol className="flex items-center gap-2 pb-6" aria-label="Progress">
          {steps.map((s, i) => (
            <li key={s.id} className="flex items-center gap-2">
              <span
                className={cn(
                  "inline-flex size-6 items-center justify-center rounded-full text-xs font-bold",
                  i < index
                    ? "bg-success text-success-foreground"
                    : i === index
                      ? "bg-primary text-primary-foreground"
                      : "bg-surface-sunken text-faint-foreground",
                )}
              >
                {i < index ? <Check className="size-3.5" /> : i + 1}
              </span>
              <span
                className={cn(
                  "text-sm font-semibold",
                  i === index ? "text-foreground" : "text-muted-foreground",
                )}
              >
                {s.label}
              </span>
              {i < steps.length - 1 && <span className="mx-1 h-px w-6 bg-border" aria-hidden />}
            </li>
          ))}
        </ol>

        <div className="lx-scroll min-h-0 flex-1 overflow-y-auto pr-2">{stepBody}</div>

        <div className="flex items-center gap-3 border-t border-border pt-4">
          <Button variant="secondary" onClick={back}>
            {index === 0 ? "Cancel" : "Back"}
          </Button>
          <Button onClick={next} disabled={!canContinue} className="min-w-[160px]">
            {isLast ? completeLabel : "Continue"}
          </Button>
        </div>
      </div>

      {/* live "what will happen" summary */}
      <aside className="sticky top-0 h-fit rounded-lg border border-border bg-surface p-5 shadow-card">
        <h3 className="text-sm font-bold tracking-wide text-muted-foreground uppercase">
          {summaryTitle}
        </h3>
        <dl className="mt-3 flex flex-col divide-y divide-border/70">
          {(summary ?? []).map((item) => (
            <div key={item.label} className="flex items-center justify-between gap-4 py-3">
              <dt className="text-sm text-muted-foreground">{item.label}</dt>
              <dd className="text-right text-sm font-semibold text-foreground">{item.value}</dd>
            </div>
          ))}
          {(!summary || summary.length === 0) && (
            <p className="py-3 text-sm text-faint-foreground">
              Your choices will appear here as you go.
            </p>
          )}
        </dl>
      </aside>
    </div>
  );
}
