'use client';

import { useEffect, useRef, type ReactNode } from 'react';
import type { LucideIcon } from 'lucide-react';
import { ChevronDown, ChevronRight, CircleAlert, CircleCheck, Inbox, Info, LoaderCircle, TriangleAlert, Unplug, X } from 'lucide-react';
import Link from 'next/link';
import { cn } from '@/lib/utils';
import { Button } from './button';
import { IconButton } from './button';
import { CountPill } from './badges';
import { plainCopy, plainText, userFacingMessage } from '@/core/user-facing-error';

/* ------------------------------------------------------- Human copy */

/** Transport wording that must never reach the screen, whatever wording layer it slipped through (Node's "fetch failed", "TypeError: ...", socket codes). */
const transportLeak = /fetch failed|failed to fetch|load failed|networkerror|network request failed|\btypeerror\b|\beconn(?:reset|refused|aborted)\b|\benotfound\b|\betimedout\b|socket hang up|\berr_[a-z_]+\b/i;

/** Plain wording for a string that may be machine text; transport failures always read as the network sentence. */
export function humanErrorText(text: string | null | undefined, fallback?: string): string {
  const out = plainText(text, fallback);
  return transportLeak.test(out) ? plainCopy.network : out;
}

/** Plain wording for any thrown value, with the same transport guarantee as `humanErrorText`. */
export function humanError(error: unknown, fallback?: string): string {
  const out = userFacingMessage(error, fallback);
  return transportLeak.test(out) ? plainCopy.network : out;
}

/* ------------------------------------------------------------ Skeleton */

export interface SkeletonProps {
  variant?: 'text' | 'circle' | 'block' | 'pill' | 'rows';
  width?: number | string;
  height?: number | string;
  /** Row count for the rows variant. */
  rows?: number;
  /** When given, wraps the skeleton in an aria-busy container announcing "Loading <label>". */
  label?: string;
  className?: string;
}

export function Skeleton({ variant = 'text', width, height, rows = 5, label, className }: SkeletonProps) {
  const style = { width, height };
  const shape = variant === 'circle' ? 'rounded-pill' : variant === 'pill' ? 'rounded-pill' : variant === 'block' ? 'rounded-md' : 'rounded-xs';
  const defaults = variant === 'text' ? 'h-4 w-full' : variant === 'circle' ? 'size-avatar-md' : variant === 'pill' ? 'h-6 w-20' : variant === 'block' ? 'h-32 w-full' : '';
  const body = variant === 'rows' ? (
    <div className="flex flex-col gap-0.5" style={style}>
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="flex h-table-row items-center gap-3 px-3">
          <span className="sl-skeleton size-avatar-sm rounded-pill" />
          <span className="sl-skeleton h-3 flex-1" />
          <span className="sl-skeleton h-3 w-16" />
          <span className="sl-skeleton h-3 w-12" />
        </div>
      ))}
    </div>
  ) : (
    <span aria-hidden="true" style={style} className={cn('sl-skeleton block', shape, defaults, className)} />
  );
  if (!label) return variant === 'rows' ? <div aria-hidden="true" className={className}>{body}</div> : body;
  return (
    <div aria-busy="true" className={cn(variant === 'rows' && className)}>
      <span className="sr-only">Loading {label}</span>
      <div aria-hidden="true">{body}</div>
    </div>
  );
}

/* ---------------------------------------------------------- EmptyState */

export interface EmptyStateProps {
  title: ReactNode;
  body?: ReactNode;
  icon?: LucideIcon;
  action?: ReactNode;
  secondaryAction?: ReactNode;
  variant?: 'inline' | 'panel' | 'table';
  /** Announce when the state appears after a user action (filter). */
  announce?: boolean;
  colSpan?: number;
  headingLevel?: 2 | 3 | 4;
  className?: string;
}

