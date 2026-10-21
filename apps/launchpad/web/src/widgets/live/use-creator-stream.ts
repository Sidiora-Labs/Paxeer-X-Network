'use client';
import { useCallback, useEffect, useRef, useState } from 'react';
import { useAccount, useSignMessage } from 'wagmi';
import { useAccountSession } from '@/widgets/profile/use-account-session';
import { assertAccount } from '@/widgets/profile/account-transport';
import { OPERATION_ENVIRONMENT } from '@/core/operation-envelope';
import { humanError } from '@/ui/primitives';
import {
  controlStream,
  editStream,
  getKeyRotation,
  getPoolStreams,
  getPrivateIngestKey,
  getStream,
  getStreamOperation,
  requestStream,
  type PrivateIngestKey,
  type StreamOperation,
  type StreamPublic,
  type StreamSigningContext,
} from '@/core/clients/livestream-api';

export type ConsoleStatus = StreamOperation['status'];
export type ConsoleAction = 'create' | 'edit' | 'start' | 'end' | 'credential' | 'rotate' | 'restore';
export type TimelineStepId = 'requested' | 'provisioning' | 'ready' | 'live' | 'ended';
export type TimelineStepState = 'done' | 'current' | 'upcoming';
export interface TimelineStep { id: TimelineStepId; label: string; state: TimelineStepState }
export interface Timeline { steps: TimelineStep[]; halted: boolean }
export interface ActionAvailability { edit: boolean; start: boolean; end: boolean; credential: boolean; rotate: boolean; restore: boolean }

export const PAXEER_CHAIN_ID = 125;
export const TITLE_MAX = 120;
const OPERATION_POLL_MS = 3000;
const ROTATION_POLL_MS = 3000;
const LIVE_POLL_MS = 10000;
const evmAddress = /^0x[0-9a-fA-F]{40}$/;
const stepOrder: readonly TimelineStepId[] = ['requested', 'provisioning', 'ready', 'live', 'ended'];
const stepLabel: Record<TimelineStepId, string> = { requested: 'Requested', provisioning: 'Setting up', ready: 'Ready', live: 'Live', ended: 'Ended' };
const stepPosition: Record<ConsoleStatus, number> = { requested: 0, provisioning: 1, ready: 2, live: 3, ending: 3, ended: 4, failed: -1, suspended: -1 };

/** Ownership gate: both addresses must be well formed and equal, case-insensitive. */
export function isPoolCreator(wallet: string | null | undefined, creator: string | null | undefined): boolean {
  if (!wallet || !creator || !evmAddress.test(wallet) || !evmAddress.test(creator)) return false;
  return wallet.toLowerCase() === creator.toLowerCase();
}

/** A poll result only replaces the held operation when it is the same operation at an equal or newer revision. */
export function mergeOperation(current: StreamOperation | null, next: StreamOperation): StreamOperation {
  if (!current) return next;
  if (current.operationId !== next.operationId) return current;
  return BigInt(next.revision) >= BigInt(current.revision) ? next : current;
}

/** A public projection the creator can pick up again after a reload: anything the service has not closed. */
export function isResumable(stream: StreamPublic): boolean {
  if (stream.status !== undefined) return !['ended', 'failed'].includes(stream.status);
  return stream.endedAt === null;
}

export function timelineSteps(status: ConsoleStatus | null): Timeline {
  const position = status === null ? -1 : stepPosition[status];
  const halted = status === 'failed' || status === 'suspended';
  return {
    halted,
    steps: stepOrder.map((id, index) => ({
      id,
      label: id === 'live' && status === 'ending' ? 'Ending' : stepLabel[id],
      state: position < 0 ? 'upcoming' : index < position ? 'done' : index === position ? 'current' : 'upcoming',
    })),
  };
}

export function statusLabel(status: ConsoleStatus): string {
  switch (status) {
    case 'requested': return 'Stream created';
    case 'provisioning': return 'Setting up your stream...';
    case 'ready': return 'Ready. Point your encoder at your stream key, then go live';
    case 'live': return 'You are live';
    case 'ending': return 'Ending your stream...';
    case 'ended': return 'Stream ended';
    case 'failed': return "Couldn't set up this stream, try again";
    case 'suspended': return 'This stream is paused';
  }
}

export function actionAvailability(operation: StreamOperation | null, replacing: boolean): ActionAvailability {
  if (!operation) return { edit: false, start: false, end: false, credential: false, rotate: false, restore: false };
  const status = operation.status;
  const settled = status === 'ready' || status === 'live';
  return {
    edit: status !== 'ended' && status !== 'failed',
    start: status === 'ready',
    end: status === 'live',
    credential: settled,
    rotate: !replacing && settled,
    restore: !replacing && (status === 'suspended' || status === 'ended'),
  };
}

