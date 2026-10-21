import path from 'node:path';
import { AtomicJsonStore } from './atomic-json-store';

const DATA_DIR =
  process.env.PUSH_DATA_DIR ?? '/data/paxport/push';

export interface StoredSubscription {
  endpoint: string;
  keys: { p256dh: string; auth: string };
  walletAddress: string;
  userAgent?: string;
  createdAt: number;
  lastActiveAt: number;
  lastNotifiedAt?: number;
  tags?: string[];
}

export interface Campaign {
  id: string;
  title: string;
  body: string;
  url?: string;
  icon?: string;
  image?: string;
  tag?: string;
  targetTags?: string[];
  targetAddresses?: string[];
  scheduledAt?: number;
  sentAt?: number;
  sentCount?: number;
  failedCount?: number;
}

interface StoreEnvelope<T> {
  version: 1;
  updatedAt: number;
  records: T[];
}

function isRecord(input: unknown): input is Record<string, unknown> {
  return typeof input === 'object' && input !== null && !Array.isArray(input);
}

function boundedString(
  input: unknown,
  name: string,
  minimum: number,
  maximum: number,
): string {
  if (
    typeof input !== 'string' ||
    input.length < minimum ||
    input.length > maximum
  ) {
    throw new TypeError(`${name} is invalid`);
  }
  return input;
}

function parseStringArray(
  input: unknown,
  name: string,
  maximumItems: number,
  maximumLength: number,
): string[] | undefined {
  if (input === undefined) return undefined;
  if (!Array.isArray(input) || input.length > maximumItems) {
    throw new TypeError(`${name} is invalid`);
  }
  return input.map((value) => boundedString(value, name, 1, maximumLength));
}

function parseTimestamp(input: unknown, name: string): number {
  if (
    typeof input !== 'number' ||
    !Number.isSafeInteger(input) ||
    input < 0
  ) {
    throw new TypeError(`${name} is invalid`);
  }
  return input;
}

export function parseStoredSubscription(input: unknown): StoredSubscription {
  if (!isRecord(input) || !isRecord(input.keys)) {
    throw new TypeError('Subscription is invalid');
  }
  const endpoint = boundedString(input.endpoint, 'endpoint', 12, 2048);
  const parsedEndpoint = new URL(endpoint);
  if (parsedEndpoint.protocol !== 'https:') {
    throw new TypeError('endpoint is invalid');
  }
  const walletAddress = boundedString(
    input.walletAddress,
    'walletAddress',
    42,
    42,
  ).toLowerCase();
  if (!/^0x[0-9a-f]{40}$/.test(walletAddress)) {
    throw new TypeError('walletAddress is invalid');
  }
  return {
    endpoint,
    keys: {
      p256dh: boundedString(input.keys.p256dh, 'keys.p256dh', 16, 256),
      auth: boundedString(input.keys.auth, 'keys.auth', 8, 128),
    },
    walletAddress,
    userAgent:
      input.userAgent === undefined
        ? undefined
        : boundedString(input.userAgent, 'userAgent', 0, 512),
    createdAt: parseTimestamp(input.createdAt, 'createdAt'),
    lastActiveAt: parseTimestamp(input.lastActiveAt, 'lastActiveAt'),
    lastNotifiedAt:
      input.lastNotifiedAt === undefined
        ? undefined
        : parseTimestamp(input.lastNotifiedAt, 'lastNotifiedAt'),
    tags: parseStringArray(input.tags, 'tags', 32, 64),
  };
}

export function parseCampaign(input: unknown): Campaign {
  if (!isRecord(input)) throw new TypeError('Campaign is invalid');
  const optionalString = (
    value: unknown,
    name: string,
    maximum: number,
  ): string | undefined =>
    value === undefined ? undefined : boundedString(value, name, 1, maximum);
  return {
    id: boundedString(input.id, 'id', 1, 128),
    title: boundedString(input.title, 'title', 1, 120),
    body: boundedString(input.body, 'body', 1, 500),
    url: optionalString(input.url, 'url', 512),
    icon: optionalString(input.icon, 'icon', 512),
    image: optionalString(input.image, 'image', 512),
    tag: optionalString(input.tag, 'tag', 128),
    targetTags: parseStringArray(input.targetTags, 'targetTags', 32, 64),
    targetAddresses: parseStringArray(
      input.targetAddresses,
      'targetAddresses',
      500,
      42,
    ),
    scheduledAt:
      input.scheduledAt === undefined
        ? undefined
        : parseTimestamp(input.scheduledAt, 'scheduledAt'),
    sentAt:
      input.sentAt === undefined
        ? undefined
        : parseTimestamp(input.sentAt, 'sentAt'),
    sentCount:
      input.sentCount === undefined
        ? undefined
        : parseTimestamp(input.sentCount, 'sentCount'),
    failedCount:
      input.failedCount === undefined
        ? undefined
        : parseTimestamp(input.failedCount, 'failedCount'),
  };
}

function envelopeParser<T>(
  itemParser: (input: unknown) => T,
): (input: unknown) => StoreEnvelope<T> {
  return (input) => {
    if (
      !isRecord(input) ||
      input.version !== 1 ||
      !Array.isArray(input.records)
    ) {
      throw new TypeError('Store envelope is invalid');
    }
    return {
      version: 1,
      updatedAt: parseTimestamp(input.updatedAt, 'updatedAt'),
      records: input.records.map(itemParser),
    };
  };
}