export function EmptyState({ title, body, icon: Icon = Inbox, action, secondaryAction, variant = 'panel', announce = false, colSpan, headingLevel = 3, className }: EmptyStateProps) {
  const Heading = `h${headingLevel}` as const;
  if (variant === 'inline') {
    return <p role={announce ? 'status' : undefined} className={cn('text-sm text-text-faint', className)}>{title}{body ? <> {body}</> : null}</p>;
  }
  const panel = (
    <div role={announce ? 'status' : undefined} className={cn('flex flex-col items-center justify-center gap-3 p-8 text-center', variant === 'table' && 'min-h-40', className)}>
      <Icon aria-hidden="true" className="size-8 text-text-faint" />
      <div className="flex flex-col gap-1">
        <Heading className="text-md font-bold text-text">{title}</Heading>
        {body ? <p className="max-w-sm text-sm text-text-muted">{body}</p> : null}
      </div>
      {action || secondaryAction ? <div className="flex flex-wrap items-center justify-center gap-2">{action}{secondaryAction}</div> : null}
    </div>
  );
  if (variant === 'table') {
    return (
      <tr>
        <td colSpan={colSpan ?? 99} className="p-0">{panel}</td>
      </tr>
    );
  }
  return panel;
}

/* ---------------------------------------------------- UnavailableState */

/** Final words that end in "s" but are singular nouns. */
const singularS = new Set(['news', 'status', 'analysis', 'basis', 'gas', 'series', 'canvas', 'bonus', 'address', 'access', 'progress', 'process', 'campus', 'chassis', 'thesis', 'axis', 'alias', 'atlas', 'plus', 'pass', 'class', 'loss', 'business', 'radius', 'consensus', 'census']);

/**
 * True when a section title reads as plural: "Callouts", "Top trades",
 * "Live stats", "Price and 1h change". The last word decides, except that a
 * compound joined by "and" is always plural.
 */
export function isPluralTitle(title: string): boolean {
  const words = title.trim().toLowerCase().replace(/[^a-z0-9\s-]/g, '').split(/\s+/).filter(Boolean);
  if (words.length === 0) return false;
  if (words.includes('and')) return true;
  const last = words[words.length - 1].split('-').pop() ?? '';
  if (last.length < 3 || !last.endsWith('s')) return false;
  if (singularS.has(last)) return false;
  if (/(?:ss|us|is)$/.test(last)) return false;
  return true;
}

/** "<title> is not available yet" or "<title> are not available yet". `plural: 'auto'` infers from the title. */
export function notAvailableTitle(title: string, plural: boolean | 'auto' = false): string {
  const many = plural === 'auto' ? isPluralTitle(title) : plural;
  return `${title} ${many ? 'are' : 'is'} not available yet`;
}

export interface UnavailableStateProps {
  /** Contract operation id (IF_*) or path the request targeted. */
  operation: string;
  /** HTTP status observed; null when the request never produced a response. */
  status: number | null;
  /** Body copy. Machine text (codes, statuses, "Failed to fetch") is reworded before it is shown. */
  message?: string;
  onRetry?: () => void;
  title?: string;
  /**
   * Verb agreement for the title. Omitted keeps the historical "is" wording;
   * `true` reads "<title> are not available yet"; `'auto'` infers it from the
   * title ("Top trades", "Callouts" -> are; "This coin" -> is).
   */
  plural?: boolean | 'auto';
  /** The thrown value behind the state, when there is one: used for plain body copy when `message` is absent. */
  error?: unknown;
  /**
   * `panel` (default) fills a frame with icon, heading, body and retry; `inline` is one line of
   * faint text; `chip` is a compact pill ("<title> is/are not available yet" plus an optional
   * "Try again") sized for a rail, card header or table toolbar, so the surrounding frame stays.
   */
  variant?: 'inline' | 'panel' | 'chip';
  /** Panel only: heading element, h3 by default. */
  headingLevel?: 2 | 3 | 4;
  /**
   * Surface the state sits on (1 default). The chip takes the next layer up; the panel's
   * retry button does too, so it reads on a surface-2 or surface-3 card without a stroke.
   */
  surface?: 1 | 2 | 3;
  /** Chip only: wrap the text to two lines instead of truncating (see StateNotice `wrap`). */
  wrap?: boolean;
  className?: string;
}

/** Retry button background one layer above the surface it sits on (Button secondary is surface-2). */
const retryOnSurface = { 1: undefined, 2: 'bg-surface-3', 3: 'bg-surface-hover' } as const;