export function credentialRemainingMs(credential: PrivateIngestKey | null, now = Date.now()): number {
  if (!credential) return 0;
  return Math.max(0, Date.parse(credential.visibilityUntil) - now);
}

/** Milliseconds the broadcast has been live, from the service start time when the projection carries one. */
export function liveElapsedMs(stream: StreamPublic | null, liveSince: number | null, now = Date.now()): number | null {
  const started = stream?.startedAt !== null && stream?.startedAt !== undefined ? stream.startedAt * 1000 : liveSince;
  if (started === null) return null;
  return Math.max(0, now - started);
}

export interface CreatorStream {
  signedIn: boolean;
  authenticating: boolean;
  authError: string | null;
  authenticate: () => Promise<void>;
  walletReady: boolean;
  accessToken: string | null;
  operation: StreamOperation | null;
  publicStream: StreamPublic | null;
  liveSince: number | null;
  replacing: boolean;
  discovering: boolean;
  pending: ConsoleAction | null;
  error: string | null;
  credential: PrivateIngestKey | null;
  title: string;
  setTitle: (value: string) => void;
  privatePlayback: boolean;
  setPrivatePlayback: (value: boolean) => void;
  availability: ActionAvailability;
  timeline: Timeline;
  run: (action: ConsoleAction) => Promise<void>;
  hideCredential: () => void;
  dismissError: () => void;
}

