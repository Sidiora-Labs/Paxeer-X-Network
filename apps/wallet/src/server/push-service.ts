// ── Push Notification Service ────────────────────────────────────────────────
// Server-side Web Push delivery via VAPID. Handles single, targeted, and
// broadcast push with retry, cleanup of expired subscriptions, and campaigns.

import webpush from 'web-push';
import {
  getAllSubscriptions,
  getSubscriptionsByWallet,
  getSubscriptionsByTag,
  getInactiveSubscriptions,
  removeSubscription,
  updateLastNotified,
  getPendingCampaigns,
  markCampaignSent,
  type StoredSubscription,
  type Campaign,
} from './push-store';

// ── VAPID Configuration ─────────────────────────────────────────────────────

const VAPID_PUBLIC_KEY = process.env.NEXT_PUBLIC_VAPID_PUBLIC_KEY || '';
const VAPID_PRIVATE_KEY = process.env.VAPID_PRIVATE_KEY || '';
const VAPID_SUBJECT = process.env.VAPID_SUBJECT || 'mailto:admin@paxeer.app';

let vapidConfigured = false;

function ensureVapid(): void {
  if (vapidConfigured) return;
  if (!VAPID_PUBLIC_KEY || !VAPID_PRIVATE_KEY) {
    throw new Error(
      'VAPID keys not configured. Set NEXT_PUBLIC_VAPID_PUBLIC_KEY and VAPID_PRIVATE_KEY env vars. ' +
      'Generate with: npx web-push generate-vapid-keys',
    );
  }
  webpush.setVapidDetails(VAPID_SUBJECT, VAPID_PUBLIC_KEY, VAPID_PRIVATE_KEY);
  vapidConfigured = true;
}

// ── Push Payload Builder ────────────────────────────────────────────────────

export interface PushPayload {
  title: string;
  body: string;
  icon?: string;
  badge?: string;
  image?: string;
  url?: string;
  tag?: string;
  actions?: { action: string; title: string }[];
  requireInteraction?: boolean;
}

function buildPayload(payload: PushPayload): string {
  return JSON.stringify({
    title: payload.title,
    body: payload.body,
    icon: payload.icon || '/wallet/icons/android/launchericon-192x192.png',
    badge: payload.badge || '/wallet/icons/android/launchericon-96x96.png',
    image: payload.image,
    url: payload.url || '/wallet/',
    tag: payload.tag || 'paxeer-push',
    actions: payload.actions || [],
    requireInteraction: payload.requireInteraction || false,
  });
}

// ── Send to a single subscription ───────────────────────────────────────────

export async function sendToSubscription(
  sub: StoredSubscription,
  payload: PushPayload,
): Promise<{ success: boolean; removed: boolean }> {
  ensureVapid();

  const pushSub = {
    endpoint: sub.endpoint,
    keys: sub.keys,
  };

  try {
    await webpush.sendNotification(pushSub, buildPayload(payload), {
      TTL: 86400, // 24 hours
      urgency: 'normal',
    });
    return { success: true, removed: false };
  } catch (err: any) {
    // 404 or 410 = subscription expired/invalid — remove it
    if (err.statusCode === 404 || err.statusCode === 410) {
      await removeSubscription(sub.endpoint);
      return { success: false, removed: true };
    }
    console.error('[Push] Delivery failed', {
      statusCode: typeof err?.statusCode === 'number' ? err.statusCode : undefined,
    });
    return { success: false, removed: false };
  }
}

// ── Send to multiple subscriptions ──────────────────────────────────────────

export interface SendResult {
  total: number;
  sent: number;
  failed: number;
  removed: number;
}

async function sendToMany(
  subs: StoredSubscription[],
  payload: PushPayload,
): Promise<SendResult> {
  const result: SendResult = { total: subs.length, sent: 0, failed: 0, removed: 0 };
  if (subs.length === 0) return result;

  // Send in batches of 50 to avoid overwhelming the push service
  const BATCH_SIZE = 50;
  const successEndpoints: string[] = [];

  for (let i = 0; i < subs.length; i += BATCH_SIZE) {
    const batch = subs.slice(i, i + BATCH_SIZE);
    const results = await Promise.allSettled(
      batch.map((sub) => sendToSubscription(sub, payload)),
    );

    for (let j = 0; j < results.length; j++) {
      const r = results[j];
      if (r.status === 'fulfilled') {
        if (r.value.success) {
          result.sent++;
          successEndpoints.push(batch[j].endpoint);
        } else if (r.value.removed) {
          result.removed++;
        } else {
          result.failed++;
        }
      } else {
        result.failed++;
      }
    }
  }

  // Update lastNotifiedAt for successful sends
  if (successEndpoints.length > 0) {
    await updateLastNotified(successEndpoints);
  }

  return result;
}

