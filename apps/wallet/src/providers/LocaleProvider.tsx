'use client';

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react';
import {
  DEFAULT_LOCALE,
  isRtlLocale,
  isValidLocale,
  LOCALE_DISPLAY_NAMES,
  migrateLocale,
  SUPPORTED_LOCALES,
  type Locale,
} from '@/lib/locale';
import { getMessages, type MessageCatalog } from '@/lib/locale/messages';
import {
  formatCurrency,
  formatDate,
  formatNumber,
  formatTokenAmount,
  formatCompactNumber,
  formatTime,
  formatRelativeTime,
  formatPercent,
  plural,
} from '@/lib/locale/formatters';
import { preferencesRepository } from '@/platform/storage/repositories';
import { CURRENCY_RATES_EVENT } from '@/lib/currency';
import {
  getPriorityMessages,
  type PriorityMessages,
} from '@/lib/locale/priority-messages';

export type { Locale, MessageCatalog };

interface LocaleContextValue {
  readonly locale: Locale;
  readonly messages: MessageCatalog;
  readonly t: MessageCatalog;
  readonly p: PriorityMessages;
  readonly dir: 'ltr' | 'rtl';
  readonly setLocale: (locale: Locale) => void;
  readonly supportedLocales: readonly Locale[];
  readonly localeDisplayName: (locale: Locale) => string;
  readonly formatNumber: (value: number, options?: Intl.NumberFormatOptions) => string;
  readonly formatCurrency: (value: number, currency?: string) => string;
  readonly formatTokenAmount: (value: string | number, decimals: number, symbol?: string) => string;
  readonly formatCompactNumber: (value: number) => string;
  readonly formatDate: (value: number | Date, options?: Intl.DateTimeFormatOptions) => string;
  readonly formatTime: (value: number | Date) => string;
  readonly formatRelativeTime: (value: number, unit?: Intl.RelativeTimeFormatUnit) => string;
  readonly formatPercent: (value: number, fractionDigits?: number) => string;
  readonly plural: (
    count: number,
    forms: { zero?: string; one: string; two?: string; few?: string; many?: string; other: string },
  ) => string;
}

const LocaleContext = createContext<LocaleContextValue | null>(null);

function readInitialLocale(): Locale {
  try {
    const stored = preferencesRepository.read();
    return migrateLocale(stored.language);
  } catch {
    return DEFAULT_LOCALE;
  }
}

export function LocaleProvider({ children }: { children: ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>(readInitialLocale);
  const [, setCurrencyRevision] = useState(0);

  const dir = isRtlLocale(locale) ? 'rtl' : 'ltr';

  useEffect(() => {
    const root = document.documentElement;
    root.lang = locale;
    root.dir = dir;
    return () => {
      root.lang = DEFAULT_LOCALE;
      root.dir = 'ltr';
    };
  }, [locale, dir]);

  useEffect(() => {
    const refresh = () => setCurrencyRevision((value) => value + 1);
    window.addEventListener(CURRENCY_RATES_EVENT, refresh);
    return () => window.removeEventListener(CURRENCY_RATES_EVENT, refresh);
  }, []);

  const setLocale = useCallback((next: Locale) => {
    if (!isValidLocale(next)) return;
    setLocaleState(next);
    try {
      const current = preferencesRepository.read();
      preferencesRepository.write({ ...current, language: next });
    } catch {
      // preference write failure is non-fatal; locale remains active in session
    }
  }, []);

  const ctx = useMemo<LocaleContextValue>(() => {
    const messages = getMessages(locale);
    const tag = locale;
    return {
      locale,
      messages,
      t: messages,
      p: getPriorityMessages(locale),
      dir,
      setLocale,
      supportedLocales: SUPPORTED_LOCALES,
      localeDisplayName: (l: Locale) => LOCALE_DISPLAY_NAMES[l] ?? l,
      formatNumber: (value, options) => formatNumber(value, tag, options),
      formatCurrency: (value, currency) => formatCurrency(value, tag, currency),
      formatTokenAmount: (value, decimals, symbol) =>
        formatTokenAmount(value, decimals, tag, symbol),
      formatCompactNumber: (value) => formatCompactNumber(value, tag),
      formatDate: (value, options) => formatDate(value, tag, options),
      formatTime: (value) => formatTime(value, tag),
      formatRelativeTime: (value, unit) => formatRelativeTime(value, tag, unit),
      formatPercent: (value, fractionDigits) => formatPercent(value, tag, fractionDigits),
      plural: (count, forms) => plural(count, tag, forms),
    };
  }, [locale, dir, setLocale]);

  return <LocaleContext.Provider value={ctx}>{children}</LocaleContext.Provider>;
}

export function useLocale(): LocaleContextValue {
  const ctx = useContext(LocaleContext);
  if (!ctx) throw new Error('useLocale must be used within LocaleProvider');
  return ctx;
}