export function useCreatorStream(poolAddress: string): CreatorStream {
  const identity = useAccountSession();
  const wallet = useAccount();
  const signer = useSignMessage();
  const [title, setTitleState] = useState('');
  const [privatePlayback, setPrivatePlayback] = useState(false);
  const [operation, setOperation] = useState<StreamOperation | null>(null);
  const [publicStream, setPublicStream] = useState<StreamPublic | null>(null);
  const [liveSince, setLiveSince] = useState<number | null>(null);
  const [replacement, setReplacement] = useState<string | null>(null);
  const [pending, setPending] = useState<ConsoleAction | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [credential, setCredential] = useState<PrivateIngestKey | null>(null);
  const [discovering, setDiscovering] = useState(false);
  const requestKey = useRef<string | null>(null);
  const epoch = useRef(0);
  const operationRef = useRef<StreamOperation | null>(null);
  operationRef.current = operation;

  const session = identity.session;
  const sessionId = session?.session.sessionId;
  const walletReady = !!session && wallet.chainId === PAXEER_CHAIN_ID && !!wallet.address && wallet.address.toLowerCase() === session.session.wallet.toLowerCase();
  const status = operation?.status ?? null;
  const streamId = operation?.streamId ?? null;
  const operationId = operation?.operationId ?? null;

  useEffect(() => {
    epoch.current++;
    setReplacement(null);
    setOperation(null);
    setPublicStream(null);
    setLiveSince(null);
    setCredential(null);
    requestKey.current = null;
    setError(null);
    setPending(null);
  }, [sessionId, wallet.address, wallet.chainId, poolAddress]);

  useEffect(() => { setCredential(null); }, [operation?.revision]);

  useEffect(() => {
    if (status === 'live') setLiveSince(value => value ?? Date.now());
    else if (status !== 'ending') setLiveSince(null);
  }, [status]);

  useEffect(() => {
    if (!credential) return;
    const timer = setTimeout(() => setCredential(null), credentialRemainingMs(credential));
    return () => clearTimeout(timer);
  }, [credential]);

  useEffect(() => {
    if (!session || !evmAddress.test(poolAddress) || operation) return;
    const current = session;
    let canceled = false;
    setDiscovering(true);
    const discover = async () => {
      try {
        assertAccount(current);
        const streams = await getPoolStreams(poolAddress);
        if (canceled) return;
        const mine = streams.find(stream => stream.creatorWallet.toLowerCase() === current.session.wallet.toLowerCase() && isResumable(stream));
        if (!mine) return;
        const next = await getStreamOperation(mine.id, current.accessToken);
        assertAccount(current);
        if (canceled) return;
        setOperation(held => held ?? next);
        setTitleState(value => value || mine.title);
      } catch (failure) {
        if (!canceled) setError(humanError(failure, "Couldn't check for your stream, try again"));
      } finally {
        if (!canceled) setDiscovering(false);
      }
    };
    void discover();
    return () => { canceled = true; };
  }, [session, poolAddress, operation]);

  useEffect(() => {
    if (!operationId || !session || status === 'ended' || status === 'failed') return;
    let canceled = false;
    let inFlight = false;
    const current = session;
    const id = operationId;
    const refresh = async () => {
      if (inFlight) return;
      inFlight = true;
      try {
        assertAccount(current);
        const next = await getStreamOperation(id, current.accessToken);
        assertAccount(current);
        if (!canceled) setOperation(old => mergeOperation(old, next));
      } catch (failure) {
        if (!canceled) setError(humanError(failure, "Couldn't load your stream status, try again"));
      } finally {
        inFlight = false;
      }
    };
    const timer = setInterval(() => void refresh(), OPERATION_POLL_MS);
    return () => { canceled = true; clearInterval(timer); };
  }, [operationId, status, session]);

  useEffect(() => {
    if (!streamId || (status !== 'live' && status !== 'ending')) {
      setPublicStream(null);
      return;
    }
    let canceled = false;
    let inFlight = false;
    const refresh = async () => {
      if (inFlight) return;
      inFlight = true;
      try {
        const next = await getStream(streamId);
        if (!canceled) setPublicStream(next);
      } catch (failure) {
        if (!canceled) setError(humanError(failure, "Couldn't load viewers, try again"));
      } finally {
        inFlight = false;
      }
    };
    void refresh();
    const timer = setInterval(() => void refresh(), LIVE_POLL_MS);
    return () => { canceled = true; clearInterval(timer); };
  }, [streamId, status]);

  useEffect(() => {
    if (!replacement || !session) return;
    let canceled = false;
    let inFlight = false;
    const current = session;
    const refresh = async () => {
      if (inFlight) return;
      inFlight = true;
      try {
        assertAccount(current);
        const rotation = await getKeyRotation(replacement, current.accessToken);
        assertAccount(current);
        if (rotation.newStreamId && rotation.status === 'ready') {
          const next = await getStreamOperation(rotation.newStreamId, current.accessToken);
          assertAccount(current);
          if (!canceled) {
            setOperation(next);
            setReplacement(null);
            setCredential(null);
          }
        }
      } catch (failure) {
        if (!canceled) setError(humanError(failure, "Couldn't reset your stream key, try again"));
      } finally {
        inFlight = false;
      }
    };
    void refresh();
    const timer = setInterval(() => void refresh(), ROTATION_POLL_MS);
    return () => { canceled = true; clearInterval(timer); };
  }, [replacement, session]);

  const setTitle = useCallback((value: string) => {
    setTitleState(value.slice(0, TITLE_MAX));
    requestKey.current = null;
  }, []);

  const run = useCallback(async (action: ConsoleAction) => {
    if (pending || !session) return;
    const current = session;
    const captured = epoch.current;
    setPending(action);
    setError(null);
    setCredential(null);
    try {
      assertAccount(current);
      if (wallet.chainId !== PAXEER_CHAIN_ID || wallet.address?.toLowerCase() !== current.session.wallet.toLowerCase()) throw new Error('Switch to your signed-in wallet on Paxeer');
      const context: StreamSigningContext = {
        wallet: current.session.wallet,
        environment: OPERATION_ENVIRONMENT,
        signMessage: message => signer.signMessageAsync({ message }),
        assertCurrent: () => {
          assertAccount(current);
          if (epoch.current !== captured) throw new Error('Your wallet changed, try again');
        },
      };
      const held = operationRef.current;
      if (action === 'credential') {
        if (!held) throw new Error('Create a stream first');
        const value = await getPrivateIngestKey(held.streamId, current.accessToken, context);
        context.assertCurrent();
        setCredential(value);
        return;
      }
      let next: StreamOperation;
      if (action === 'create') {
        requestKey.current ??= crypto.randomUUID();
        next = await requestStream(poolAddress, title.trim(), requestKey.current, current.accessToken, context);
      } else {
        if (!held) throw new Error('Create a stream first');
        next = action === 'edit'
          ? await editStream(held.streamId, held.revision, title.trim(), current.accessToken, context, privatePlayback)
          : await controlStream(held.streamId, held.revision, action, current.accessToken, context);
      }
      context.assertCurrent();
      setOperation(next);
      if (action === 'rotate' || action === 'restore') setReplacement(next.streamId);
    } catch (failure) {
      if (epoch.current === captured) setError(humanError(failure, 'Something went wrong, try again'));
    } finally {
      if (epoch.current === captured) setPending(null);
    }
  }, [pending, session, wallet.chainId, wallet.address, signer, poolAddress, title, privatePlayback]);

  return {
    signedIn: !!session,
    authenticating: identity.authenticating,
    authError: identity.error,
    authenticate: identity.authenticate,
    walletReady,
    accessToken: session?.accessToken ?? null,
    operation,
    publicStream,
    liveSince,
    replacing: replacement !== null,
    discovering,
    pending,
    error,
    credential,
    title,
    setTitle,
    privatePlayback,
    setPrivatePlayback,
    availability: actionAvailability(operation, replacement !== null),
    timeline: timelineSteps(operation?.status ?? null),
    run,
    hideCredential: () => setCredential(null),
    dismissError: () => setError(null),
  };
}
