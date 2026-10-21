'use client';

import type { ReactNode } from 'react';
import { Check, CircleAlert, LoaderCircle, Minus } from 'lucide-react';
import { cn } from '@/lib/utils';

export type LaunchStepStatus = 'pending' | 'active' | 'done' | 'skipped' | 'error';

export interface LaunchStep {
  id: string;
  label: string;
  status: LaunchStepStatus;
  detail?: ReactNode;
  body?: ReactNode;
}

export interface LaunchProgressProps {
  steps: LaunchStep[];
  message?: string;
  busy?: boolean;
  className?: string;
}

const markerClass: Record<LaunchStepStatus, string> = {
  pending: 'bg-surface-3 text-text-faint',
  active: 'bg-accent-soft-strong text-accent',
  done: 'bg-accent text-on-accent',
  skipped: 'bg-surface-3 text-text-faint',
  error: 'bg-danger-soft text-danger',
};

const statusText: Record<LaunchStepStatus, string> = {
  pending: 'not started',
  active: 'in progress',
  done: 'done',
  skipped: 'skipped',
  error: 'needs attention',
};

export function LaunchProgress({ steps, message, busy = false, className }: LaunchProgressProps) {
  return (
    <div className={cn('flex flex-col gap-3', className)}>
      <ol aria-label="Steps" className="flex flex-col gap-1">
        {steps.map((step, index) => {
          const open = step.status === 'active' || step.status === 'error';
          return (
            <li
              key={step.id}
              aria-current={step.status === 'active' ? 'step' : undefined}
              className={cn('flex gap-3 rounded-md p-3', open ? 'bg-surface-2' : 'bg-transparent')}
            >
              <span aria-hidden="true" className={cn('flex size-7 shrink-0 items-center justify-center rounded-pill text-xs font-bold tabular-nums', markerClass[step.status])}>
                {step.status === 'done' ? <Check className="size-icon-sm" />
                  : step.status === 'error' ? <CircleAlert className="size-icon-sm" />
                    : step.status === 'skipped' ? <Minus className="size-icon-sm" />
                      : step.status === 'active' && busy ? <LoaderCircle className="size-icon-sm animate-spin" />
                        : index + 1}
              </span>
              <div className="flex min-w-0 flex-1 flex-col gap-2">
                <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5">
                  <p className={cn('text-sm font-bold', step.status === 'pending' || step.status === 'skipped' ? 'text-text-muted' : 'text-text')}>
                    {step.label}
                    <span className="sr-only">, {statusText[step.status]}</span>
                  </p>
                  {step.detail ? <p className={cn('text-xs', step.status === 'error' ? 'text-danger' : 'text-text-faint')}>{step.detail}</p> : null}
                </div>
                {open && step.body ? <div className="flex flex-col gap-3">{step.body}</div> : null}
              </div>
            </li>
          );
        })}
      </ol>
      {message ? <p role="status" className="text-xs text-text-muted">{message}</p> : null}
    </div>
  );
}
