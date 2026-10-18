'use client';

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { RotateCw, Unplug } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Button, NoticeBar, humanError } from '@/ui/primitives';

/** One section of the profile page whose read failed. */
export interface ProfileIssue {
  /** Section name people recognise: "Follower counts", "Portfolio value". */
  label: string;
  /** The thrown value; reworded before it is shown, never printed raw. */
  error: unknown;
  retry?: () => void;
}

interface ProfileIssuesStore {
  report: (key: string, issue: ProfileIssue | null) => void;
}

const ProfileIssuesContext = createContext<ProfileIssuesStore | null>(null);
const ProfileIssuesListContext = createContext<Record<string, ProfileIssue>>({});

/**
 * Collects read failures from every block on the profile page so the page
 * shows one notice instead of a red paragraph per block. Blocks rendered
 * outside the provider keep their own inline error.
 */
export function ProfileIssuesProvider({ children }: { children: ReactNode }) {
  const [issues, setIssues] = useState<Record<string, ProfileIssue>>({});
  const report = useCallback((key: string, issue: ProfileIssue | null) => {
    setIssues(current => {
      const existing = current[key];
      if (!issue) {
        if (!existing) return current;
        const next = { ...current };
        delete next[key];
        return next;
      }
      return { ...current, [key]: issue };
    });
  }, []);
  const store = useMemo(() => ({ report }), [report]);
  return (
    <ProfileIssuesContext.Provider value={store}>
      <ProfileIssuesListContext.Provider value={issues}>{children}</ProfileIssuesListContext.Provider>
    </ProfileIssuesContext.Provider>
  );
}

/**
 * Reports a block's failure to the page notice. Returns true when a page
 * notice is collecting failures, in which case the block shows a quiet
 * placeholder ("—") rather than its own error copy.
 */
export function useProfileIssue(key: string, issue: ProfileIssue | null): boolean {
  const store = useContext(ProfileIssuesContext);
  const label = issue?.label ?? null;
  // Keyed on the worded reason, not the error object, so a fresh Error per render cannot loop the page notice.
  const reason = issue ? humanError(issue.error) : null;
  const latest = useRef(issue);
  useEffect(() => { latest.current = issue; });
  const hasRetry = Boolean(issue?.retry);
  useEffect(() => {
    if (!store) return;
    if (label === null || reason === null) { store.report(key, null); return; }
    store.report(key, { label, error: latest.current?.error, retry: hasRetry ? () => latest.current?.retry?.() : undefined });
  }, [store, key, label, reason, hasRetry]);
  useEffect(() => () => store?.report(key, null), [store, key]);
  return store !== null;
}

/** Plain-language list: "A", "A and B", "A, B and C". */
export function joinLabels(labels: string[]): string {
  if (labels.length <= 1) return labels[0] ?? '';
  return `${labels.slice(0, -1).join(', ')} and ${labels[labels.length - 1]}`;
}

/** Copy for the page notice: one sentence naming the reason, shared when every failure has the same cause. */
export function profileIssueCopy(issues: ProfileIssue[]): { title: string; body: string } | null {
  if (issues.length === 0) return null;
  const messages = [...new Set(issues.map(issue => humanError(issue.error)))];
  const body = messages.length === 1 ? messages[0] : "Some of this profile didn't load, try again in a moment";
  return { title: "Some of this profile didn't load", body };
}

/** The single page-level notice for every failed read on the profile. Renders nothing while all reads are healthy. */
export function ProfileIssuesNotice({ className }: { className?: string }) {
  const issues = useContext(ProfileIssuesListContext);
  const list = Object.values(issues);
  const copy = profileIssueCopy(list);
  if (!copy) return null;
  const retries = list.map(issue => issue.retry).filter((retry): retry is () => void => Boolean(retry));
  const labels = [...new Set(list.map(issue => issue.label))];
  return (
    <div data-profile-notice className={className}>
      <NoticeBar
        tone="neutral"
        variant="bar"
        wrap
        announce
        icon={Unplug}
        title={copy.title}
        className="rounded-none px-4 py-3 md:px-5"
        action={retries.length > 0 ? (
          <Button variant="secondary" size="sm" icon={<RotateCw />} onClick={() => retries.forEach(retry => retry())}>Try again</Button>
        ) : undefined}
      >
        <p>{copy.body}</p>
        <details className="mt-1 text-xs text-text-faint">
          <summary className="cursor-pointer select-none hover:text-text-muted">Details</summary>
          <p className="mt-1">Missing right now: {joinLabels(labels)}. Each shows a dash until it loads.</p>
        </details>
      </NoticeBar>
    </div>
  );
}

/** The quiet placeholder a block shows in a slot whose value failed to load. */
export function Dash({ label, className }: { label: string; className?: string }) {
  return <span className={cn('text-text-faint', className)}><span aria-hidden="true">—</span><span className="sr-only">{label} not loaded</span></span>;
}

/** What a tab panel shows when its read failed and the page notice carries the reason. */
export function CollectedPlaceholder({ label, className }: { label: string; className?: string }) {
  return (
    <div data-collected-placeholder className={cn('flex min-h-40 items-center justify-center px-4 py-10 text-center text-sm text-text-faint', className)}>
      {label} will show here once they load.
    </div>
  );
}
