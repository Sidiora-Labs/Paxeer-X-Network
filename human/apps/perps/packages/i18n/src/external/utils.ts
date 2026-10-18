import { defaultLng, defaultNS } from "../constant";
import i18n from "../i18n";

/**
 * preload default locale resources to prevent flickering of raw translation keys
 */
export const preloadDefaultResource = (messages: Record<string, string>) => {
  i18n.addResourceBundle(defaultLng, defaultNS, messages, true, true);
};
