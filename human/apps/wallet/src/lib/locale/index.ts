export type Locale = 'en' | 'es' | 'fr' | 'de' | 'pt' | 'ar' | 'it' | 'hi' | 'ja' | 'ko';

export const SUPPORTED_LOCALES: readonly Locale[] = [
  'en',
  'es',
  'fr',
  'de',
  'pt',
  'ar',
  'it',
  'hi',
  'ja',
  'ko',
] as const;

export const DEFAULT_LOCALE: Locale = 'en';

const RTL_LOCALES: ReadonlySet<string> = new Set(['ar', 'he', 'fa', 'ur']);

const LEGACY_LOCALE_MAP: Readonly<Record<string, Locale>> = {
  English: 'en',
  Español: 'es',
  Spanish: 'es',
  Français: 'fr',
  French: 'fr',
  Deutsch: 'de',
  German: 'de',
  Português: 'pt',
  Portuguese: 'pt',
  العربية: 'ar',
  Arabic: 'ar',
  Italiano: 'it',
  Italian: 'it',
  हिन्दी: 'hi',
  Hindi: 'hi',
  日本語: 'ja',
  Japanese: 'ja',
  한국어: 'ko',
  Korean: 'ko',
};

export const LOCALE_DISPLAY_NAMES: Readonly<Record<Locale, string>> = {
  en: 'English',
  es: 'Español',
  fr: 'Français',
  de: 'Deutsch',
  pt: 'Português',
  ar: 'العربية',
  it: 'Italiano',
  hi: 'हिन्दी',
  ja: '日本語',
  ko: '한국어',
};

export function isRtlLocale(locale: string): boolean {
  return RTL_LOCALES.has(locale);
}

export function isValidLocale(value: unknown): value is Locale {
  return typeof value === 'string' && (SUPPORTED_LOCALES as readonly string[]).includes(value);
}

export function migrateLocale(legacy: unknown): Locale {
  if (isValidLocale(legacy)) return legacy;
  if (typeof legacy === 'string') {
    const mapped = LEGACY_LOCALE_MAP[legacy];
    if (mapped) return mapped;
  }
  return DEFAULT_LOCALE;
}
