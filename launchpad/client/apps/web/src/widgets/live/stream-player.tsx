'use client';
import { useEffect, useRef, useState, type ReactNode } from 'react';
import { Play, Volume2, VolumeX } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Button, IconButton } from '@/ui/primitives';
import { approvedPlaybackURL, createViewerSession, getStream, type StreamPublic, type ViewerSession } from '@/core/clients/livestream-api';
import { heartbeatBackoffMs, heartbeatOrRenew, startHeartbeatLoop, viewerHeartbeatMs, type HeartbeatLoop } from './viewer-heartbeat';

export type StreamPlayerState = 'loading' | 'connecting' | 'live' | 'buffering' | 'ended' | 'blocked' | 'failed' | 'unavailable';
type PlaybackPhase = 'connecting' | 'live' | 'buffering' | 'ended' | 'failed' | 'unavailable';

export interface StreamPlayerBaseProps {
  /** Start muted (default true) so autoplay is permitted; the viewer unmutes on the media. */
  muted?: boolean;
  /** Start playback as soon as the manifest is ready (default true). When the browser refuses, a poster with a play button appears. */
  autoplay?: boolean;
  /** Poster shown before the first frame and behind every non-playing state (the stream thumbnail when the service has one). */
  poster?: string | null;
  accessToken?: string;
  /** Native media controls (default true). */
  controls?: boolean;
  /** Show the connecting/live/reconnecting line on the media; terminal states are always written over the media. */
  statusLine?: boolean;
  onStateChange?: (state: StreamPlayerState) => void;
  /** Rendered inside the ended/blocked/failed/unavailable overlay (for example a link back to the coin). */
  children?: ReactNode;
  className?: string;
}

export type StreamPlayerProps = StreamPlayerBaseProps & (
  | { stream: StreamPublic; streamId?: undefined }
  | { streamId: string; stream?: undefined }
);

const terminal: ReadonlySet<StreamPlayerState> = new Set(['ended', 'blocked', 'failed', 'unavailable']);
const streamPollMs = 5000;
const heartbeatMs = viewerHeartbeatMs;
const maxRetries = 4;

/** Map the service projection onto the player lifecycle; `playable` means HLS playback may attach. */
export function lifecycleState(stream: StreamPublic | null, unavailable: boolean): StreamPlayerState | 'playable' {
  if (unavailable) return 'unavailable';
  if (!stream) return 'loading';
  if (stream.status === 'suspended') return 'blocked';
  if (stream.status === 'ending' || stream.status === 'ended') return 'ended';
  if (stream.status === 'failed') return 'failed';
  if (stream.isLive && stream.playbackUrl) return 'playable';
  if (stream.status === undefined && stream.endedAt !== null) return 'ended';
  return 'connecting';
}

export function playerLabel(state: StreamPlayerState, stream: StreamPublic | null): string {
  switch (state) {
    case 'loading': return 'Loading stream';
    case 'connecting': return stream?.isLive ? 'Connecting...' : 'The stream is starting';
    case 'live': return 'Live';
    case 'buffering': return 'Reconnecting...';
    case 'ended': return stream?.status === 'ending' ? 'Stream is ending' : 'Stream ended';
    case 'blocked': return "This stream isn't available";
    case 'failed': return "Can't play this stream right now";
    case 'unavailable': return "Streams can't load right now";
  }
}

function usePolledStream(streamId: string | undefined): { stream: StreamPublic | null; unavailable: boolean } {
  const [stream, setStream] = useState<StreamPublic | null>(null);
  const [unavailable, setUnavailable] = useState(false);
  const revision = useRef<bigint | null>(null);
  useEffect(() => {
    revision.current = null;
    setStream(null);
    setUnavailable(false);
    if (!streamId) return;
    let disposed = false;
    let pending = false;
    const refresh = async () => {
      if (pending) return;
      pending = true;
      try {
        const next = await getStream(streamId);
        if (disposed) return;
        if (!next) { setUnavailable(true); return; }
        const incoming = BigInt(next.revision ?? '0');
        if (revision.current !== null && incoming < revision.current) return;
        revision.current = incoming;
        setStream(next);
        setUnavailable(false);
      } catch {
        if (!disposed) setUnavailable(true);
      } finally {
        pending = false;
      }
    };
    void refresh();
    const timer = setInterval(() => void refresh(), streamPollMs);
    return () => { disposed = true; clearInterval(timer); };
  }, [streamId]);
  return { stream, unavailable };
}

function approvedSegment(requestUrl: string): URL {
  const requested = new URL(requestUrl);
  if (requested.protocol !== 'https:' || requested.hostname !== 'livepeercdn.studio' || requested.port || requested.username || requested.password) throw new Error('Unapproved playback source');
  return requested;
}

