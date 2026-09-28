import { defineStorageRepository } from './registry';
import { migrateLocale, type Locale } from '@/lib/locale';

const ADDRESS = /^0x[0-9a-fA-F]{40}$/;
const HASH = /^0x[0-9a-fA-F]{64}$/;
const HTTPS_URL = /^https:\/\//i;
const PROHIBITED = [
  'mnemonic',
  'private key',
  'unlock secret',
  'session capability',
  'approval payload',
  'push secret',
  'authentication credential',
] as const;

function record(input: unknown): Record<string, unknown> {
  if (typeof input !== 'object' || input === null || Array.isArray(input)) {
    throw new TypeError('Expected an object');
  }
  return input as Record<string, unknown>;
}

function stringValue(
  input: unknown,
  minimum: number,
  maximum: number,
): string {
  if (
    typeof input !== 'string' ||
    input.length < minimum ||
    input.length > maximum
  ) {
    throw new TypeError('String is outside allowed bounds');
  }
  return input;
}

function integer(input: unknown, minimum = 0): number {
  if (
    typeof input !== 'number' ||
    !Number.isSafeInteger(input) ||
    input < minimum
  ) {
    throw new TypeError('Integer is invalid');
  }
  return input;
}

function exactKeys(
  input: Record<string, unknown>,
  allowed: readonly string[],
): void {
  if (Object.keys(input).some((key) => !allowed.includes(key))) {
    throw new TypeError('Record contains unknown fields');
  }
}

function parseJsonValue(
  input: unknown,
  depth = 0,
): string | number | boolean | null | JsonValue[] | { [key: string]: JsonValue } {
  if (depth > 6) throw new TypeError('Cached metadata is too deeply nested');
  if (
    input === null ||
    typeof input === 'boolean' ||
    (typeof input === 'number' && Number.isFinite(input))
  ) {
    return input;
  }
  if (typeof input === 'string') return stringValue(input, 0, 4_096);
  if (Array.isArray(input)) {
    if (input.length > 200) throw new TypeError('Cached metadata array is too large');
    return input.map((value) => parseJsonValue(value, depth + 1));
  }
  const source = record(input);
  if (Object.keys(source).length > 200) {
    throw new TypeError('Cached metadata object is too large');
  }
  return Object.fromEntries(
    Object.entries(source).map(([key, value]) => [
      stringValue(key, 1, 128),
      parseJsonValue(value, depth + 1),
    ]),
  );
}

export type JsonValue =
  | string
  | number
  | boolean
  | null
  | JsonValue[]
  | { [key: string]: JsonValue };

export type CustodyChoice = 'embedded' | 'funded';

function parseCustodyChoice(input: unknown): CustodyChoice | null {
  if (input === null || input === 'self-custody') return null;
  if (input === 'embedded' || input === 'funded') {
    return input;
  }
  throw new TypeError('Custody choice is invalid');
}

export const custodyChoiceRepository = defineStorageRepository({
  id: 'custody-choice',
  key: 'paxport:v1:custody-choice',
  owner: 'custody-choice',
  schema: 'CustodyChoice | null',
  version: 1,
  area: 'local',
  sensitivity: 'security-relevant',
  retention: 'Until explicit custody switch, reset, or uninstall',
  quotaBytes: 256,
  migration: 'Parse legacy paxeer:wallet-kind enum and rewrite v1 envelope',
  resetOn: ['reset', 'custody-switch', 'uninstall'],
  corruption: 'fail-closed',
  prohibitedData: PROHIBITED,
  fallback: () => null,
  parse: parseCustodyChoice,
  legacyKeys: ['paxeer:wallet-kind'],
  migrateLegacy: (storage) => storage.getItem('paxeer:wallet-kind') ?? undefined,
});

export interface ContactRecord {
  id: string;
  name: string;
  address: string;
  note?: string;
  createdAt: number;
  updatedAt: number;
}

function parseContact(input: unknown): ContactRecord {
  const value = record(input);
  exactKeys(value, ['id', 'name', 'address', 'note', 'createdAt', 'updatedAt']);
  const address = stringValue(value.address, 42, 42);
  if (!ADDRESS.test(address)) throw new TypeError('Contact address is invalid');
  return {
    id: stringValue(value.id, 1, 80),
    name: stringValue(value.name, 1, 80),
    address,
    note:
      value.note === undefined ? undefined : stringValue(value.note, 1, 280),
    createdAt: integer(value.createdAt),
    updatedAt: integer(value.updatedAt),
  };
}

