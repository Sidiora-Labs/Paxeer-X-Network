import React from 'react';
import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import type { StreamOperation, StreamPublic } from '@/core/clients/livestream-api';
import { CreatorConsoleView, clockLabel } from './creator-console';
import { actionAvailability, credentialRemainingMs, isPoolCreator, isResumable, liveElapsedMs, mergeOperation, statusLabel, timelineSteps, type ConsoleStatus, type CreatorStream } from './use-creator-stream';

const BORDER = /\b(border(?!-0\b|-none\b)(-[a-z0-9/\[\]-]+)?|divide-[a-z]|ring-(?!offset)[a-z0-9]|outline-(?!none|focus|2|offset)[a-z]+)\b/;
const html = (node: React.ReactElement) => renderToStaticMarkup(node);
const pool = `0x${'a'.repeat(40)}`;
const wallet = `0x${'b'.repeat(40)}`;
const uuid = '11111111-1111-4111-8111-111111111111';
const other = '22222222-2222-4222-8222-222222222222';

function operation(status: ConsoleStatus, revision = '3'): StreamOperation {
  return { operationId: uuid, streamId: uuid, status, revision, retryAt: null };
}

function publicStream(partial: Partial<StreamPublic> = {}): StreamPublic {
  return { id: uuid, poolAddress: pool, creatorWallet: wallet, title: 'Launch night', playbackUrl: null, playbackId: 'pb', thumbnailUrl: null, isLive: false, viewerCount: 0, startedAt: null, endedAt: null, createdAt: 1_700_000_000, ...partial };
}

function consoleState(partial: Partial<CreatorStream> = {}): CreatorStream {
  const op = partial.operation ?? null;
  return {
    signedIn: true,
    authenticating: false,
    authError: null,
    authenticate: async () => {},
    walletReady: true,
    accessToken: 'token',
    operation: op,
    publicStream: null,
    liveSince: null,
    replacing: false,
    discovering: false,
    pending: null,
    error: null,
    credential: null,
    title: 'Launch night',
    setTitle: () => {},
    privatePlayback: false,
    setPrivatePlayback: () => {},
    availability: actionAvailability(op, false),
    timeline: timelineSteps(op?.status ?? null),
    run: async () => {},
    hideCredential: () => {},
    dismissError: () => {},
    ...partial,
  };
}

describe('creator stream state machine', () => {
  it('gates on a well formed, case-insensitive creator match', () => {
    expect(isPoolCreator(wallet, wallet.toUpperCase().replace('0X', '0x'))).toBe(true);
    expect(isPoolCreator(wallet, pool)).toBe(false);
    expect(isPoolCreator(wallet, null)).toBe(false);
    expect(isPoolCreator('0x123', '0x123')).toBe(false);
  });

  it('keeps the newest revision of the same operation and ignores foreign operations', () => {
    const held = operation('ready', '5');
    expect(mergeOperation(null, held)).toBe(held);
    expect(mergeOperation(held, operation('live', '4'))).toBe(held);
    expect(mergeOperation(held, operation('live', '6')).status).toBe('live');
    expect(mergeOperation(held, { ...operation('live', '9'), operationId: other, streamId: other })).toBe(held);
  });

  it('resumes only broadcasts the service has not closed', () => {
    expect(isResumable(publicStream())).toBe(true);
    expect(isResumable(publicStream({ endedAt: 1_700_000_500 }))).toBe(false);
    expect(isResumable(publicStream({ status: 'ended' }))).toBe(false);
    expect(isResumable(publicStream({ status: 'suspended' }))).toBe(true);
  });

  it('walks the timeline requested, provisioning, ready, live, ended', () => {
    expect(timelineSteps(null).steps.every(step => step.state === 'upcoming')).toBe(true);
    const ready = timelineSteps('ready');
    expect(ready.steps.map(step => step.state)).toEqual(['done', 'done', 'current', 'upcoming', 'upcoming']);
    expect(timelineSteps('ending').steps[3]).toMatchObject({ label: 'Ending', state: 'current' });
    expect(timelineSteps('ended').steps[4].state).toBe('current');
    expect(timelineSteps('failed').halted).toBe(true);
    expect(timelineSteps('suspended').halted).toBe(true);
  });

  it('enables Go live only at ready and End only while live', () => {
    for (const status of ['requested', 'provisioning', 'ready', 'live', 'ending', 'ended', 'failed', 'suspended'] as const) {
      const a = actionAvailability(operation(status), false);
      expect(a.start).toBe(status === 'ready');
      expect(a.end).toBe(status === 'live');
      expect(a.credential).toBe(status === 'ready' || status === 'live');
      expect(statusLabel(status).length).toBeGreaterThan(0);
    }
    expect(actionAvailability(operation('ready'), true).rotate).toBe(false);
    expect(actionAvailability(operation('suspended'), false).restore).toBe(true);
    expect(actionAvailability(null, false).start).toBe(false);
  });

  it('measures credential visibility and live elapsed time', () => {
    const now = Date.UTC(2026, 0, 1, 12, 0, 0);
    const credential = { streamKey: 'k', ingestEndpoint: 'rtmps://ingest.example/live', visibilityUntil: new Date(now + 90_000).toISOString() };
    expect(credentialRemainingMs(credential, now)).toBe(90_000);
    expect(credentialRemainingMs(credential, now + 100_000)).toBe(0);
    expect(credentialRemainingMs(null, now)).toBe(0);
    expect(liveElapsedMs(null, null, now)).toBeNull();
    expect(liveElapsedMs(null, now - 5_000, now)).toBe(5_000);
    expect(liveElapsedMs(publicStream({ startedAt: Math.floor(now / 1000) - 3700 }), now, now)).toBe(3_700_000);
    expect(clockLabel(3_700_000)).toBe('1:01:40');
    expect(clockLabel(65_000)).toBe('01:05');
  });
});