/**
 * Shared HLS player: viewer session plus heartbeat, muted autoplay with an
 * on-media unmute, poster-with-play when autoplay is refused, jittered
 * reconnect backoff, and honest terminal states from the service projection.
 */
export function StreamPlayer(props: StreamPlayerProps) {
  const { muted = true, autoplay = true, poster = null, accessToken, controls = true, statusLine = false, onStateChange, children, className } = props;
  const polled = usePolledStream(props.stream ? undefined : props.streamId);
  const stream = props.stream ?? polled.stream;
  const lifecycle = lifecycleState(stream, props.stream === undefined && polled.unavailable);
  const playable = lifecycle === 'playable';
  const sourceId = stream?.id;
  const sourceUrl = stream?.playbackUrl;

  const video = useRef<HTMLVideoElement>(null);
  const [phase, setPhase] = useState<PlaybackPhase>('connecting');
  const [isMuted, setIsMuted] = useState(muted);
  const [needsGesture, setNeedsGesture] = useState(!autoplay);
  const [attempt, setAttempt] = useState(0);
  const state: StreamPlayerState = playable ? phase : lifecycle;

  const stateChange = useRef(onStateChange);
  useEffect(() => { stateChange.current = onStateChange; }, [onStateChange]);
  useEffect(() => { stateChange.current?.(state); }, [state]);

  useEffect(() => {
    const element = video.current;
    if (element) element.muted = isMuted;
  }, [isMuted, playable]);

  useEffect(() => {
    const element = video.current;
    if (!playable || !sourceId || !sourceUrl || !element) return;
    let disposed = false;
    let destroy: (() => void) | undefined;
    let reload: (() => void) | undefined;
    let retryTimer: ReturnType<typeof setTimeout> | undefined;
    let heartbeat: HeartbeatLoop | undefined;
    let retries = 0;
    let session: ViewerSession | undefined;
    setPhase('connecting');
    setNeedsGesture(!autoplay);

    let url: URL;
    try { url = approvedPlaybackURL(sourceUrl); } catch { setPhase('failed'); return; }
    const native = element.canPlayType('application/vnd.apple.mpegurl') !== '';

    const tryPlay = () => {
      if (disposed || !autoplay) return;
      element.play()
        .then(() => { if (!disposed) setNeedsGesture(false); })
        .catch((error: unknown) => { if (!disposed && error instanceof DOMException && error.name === 'NotAllowedError') setNeedsGesture(true); });
    };
    const retry = () => {
      if (disposed) return;
      if (retries >= maxRetries) {
        clearTimeout(retryTimer);
        destroy?.();
        destroy = undefined;
        element.pause();
        element.removeAttribute('src');
        element.load();
        setPhase('failed');
        return;
      }
      setPhase('buffering');
      const delay = Math.min(15000, 1000 * 2 ** retries++) * (0.75 + Math.random() * 0.5);
      clearTimeout(retryTimer);
      retryTimer = setTimeout(() => { if (!disposed) reload?.(); }, delay);
    };
    const nativeError = () => retry();
    const nativeReady = () => tryPlay();

    const start = async () => {
      try {
        session = await createViewerSession(sourceId, accessToken);
        if (disposed) return;
        url.searchParams.set('accessKey', session.token);
        if (native) {
          reload = () => {
            if (!session) return;
            url.searchParams.set('accessKey', session.token);
            element.src = url.href;
            element.load();
          };
          element.addEventListener('error', nativeError);
          element.addEventListener('loadedmetadata', nativeReady);
          reload();
          return;
        }
        const { default: Hls } = await import('hls.js');
        if (disposed) return;
        if (!Hls.isSupported()) { setPhase('failed'); return; }
        const hls = new Hls({
          enableWorker: true,
          manifestLoadingMaxRetry: 0,
          fragLoadingMaxRetry: 0,
          xhrSetup: (xhr, requestUrl) => {
            let requested: URL;
            try { requested = approvedSegment(requestUrl); } catch (error) { xhr.abort(); throw error; }
            if (session && requested.pathname.endsWith('.m3u8')) xhr.setRequestHeader('Livepeer-Access-Key', session.token);
          },
        });
        destroy = () => hls.destroy();
        reload = () => {
          if (!session) return;
          url.searchParams.set('accessKey', session.token);
          hls.stopLoad();
          hls.loadSource(url.href);
          hls.startLoad();
        };
        hls.on(Hls.Events.ERROR, (_, data) => { if (!disposed && data.fatal) retry(); });
        hls.on(Hls.Events.MANIFEST_PARSED, () => tryPlay());
        hls.loadSource(url.href);
        hls.attachMedia(element);
      } catch {
        if (!disposed) setPhase('unavailable');
      }
    };
    void start();

    // Resolves to a hold-off in milliseconds when the runtime rate-limited the
    // heartbeat (or the viewer-session request); the loop sends nothing until it passes.
    const beat = async (): Promise<number | undefined> => {
      if (disposed || element.paused || element.ended) return undefined;
      try {
        if (!session || Date.parse(session.expiresAt) <= Date.now() + heartbeatMs) {
          session = await createViewerSession(sourceId, accessToken);
          if (!disposed && native) reload?.();
        }
        if (disposed) return undefined;
        const outcome = await heartbeatOrRenew(session, accessToken);
        if (disposed) return undefined;
        session = outcome.session;
        if (outcome.renewed && native) reload?.();
        return outcome.retryAfterMs;
      } catch (error) {
        const backoff = heartbeatBackoffMs(error, heartbeatMs);
        if (backoff !== null) return backoff;
        session = undefined;
        return undefined;
      }
    };
    const startHeartbeat = () => { if (heartbeat) heartbeat.kick(); else heartbeat = startHeartbeatLoop(beat, heartbeatMs); };
    element.addEventListener('playing', startHeartbeat);

    return () => {
      disposed = true;
      clearTimeout(retryTimer);
      heartbeat?.stop();
      destroy?.();
      element.removeEventListener('error', nativeError);
      element.removeEventListener('loadedmetadata', nativeReady);
      element.removeEventListener('playing', startHeartbeat);
      element.pause();
      element.removeAttribute('src');
      element.load();
      session = undefined;
    };
  }, [playable, sourceId, sourceUrl, accessToken, autoplay, attempt]);

  const play = () => {
    const element = video.current;
    if (!element) return;
    element.play().then(() => setNeedsGesture(false)).catch(() => undefined);
  };
  const toggleMuted = () => {
    const element = video.current;
    if (!element) return;
    const next = !element.muted;
    element.muted = next;
    setIsMuted(next);
    if (!next && element.paused) play();
  };

  const label = playerLabel(state, stream);
  const stopped = terminal.has(state);
  const overlay = !playable || stopped || needsGesture;

  return (
    <div data-player-state={state} data-stream-id={stream?.id} className={cn('relative aspect-video w-full overflow-hidden rounded-lg bg-surface-2', className)}>
      {playable && stream ? (
        <video
          ref={video}
          className="size-full bg-canvas object-contain"
          aria-label={stream.title}
          playsInline
          controls={controls}
          poster={poster ?? undefined}
          muted={isMuted}
          autoPlay={autoplay}
          onPlaying={() => setPhase(current => (current === 'connecting' || current === 'buffering' || current === 'live' ? 'live' : current))}
          onWaiting={() => setPhase(current => (current === 'connecting' || current === 'live' ? 'buffering' : current))}
          onEnded={() => setPhase('ended')}
          onVolumeChange={event => setIsMuted(event.currentTarget.muted)}
        />
      ) : poster ? (
        <img src={poster} alt="" className="size-full object-cover" />
      ) : null}

      {overlay ? (
        <div className={cn('absolute inset-0 flex flex-col items-center justify-center gap-3 p-4 text-center', (poster || playable) && 'bg-media-overlay')}>
          {!stopped && playable ? (
            <IconButton label="Play stream" icon={<Play className="size-7" />} variant="on-media" size="lg" onClick={play} />
          ) : null}
          {stopped || !playable ? <p className="text-sm font-bold text-text">{label}</p> : null}
          {stopped && playable && state === 'failed' ? (
            <Button variant="secondary" size="sm" onClick={() => setAttempt(current => current + 1)}>Try again</Button>
          ) : null}
          {stopped ? children : null}
        </div>
      ) : null}

      {playable && !stopped && !needsGesture ? (
        isMuted ? (
          <IconButton label="Unmute" icon={<VolumeX className="size-7" />} variant="on-media" size="lg" className="absolute left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2" onClick={toggleMuted} />
        ) : (
          <IconButton label="Mute" icon={<Volume2 className="size-5" />} variant="on-media" size="sm" className="absolute right-3 top-3" onClick={toggleMuted} />
        )
      ) : null}

      <p
        role="status"
        className={cn(statusLine && playable && !stopped
          ? 'absolute bottom-3 left-3 rounded-pill bg-media-overlay px-2 py-0.5 text-xs text-text'
          : 'sr-only')}
      >
        {label}
      </p>
    </div>
  );
}
