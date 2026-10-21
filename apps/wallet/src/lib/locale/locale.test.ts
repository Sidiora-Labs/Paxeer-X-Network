import { describe, it, expect } from 'vitest';
import {
  SUPPORTED_LOCALES,
  DEFAULT_LOCALE,
  isRtlLocale,
  isValidLocale,
  migrateLocale,
  LOCALE_DISPLAY_NAMES,
} from './index';
import { getMessages, MESSAGE_CATALOGS } from './messages';
import {
  formatNumber,
  formatCurrency,
  formatTokenAmount,
  formatCompactNumber,
  formatDate,
  formatPercent,
  plural,
} from './formatters';

describe('locale codes', () => {
  it('exports stable locale codes', () => {
    expect(SUPPORTED_LOCALES).toEqual([
      'en', 'es', 'fr', 'de', 'pt', 'ar', 'it', 'hi', 'ja', 'ko',
    ]);
  });

  it('defaults to en', () => {
    expect(DEFAULT_LOCALE).toBe('en');
  });

  it('identifies RTL locales', () => {
    expect(isRtlLocale('ar')).toBe(true);
    expect(isRtlLocale('en')).toBe(false);
    expect(isRtlLocale('he')).toBe(true);
    expect(isRtlLocale('es')).toBe(false);
  });

  it('validates locale codes', () => {
    expect(isValidLocale('en')).toBe(true);
    expect(isValidLocale('ar')).toBe(true);
    expect(isValidLocale('xx')).toBe(false);
    expect(isValidLocale('English')).toBe(false);
    expect(isValidLocale(null)).toBe(false);
  });
});

describe('legacy locale migration', () => {
  it('passes through valid locale codes', () => {
    expect(migrateLocale('en')).toBe('en');
    expect(migrateLocale('ar')).toBe('ar');
  });

  it('migrates legacy display names', () => {
    expect(migrateLocale('English')).toBe('en');
    expect(migrateLocale('Español')).toBe('es');
    expect(migrateLocale('Spanish')).toBe('es');
    expect(migrateLocale('Français')).toBe('fr');
    expect(migrateLocale('French')).toBe('fr');
    expect(migrateLocale('Deutsch')).toBe('de');
    expect(migrateLocale('German')).toBe('de');
    expect(migrateLocale('Português')).toBe('pt');
    expect(migrateLocale('Portuguese')).toBe('pt');
    expect(migrateLocale('العربية')).toBe('ar');
    expect(migrateLocale('Arabic')).toBe('ar');
  });

  it('falls back to default for unknown values', () => {
    expect(migrateLocale('Klingon')).toBe(DEFAULT_LOCALE);
    expect(migrateLocale('')).toBe(DEFAULT_LOCALE);
    expect(migrateLocale(null)).toBe(DEFAULT_LOCALE);
    expect(migrateLocale(undefined)).toBe(DEFAULT_LOCALE);
    expect(migrateLocale(42)).toBe(DEFAULT_LOCALE);
  });
});

describe('message catalogs', () => {
  it('has a catalog for every supported locale', () => {
    for (const locale of SUPPORTED_LOCALES) {
      const catalog = getMessages(locale);
      expect(catalog).toBeDefined();
      expect(catalog.common.ok).toBeTruthy();
      expect(catalog.nav.portfolio).toBeTruthy();
    }
  });

  it('falls back to English for unknown locale', () => {
    // @ts-expect-error testing invalid locale
    expect(getMessages('xx').common.ok).toBe(MESSAGE_CATALOGS.en.common.ok);
  });

  it('Arabic catalog has RTL-appropriate content', () => {
    const ar = getMessages('ar');
    expect(ar.common.ok).toBe('موافق');
    expect(ar.nav.send).toBe('إرسال');
  });

  it('all catalogs have identical key structure', () => {
    const enKeys = Object.keys(MESSAGE_CATALOGS.en).sort();
    for (const locale of SUPPORTED_LOCALES) {
      expect(Object.keys(MESSAGE_CATALOGS[locale]).sort()).toEqual(enKeys);
      for (const section of enKeys) {
        const enSection = (MESSAGE_CATALOGS.en as unknown as Record<string, Record<string, string>>)[section];
        const localeSection = (MESSAGE_CATALOGS[locale] as unknown as Record<string, Record<string, string>>)[section];
        expect(Object.keys(localeSection).sort()).toEqual(Object.keys(enSection).sort());
      }
    }
  });
});

describe('formatters', () => {
  it('formats numbers per locale', () => {
    const enFormatted = formatNumber(1234.56, 'en');
    expect(enFormatted).toContain('1');
    expect(enFormatted).toContain('234');
  });

  it('formats currency per locale', () => {
    const usd = formatCurrency(1234.56, 'en', 'USD');
    expect(usd).toContain('1');
    expect(usd).toContain('234');
  });

  it('formats token amounts with symbol', () => {
    const amount = formatTokenAmount('1234.567890', 18, 'en', 'PAX');
    expect(amount).toContain('PAX');
    expect(amount).toContain('1');
    expect(amount).toContain('234');
  });

  it('formats token amounts without symbol', () => {
    const amount = formatTokenAmount('0.5', 18, 'en');
    expect(amount).not.toContain('undefined');
  });

  it('handles zero and NaN in token amounts', () => {
    expect(formatTokenAmount('0', 18, 'en', 'ETH')).toContain('0');
    expect(formatTokenAmount('not-a-number', 18, 'en', 'ETH')).toContain('0');
  });

  it('formats compact numbers', () => {
    const compact = formatCompactNumber(1500000, 'en');
    expect(compact).toContain('M');
  });

  it('formats dates', () => {
    const date = formatDate(new Date(2025, 0, 15), 'en');
    expect(date).toBeTruthy();
    expect(date.length).toBeGreaterThan(0);
  });

  it('formats percentages', () => {
    expect(formatPercent(5.5, 'en')).toContain('5');
    expect(formatPercent(-2.3, 'en')).toContain('2');
  });

  it('handles pluralization', () => {
    const zero = plural(0, 'en', { one: '%d item', other: '%d items' });
    const one = plural(1, 'en', { one: '%d item', other: '%d items' });
    const many = plural(5, 'en', { one: '%d item', other: '%d items' });
    expect(typeof zero).toBe('string');
    expect(typeof one).toBe('string');
    expect(typeof many).toBe('string');
  });
});

describe('locale display names', () => {
  it('has a display name for every supported locale', () => {
    for (const locale of SUPPORTED_LOCALES) {
      expect(LOCALE_DISPLAY_NAMES[locale]).toBeTruthy();
    }
  });

  it('Arabic display name is in Arabic script', () => {
    expect(LOCALE_DISPLAY_NAMES.ar).toBe('العربية');
  });
});