function parseContacts(input: unknown): ContactRecord[] {
  if (!Array.isArray(input) || input.length > 500) {
    throw new TypeError('Contacts are invalid');
  }
  const contacts = input.map(parseContact);
  const ids = new Set<string>();
  const addresses = new Set<string>();
  for (const contact of contacts) {
    const address = contact.address.toLowerCase();
    if (ids.has(contact.id) || addresses.has(address)) {
      throw new TypeError('Contacts contain duplicates');
    }
    ids.add(contact.id);
    addresses.add(address);
  }
  return contacts;
}

export const contactsRepository = defineStorageRepository({
  id: 'contacts',
  key: 'paxport:v1:contacts',
  owner: 'contacts',
  schema: 'ContactRecord[0..500]',
  version: 1,
  area: 'local',
  sensitivity: 'private-metadata',
  retention: 'Until user deletion, reset, or uninstall',
  quotaBytes: 262_144,
  migration: 'Validate legacy paxeer_contacts array and rewrite v1 envelope',
  resetOn: ['reset', 'uninstall'],
  corruption: 'reset-and-signal',
  prohibitedData: PROHIBITED,
  fallback: () => [],
  parse: parseContacts,
  legacyKeys: ['paxeer_contacts'],
  migrateLegacy: (storage) => {
    const raw = storage.getItem('paxeer_contacts');
    return raw === null ? undefined : JSON.parse(raw);
  },
});

export interface RecentRecipientRecord {
  address: string;
  label?: string;
  timestamp: number;
}

function parseRecentRecipients(input: unknown): RecentRecipientRecord[] {
  if (!Array.isArray(input) || input.length > 5) {
    throw new TypeError('Recent recipients are invalid');
  }
  return input.map((item) => {
    const value = record(item);
    exactKeys(value, ['address', 'label', 'timestamp']);
    const address = stringValue(value.address, 42, 42);
    if (!ADDRESS.test(address)) throw new TypeError('Recipient address is invalid');
    return {
      address,
      label:
        value.label === undefined
          ? undefined
          : stringValue(value.label, 1, 80),
      timestamp: integer(value.timestamp),
    };
  });
}

export const recentRecipientsRepository = defineStorageRepository({
  id: 'recent-recipients',
  key: 'paxport:v1:recent-recipients',
  owner: 'contacts',
  schema: 'RecentRecipientRecord[0..5]',
  version: 1,
  area: 'local',
  sensitivity: 'private-metadata',
  retention: 'Five most recent recipients until reset, logout, or uninstall',
  quotaBytes: 4_096,
  migration: 'Validate legacy paxeer_recent_recipients and rewrite v1 envelope',
  resetOn: ['reset', 'logout', 'account-removal', 'uninstall'],
  corruption: 'reset-and-signal',
  prohibitedData: PROHIBITED,
  fallback: () => [],
  parse: parseRecentRecipients,
  legacyKeys: ['paxeer_recent_recipients'],
  migrateLegacy: (storage) => {
    const raw = storage.getItem('paxeer_recent_recipients');
    return raw === null ? undefined : JSON.parse(raw);
  },
});

export interface AppPreferences {
  currency: string;
  language: Locale;
  customRpc: string | null;
  customNonce: number | null;
  feeMode: 'auto' | 'economy' | 'priority' | 'custom';
  customMaxFeeGwei: string | null;
  customPriorityFeeGwei: string | null;
  developerMode: boolean;
  showHexData: boolean;
  notifications: Record<string, boolean>;
}

const DEFAULT_NOTIFICATIONS = {
  tx_received: true,
  tx_sent: true,
  price_alert: true,
  security: true,
  news: true,
};

