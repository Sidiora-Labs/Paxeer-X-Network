import { createContext, useContext } from "react";
import { LocaleCode } from "../types";

export type Language = {
  localCode: LocaleCode;
  displayName: string;
};

export type PopupMode = "modal" | "dropdown" | "sheet";
export type PopupProps = {
  /**
   * popup mode
   * - modal: show in a dialog
   * - dropdown: show in a dropdown menu
   * - sheet: show in a sheet (only for mobile)
   * @default modal
   */
  mode?: PopupMode;
  /** popup class name */
  className?: string;
  /** popup style */
  style?: React.CSSProperties;
};

export type LanguageContextState = {
  /**
   * custom languages
   */
  languages: Language[];
  /**
   * called before language changed
   */
  onLanguageBeforeChanged: (lang: LocaleCode) => Promise<void>;
  /**
   * called when language changed
   */
  onLanguageChanged: (lang: LocaleCode) => Promise<void>;
  /**
   * popup options for language switcher
   */
  popup?: PopupProps;
};

export const LanguageContext = createContext<LanguageContextState>({
  languages: [],
  onLanguageBeforeChanged: () => Promise.resolve(),
  onLanguageChanged: () => Promise.resolve(),
});

export const useLanguageContext = () => {
  return useContext(LanguageContext);
};
