import { LivestreamApiError, createViewerSession, heartbeatViewer, type ViewerSession } from '@/core/clients/livestream-api';

export interface HeartbeatOutcome {
  /** The session to keep beating with; undefined when the stream no longer admits viewer sessions. */
  session: ViewerSession | undefined;
  /** True when the viewer token was replaced, so playback must reload with the new access key. */
  renewed: boolean;
  /** Set when the runtime rate-limited the heartbeat: no heartbeat may be sent for this many milliseconds. */
  retryAfterMs?: number;
}

/**
 * True for the heartbeat answer to an invalid, expired or superseded viewer
 * token: 401 AUTH_EXPIRED from the contract envelope, or a code-less 401. The
 * heartbeat carries no account credential, so a 401 there is always the
 * viewer token.
 */
export function isViewerTokenExpired(error: unknown): boolean {
  return error instanceof LivestreamApiError && error.status === 401 && (error.code === 'AUTH_EXPIRED' || error.code === null);
}

/** True for 409 STATE_CONFLICT (or a code-less 409): the stream is no longer LIVE. */
export function isStreamNotLive(error: unknown): boolean {
  return error instanceof LivestreamApiError && error.status === 409 && (error.code === 'STATE_CONFLICT' || error.code === null);
}

/**
 * Milliseconds to hold off after a rate-limited heartbeat (429 RATE_LIMITED,
 * or a code-less 429), or null for any other failure. The envelope's
 * retryAfterMs (or the Retry-After header) decides; without either the
 * caller's regular interval applies.
 */
export function heartbeatBackoffMs(error: unknown, fallbackMs: number): number | null {
  if (!(error instanceof LivestreamApiError) || error.status !== 429) return null;
  if (error.code !== null && error.code !== 'RATE_LIMITED') return null;
  return error.retryAfterMs !== null && error.retryAfterMs > 0 ? error.retryAfterMs : fallbackMs;
}

/** Regular heartbeat cadence of the player; also the hold-off for a 429 that names no delay. */
export const viewerHeartbeatMs = 30000;

/**
 * One viewer heartbeat. A 401 AUTH_EXPIRED is about the anonymous viewer
 * token, not the person's sign-in, so it opens a new viewer session and beats
 * with that instead of surfacing sign-in copy. A 409 STATE_CONFLICT means the
 * stream is no longer LIVE: the session is dropped and the stream poll moves
 * the player to its ended state. A 429 RATE_LIMITED keeps the session (its
 * token is still valid) and reports the hold-off in `retryAfterMs`. Every
 * other failure is thrown to the caller.
 */
export async function heartbeatOrRenew(session: ViewerSession, accessToken?: string): Promise<HeartbeatOutcome> {
  try {
    await heartbeatViewer(session);
    return { session, renewed: false };
  } catch (error) {
    const backoff = heartbeatBackoffMs(error, viewerHeartbeatMs);
    if (backoff !== null) return { session, renewed: false, retryAfterMs: backoff };
    if (isViewerTokenExpired(error)) {
      const next = await createViewerSession(session.streamId, accessToken);
      try {
        await heartbeatViewer(next);
      } catch (renewedError) {
        const renewedBackoff = heartbeatBackoffMs(renewedError, viewerHeartbeatMs);
        if (renewedBackoff === null) throw renewedError;
        return { session: next, renewed: true, retryAfterMs: renewedBackoff };
      }
      return { session: next, renewed: true };
    }
    if (isStreamNotLive(error)) return { session: undefined, renewed: false };
    throw error;
  }
}

export interface HeartbeatTimers {
  set(run: () => void, ms: number): unknown;
  clear(handle: unknown): void;
}

const browserTimers: HeartbeatTimers = {
  set: (run, ms) => setTimeout(run, ms),
  clear: handle => clearTimeout(handle as ReturnType<typeof setTimeout>),
};

export interface HeartbeatLoop {
  /** Beat now, unless a beat is in flight or a rate-limit hold-off is still running. */
  kick(): void;
  stop(): void;
}

/**
 * Drives `beat` every `intervalMs`. When a beat reports a hold-off (the
 * `retryAfterMs` of a rate-limited heartbeat) the next beat is scheduled
 * exactly that far out and `kick` is ignored until it has passed, so no
 * heartbeat is sent inside the window the server asked for.
 */
export function startHeartbeatLoop(beat: () => Promise<number | undefined>, intervalMs: number = viewerHeartbeatMs, timers: HeartbeatTimers = browserTimers, now: () => number = Date.now): HeartbeatLoop {
  let handle: unknown;
  let stopped = false;
  let running = false;
  let notBefore = 0;
  const schedule = (ms: number) => {
    if (handle !== undefined) timers.clear(handle);
    handle = stopped ? undefined : timers.set(() => { handle = undefined; void tick(); }, ms);
  };
  const tick = async () => {
    if (stopped || running) return;
    if (now() < notBefore) { schedule(notBefore - now()); return; }
    running = true;
    let delay = intervalMs;
    try {
      const holdOff = await beat();
      if (typeof holdOff === 'number' && Number.isFinite(holdOff) && holdOff > 0) {
        delay = holdOff;
        notBefore = now() + holdOff;
      }
    } catch {
      // The caller's beat decides what a failure means for its session; the cadence continues.
    } finally {
      running = false;
    }
    schedule(delay);
  };
  void tick();
  return {
    kick: () => { if (!stopped && !running && now() >= notBefore) void tick(); },
    stop: () => { stopped = true; if (handle !== undefined) timers.clear(handle); handle = undefined; },
  };
}
