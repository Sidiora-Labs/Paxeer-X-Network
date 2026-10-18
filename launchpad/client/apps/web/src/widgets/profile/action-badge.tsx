import { ArrowDownLeft, ArrowUpRight } from 'lucide-react';
import { cn } from '@/lib/utils';
import type { ActivityKind } from './activity-transport';

const actionStyle: Record<ActivityKind, { label: string; tone: string; icon: typeof ArrowDownLeft }> = {
  receive: { label: 'Receive', tone: 'bg-success-soft text-success', icon: ArrowDownLeft },
  send: { label: 'Send', tone: 'bg-danger-soft text-danger', icon: ArrowUpRight },
};

export const actionLabel = (kind: ActivityKind): string => actionStyle[kind].label;

/** Inflows (receive) read in the success tint, outflows (send) in the danger tint. The badge text is the action name. */
export function ActionBadge({ kind, className }: { kind: ActivityKind; className?: string }) {
  const { label, tone, icon: Icon } = actionStyle[kind];
  return (
    <span className={cn('inline-flex h-6 items-center gap-1 rounded-pill px-2 text-xs font-bold', tone, className)}>
      <Icon aria-hidden="true" className="size-icon-sm" />
      {label}
    </span>
  );
}
