import {
  type FC,
  type PropsWithChildren,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { defaultLanguages, defaultNS } from "../constant";
import i18n from "../i18n";
import { Backend, BackendOptions } from "../resourceBundles";
import { LocaleCode } from "../types";
import { parseI18nLang, syncDocumentDirection } from "../utils";
import {
  Language,
  LanguageContext,
  LanguageContextState,
} from "./languageContext";

export type LanguageProviderProps = PropsWithChildren<
  {
    backend?: BackendOptions;
    /**
     * supported languages, you can select supported languages from default languages
     */
    supportedLanguages?: LocaleCode[];
    /** optional conversion function to use to modify the detected language code */
    convertDetectedLanguage?: (lang: string) => LocaleCode;
  } & Partial<LanguageContextState>
>;

export const LanguageProvider: FC<LanguageProviderProps> = (props) => {
  const {
    children,
    backend,
    popup,
    supportedLanguages,
    onLanguageChanged,
    convertDetectedLanguage,
    onLanguageBeforeChanged,
  } = props;

  const [languages, setLanguages] = useState<Language[]>(defaultLanguages);

  const backendRef = useRef(new Backend(backend!));

  useEffect(() => {
    if (Array.isArray(props.languages)) {
      setLanguages(props.languages);
    } else if (Array.isArray(supportedLanguages)) {
      setLanguages(
        supportedLanguages
          .map((localCode) =>
            defaultLanguages.find((l) => l.localCode === localCode),
          )
          .filter((item) => !!item),
      );
    }
  }, [props.languages, supportedLanguages]);

  useEffect(() => {
    // init language when refresh page
    const initLanguage = async () => {
      const lang =
        typeof convertDetectedLanguage === "function"
          ? convertDetectedLanguage(i18n.language)
          : parseI18nLang(i18n.language);
      await backendRef.current.loadLanguage(lang, defaultNS);
      // if browser language is not a valid language, change language
      if (lang !== i18n.language) {
        await i18n.changeLanguage(lang);
      }
    };

    initLanguage();
  }, [i18n.language]);

  useEffect(() => {
    syncDocumentDirection(i18n.language);

    const handleLanguageChange = (lang: string) => {
      syncDocumentDirection(lang);
    };

    i18n.on("languageChanged", handleLanguageChange);

    return () => {
      i18n.off("languageChanged", handleLanguageChange);
    };
  }, []);

  const languageBeforeChangedHandle = useCallback(
    async (lang: LocaleCode) => {
      await onLanguageBeforeChanged?.(lang);
      // load language when language before changed
      await backendRef.current.loadLanguage(lang, defaultNS);
    },
    [onLanguageBeforeChanged],
  );

  const languageChangedHandle = useCallback(
    async (lang: LocaleCode) => {
      onLanguageChanged?.(lang);
    },
    [onLanguageChanged],
  );

  const memoizedValue = useMemo<LanguageContextState>(() => {
    return {
      popup: popup,
      languages: languages,
      onLanguageBeforeChanged: languageBeforeChangedHandle,
      onLanguageChanged: languageChangedHandle,
    };
  }, [popup, languages, languageBeforeChangedHandle, languageChangedHandle]);

  return (
    <LanguageContext.Provider value={memoizedValue}>
      {children}
    </LanguageContext.Provider>
  );
};
