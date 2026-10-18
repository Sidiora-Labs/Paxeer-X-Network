import { useMemo } from "react";
import {
  arSA,
  enUS,
  zhCN,
  faIR,
  he,
  ja,
  es,
  ko,
  vi,
  de,
  fr,
  nl,
} from "date-fns/locale";
import {
  useTranslation,
  useLocaleCode,
  LocaleEnum,
} from "@paxeer-network/i18n";
import { Locale } from "@paxeer-network/ui";

export function useUILocale() {
  const { t } = useTranslation();
  const localeCode = useLocaleCode();

  return useMemo<Locale>(() => {
    const calendarLocale = {
      [LocaleEnum.en]: enUS,
      [LocaleEnum.ar]: arSA,
      [LocaleEnum.fa]: faIR,
      [LocaleEnum.he]: he,
      [LocaleEnum.zh]: zhCN,
      // [LocaleEnum.ja]: ja,
      // [LocaleEnum.es]: es,
      // [LocaleEnum.ko]: ko,
      // [LocaleEnum.vi]: vi,
      // [LocaleEnum.de]: de,
      // [LocaleEnum.fr]: fr,
      // [LocaleEnum.nl]: nl,
    };
    return {
      locale: localeCode,
      dialog: {
        ok: t("common.ok"),
        cancel: t("common.cancel"),
      },
      modal: {
        confirm: t("common.confirm"),
        cancel: t("common.cancel"),
      },
      pagination: {
        morePages: t("ui.pagination.morePages"),
        rowsPerPage: t("ui.pagination.rowsPerPage"),
      },
      picker: {
        selectDate: t("ui.picker.selectDate"),
        dayPicker: calendarLocale[localeCode as keyof typeof calendarLocale],
      },
      empty: {
        description: t("ui.empty.description"),
      },
    } as const;
  }, [t, localeCode]);
}