describe('CreatorConsoleView', () => {
  it('asks for a signed session before anything else', () => {
    const out = html(<CreatorConsoleView poolAddress={pool} stream={consoleState({ signedIn: false, authError: 'Connect your wallet on Paxeer chain125 first.' })} />);
    expect(out).toContain('Sign in with your wallet');
    expect(out).toContain('role="alert"');
    expect(out).not.toContain('Go live');
    expect(BORDER.test(out)).toBe(false);
  });

  it('shows the title form and no controls before a broadcast exists', () => {
    const out = html(<CreatorConsoleView poolAddress={pool} stream={consoleState({ title: '' })} />);
    expect(out).toContain('Create stream');
    expect(out).toContain('One live stream per coin');
    expect(out).not.toContain('Stream progress');
    expect(BORDER.test(out)).toBe(false);
  });

  it('enables Go live at ready and End while live', () => {
    const ready = html(<CreatorConsoleView poolAddress={pool} stream={consoleState({ operation: operation('ready') })} />);
    expect(ready).toContain('aria-current="step"');
    expect(ready).toMatch(/<button[^>]*type="button"[^>]*>(?:(?!<\/button>).)*Go live/);
    expect(ready).toMatch(/<button[^>]*disabled=""[^>]*>(?:(?!<\/button>).)*End stream/);
    expect(ready).not.toMatch(/<button[^>]*disabled=""[^>]*>(?:(?!<\/button>).)*Go live/);
    expect(ready).toContain('Point your encoder');
    const live = html(<CreatorConsoleView poolAddress={pool} stream={consoleState({ operation: operation('live'), liveSince: Date.now() - 61_000, publicStream: publicStream({ isLive: true, viewerCount: 12 }) })} />);
    expect(live).toMatch(/<button[^>]*disabled=""[^>]*>(?:(?!<\/button>).)*Go live/);
    expect(live).not.toMatch(/<button[^>]*disabled=""[^>]*>(?:(?!<\/button>).)*End stream/);
    expect(live).toContain('You are live');
    expect(live).toContain('Viewers');
    expect(live).toContain('>12<');
    expect(BORDER.test(ready) || BORDER.test(live)).toBe(false);
  });

  it('renders the credential panel masked with copy controls and the hide countdown', () => {
    const credential = { streamKey: 'abcDEF123456', ingestEndpoint: 'rtmps://ingest.example/live', visibilityUntil: new Date(Date.now() + 120_000).toISOString() };
    const out = html(<CreatorConsoleView poolAddress={pool} stream={consoleState({ operation: operation('ready'), credential })} />);
    expect(out).toContain('rtmps://ingest.example/live');
    expect(out).not.toContain('abcDEF123456');
    expect(out).toContain('aria-label="Copy server"');
    expect(out).toContain('aria-label="Copy stream key"');
    expect(out).toContain('aria-label="Show key"');
    expect(out).toContain('Hides in');
    expect(out).toContain('Hide stream key');
    expect(BORDER.test(out)).toBe(false);
  });

  it('surfaces server rejections as dismissible messages', () => {
    const out = html(<CreatorConsoleView poolAddress={pool} stream={consoleState({ operation: operation('ready'), error: 'This stream action is not permitted.' })} />);
    expect(out).toContain('This stream action is not permitted.');
    expect(out).toContain('aria-label="Dismiss"');
    const rotating = html(<CreatorConsoleView poolAddress={pool} stream={consoleState({ operation: operation('ready'), replacing: true, availability: actionAvailability(operation('ready'), true) })} />);
    expect(rotating).toContain('Resetting your stream key');
    expect(rotating).toMatch(/<button[^>]*disabled=""[^>]*>(?:(?!<\/button>).)*Reset stream key/);
  });
});
