'use client';
import { useEffect, useRef, useState } from 'react';
import { useAccountSession } from '@/widgets/profile/use-account-session';
import { LivestreamApiError, getPrivatePlayback, getPoolStreams, type StreamPublic } from '@/core/clients/livestream-api';
import { StreamPlayer } from '@/widgets/live/stream-player';
import { LiveBadge } from '@/widgets/live/live-badge';
import { ViewerCountChip } from '@/widgets/live/viewer-count-chip';

type DirectoryState = 'loading' | 'unavailable' | 'empty' | 'ready';
const poolPollMs = 3000;

/** Coin-page livestream: resolves the pool's current stream and hands it to the shared StreamPlayer. */
export default function LiveStream({ poolAddress }: { poolAddress: string }) {
  const identity = useAccountSession();
  const accessToken = identity.session?.accessToken;
  const sessionWallet = identity.session?.session.wallet;
  const [state, setState] = useState<DirectoryState>('loading');
  const [stream, setStream] = useState<StreamPublic | null>(null);
  const revision = useRef<{ id: string; revision: bigint } | null>(null);

  useEffect(() => {
    let disposed = false;
    let pending = false;
    revision.current = null;
    setStream(null);
    setState('loading');
    const refresh = async () => {
      if (pending) return;
      pending = true;
      try {
        const rows = await getPoolStreams(poolAddress, false);
        if (disposed) return;
        let current = rows.find(row => row.isLive) ?? rows[0];
        if (current?.status === 'suspended' && accessToken && sessionWallet?.toLowerCase() === current.creatorWallet.toLowerCase()) {
          try { current = await getPrivatePlayback(current.id, accessToken); }
          catch (failure) { if (!(failure instanceof LivestreamApiError) || failure.status !== 403) throw failure; }
        }
        if (disposed) return;
        if (!current) { setStream(null); setState('empty'); return; }
        const incoming = BigInt(current.revision ?? '0');
        if (revision.current?.id === current.id && incoming < revision.current.revision) return;
        revision.current = { id: current.id, revision: incoming };
        setStream(current);
        const shown = current.isLive || current.status === 'suspended' || current.status === 'ending' || current.status === 'ended';
        setState(shown ? 'ready' : 'empty');
      } catch {
        if (!disposed) { setStream(null); setState('unavailable'); }
      } finally {
        pending = false;
      }
    };
    void refresh();
    const timer = setInterval(() => void refresh(), poolPollMs);
    return () => { disposed = true; clearInterval(timer); };
  }, [poolAddress, accessToken, sessionWallet]);

  if (state === 'empty') return null;
  return (
    <section data-stream-id={stream?.id} data-stream-revision={stream?.revision} aria-label="Livestream" className="flex flex-col gap-3 rounded-lg bg-surface-1 p-3">
      {state !== 'ready' || !stream ? (
        <p role="status" className="text-sm text-text-muted">{state === 'unavailable' ? "Livestream can't load right now" : 'Loading livestream...'}</p>
      ) : (
        <>
          <div className="flex flex-wrap items-center gap-2">
            {stream.isLive ? <LiveBadge /> : null}
            <h3 className="min-w-0 flex-1 truncate text-sm font-bold text-text">{stream.title}</h3>
            {stream.isLive ? <ViewerCountChip count={stream.viewerCount} quality={stream.viewerQuality} /> : null}
          </div>
          <StreamPlayer stream={stream} accessToken={accessToken} poster={stream.thumbnailUrl} statusLine />
        </>
      )}
    </section>
  );
}