function parsePreferences(input: unknown): AppPreferences {
  const value = record(input);
  exactKeys(value, [
    'currency',
    'language',
    'customRpc',
    'customNonce',
    'feeMode',
    'customMaxFeeGwei',
    'customPriorityFeeGwei',
    'developerMode',
    'showHexData',
    'notifications',
  ]);
  const notifications = record(value.notifications);
  if (
    Object.keys(notifications).length > 32 ||
    Object.entries(notifications).some(
      ([key, enabled]) => !/^[a-z0-9_-]{1,40}$/.test(key) || typeof enabled !== 'boolean',
    )
  ) {
    throw new TypeError('Notification preferences are invalid');
  }
  const customRpc =
    value.customRpc === null ? null : stringValue(value.customRpc, 8, 512);
  if (customRpc !== null && !HTTPS_URL.test(customRpc)) {
    throw new TypeError('Custom RPC URL is invalid');
  }
  if (
    typeof value.developerMode !== 'boolean' ||
    typeof value.showHexData !== 'boolean'
  ) {
    throw new TypeError('Developer preferences are invalid');
  }
  let customNonce: number | null = null;
  if (value.customNonce !== null && value.customNonce !== undefined) {
    if (typeof value.customNonce !== 'number' || !Number.isInteger(value.customNonce) || value.customNonce < 0) {
      throw new TypeError('Custom nonce must be a non-negative integer');
    }
    customNonce = value.customNonce;
  }
  const feeMode =
    value.feeMode === undefined
      ? 'auto'
      : value.feeMode;
  if (
    feeMode !== 'auto' &&
    feeMode !== 'economy' &&
    feeMode !== 'priority' &&
    feeMode !== 'custom'
  ) {
    throw new TypeError('Fee mode is invalid');
  }
  const parseOptionalDecimal = (input: unknown): string | null => {
    if (input === null || input === undefined) return null;
    const parsed = stringValue(input, 1, 32);
    if (!/^\d+(?:\.\d{1,9})?$/.test(parsed) || Number(parsed) <= 0) {
      throw new TypeError('Custom fee is invalid');
    }
    return parsed;
  };
  return {
    currency: stringValue(value.currency, 3, 8),
    language: migrateLocale(stringValue(value.language, 2, 32)),
    customRpc,
    customNonce,
    feeMode,
    customMaxFeeGwei: parseOptionalDecimal(value.customMaxFeeGwei),
    customPriorityFeeGwei: parseOptionalDecimal(value.customPriorityFeeGwei),
    developerMode: value.developerMode,
    showHexData: value.showHexData,
    notifications: Object.fromEntries(
      Object.entries(notifications).map(([key, enabled]) => [key, enabled as boolean]),
    ),
  };
}

const defaultPreferences = (): AppPreferences => ({
  currency: 'USD',
  language: 'en',
  customRpc: null,
  customNonce: null,
  feeMode: 'auto',
  customMaxFeeGwei: null,
  customPriorityFeeGwei: null,
  developerMode: false,
  showHexData: false,
  notifications: { ...DEFAULT_NOTIFICATIONS },
});

export const preferencesRepository = defineStorageRepository({
  id: 'preferences',
  key: 'paxport:v1:preferences',
  owner: 'preferences',
  schema: 'AppPreferences',
  version: 1,
  area: 'local',
  sensitivity: 'preferences',
  retention: 'Until reset or uninstall',
  quotaBytes: 16_384,
  migration: 'Merge and validate seven legacy preference records',
  resetOn: ['reset', 'uninstall'],
  corruption: 'reset-and-signal',
  prohibitedData: PROHIBITED,
  fallback: defaultPreferences,
  parse: parsePreferences,
  legacyKeys: [
    'paxeer_currency',
    'paxeer_language',
    'paxeer_custom_rpc',
    'paxeer_dev_mode',
    'paxeer_hex_data',
    'paxeer_notif_prefs',
  ],
  migrateLegacy: (storage) => {
    const keys = [
      'paxeer_currency',
      'paxeer_language',
      'paxeer_custom_rpc',
      'paxeer_dev_mode',
      'paxeer_hex_data',
      'paxeer_notif_prefs',
    ];
    if (keys.every((key) => storage.getItem(key) === null)) return undefined;
    const defaults = defaultPreferences();
    const rawNotifications = storage.getItem('paxeer_notif_prefs');
    return {
      currency: storage.getItem('paxeer_currency') ?? defaults.currency,
      language: storage.getItem('paxeer_language') ?? defaults.language,
      customRpc: storage.getItem('paxeer_custom_rpc'),
      developerMode: storage.getItem('paxeer_dev_mode') === 'true',
      showHexData: storage.getItem('paxeer_hex_data') === 'true',
      notifications:
        rawNotifications === null
          ? defaults.notifications
          : JSON.parse(rawNotifications),
    };
  },
});

