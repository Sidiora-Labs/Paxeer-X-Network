import {
  LanguageSwitcherScriptOptions,
  useLanguageSwitcherScript,
} from "./languageSwitcher.script";
import { LanguageSwitcher } from "./languageSwitcher.ui";

export type LanguageSwitcherWidgetProps = LanguageSwitcherScriptOptions;

export const LanguageSwitcherWidget = (props: LanguageSwitcherWidgetProps) => {
  const state = useLanguageSwitcherScript(props);
  return <LanguageSwitcher {...state} />;
};
