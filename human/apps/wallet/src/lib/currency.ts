/**
 * Currency conversion service.
 *
 * Fetches USD-based exchange rates from ExchangeRate-API's open endpoint, caches them
 * with freshness tracking, and exposes conversion helpers for the UI layer.
 *
 * Design:
 * - Rates are fetched once and cached for RATE_TTL_MS (5 minutes).
 * - On fetch failure, stale rates are served until STALE_TTL_MS (1 hour).
 * - All rates are USD-based; cross-rates are computed through USD.
 * - No API key is required for the public latest/USD endpoint.
 */

import {
  currencyRatesRepository,
  preferencesRepository,
} from '@/platform/storage/repositories';

// Supported fiat currencies (matching the settings dropdown)
export const FIAT_CURRENCIES = [
  'USD', 'EUR', 'GBP', 'AED', 'AUD', 'CAD', 'CHF', 'CNY', 'JPY', 'KRW',
  'ZAR', 'NGN', 'BRL',
] as const;

export type FiatCurrency = (typeof FIAT_CURRENCIES)[number];

interface RateCache {
  /** Map of currency code -> USD exchange rate (1 USD = X units of currency) */
  rates: Record<string, number>;
  /** Timestamp when rates were last successfully fetched */
  fetchedAt: number;
}

const RATE_TTL_MS = 5 * 60 * 1_000; // 5 minutes
const STALE_TTL_MS = 60 * 60 * 1_000; // 1 hour

let cache: RateCache = { rates: { USD: 1 }, fetchedAt: 0 };
let inFlight: Promise<void> | null = null;

export const CURRENCY_RATES_EVENT = 'paxport:currency-rates';

function announceRates(): void {
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new Event(CURRENCY_RATES_EVENT));
  }
}

export function getActiveCurrency(): FiatCurrency {
  try {
    const stored = preferencesRepository.read().currency;
    if (stored && FIAT_CURRENCIES.includes(stored as FiatCurrency)) {
      return stored as FiatCurrency;
    }
  } catch {
    // Fall through
  }
  return 'USD';
}

function isFresh(): boolean {
  return Date.now() - cache.fetchedAt < RATE_TTL_MS;
}

function isStale(): boolean {
  return Date.now() - cache.fetchedAt < STALE_TTL_MS;
}

async function fetchRates(): Promise<void> {
  const url = 'https://open.er-api.com/v6/latest/USD';
  try {
    const res = await fetch(url, { signal: AbortSignal.timeout(10_000) });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const data = await res.json() as {
      result?: unknown;
      rates?: Record<string, unknown>;
    };
    if (data.result !== 'success' || !data.rates) {
      throw new Error('Exchange-rate response is invalid');
    }
    const rates: Record<string, number> = { USD: 1 };
    for (const currency of FIAT_CURRENCIES) {
      if (currency === 'USD') continue;
      const rate = data.rates[currency];
      if (typeof rate === 'number' && Number.isFinite(rate) && rate > 0) {
        rates[currency] = rate;
      }
    }
    if (Object.keys(rates).length !== FIAT_CURRENCIES.length) {
      throw new Error('Exchange-rate response is incomplete');
    }
    cache = { rates, fetchedAt: Date.now() };
    currencyRatesRepository.write({
      base: 'USD',
      rates,
      fetchedAt: cache.fetchedAt,
    });
    announceRates();
  } catch {
    announceRates();
  }
}

/**
 * Ensure rates are loaded. Returns immediately if cache is fresh;
 * otherwise fetches in background and serves stale data.
 */
export async function ensureRates(): Promise<void> {
  if (isFresh()) return;
  if (!inFlight) {
    inFlight = fetchRates().finally(() => {
      inFlight = null;
    });
  }
  await inFlight;
}

/**
 * Get the exchange rate for a currency relative to USD.
 * Returns 1 for USD; returns cached rate for others.
 */
export function getRate(currency: FiatCurrency): number | null {
  if (currency === 'USD') return 1;
  const rate = cache.rates[currency];
  return typeof rate === 'number' && Number.isFinite(rate) && rate > 0
    ? rate
    : null;
}

/**
 * Convert a USD amount to the active currency.
 * Returns the converted amount, or null if conversion is unavailable.
 */
export function convertFromUsd(usdAmount: number, currency?: FiatCurrency): number | null {
  const target = currency ?? getActiveCurrency();
  const rate = getRate(target);
  if (rate === null) return null;
  return usdAmount * rate;
}

/**
 * Format a USD value in the active currency.
 * Uses the locale formatters under the hood.
 */
export function formatUsdAsFiat(
  usdAmount: number,
  locale: string,
  currency?: FiatCurrency,
): string {
  const target = currency ?? getActiveCurrency();
  const fractionDigits =
    Math.abs(usdAmount) > 0 && Math.abs(usdAmount) < 0.01 ? 8 : 2;
  if (target === 'USD') {
    return new Intl.NumberFormat(locale, {
      style: 'currency',
      currency: 'USD',
      minimumFractionDigits: 2,
      maximumFractionDigits: fractionDigits,
    }).format(usdAmount);
  }
  const converted = convertFromUsd(usdAmount, target);
  if (converted === null) {
    return new Intl.NumberFormat(locale, {
      style: 'currency',
      currency: 'USD',
      minimumFractionDigits: 2,
      maximumFractionDigits: fractionDigits,
    }).format(usdAmount);
  }
  return new Intl.NumberFormat(locale, {
    style: 'currency',
    currency: target,
    minimumFractionDigits: 2,
    maximumFractionDigits: fractionDigits,
  }).format(converted);
}

/**
 * Get a human-readable freshness label for the current rates.
 */
export function getRatesFreshness(): 'fresh' | 'stale' | 'unavailable' {
  if (isFresh()) return 'fresh';
  if (isStale()) return 'stale';
  return 'unavailable';
}

/** Initialize the currency service (call once at app startup). */
export function initCurrencyService(): void {
  try {
    const stored = currencyRatesRepository.read();
    cache = { rates: stored.rates, fetchedAt: stored.fetchedAt };
  } catch {
    cache = { rates: { USD: 1 }, fetchedAt: 0 };
  }
  void ensureRates();
}

export function announceCurrencyChanged(): void {
  announceRates();
}
