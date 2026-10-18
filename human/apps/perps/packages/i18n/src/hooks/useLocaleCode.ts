import { useEffect, useState } from "react";
import i18n from "../i18n";
import { LocaleCode } from "../types";
import { parseI18nLang } from "../utils";

export function useLocaleCode() {
  const [localeCode, setLocaleCode] = useState<LocaleCode>(
    parseI18nLang(i18n.language),
  );

  useEffect(() => {
    const handleLanguageChange = (lng: LocaleCode) => {
      setLocaleCode(lng);
    };

    i18n.on("languageChanged", handleLanguageChange);

    return () => {
      i18n.off("languageChanged", handleLanguageChange);
    };
  }, [i18n]);

  return localeCode;
}
