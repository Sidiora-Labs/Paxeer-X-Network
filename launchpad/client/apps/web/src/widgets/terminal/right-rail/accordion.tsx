'use client';
import { useCallback, useSyncExternalStore, type ReactNode } from 'react';
import { ChevronDown } from 'lucide-react';
import { cn } from '@/lib/utils';

export type AccordionId = 'trade' | 'security' | 'similar';
export type AccordionState = Record<AccordionId, boolean>;

export const accordionStorageKey = 'sidiora-terminal-accordions';
const defaults: AccordionState = { trade: true, security: false, similar: true };

export function parseAccordions(raw: string | null): AccordionState {
  if (!raw) return { ...defaults };
  try {
    const value: unknown = JSON.parse(raw);
    if (typeof value !== 'object' || value === null) return { ...defaults };
    const row = value as Record<string, unknown>;
    const pick = (id: AccordionId) => (typeof row[id] === 'boolean' ? row[id] as boolean : defaults[id]);
    return { trade: pick('trade'), security: pick('security'), similar: pick('similar') };
  } catch {
    return { ...defaults };
  }
}

const listeners = new Set<() => void>();
let cache: { raw: string | null; state: AccordionState } | null = null;
const serverState: AccordionState = { ...defaults };

function readRaw(): string | null {
  try { return window.localStorage.getItem(accordionStorageKey); } catch { return null; }
}
function snapshot(): AccordionState {
  const raw = readRaw();
  if (!cache || cache.raw !== raw) cache = { raw, state: parseAccordions(raw) };
  return cache.state;
}
function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

/** Open / closed state of the right-rail sections, persisted per device. */
export function useAccordion(id: AccordionId): [boolean, (open: boolean) => void] {
  const state = useSyncExternalStore(subscribe, snapshot, () => serverState);
  const set = useCallback((open: boolean) => {
    const next = { ...snapshot(), [id]: open };
    const raw = JSON.stringify(next);
    try { window.localStorage.setItem(accordionStorageKey, raw); } catch { /* storage blocked: in-memory only */ }
    cache = { raw: readRaw() ?? raw, state: next };
    for (const listener of listeners) listener();
  }, [id]);
  return [state[id], set];
}

/** Section card with a title toggle, optional trailing controls outside the toggle, and a persisted caret. */
export function AccordionCard({ id, title, trailing, children }: { id: AccordionId; title: string; trailing?: ReactNode; children: ReactNode }) {
  const [open, setOpen] = useAccordion(id);
  const buttonId = `terminal-accordion-${id}`;
  const panelId = `${buttonId}-panel`;
  return (
    <section aria-labelledby={buttonId} className="flex flex-col rounded-md bg-surface-1">
      <div className="flex h-9 items-center gap-1 px-2">
        <button
          type="button"
          id={buttonId}
          aria-expanded={open}
          aria-controls={panelId}
          onClick={() => setOpen(!open)}
          className="flex h-7 min-w-0 flex-1 items-center gap-1 rounded-sm px-1 text-left text-sm font-bold text-text transition-colors duration-fast hover:bg-surface-hover focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
        >
          <span className="min-w-0 flex-1 truncate">{title}</span>
        </button>
        {open ? trailing : null}
        <button
          type="button"
          aria-label={`${open ? 'Collapse' : 'Expand'} ${title}`}
          aria-expanded={open}
          aria-controls={panelId}
          onClick={() => setOpen(!open)}
          className="inline-flex size-7 items-center justify-center rounded-sm text-text-faint transition-colors duration-fast hover:bg-surface-hover hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
        >
          <ChevronDown aria-hidden="true" className={cn('size-icon-sm transition-transform duration-fast', open && 'rotate-180')} />
        </button>
      </div>
      <div id={panelId} role="region" aria-labelledby={buttonId} hidden={!open} className="px-2 pb-2">
        {open ? children : null}
      </div>
    </section>
  );
}
