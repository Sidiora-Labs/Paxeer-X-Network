import {
  convertFromUsd,
  formatUsdAsFiat,
  type FiatCurrency,
} from '@/lib/currency';
import { preferencesRepository } from '@/platform/storage/repositories';

export function shortenAddress(addr: string, chars = 4): string {
  if (!addr) return '';
  return `${addr.slice(0, chars + 2)}...${addr.slice(-chars)}`;
}

export function formatBalance(wei: string | bigint, decimals = 18, display = 4): string {
  // If the value is already a decimal string (not a raw wei integer), format it directly
  if (typeof wei === 'string' && wei.includes('.')) {
    const num = parseFloat(wei);
    if (isNaN(num)) return '0.' + '0'.repeat(display);
    return num.toFixed(display);
  }

  try {
    const raw = typeof wei === 'string' ? BigInt(wei) : wei;
    const divisor = BigInt(10) ** BigInt(decimals);
    const whole = raw / divisor;
    const frac = raw % divisor;
    const fracStr = frac.toString().padStart(decimals, '0').slice(0, display);
    return `${whole}.${fracStr}`;
  } catch {
    // Fallback for any unexpected format
    const num = parseFloat(String(wei));
    if (isNaN(num)) return '0.' + '0'.repeat(display);
    return num.toFixed(display);
  }
}

export function formatUsd(value: number): string {
  const preferences = preferencesRepository.read();
  return formatUsdAsFiat(
    value,
    preferences.language,
    preferences.currency as FiatCurrency,
  );
}

export function formatPrice(price: number): string {
  return formatUsd(price);
}

export function formatCompactUsd(value: number): string {
  const preferences = preferencesRepository.read();
  const currency = preferences.currency as FiatCurrency;
  const converted = convertFromUsd(value, currency);
  const displayCurrency = converted === null ? 'USD' : currency;
  return new Intl.NumberFormat(preferences.language, {
    style: 'currency',
    currency: displayCurrency,
    notation: 'compact',
    compactDisplay: 'short',
    maximumFractionDigits: 2,
  }).format(converted ?? value);
}

export function formatCompactNumber(value: number): string {
  if (value >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(1)}B`;
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`;
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`;
  return value.toFixed(0);
}