export function UnavailableState({ operation, status, message, onRetry, title = 'This section', plural = false, error, variant = 'panel', headingLevel = 3, surface = 1, wrap = false, className }: UnavailableStateProps) {
  // The operation id and status are technical detail: they are never rendered.
  void operation; void status;
  const fallback = 'Check back soon';
  const body = message !== undefined || error === undefined ? humanErrorText(message, fallback) : humanError(error, fallback);
  const heading = notAvailableTitle(title, plural);
  const Heading = `h${headingLevel}` as const;
  if (variant === 'chip') {
    return <StateNotice kind="unavailable" onRetry={onRetry} surface={surface} wrap={wrap} className={className}>{heading}</StateNotice>;
  }
  if (variant === 'inline') {
    return (
      <p role="status" className={cn('text-sm text-text-faint', className)}>
        {heading}
      </p>
    );
  }
  return (
    <div role="status" className={cn('flex flex-col items-center justify-center gap-3 p-8 text-center', className)}>
      <Unplug aria-hidden="true" className="size-8 text-text-faint" />
      <div className="flex flex-col gap-1">
        <Heading className="text-md font-bold text-text">{heading}</Heading>
        <p className="max-w-sm text-sm text-text-muted">{body}</p>
      </div>
      {onRetry ? <Button variant="secondary" size="sm" onClick={onRetry} className={retryOnSurface[surface]}>Try again</Button> : null}
    </div>
  );
}

/* --------------------------------------------------------- StateNotice */

const errorFallback = 'Something went wrong, try again';

export type StateNoticeKind = 'unavailable' | 'error' | 'empty' | 'loading';

export interface StateNoticeProps {
  /** What the notice reports. Picks the icon, the live-region role and the default copy. */
  kind?: StateNoticeKind;
  /**
   * The short sentence. Strings (and runs of strings) are reworded for `error` and `unavailable`
   * so transport text ("Failed to fetch", HTTP 503, NETWORK_ERROR) never reaches the screen.
   * Omitted: built from `title` + `plural` for `unavailable`, from `error` for `error`, else a default.
   */
  children?: ReactNode;
  /** `unavailable` only, when `children` is absent: the section name for "<title> is/are not available yet". */
  title?: string;
  /** Verb agreement for `title`: false (default) "is", true "are", 'auto' inferred from the title. */
  plural?: boolean | 'auto';
  /** The thrown value behind an `error` notice: plain wording when `children` is absent. */
  error?: unknown;
  /** Shows a small "Try again" (unavailable) or "Retry" (error) text button inside the pill. */
  onRetry?: () => void;
  retrying?: boolean;
  /** Accepted for compatibility; technical detail is never rendered. */
  detail?: string | null;
  /** Surface the pill sits on; the pill takes the next layer up so it reads without a stroke. */
  surface?: 1 | 2 | 3;
  /** Let the text wrap to at most two lines (pill grows); the action stays on the row or drops below when narrow. */
  wrap?: boolean;
  className?: string;
}

const noticeKind: Record<StateNoticeKind, { Icon: LucideIcon; icon: string; fallback: string }> = {
  unavailable: { Icon: Unplug, icon: 'text-text-faint', fallback: 'Not available yet' },
  error: { Icon: CircleAlert, icon: 'text-danger', fallback: "Couldn't load" },
  empty: { Icon: Inbox, icon: 'text-text-faint', fallback: 'Nothing here yet' },
  loading: { Icon: LoaderCircle, icon: 'text-text-faint animate-spin motion-reduce:animate-none', fallback: 'Loading...' },
};

/** Joins children that are plain strings or numbers; null when any part is an element. */
function textOf(children: ReactNode): string | null {
  if (typeof children === 'string' || typeof children === 'number') return String(children);
  if (Array.isArray(children) && children.length > 0 && children.every(part => typeof part === 'string' || typeof part === 'number')) return children.join('');
  return null;
}

/**
 * Compact state pill for rails, card headers and toolbars: icon, one truncated line and an
 * optional text retry. Use it inside a frame that keeps its layout; use the panel states to
 * fill an empty frame.
 */
