'use client';
import type { ReactNode } from 'react';
import { Info, Trophy } from 'lucide-react';
import { cn } from '@/lib/utils';
import type { Loadable } from '@/core/market/unsupported';
import { technicalDetail } from '@/core/user-facing-error';
import { CountdownProgress, DisclosureRow, ErrorState, Skeleton, StatusBadge, UnavailableState } from '@/ui/primitives';
import { formatUsdl } from '@/utils/format';
import { CupRulesDisclosure } from './cup-rules-disclosure';
import { cupBadge, cupModeLabel, type Cup } from './cups-contract';

export interface CupHeroProps {
  cup: Loadable<Cup | null>;
  onRetry: () => void;
  /** Period control rendered inside the frame, bottom-left above the cup name (the pump.fun hero anatomy). */
  control?: ReactNode;
  className?: string;
}

/**
 * The frame never collapses: every state keeps the same art area (tall on
 * phones, 16:7 from md), the in-frame control, a name row, a prize line and a
 * rules row, so the page structure survives a failed or unserved cup query.
 * The art area sits above the text block, which carries the flat media tint
 * (pump.fun anatomy). The frame grows with its content
 * instead of clipping it, so a long notice on a narrow phone never hides the
 * rules row.
 */
const frame = 'relative flex flex-col rounded-lg bg-surface-1 aspect-[3/4] min-h-[360px] sm:aspect-[4/3] md:aspect-[16/7] md:min-h-[340px]';
const block = 'relative mt-auto flex flex-col gap-2 rounded-b-lg bg-media-overlay px-4 pt-4 pb-1 md:px-6 md:pt-5';
const nameClass = 'text-md font-bold uppercase tracking-[0.04em] text-text';
const prizeClass = 'text-[32px] font-extrabold leading-none tabular-nums md:text-[40px]';
const footRow = '-mx-3 flex h-12 items-center gap-3 rounded-sm px-3 text-sm';
const rulesLabel = 'Schedule, scoring, eligibility and rules';

/**
 * Our own flat mark in the art area (decorative). It takes whatever height the
 * text block leaves and scales the disc down to fit, so the block never covers it.
 */
function Mark() {
  return (
    <div aria-hidden="true" data-cup-art className="pointer-events-none flex min-h-16 flex-1 items-center justify-center py-3">
      <div className="flex aspect-square h-full max-h-24 items-center justify-center rounded-pill bg-surface-2 lg:max-h-32">
        <Trophy className="size-1/2 text-text-faint" />
      </div>
    </div>
  );
}

function Control({ children }: { children?: ReactNode }) {
  return children ? <div className="mb-1 flex">{children}</div> : null;
}

/**
 * Cup hero (components.md 38). Variants come from the real IF_CUP_CURRENT
 * answer: live, scheduled, finished, none (served `cup: null`), and the
 * unavailable state when no service serves `/v2/cups/current`. Art is a flat
 * surface-2 mark with our own trophy; the bottom block sits on the media tint.
 * No prize figure or ranking is ever shown unless it came from the record.
 * Unavailable and failed answers use the shared state notices (chip variant)
 * inside the frame rather than replacing it.
 */
export function CupHero({ cup, onRetry, control, className }: CupHeroProps) {
  if (cup.state === 'loading') {
    return (
      <section aria-busy="true" aria-label="Competition" data-cup-state="loading" className={cn(frame, className)}>
        <Mark />
        <div className={block}>
          <Control>{control}</Control>
          <Skeleton variant="text" width={180} height={20} label="competition" />
          <Skeleton variant="text" width={220} height={40} />
          <div className={footRow}><Skeleton variant="text" width={260} height={16} /></div>
        </div>
      </section>
    );
  }

  if (cup.state === 'unsupported' || cup.state === 'error' || cup.data === null) {
    // Every case reads as plain copy; the unserved route keeps its operation and status as a developer-only attribute.
    const title = cup.state === 'error' ? "Couldn't load the cup" : 'No competition scheduled';
    const body = cup.state === 'unsupported'
      ? 'Prizes and results show up here once cups start'
      : cup.state === 'ready'
        ? 'There is no cup running right now, check the 1D, 1W and 1M boards'
        : null;
    const detail = cup.state === 'unsupported' ? technicalDetail(cup.operation, cup.status) : null;
    return (
      <section aria-label="Competition" title={detail ?? undefined} data-operation={detail ?? undefined} data-cup-state={cup.state === 'ready' ? 'none' : cup.state} className={cn(frame, className)}>
        <Mark />
        <div className={block}>
          <Control>{control}</Control>
          <h2 className={nameClass}>{title}</h2>
          <p className="text-xl font-bold text-text-faint">No prize pool announced</p>
          {cup.state === 'unsupported' ? (
            <UnavailableState variant="chip" wrap title="Competitions" plural operation={cup.operation} status={cup.status} onRetry={onRetry} className="self-start" />
          ) : null}
          {cup.state === 'error' ? <ErrorState variant="chip" wrap error={cup.error} onRetry={cup.retry} className="self-start" /> : null}
          {body ? <p className="max-w-lg text-sm text-text-muted">{body}</p> : null}
          <DisclosureRow
            id="cup-rules-unavailable"
            icon={Info}
            label={rulesLabel}
            disabled
            trailing={<span className="hidden shrink-0 text-xs font-semibold text-text-faint sm:inline">Coming with the next cup</span>}
            className="-mx-3"
          />
        </div>
      </section>
    );
  }

  const record = cup.data;
  const badge = cupBadge(record.status);
  return (
    <section aria-labelledby="cup-hero-title" data-cup-state={record.status} className={cn(frame, className)}>
      <Mark />
      <div className={block}>
        <Control>{control}</Control>
        <div className="flex flex-wrap items-center gap-2">
          <h2 id="cup-hero-title" className={nameClass}>{record.name}</h2>
          <StatusBadge status={badge.status} size={record.status === 'live' ? 'md' : 'sm'}>{badge.label}</StatusBadge>
          <span className="text-xs text-text-muted">{cupModeLabel[record.mode]}</span>
        </div>
        <p className={cn(prizeClass, 'text-success')} data-numeric>
          <span className="sr-only">Prize pool </span>
          {formatUsdl(record.prizePoolRaw, { fractionDigits: 0 })}
        </p>
        <p className="text-sm text-text-muted">
          {record.status === 'scheduled' ? <>Starts in <CountdownProgress endsAt={record.startsAt} variant="text" className="inline" /></> : null}
          {record.status === 'live' ? <>Ends in <CountdownProgress endsAt={record.endsAt} startsAt={record.startsAt} variant="text" className="inline" /></> : null}
          {record.status === 'finished' ? <>Ended {new Date(record.endsAt).toLocaleDateString(undefined, { dateStyle: 'medium' })}, final results below</> : null}
        </p>
        <CupRulesDisclosure cup={record} className="-mx-3" />
      </div>
    </section>
  );
}
