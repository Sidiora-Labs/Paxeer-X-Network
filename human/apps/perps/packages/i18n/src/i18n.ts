// import i18n from "i18next";
// import { initReactI18next } from "react-i18next";
import { createInstance, type Callback, type InitOptions } from "i18next";
import LanguageDetector from "i18next-browser-languagedetector";
import {
  defaultLng,
  defaultNS,
  i18nCookieKey,
  i18nLocalStorageKey,
  LocaleEnum,
} from "./constant";
import { en } from "./locale/en";
import { preloadExternalResources } from "./resourceBundles/externalResources";
import type { LocaleCode } from "./types";

export const resources = {
  [defaultLng]: { [defaultNS]: en },
};

const languageDetector = new LanguageDetector(null, {
  lookupLocalStorage: i18nLocalStorageKey,
  lookupCookie: i18nCookieKey,
  caches: ["localStorage", "cookie"],
});

// i18n
//   // passes i18n down to react-i18next
//   .use(initReactI18next)
//   .init({
//     // the translations
//     resources,
//     // language to use, more information here: https://www.i18next.com/overview/configuration-options#languages-namespaces-resources
//     // you can use the i18n.changeLanguage function to change the language manually: https://www.i18next.com/overview/api#changelanguage
//     // if you're using a language detector, do not define the lng option
//     lng: "en",
//     fallbackLng: "en",
//     interpolation: {
//       // react already safes from xss => https://www.i18next.com/translation-function/interpolation#unescape
//       escapeValue: false,
//     },
//   });

export function createI18nInstance(options?: InitOptions) {
  return createInstance({
    // lng: defaultLng,
    fallbackLng: defaultLng,
    // debug: true,
    interpolation: {
      escapeValue: false,
    },
    ...options,
  }).use(languageDetector);
}

// https://react.i18next.com/latest/i18nextprovider#when-to-use
const i18n = createI18nInstance({
  resources,
});

const parseChangeLanguageTarget = (lang?: string): LocaleCode | undefined => {
  if (!lang) {
    return undefined;
  }

  const localeCodes = Object.values(LocaleEnum);

  if (localeCodes.includes(lang as LocaleEnum)) {
    return lang as LocaleCode;
  }

  const match = lang.match(/^([a-z]{2})/i);

  if (match && localeCodes.includes(match[1] as LocaleEnum)) {
    return match[1] as LocaleCode;
  }

  return lang as LocaleCode;
};

const originalChangeLanguage = i18n.changeLanguage.bind(i18n);

i18n.changeLanguage = (async (lang?: string, callback?: Callback) => {
  const localeCode = parseChangeLanguageTarget(lang);

  if (localeCode) {
    await preloadExternalResources(localeCode);
  }

  return originalChangeLanguage(lang, callback);
}) as typeof i18n.changeLanguage;

i18n.init();

export default i18n;