export function StateNotice({ kind = 'unavailable', children, title, plural = false, error, onRetry, retrying = false, surface = 1, wrap = false, className }: StateNoticeProps) {
  const meta = noticeKind[kind];
  const Icon = meta.Icon;
  const machine = kind === 'error' || kind === 'unavailable';
  const given = children === undefined || children === null || children === false ? null : children;
  const text = given === null ? null : textOf(given);
  let body: ReactNode;
  if (given === null) {
    if (kind === 'unavailable' && title) body = notAvailableTitle(title, plural);
    else if (kind === 'error' && error !== undefined) body = humanError(error, errorFallback);
    else body = meta.fallback;
  } else body = machine && text !== null ? humanErrorText(text, meta.fallback) : given;
  const plain = typeof body === 'string' ? body : undefined;
  const bg = surface === 1 ? 'bg-surface-2' : surface === 2 ? 'bg-surface-3' : 'bg-surface-hover';
  return (
    <span
      role={kind === 'error' ? 'alert' : 'status'}
      aria-busy={kind === 'loading' || retrying ? true : undefined}
      title={plain}
      data-state={kind}
      className={cn(
        'inline-flex min-w-0 max-w-full items-center gap-1.5 px-2 text-xs text-text-muted',
        wrap ? 'min-h-6 flex-wrap gap-y-0.5 rounded-md py-1' : 'h-6 rounded-pill',
        bg,
        className,
      )}
    >
      <Icon aria-hidden="true" className={cn('size-icon-sm shrink-0', meta.icon)} />
      <span className={wrap ? 'line-clamp-2 min-w-0 flex-1 basis-32' : 'min-w-0 truncate'}>{body}</span>
      {onRetry ? (
        <button
          type="button"
          onClick={onRetry}
          disabled={retrying}
          className="shrink-0 rounded-xs font-semibold text-text transition-colors duration-fast hover:text-text-muted disabled:cursor-wait disabled:text-text-faint"
        >
          Try again
        </button>
      ) : null}
    </span>
  );
}

/* ---------------------------------------------------------- ErrorState */

export interface ErrorStateProps {
  title?: ReactNode;
  message?: ReactNode;
  onRetry?: () => void;
  retrying?: boolean;
  /** Accepted for compatibility; reference ids are logged, never shown. */
  errorId?: string;
  /** The thrown value. When `message` is absent its plain wording is shown; its code and status are never rendered. */
  error?: unknown;
  /** `panel` (default), `inline` (one line of danger text) or `chip` (compact pill with an optional Retry, for rails and card headers). */
  variant?: 'inline' | 'panel' | 'chip';
  /** Chip only: wrap the text to two lines instead of truncating (see StateNotice `wrap`). */
  wrap?: boolean;
  /** Move focus to the heading when the panel replaces content. */
  autoFocus?: boolean;
  className?: string;
}


export function ErrorState({ title: rawTitle = 'Something went wrong', message: rawMessage, onRetry, retrying = false, error, variant = 'panel', wrap = false, autoFocus = false, className }: ErrorStateProps) {
  // Callers often pass `error.message`; transport codes ("Failed to fetch", HTTP 503, NETWORK_INTERRUPTED) are reworded here so none reaches the screen.
  const message = rawMessage === undefined || rawMessage === null
    ? (error === undefined ? errorFallback : humanError(error, errorFallback))
    : typeof rawMessage === 'string' ? humanErrorText(rawMessage, errorFallback) : rawMessage;
  const title = typeof rawTitle === 'string' ? humanErrorText(rawTitle, 'Something went wrong') : rawTitle;
  const headingRef = useRef<HTMLHeadingElement | null>(null);
  useEffect(() => {
    if (autoFocus) headingRef.current?.focus();
  }, [autoFocus]);
  if (variant === 'chip') {
    return <StateNotice kind="error" onRetry={onRetry} retrying={retrying} wrap={wrap} className={className}>{message}</StateNotice>;
  }
  if (variant === 'inline') {
    return (
      <p role="alert" className={cn('inline-flex items-center gap-1 text-xs text-danger', className)}>
        <CircleAlert aria-hidden="true" className="size-icon-sm shrink-0" />
        {message}
      </p>
    );
  }
  return (
    <div role="alert" className={cn('flex flex-col items-center justify-center gap-3 p-8 text-center', className)}>
      <TriangleAlert aria-hidden="true" className="size-8 text-danger" />
      <div className="flex flex-col gap-1">
        <h3 ref={headingRef} tabIndex={autoFocus ? -1 : undefined} className="text-md font-bold text-text outline-none">{title}</h3>
        <p className="max-w-sm text-sm text-text-muted">{message}</p>
      </div>
      {onRetry ? <Button variant="secondary" size="sm" onClick={onRetry} loading={retrying}>Try again</Button> : null}
    </div>
  );
}

/* ------------------------------------------------------------ LoadMore */