export interface CurrencyRatesRecord {
  base: 'USD';
  rates: Record<string, number>;
  fetchedAt: number;
}

function parseCurrencyRates(input: unknown): CurrencyRatesRecord {
  const value = record(input);
  exactKeys(value, ['base', 'rates', 'fetchedAt']);
  if (value.base !== 'USD') throw new TypeError('Currency rate base is invalid');
  const rates = record(value.rates);
  if (
    Object.keys(rates).length > 32 ||
    Object.entries(rates).some(
      ([code, rate]) =>
        !/^[A-Z]{3}$/.test(code) ||
        typeof rate !== 'number' ||
        !Number.isFinite(rate) ||
        rate <= 0,
    )
  ) {
    throw new TypeError('Currency rates are invalid');
  }
  return {
    base: 'USD',
    rates: Object.fromEntries(
      Object.entries(rates).map(([code, rate]) => [code, rate as number]),
    ),
    fetchedAt: integer(value.fetchedAt),
  };
}

export const currencyRatesRepository = defineStorageRepository({
  id: 'currency-rates',
  key: 'paxport:v1:currency-rates',
  owner: 'preferences',
  schema: 'CurrencyRatesRecord',
  version: 1,
  area: 'local',
  sensitivity: 'public',
  retention: 'Until stale, reset, or uninstall',
  quotaBytes: 4_096,
  migration: 'No legacy currency-rate cache',
  resetOn: ['reset', 'uninstall'],
  corruption: 'reset-and-signal',
  prohibitedData: PROHIBITED,
  fallback: (): CurrencyRatesRecord => ({
    base: 'USD',
    rates: { USD: 1 },
    fetchedAt: 0,
  }),
  parse: parseCurrencyRates,
});

export interface PortfolioFilters {
  hideDust: boolean;
  hiddenTokens: string[];
}

function parsePortfolioFilters(input: unknown): PortfolioFilters {
  const value = record(input);
  exactKeys(value, ['hideDust', 'hiddenTokens']);
  if (
    typeof value.hideDust !== 'boolean' ||
    !Array.isArray(value.hiddenTokens) ||
    value.hiddenTokens.length > 500
  ) {
    throw new TypeError('Portfolio filters are invalid');
  }
  const hiddenTokens = value.hiddenTokens.map((address) => {
    const parsed = stringValue(address, 42, 42).toLowerCase();
    if (!ADDRESS.test(parsed)) throw new TypeError('Hidden token is invalid');
    return parsed;
  });
  return { hideDust: value.hideDust, hiddenTokens: [...new Set(hiddenTokens)] };
}

export const portfolioFiltersRepository = defineStorageRepository({
  id: 'portfolio-filters',
  key: 'paxport:v1:portfolio-filters',
  owner: 'portfolio',
  schema: 'PortfolioFilters',
  version: 1,
  area: 'local',
  sensitivity: 'private-metadata',
  retention: 'Until reset or uninstall',
  quotaBytes: 32_768,
  migration: 'Merge and validate legacy dust and hidden-token records',
  resetOn: ['reset', 'uninstall'],
  corruption: 'reset-and-signal',
  prohibitedData: PROHIBITED,
  fallback: () => ({ hideDust: false, hiddenTokens: [] }),
  parse: parsePortfolioFilters,
  legacyKeys: ['paxeer_hide_dust', 'paxeer_hidden_tokens'],
  migrateLegacy: (storage) => {
    const dust = storage.getItem('paxeer_hide_dust');
    const hidden = storage.getItem('paxeer_hidden_tokens');
    if (dust === null && hidden === null) return undefined;
    return {
      hideDust: dust === 'true',
      hiddenTokens: hidden === null ? [] : JSON.parse(hidden),
    };
  },
});

export interface PwaDismissals {
  installAt: number | null;
  notificationAt: number | null;
}

