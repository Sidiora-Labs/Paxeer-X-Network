import type { Locale } from './index';

function localeTag(locale: Locale): string {
  const map: Record<Locale, string> = {
    en: 'en-US',
    es: 'es-ES',
    fr: 'fr-FR',
    de: 'de-DE',
    pt: 'pt-BR',
    ar: 'ar-SA',
    it: 'it-IT',
    hi: 'hi-IN',
    ja: 'ja-JP',
    ko: 'ko-KR',
  };
  return map[locale] ?? 'en-US';
}

export function formatNumber(
  value: number,
  locale: Locale,
  options?: Intl.NumberFormatOptions,
): string {
  return new Intl.NumberFormat(localeTag(locale), options).format(value);
}

export function formatCurrency(
  value: number,
  locale: Locale,
  currency: string = 'USD',
): string {
  return new Intl.NumberFormat(localeTag(locale), {
    style: 'currency',
    currency,
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(value);
}

export function formatTokenAmount(
  value: string | number,
  decimals: number,
  locale: Locale,
  symbol?: string,
): string {
  // Use BigInt-safe parsing to avoid precision loss on large values.
  let num: number;
  if (typeof value === 'string') {
    // If the string is a pure integer (possibly with leading zeros), parse via BigInt
    // to avoid float precision loss, then convert to number only for formatting.
    const trimmed = value.trim();
    if (/^\d+$/.test(trimmed)) {
      // Pure integer — safe to format directly without float conversion
      const divisor = BigInt(10) ** BigInt(decimals);
      const whole = BigInt(trimmed) / divisor;
      const frac = BigInt(trimmed) % divisor;
      const fracStr = frac.toString().padStart(decimals, '0').replace(/0+$/, '');
      const formatted = new Intl.NumberFormat(localeTag(locale), {
        minimumFractionDigits: 0,
        maximumFractionDigits: Math.min(decimals, 6),
      }).format(Number(whole));
      const fracFormatted = fracStr
        ? new Intl.NumberFormat(localeTag(locale), {
            minimumFractionDigits: 0,
            maximumFractionDigits: Math.min(decimals, 6),
          }).format(Number(`0.${fracStr}`)).replace(/^0/, '')
        : '';
      return symbol ? `${formatted}${fracFormatted}\u00A0${symbol}` : `${formatted}${fracFormatted}`;
    }
    num = parseFloat(value);
  } else {
    num = value;
  }
  if (!Number.isFinite(num)) return symbol ? `0 ${symbol}` : '0';

  const significantDigits = Math.min(decimals, 6);
  const formatted = new Intl.NumberFormat(localeTag(locale), {
    minimumFractionDigits: 0,
    maximumFractionDigits: significantDigits,
  }).format(num);

  return symbol ? `${formatted}\u00A0${symbol}` : formatted;
}

export function formatCompactNumber(
  value: number,
  locale: Locale,
): string {
  return new Intl.NumberFormat(localeTag(locale), {
    notation: 'compact',
    compactDisplay: 'short',
    maximumFractionDigits: 2,
  }).format(value);
}

export function formatDate(
  value: number | Date,
  locale: Locale,
  options?: Intl.DateTimeFormatOptions,
): string {
  const date = typeof value === 'number' ? new Date(value) : value;
  return new Intl.DateTimeFormat(localeTag(locale), {
    dateStyle: 'medium',
    ...options,
  }).format(date);
}

export function formatTime(
  value: number | Date,
  locale: Locale,
): string {
  const date = typeof value === 'number' ? new Date(value) : value;
  return new Intl.DateTimeFormat(localeTag(locale), {
    timeStyle: 'short',
  }).format(date);
}

export function formatRelativeTime(
  value: number,
  locale: Locale,
  unit: Intl.RelativeTimeFormatUnit = 'second',
): string {
  return new Intl.RelativeTimeFormat(localeTag(locale), {
    numeric: 'auto',
  }).format(value, unit);
}

export function formatPercent(
  value: number,
  locale: Locale,
  fractionDigits: number = 2,
): string {
  return new Intl.NumberFormat(localeTag(locale), {
    style: 'percent',
    minimumFractionDigits: 0,
    maximumFractionDigits: fractionDigits,
  }).format(value / 100);
}

export function plural(
  count: number,
  locale: Locale,
  forms: { zero?: string; one: string; two?: string; few?: string; many?: string; other: string },
): string {
  const pr = new Intl.PluralRules(localeTag(locale));
  const rule = pr.select(count);
  return forms[rule] ?? forms.other;
}