export interface LoadMoreProps {
  onLoadMore: () => void;
  hasMore: boolean;
  loading?: boolean;
  error?: boolean;
  /** Observe a sentinel and load automatically when it scrolls into view. */
  auto?: boolean;
  label?: string;
  endLabel?: string;
  className?: string;
}

export function LoadMore({ onLoadMore, hasMore, loading = false, error = false, auto = true, label = 'Load more', endLabel = "You're up to date", className }: LoadMoreProps) {
  const sentinelRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    if (!auto || !hasMore || loading || error) return;
    const node = sentinelRef.current;
    if (!node || typeof IntersectionObserver === 'undefined') return;
    const observer = new IntersectionObserver(entries => {
      if (entries.some(entry => entry.isIntersecting)) onLoadMore();
    }, { rootMargin: '200px' });
    observer.observe(node);
    return () => observer.disconnect();
  }, [auto, hasMore, loading, error, onLoadMore]);

  return (
    <div className={cn('flex flex-col items-center gap-2 py-3', className)}>
      <div ref={sentinelRef} aria-hidden="true" className="h-px w-full" />
      {hasMore ? (
        <Button variant="secondary" size="md" onClick={onLoadMore} loading={loading} disabled={loading}>
          {error ? 'Try again' : label}
        </Button>
      ) : (
        <p className="text-sm text-text-faint">{endLabel}</p>
      )}
      {error ? <p role="alert" className="text-xs text-danger">Couldn't load more, try again</p> : null}
    </div>
  );
}

/* ------------------------------------------------------- DisclosureRow */

export interface DisclosureRowProps {
  id: string;
  label: ReactNode;
  icon?: LucideIcon;
  optional?: boolean;
  /** Navigating variant: renders an <a> with chevron-right. */
  href?: string;
  open?: boolean;
  onToggle?: (open: boolean) => void;
  disabled?: boolean;
  trailing?: ReactNode;
  children?: ReactNode;
  className?: string;
}

export function DisclosureRow({ id, label, icon: Icon, optional = false, href, open = false, onToggle, disabled = false, trailing, children, className }: DisclosureRowProps) {
  const rowClass = cn(
    'flex h-12 w-full items-center gap-3 rounded-sm px-3 text-left text-sm font-bold text-text transition-colors duration-fast',
    disabled ? 'cursor-not-allowed text-text-disabled' : 'hover:bg-surface-hover',
  );
  const content = (
    <>
      {Icon ? <Icon aria-hidden="true" className="size-icon-md shrink-0 text-text-muted" /> : null}
      <span className="min-w-0 flex-1 truncate">
        {label}
        {optional ? <span className="ml-1 font-semibold text-text-faint">(Optional)</span> : null}
      </span>
      {trailing}
    </>
  );
  if (href) {
    return (
      <Link href={href} className={cn(rowClass, className)} aria-disabled={disabled || undefined}>
        {content}
        <ChevronRight aria-hidden="true" className="size-icon-md shrink-0 text-text-faint" />
      </Link>
    );
  }
  const panelId = `${id}-panel`;
  return (
    <div className={className}>
      <button type="button" id={id} aria-expanded={open} aria-controls={panelId} disabled={disabled} onClick={() => onToggle?.(!open)} className={rowClass}>
        {content}
        <ChevronDown aria-hidden="true" className={cn('size-icon-md shrink-0 text-text-faint transition-transform duration-fast', open && 'rotate-180')} />
      </button>
      <div id={panelId} role="region" aria-labelledby={id} hidden={!open} className="mt-1 rounded-sm bg-surface-2 p-3">
        {open ? children : null}
      </div>
    </div>
  );
}

/* ----------------------------------------------------------- NoticeBar */

export type NoticeTone = 'info' | 'warning' | 'danger' | 'success' | 'neutral';

export interface NoticeBarProps {
  tone?: NoticeTone;
  variant?: 'bar' | 'card';
  title?: ReactNode;
  children: ReactNode;
  icon?: LucideIcon;
  action?: ReactNode;
  onDismiss?: () => void;
  /** Announce system notices; promo content stays silent. */
  announce?: boolean;
  /** Surface the notice sits on. */
  surface?: 1 | 2;
  /** Bar variant only: wrap the message over several lines instead of truncating it (sentences that must be read whole). */
  wrap?: boolean;
  /** Edge-to-edge inside a frame: square corners and full width, so callers need no wrapper. */
  flush?: boolean;
  className?: string;
  /** Pass-through data attributes on the root (test hooks, analytics ids). */
  [dataAttribute: `data-${string}`]: string | number | boolean | undefined;
}

