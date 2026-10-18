import { useCallback, useMemo, useState } from "react";
import { useTrack } from "@paxeer-network/hooks";
import {
  i18n,
  LanguageContextState,
  useLanguageContext,
} from "@paxeer-network/i18n";
import { TrackerEventName } from "@paxeer-network/types";
import { useScreen } from "@paxeer-network/ui";

export type LanguageSwitcherScriptReturn = ReturnType<
  typeof useLanguageSwitcherScript
>;

export type LanguageSwitcherScriptOptions = Pick<
  LanguageContextState,
  "popup"
> & { open?: boolean; onOpenChange?: (open: boolean) => void };

export const useLanguageSwitcherScript = (
  options?: LanguageSwitcherScriptOptions,
) => {
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [selectedLang, setSelectedLang] = useState(i18n.language);
  const { languages, onLanguageBeforeChanged, onLanguageChanged, popup } =
    useLanguageContext();

  const { track, setIdentify } = useTrack();

  const { isMobile } = useScreen();

  const onOpenChange = useCallback(
    (open: boolean) => {
      if (typeof options?.onOpenChange === "function") {
        options.onOpenChange(open);
      } else {
        setOpen(open);
      }
    },
    [options?.onOpenChange, setOpen],
  );

  const onLangChange = async (lang: string, displayName: string) => {
    setLoading(true);
    setSelectedLang(lang);
    await onLanguageBeforeChanged(lang);
    await i18n.changeLanguage(lang);
    await onLanguageChanged(lang);
    onOpenChange(false);
    setLoading(false);
    track(TrackerEventName.switchLanguage, {
      language: displayName,
      language_code: lang,
    });

    setIdentify({
      language_code: lang,
    });
  };

  const _popup = useMemo(
    () => ({
      ...popup,
      ...options?.popup,
      mode:
        options?.popup?.mode || popup?.mode || (isMobile ? "sheet" : "modal"),
    }),
    [popup, options?.popup, isMobile],
  );

  const _open = useMemo(() => {
    if (typeof options?.open === "boolean") {
      return options.open;
    }
    return open;
  }, [options?.open, open]);

  return {
    open: _open,
    onOpenChange,
    languages,
    selectedLang,
    onLangChange,
    loading,
    popup: _popup,
  };
};