function legacyMigration<T>(
  itemParser: (input: unknown) => T,
): (input: unknown) => StoreEnvelope<T> {
  return (input) => {
    if (!Array.isArray(input)) throw new TypeError('Legacy store is invalid');
    return {
      version: 1,
      updatedAt: Date.now(),
      records: input.map(itemParser),
    };
  };
}

const subscriptions = new AtomicJsonStore<StoreEnvelope<StoredSubscription>>({
  filePath: path.join(DATA_DIR, 'subscriptions.json'),
  empty: () => ({ version: 1, updatedAt: Date.now(), records: [] }),
  parse: envelopeParser(parseStoredSubscription),
  migrate: legacyMigration(parseStoredSubscription),
});

const campaigns = new AtomicJsonStore<StoreEnvelope<Campaign>>({
  filePath: path.join(DATA_DIR, 'campaigns.json'),
  empty: () => ({ version: 1, updatedAt: Date.now(), records: [] }),
  parse: envelopeParser(parseCampaign),
  migrate: legacyMigration(parseCampaign),
});

export async function upsertSubscription(
  input: StoredSubscription,
): Promise<void> {
  const subscription = parseStoredSubscription(input);
  await subscriptions.update((store) => {
    const existing = store.records.find(
      (candidate) => candidate.endpoint === subscription.endpoint,
    );
    return {
      version: 1,
      updatedAt: Date.now(),
      records: [
        ...store.records.filter(
          (candidate) => candidate.endpoint !== subscription.endpoint,
        ),
        {
          ...subscription,
          createdAt: existing?.createdAt ?? subscription.createdAt,
          lastActiveAt: Date.now(),
        },
      ],
    };
  });
}

export async function removeSubscription(endpoint: string): Promise<boolean> {
  let removed = false;
  await subscriptions.update((store) => {
    const records = store.records.filter((item) => item.endpoint !== endpoint);
    removed = records.length !== store.records.length;
    return { version: 1, updatedAt: Date.now(), records };
  });
  return removed;
}

export async function getSubscription(
  endpoint: string,
): Promise<StoredSubscription | null> {
  const store = await subscriptions.read();
  return store.records.find((item) => item.endpoint === endpoint) ?? null;
}

export async function getSubscriptionsByWallet(
  walletAddress: string,
): Promise<StoredSubscription[]> {
  const store = await subscriptions.read();
  const normalized = walletAddress.toLowerCase();
  return store.records.filter((item) => item.walletAddress === normalized);
}

export async function getSubscriptionsByTag(
  tag: string,
): Promise<StoredSubscription[]> {
  const store = await subscriptions.read();
  return store.records.filter((item) => item.tags?.includes(tag));
}

export async function getAllSubscriptions(): Promise<StoredSubscription[]> {
  return (await subscriptions.read()).records;
}

export async function getSubscriptionCount(): Promise<number> {
  return (await subscriptions.read()).records.length;
}

export async function updateLastNotified(endpoints: string[]): Promise<void> {
  const targets = new Set(endpoints);
  await subscriptions.update((store) => ({
    version: 1,
    updatedAt: Date.now(),
    records: store.records.map((item) =>
      targets.has(item.endpoint)
        ? { ...item, lastNotifiedAt: Date.now() }
        : item,
    ),
  }));
}

export async function addTagToWallet(
  walletAddress: string,
  tag: string,
): Promise<number> {
  let count = 0;
  const normalized = walletAddress.toLowerCase();
  await subscriptions.update((store) => ({
    version: 1,
    updatedAt: Date.now(),
    records: store.records.map((item) => {
      if (item.walletAddress !== normalized || item.tags?.includes(tag)) return item;
      count += 1;
      return { ...item, tags: [...(item.tags ?? []), tag] };
    }),
  }));
  return count;
}

export async function getInactiveSubscriptions(
  thresholdMs: number,
): Promise<StoredSubscription[]> {
  const cutoff = Date.now() - thresholdMs;
  return (await subscriptions.read()).records.filter(
    (item) => item.lastActiveAt < cutoff,
  );
}

export async function addCampaign(input: Campaign): Promise<void> {
  const campaign = parseCampaign(input);
  await campaigns.update((store) => {
    if (store.records.some((item) => item.id === campaign.id)) {
      throw new Error('Campaign already exists');
    }
    return {
      version: 1,
      updatedAt: Date.now(),
      records: [...store.records, campaign],
    };
  });
}

export async function getCampaign(id: string): Promise<Campaign | null> {
  return (await campaigns.read()).records.find((item) => item.id === id) ?? null;
}

export async function getPendingCampaigns(): Promise<Campaign[]> {
  const now = Date.now();
  return (await campaigns.read()).records.filter(
    (item) => !item.sentAt && (!item.scheduledAt || item.scheduledAt <= now),
  );
}

export async function markCampaignSent(
  id: string,
  sentCount: number,
  failedCount: number,
): Promise<void> {
  await campaigns.update((store) => ({
    version: 1,
    updatedAt: Date.now(),
    records: store.records.map((item) =>
      item.id === id
        ? { ...item, sentAt: Date.now(), sentCount, failedCount }
        : item,
    ),
  }));
}

export async function getAllCampaigns(): Promise<Campaign[]> {
  return (await campaigns.read()).records;
}
