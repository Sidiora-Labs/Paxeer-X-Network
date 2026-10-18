import { Language } from "./provider/languageContext";

export enum LocaleEnum {
  /** English */
  en = "en",
  /** Arabic */
  ar = "ar",
  /** Persian (Farsi) */
  fa = "fa",
  /** Hebrew */
  he = "he",
  /** Chinese */
  zh = "zh",
  /** Japanese */
  ja = "ja",
  /** Spanish */
  es = "es",
  /** Korean */
  ko = "ko",
  /** Vietnamese */
  vi = "vi",
  /** German */
  de = "de",
  /** French */
  fr = "fr",
  /** Russian */
  ru = "ru",
  /** Indonesian */
  id = "id",
  /** Turkish */
  tr = "tr",
  /** Italian */
  it = "it",
  /** Portuguese */
  pt = "pt",
  /** Ukrainian */
  uk = "uk",
  /** Polish */
  pl = "pl",
  /** Dutch */
  nl = "nl",
  /** Traditional Chinese */
  tc = "tc",
}

export const defaultLanguages: Language[] = [
  { localCode: LocaleEnum.en, displayName: "English" }, // English
  { localCode: LocaleEnum.ar, displayName: "العربية" }, // Arabic
  { localCode: LocaleEnum.fa, displayName: "فارسی" }, // Persian
  { localCode: LocaleEnum.he, displayName: "עברית" }, // Hebrew
  { localCode: LocaleEnum.zh, displayName: "中文" }, // Chinese
  { localCode: LocaleEnum.ja, displayName: "日本語" }, // Japanese
  { localCode: LocaleEnum.es, displayName: "Español" }, // Spanish
  { localCode: LocaleEnum.ko, displayName: "한국어" }, // Korean
  { localCode: LocaleEnum.vi, displayName: "Tiếng Việt" }, // Vietnamese
  { localCode: LocaleEnum.de, displayName: "Deutsch" }, // German
  { localCode: LocaleEnum.fr, displayName: "Français" }, // French
  { localCode: LocaleEnum.ru, displayName: "Русский" }, // Russian
  { localCode: LocaleEnum.id, displayName: "Bahasa Indonesia" }, // Indonesian
  { localCode: LocaleEnum.tr, displayName: "Türkçe" }, // Turkish
  { localCode: LocaleEnum.it, displayName: "Italiano" }, // Italian
  { localCode: LocaleEnum.pt, displayName: "Português" }, // Portuguese
  { localCode: LocaleEnum.uk, displayName: "Українська" }, // Ukrainian
  { localCode: LocaleEnum.pl, displayName: "Polski" }, // Polish
  { localCode: LocaleEnum.nl, displayName: "Nederlands" }, // Dutch
  { localCode: LocaleEnum.tc, displayName: "繁體中文" }, // Traditional Chinese
];
export const defaultLng = LocaleEnum.en;
export const defaultNS = "translation";

export const i18nLocalStorageKey = "paxeerNetwork_i18nLng";

// preferred-language
export const i18nCookieKey = "paxeerNetwork_i18nLng";