// ── Public API ──────────────────────────────────────────────────────────────

/** Send push to ALL subscribers. */
export async function sendToAll(payload: PushPayload): Promise<SendResult> {
  return sendToMany(await getAllSubscriptions(), payload);
}

/** Send push to a specific wallet address (all their devices). */
export async function sendToWallet(
  walletAddress: string,
  payload: PushPayload,
): Promise<SendResult> {
  return sendToMany(await getSubscriptionsByWallet(walletAddress), payload);
}

/** Send push to subscribers with a specific tag. */
export async function sendToTag(
  tag: string,
  payload: PushPayload,
): Promise<SendResult> {
  return sendToMany(await getSubscriptionsByTag(tag), payload);
}

/** Send push to specific wallet addresses. */
export async function sendToWallets(
  walletAddresses: string[],
  payload: PushPayload,
): Promise<SendResult> {
  const normalized = new Set(walletAddresses.map((a) => a.toLowerCase()));
  const subs = (await getAllSubscriptions()).filter((s) =>
    normalized.has(s.walletAddress.toLowerCase()),
  );
  return sendToMany(subs, payload);
}

// ── Transaction received notification ───────────────────────────────────────

export async function notifyTransactionReceived(
  walletAddress: string,
  txHash: string,
  fromAddress?: string,
  value?: string,
  symbol?: string,
): Promise<SendResult> {
  const amountStr = value && symbol ? `${value} ${symbol}` : 'a transaction';
  const fromStr = fromAddress
    ? ` from ${fromAddress.slice(0, 6)}...${fromAddress.slice(-4)}`
    : '';

  return sendToWallet(walletAddress, {
    title: 'Transaction Received',
    body: `You received ${amountStr}${fromStr}.`,
    tag: `tx-${txHash.slice(0, 16)}`,
    url: '/',
    actions: [{ action: 'view', title: 'View' }],
  });
}

// ── Inactivity nudge ────────────────────────────────────────────────────────

export async function sendInactivityNudges(
  thresholdMs: number = 3 * 24 * 60 * 60 * 1000, // 3 days
): Promise<SendResult> {
  const inactive = await getInactiveSubscriptions(thresholdMs);
  return sendToMany(inactive, {
    title: 'We miss you!',
    body: 'Your portfolio may have changed — check in to see how your tokens are doing.',
    tag: 'inactivity-nudge',
    url: '/',
  });
}

// ── Process scheduled campaigns ─────────────────────────────────────────────

export async function processPendingCampaigns(): Promise<{
  processed: number;
  results: { campaignId: string; result: SendResult }[];
}> {
  const pending = await getPendingCampaigns();
  const results: { campaignId: string; result: SendResult }[] = [];

  for (const campaign of pending) {
    let subs: StoredSubscription[];

    if (campaign.targetAddresses && campaign.targetAddresses.length > 0) {
      const normalized = new Set(campaign.targetAddresses.map((a) => a.toLowerCase()));
      subs = (await getAllSubscriptions()).filter((s) =>
        normalized.has(s.walletAddress.toLowerCase()),
      );
    } else if (campaign.targetTags && campaign.targetTags.length > 0) {
      const tagSets = await Promise.all(
        campaign.targetTags.map((t) => getSubscriptionsByTag(t)),
      );
      const seen = new Set<string>();
      subs = [];
      for (const set of tagSets) {
        for (const s of set) {
          if (!seen.has(s.endpoint)) {
            seen.add(s.endpoint);
            subs.push(s);
          }
        }
      }
    } else {
      subs = await getAllSubscriptions();
    }

    const payload: PushPayload = {
      title: campaign.title,
      body: campaign.body,
      url: campaign.url,
      icon: campaign.icon,
      image: campaign.image,
      tag: campaign.tag || `campaign-${campaign.id}`,
    };

    const result = await sendToMany(subs, payload);
    await markCampaignSent(campaign.id, result.sent, result.failed);
    results.push({ campaignId: campaign.id, result });
  }

  return { processed: results.length, results };
}

// ── Stats ───────────────────────────────────────────────────────────────────

export async function getPushStats() {
  const subs = await getAllSubscriptions();
  const now = Date.now();
  const day = 24 * 60 * 60 * 1000;

  return {
    totalSubscriptions: subs.length,
    activeLastDay: subs.filter((s) => now - s.lastActiveAt < day).length,
    activeLastWeek: subs.filter((s) => now - s.lastActiveAt < 7 * day).length,
    uniqueWallets: new Set(subs.map((s) => s.walletAddress.toLowerCase())).size,
  };
}
