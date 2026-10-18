'use client';
import { StateNotice, UnavailableState, notAvailableTitle } from '@/ui/primitives';
import { technicalDetail } from '@/core/user-facing-error';

/** Subject of the directory failure; the shared primitive infers the plural ("are"). */
export const liveUnavailableSubject = 'Live streams';
export const liveUnavailableTitle = notAvailableTitle(liveUnavailableSubject, 'auto');

export interface LiveUnavailableProps {
  /** Contract operation id the directory request targeted. */
  operation: string;
  /** HTTP status observed; null when the request never produced a response. */
  status: number | null;
  message?: string;
  onRetry?: () => void;
  /**
   * `chip` (compact pill) inside a frame that keeps its layout: the hero frame
   * and each ranked row. `panel` fills an otherwise empty frame (the show-all grid).
   */
  variant?: 'chip' | 'panel';
  /** Surface layer the chip sits on; the pill takes the next layer up. */
  surface?: 1 | 2 | 3;
  className?: string;
}

/**
 * The live directory's unavailable message, rendered by the shared states
 * primitive so the copy and the transport-text guard live in one place. The
 * operation id and status stay developer detail: attributes outside
 * production, never body text.
 */
export function LiveUnavailable({ operation, status, onRetry, variant = 'chip', surface = 1, className }: LiveUnavailableProps) {
  if (variant === 'chip') {
    return (
      <StateNotice
        kind="unavailable"
        title={liveUnavailableSubject}
        plural="auto"
        detail={technicalDetail(operation, status)}
        onRetry={onRetry}
        surface={surface}
        className={className}
      />
    );
  }
  return (
    <UnavailableState
      title={liveUnavailableSubject}
      plural="auto"
      operation={operation}
      status={status}
      message="Something went wrong, try again"
      onRetry={onRetry}
      className={['p-4 sm:p-8 [&_button]:bg-surface-3', className].filter(Boolean).join(' ')}
    />
  );
}