const noticeTone: Record<NoticeTone, { bg: string; icon: string; Icon: LucideIcon }> = {
  info: { bg: 'bg-info-soft', icon: 'text-info', Icon: Info },
  warning: { bg: 'bg-warning-soft', icon: 'text-warning', Icon: TriangleAlert },
  danger: { bg: 'bg-danger-soft', icon: 'text-danger', Icon: CircleAlert },
  success: { bg: 'bg-success-soft', icon: 'text-success', Icon: CircleCheck },
  neutral: { bg: '', icon: 'text-text-muted', Icon: Info },
};

export function NoticeBar({ tone = 'neutral', variant = 'bar', title, children, icon, action, onDismiss, announce = false, surface = 1, wrap = false, flush = false, className, ...rest }: NoticeBarProps) {
  const dataAttributes = Object.fromEntries(Object.entries(rest).filter(([key]) => key.startsWith('data-')));
  const meta = noticeTone[tone];
  const Icon = icon ?? meta.Icon;
  // Warning and danger notices often carry a caught error's text; machine text is reworded, sentences pass through.
  const text = typeof children === 'string' ? children : Array.isArray(children) && children.length > 0 && children.every(part => typeof part === 'string' || typeof part === 'number') ? children.join('') : null;
  const body = (tone === 'danger' || tone === 'warning') && text !== null ? humanErrorText(text) : children;
  const neutralBg = surface === 1 ? 'bg-surface-2' : 'bg-surface-3';
  return (
    <div
      {...dataAttributes}
      role={announce ? 'status' : undefined}
      className={cn(
        'flex items-start gap-3 rounded-md px-4 py-3',
        flush && 'w-full rounded-none',
        tone === 'neutral' ? neutralBg : meta.bg,
        variant === 'bar' && (wrap ? 'min-h-7 py-1.5' : 'min-h-7 items-center py-1.5'),
        className,
      )}
    >
      {variant === 'card' ? (
        <span className={cn('flex size-8 shrink-0 items-center justify-center rounded-pill', surface === 1 ? 'bg-surface-3' : 'bg-surface-hover', meta.icon)}>
          <Icon aria-hidden="true" className="size-icon-md" />
        </span>
      ) : (
        <Icon aria-hidden="true" className={cn('size-icon-md shrink-0', wrap && 'mt-0.5', meta.icon)} />
      )}
      <div className="min-w-0 flex-1">
        {title ? <p className="text-sm font-bold text-text">{title}</p> : null}
        <div className={cn(variant === 'card' ? 'text-xs text-text-muted' : wrap ? 'text-sm text-text-muted' : 'truncate text-sm text-text-muted')}>{body}</div>
      </div>
      {action ? <div className="shrink-0">{action}</div> : null}
      {onDismiss ? <IconButton label="Dismiss" icon={<X />} size="sm" onClick={onDismiss} className="-mr-2 shrink-0" /> : null}
    </div>
  );
}

/* ------------------------------------------------------- SectionHeader */

export interface SectionHeaderProps {
  title: ReactNode;
  count?: number;
  description?: ReactNode;
  action?: ReactNode;
  level?: 1 | 2 | 3;
  loading?: boolean;
  className?: string;
}

export function SectionHeader({ title, count, description, action, level = 2, loading = false, className }: SectionHeaderProps) {
  const Heading = `h${level}` as const;
  return (
    <div className={cn('flex items-start justify-between gap-3', className)}>
      <div className="min-w-0">
        <Heading className={cn('flex items-center gap-2 font-bold text-text', level === 1 ? 'text-xl' : level === 2 ? 'text-md' : 'text-base')}>
          <span className="truncate">{title}</span>
          {loading ? <LoaderCircle aria-hidden="true" className="size-icon-sm animate-spin text-text-faint" /> : count !== undefined ? <CountPill count={count} /> : null}
        </Heading>
        {description ? <p className="mt-0.5 text-xs text-text-faint">{description}</p> : null}
      </div>
      {action ? <div className="shrink-0">{action}</div> : null}
    </div>
  );
}
