import i18n from "../i18n";
import { LocaleCode } from "../types";

export type BackendOptions = {
  loadPath: (lang: LocaleCode, ns: string) => string | string[] | undefined;
};

export class Backend {
  options: BackendOptions;
  cache: Set<string>;

  constructor(options: BackendOptions) {
    this.options = options;
    this.cache = new Set();
  }

  async fetchData(url: string) {
    try {
      const res = await fetch(url);
      const data = await res.json();
      return data;
    } catch (error) {
      console.error(`Failed to fetch data from ${url}:`, error);
      return {};
    }
  }

  async loadLanguage(lang: LocaleCode, ns: string) {
    if (typeof this.options?.loadPath !== "function") {
      return;
    }

    let paths = this.options.loadPath(lang, ns);

    if (!paths) {
      return;
    }

    if (typeof paths === "string") {
      paths = [paths];
    }

    if (!paths.length) {
      return;
    }

    // filter out the paths that have already been loaded
    const urls = paths.filter((path) => {
      const hasResourceBundle = i18n.hasResourceBundle(lang, ns);

      if (hasResourceBundle && this.cache.has(path)) {
        return false;
      }

      return true;
    });

    const promises = urls.map(async (url) => {
      const data = await this.fetchData(url);
      i18n.addResourceBundle(lang, ns, data, true, true);
      this.cache.add(url);
    });

    await Promise.all(promises);
  }
}
