// ── Automatic Notification Triggers ──────────────────────────────────────────
// Client-side polling for tx received, inactivity nudge, and feature announcements.
// Runs inside the app (not the SW) — uses showLocalNotification from push.ts.

import {
  showLocalNotification,
  isPushSupported,
  getNotificationPermission,
  setWalletAddressForPush,
  subscribeToPush,
  heartbeat,
} from './push';
import { notificationStateRepository } from '@/platform/storage/repositories';
import { reportBackgroundFailure } from '@/platform/status/background-failures';

// Current feature announcement version — bump this to trigger a notification
const CURRENT_FEATURE_VERSION = 2;
const FEATURE_ANNOUNCEMENT = {
  title: '$SID Airdrop Starts Tomorrow!',
  body: 'The largest $SID airdrop on Paxeer Network begins tomorrow. Hold $SID in your wallet to qualify.',
};

const INACTIVITY_THRESHOLD_MS = 3 * 24 * 60 * 60 * 1000; // 3 days

let inactivityTimer: ReturnType<typeof setInterval> | null = null;

function canNotify(): boolean {
  return isPushSupported() && getNotificationPermission() === 'granted';
}

// ── Transaction received detection ──────────────────────────────────────────
// Polling is driven by useTxCountQuery (TanStack Query, 60 s). Call
// handleTxCount(currentCount) from useNotificationLifecycle whenever the
// query result updates; this function owns only the comparison + notification.

export async function handleTxCount(currentCount: number): Promise<void> {
  if (!canNotify()) return;

  const state = notificationStateRepository.read();
  const lastCount = state.lastTxCount ?? -1;

  // First run — just store the baseline, don't notify
  if (lastCount === -1) {
    notificationStateRepository.write({ ...state, lastTxCount: currentCount });
    return;
  }

  if (currentCount > lastCount) {
    const newTxs = currentCount - lastCount;
    await showLocalNotification('Transaction Received', {
      body: newTxs === 1
        ? 'You received a new transaction.'
        : `You received ${newTxs} new transactions.`,
      tag: 'tx-received',
      data: { url: '/' },
    } as NotificationOptions);
  }

  notificationStateRepository.write({ ...state, lastTxCount: currentCount });
}

// ── Inactivity nudge ────────────────────────────────────────────────────────

export function recordActivity(): void {
  notificationStateRepository.update((current) => ({
    ...current,
    lastActive: Date.now(),
  }));
}

async function checkInactivity(): Promise<void> {
  if (!canNotify()) return;

  const lastActive = notificationStateRepository.read().lastActive;
  if (lastActive === null) {
    recordActivity();
    return;
  }

  const elapsed = Date.now() - lastActive;

  if (elapsed >= INACTIVITY_THRESHOLD_MS) {
    await showLocalNotification('We miss you!', {
      body: 'Check your portfolio — your tokens may have moved.',
      tag: 'inactivity',
      data: { url: '/' },
    } as NotificationOptions);
    // Reset so we don't spam — next nudge after another 3 days
    recordActivity();
  }
}

// ── Feature announcements ───────────────────────────────────────────────────

async function checkFeatureAnnouncement(): Promise<void> {
  if (!canNotify()) return;

  const state = notificationStateRepository.read();
  const seen = state.seenFeatures;
  if (seen >= CURRENT_FEATURE_VERSION) return;

  await showLocalNotification(FEATURE_ANNOUNCEMENT.title, {
    body: FEATURE_ANNOUNCEMENT.body,
    tag: 'feature-announcement',
    data: { url: '/' },
  } as NotificationOptions);

  notificationStateRepository.write({
    ...state,
    seenFeatures: CURRENT_FEATURE_VERSION,
  });
}

// ── Lifecycle ───────────────────────────────────────────────────────────────

export async function startNotificationTriggers(walletAddress: string): Promise<void> {
  stopNotificationTriggers();

  // Associate wallet address with push subscription on the server
  setWalletAddressForPush(walletAddress);

  // Ensure the browser is subscribed to push (registers with our backend)
  try {
    await subscribeToPush();
  } catch {
    reportBackgroundFailure({
      domain: 'platform',
      kind: 'unavailable',
      code: 'PUSH_SUBSCRIBE_FAILED',
      message: 'Notifications could not be enabled.',
      retryable: true,
    });
  }

  // Send heartbeat to keep server-side lastActiveAt fresh
  heartbeat().catch(() => {});

  if (!canNotify()) return;

  // Record activity on start
  recordActivity();

  // Check feature announcements once on start (local fallback)
  checkFeatureAnnouncement();

  // New-tx detection is driven by useTxCountQuery in useNotificationLifecycle.
  // Heartbeat every 10 minutes to keep server-side subscription active
  inactivityTimer = setInterval(() => heartbeat().catch(() => {}), 10 * 60 * 1000);
}

export function stopNotificationTriggers(): void {
  if (inactivityTimer) { clearInterval(inactivityTimer); inactivityTimer = null; }
}
