import { defaultLng, defaultNS } from "../constant";
import i18n from "../i18n";

/**
 * Register the default language bundle before your React tree renders to reduce
 * flicker of raw translation keys.
 */
export const registerDefaultResource = (messages: Record<string, string>) => {
  i18n.addResourceBundle(defaultLng, defaultNS, messages, true, true);
};
