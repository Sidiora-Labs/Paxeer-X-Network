import { defaultNS } from "../constant";
import i18n from "../i18n";
import type { AsyncResources, LocaleCode, Resources } from "../types";

export async function registerResources(
  resources: Resources | AsyncResources | undefined,
  localeCode: LocaleCode,
) {
  if (typeof resources === "function") {
    const resource = await resources(localeCode, defaultNS);
    i18n.addResourceBundle(localeCode, defaultNS, resource, true, true);
    return;
  }
  if (resources) {
    Object.entries(resources).forEach(([locale, messages]) => {
      i18n.addResourceBundle(locale, defaultNS, messages, true, true);
    });
  }
}
