import type { LocaleCode } from "../types";

export type ExternalResourcePreloader = (lang: LocaleCode) => Promise<void>;

const externalResourcePreloaders = new Set<ExternalResourcePreloader>();

export function registerExternalResourcePreloader(
  preloader: ExternalResourcePreloader,
) {
  externalResourcePreloaders.add(preloader);

  return () => {
    externalResourcePreloaders.delete(preloader);
  };
}

export async function preloadExternalResources(lang: LocaleCode) {
  await Promise.all(
    Array.from(externalResourcePreloaders).map(async (preloader) => {
      try {
        await preloader(lang);
      } catch (error) {
        console.warn("[i18n] failed to preload external resources", error);
      }
    }),
  );
}