function parsePwaDismissals(input: unknown): PwaDismissals {
  const value = record(input);
  exactKeys(value, ['installAt', 'notificationAt']);
  return {
    installAt: value.installAt === null ? null : integer(value.installAt),
    notificationAt:
      value.notificationAt === null ? null : integer(value.notificationAt),
  };
}

export const pwaDismissalsRepository = defineStorageRepository({
  id: 'pwa-dismissals',
  key: 'paxport:v1:pwa-dismissals',
  owner: 'pwa',
  schema: 'PwaDismissals',
  version: 1,
  area: 'local',
  sensitivity: 'preferences',
  retention: 'Install prompt 7 days; notification prompt 3 days; pruned on read',
  quotaBytes: 512,
  migration: 'Merge legacy prompt timestamps and rewrite v1 envelope',
  resetOn: ['reset', 'uninstall'],
  corruption: 'reset-and-signal',
  prohibitedData: PROHIBITED,
  fallback: () => ({ installAt: null, notificationAt: null }),
  parse: parsePwaDismissals,
  legacyKeys: ['pwa-install-dismissed', 'pwa-notif-dismissed'],
  migrateLegacy: (storage) => {
    const install = storage.getItem('pwa-install-dismissed');
    const notification = storage.getItem('pwa-notif-dismissed');
    if (install === null && notification === null) return undefined;
    return {
      installAt: install === null ? null : Number(install),
      notificationAt: notification === null ? null : Number(notification),
    };
  },
});

export interface NotificationTriggerState {
  lastTxCount: number | null;
  lastActive: number | null;
  seenFeatures: number;
}

function parseNotificationState(input: unknown): NotificationTriggerState {
  const value = record(input);
  exactKeys(value, ['lastTxCount', 'lastActive', 'seenFeatures']);
  return {
    lastTxCount:
      value.lastTxCount === null ? null : integer(value.lastTxCount),
    lastActive: value.lastActive === null ? null : integer(value.lastActive),
    seenFeatures: integer(value.seenFeatures),
  };
}

export const notificationStateRepository = defineStorageRepository({
  id: 'notification-state',
  key: 'paxport:v1:notification-state',
  owner: 'notifications',
  schema: 'NotificationTriggerState',
  version: 1,
  area: 'local',
  sensitivity: 'private-metadata',
  retention: 'Until logout, account removal, reset, or uninstall',
  quotaBytes: 1_024,
  migration: 'Merge three legacy notification counters',
  resetOn: ['reset', 'logout', 'account-removal', 'uninstall'],
  corruption: 'reset-and-signal',
  prohibitedData: PROHIBITED,
  fallback: () => ({ lastTxCount: null, lastActive: null, seenFeatures: 0 }),
  parse: parseNotificationState,
  legacyKeys: [
    'paxeer_notif_last_tx_count',
    'paxeer_notif_last_active',
    'paxeer_notif_seen_features',
  ],
  migrateLegacy: (storage) => {
    const tx = storage.getItem('paxeer_notif_last_tx_count');
    const active = storage.getItem('paxeer_notif_last_active');
    const features = storage.getItem('paxeer_notif_seen_features');
    if (tx === null && active === null && features === null) return undefined;
    return {
      lastTxCount: tx === null ? null : Number(tx),
      lastActive: active === null ? null : Number(active),
      seenFeatures: features === null ? 0 : Number(features),
    };
  },
});

export interface MetadataCacheEntry {
  ts: number;
  data: JsonValue;
}

export type MetadataCache = Record<string, MetadataCacheEntry>;

function parseMetadataCache(input: unknown): MetadataCache {
  const value = record(input);
  if (Object.keys(value).length > 200) throw new TypeError('Metadata cache is too large');
  return Object.fromEntries(
    Object.entries(value).map(([key, entry]) => {
      const source = record(entry);
      exactKeys(source, ['ts', 'data']);
      return [
        stringValue(key, 1, 160),
        { ts: integer(source.ts), data: parseJsonValue(source.data) },
      ];
    }),
  );
}

export const metadataCacheRepository = defineStorageRepository<MetadataCache>({
  id: 'metadata-cache',
  key: 'paxport:v1:metadata-cache',
  owner: 'metadata',
  schema: 'Record<string, MetadataCacheEntry>',
  version: 1,
  area: 'local',
  sensitivity: 'public',
  retention: 'Per-entry caller TTL; at most 200 entries',
  quotaBytes: 524_288,
  migration: 'Collect, validate, and remove legacy pax:meta:* entries',
  resetOn: ['reset', 'uninstall'],
  corruption: 'reset-and-signal',
  prohibitedData: PROHIBITED,
  fallback: () => ({}),
  parse: parseMetadataCache,
  migrateLegacy: (storage) => {
    const migrated: MetadataCache = {};
    const keys: string[] = [];
    for (let index = 0; index < storage.length; index += 1) {
      const key = storage.key(index);
      if (key?.startsWith('pax:meta:')) keys.push(key);
    }
    if (keys.length === 0) return undefined;
    for (const key of keys.slice(0, 200)) {
      const raw = storage.getItem(key);
      if (raw !== null) migrated[key.slice('pax:meta:'.length)] = JSON.parse(raw);
      storage.removeItem(key);
    }
    return migrated;
  },
});

export interface PendingSendRecord {
  tokenAddress?: string;
  symbol: string;
  amount: string;
  decimals: number;
  recipient: string;
  txHash: string;
  timestamp: number;
}

function parsePendingSend(input: unknown): PendingSendRecord | null {
  if (input === null) return null;
  const value = record(input);
  exactKeys(value, [
    'tokenAddress',
    'symbol',
    'amount',
    'decimals',
    'recipient',
    'txHash',
    'timestamp',
  ]);
  const tokenAddress =
    value.tokenAddress === undefined
      ? undefined
      : stringValue(value.tokenAddress, 42, 42);
  const recipient = stringValue(value.recipient, 42, 42);
  const txHash = stringValue(value.txHash, 66, 66);
  if (
    (tokenAddress && !ADDRESS.test(tokenAddress)) ||
    !ADDRESS.test(recipient) ||
    !HASH.test(txHash)
  ) {
    throw new TypeError('Pending send identifiers are invalid');
  }
  const decimals = integer(value.decimals);
  if (decimals > 36) throw new TypeError('Pending send decimals are invalid');
  return {
    tokenAddress,
    symbol: stringValue(value.symbol, 1, 20),
    amount: stringValue(value.amount, 1, 100),
    decimals,
    recipient,
    txHash,
    timestamp: integer(value.timestamp),
  };
}

export const pendingSendRepository = defineStorageRepository({
  id: 'pending-send',
  key: 'paxport:v1:pending-send',
  owner: 'operations',
  schema: 'PendingSendRecord | null',
  version: 1,
  area: 'session',
  sensitivity: 'private-metadata',
  retention: 'Single browser session and no more than two minutes',
  quotaBytes: 4_096,
  migration: 'Validate legacy session pending-send record',
  resetOn: ['reset', 'logout', 'custody-switch', 'account-removal', 'uninstall'],
  corruption: 'reset-and-signal',
  prohibitedData: [...PROHIBITED, 'full transaction approval'],
  fallback: () => null,
  parse: parsePendingSend,
  legacyKeys: ['paxeer_pending_send'],
  migrateLegacy: (storage) => {
    const raw = storage.getItem('paxeer_pending_send');
    return raw === null ? undefined : JSON.parse(raw);
  },
});

function parseAnnouncementVersion(input: unknown): string | null {
  if (input === null) return null;
  const version = stringValue(input, 1, 32);
  if (!/^[0-9]+(?:\.[0-9]+){0,3}$/.test(version)) {
    throw new TypeError('Announcement version is invalid');
  }
  return version;
}

export const announcementRepository = defineStorageRepository({
  id: 'announcement',
  key: 'paxport:v1:announcement',
  owner: 'pwa',
  schema: 'Semver string | null',
  version: 1,
  area: 'local',
  sensitivity: 'preferences',
  retention: 'Until a newer shipped announcement or reset',
  quotaBytes: 256,
  migration: 'Validate legacy whats-new version',
  resetOn: ['reset', 'uninstall'],
  corruption: 'reset-and-signal',
  prohibitedData: PROHIBITED,
  fallback: () => null,
  parse: parseAnnouncementVersion,
  legacyKeys: ['paxeer_whats_new_seen'],
  migrateLegacy: (storage) => storage.getItem('paxeer_whats_new_seen') ?? undefined,
});
