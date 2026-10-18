import React, { act } from "react";
import { createRoot, Root } from "react-dom/client";
import { useRegisterExternalResources } from "../src/hooks";
import i18n from "../src/i18n";
import {
  preloadExternalResources,
  registerExternalResourcePreloader,
} from "../src/resourceBundles/externalResources";
import type { AsyncResources } from "../src/types";

(
  globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }
).IS_REACT_ACT_ENVIRONMENT = true;

describe("external resource preloaders", () => {
  it("registers and unregisters external resource preloaders", async () => {
    const calls: string[] = [];
    const unregister = registerExternalResourcePreloader(async (lang) => {
      calls.push(lang);
    });

    await preloadExternalResources("zh");
    expect(calls).toEqual(["zh"]);

    unregister();

    await preloadExternalResources("ja");
    expect(calls).toEqual(["zh"]);
  });

  it("does not reject when an external preloader fails", async () => {
    const warn = jest.spyOn(console, "warn").mockImplementation(() => {});
    const unregister = registerExternalResourcePreloader(async () => {
      throw new Error("missing locale");
    });

    await expect(preloadExternalResources("zh")).resolves.toBeUndefined();
    expect(warn).toHaveBeenCalled();

    unregister();
    warn.mockRestore();
  });

  it("preloads external resources before changing language", async () => {
    const calls: string[] = [];
    const unregister = registerExternalResourcePreloader(async (lang) => {
      calls.push(`preload:${lang}`);
    });

    await i18n.changeLanguage("zh");

    expect(calls).toEqual(["preload:zh"]);
    expect(i18n.language).toBe("zh");

    unregister();
    await i18n.changeLanguage("en");
  });

  it("does not emit languageChanged until external resources finish preloading", async () => {
    await i18n.changeLanguage("en");

    let resolvePreload: () => void = () => {};
    const preloadPromise = new Promise<void>((resolve) => {
      resolvePreload = resolve;
    });
    const calls: string[] = [];
    const unregister = registerExternalResourcePreloader(async (lang) => {
      calls.push(`preload:${lang}`);
      await preloadPromise;
      calls.push(`preloaded:${lang}`);
    });
    const handleLanguageChanged = (lang: string) => {
      calls.push(`changed:${lang}`);
    };

    i18n.on("languageChanged", handleLanguageChanged);
    const changePromise = i18n.changeLanguage("zh");
    await Promise.resolve();

    expect(calls).toEqual(["preload:zh"]);
    expect(i18n.language).toBe("en");

    resolvePreload();
    await changePromise;

    expect(calls).toEqual(["preload:zh", "preloaded:zh", "changed:zh"]);
    expect(i18n.language).toBe("zh");

    i18n.off("languageChanged", handleLanguageChanged);
    unregister();
    await i18n.changeLanguage("en");
  });

  it("deduplicates pending async resource loads for the same loader and locale", async () => {
    await i18n.changeLanguage("en");

    let resolveLoad: (messages: Record<string, string>) => void = () => {};
    const loadPromise = new Promise<Record<string, string>>((resolve) => {
      resolveLoad = resolve;
    });
    const resources = jest.fn<
      ReturnType<AsyncResources>,
      Parameters<AsyncResources>
    >(() => loadPromise);
    const container = document.createElement("div");
    const root: Root = createRoot(container);

    function TestComponent() {
      useRegisterExternalResources(resources);
      return null;
    }

    await act(async () => {
      root.render(React.createElement(TestComponent));
    });

    const firstPreload = preloadExternalResources("en");
    const secondPreload = preloadExternalResources("en");
    await Promise.resolve();

    expect(resources).toHaveBeenCalledTimes(1);
    expect(resources).toHaveBeenCalledWith("en", "translation");

    resolveLoad({ "test.external": "External" });
    await Promise.all([loadPromise, firstPreload, secondPreload]);

    await act(async () => {
      root.unmount();
    });
  });

  it("retries async resource loads after a failed effect load", async () => {
    await i18n.changeLanguage("en");

    const warn = jest.spyOn(console, "warn").mockImplementation(() => {});
    const resources = jest
      .fn<ReturnType<AsyncResources>, Parameters<AsyncResources>>()
      .mockRejectedValueOnce(new Error("temporary failure"))
      .mockResolvedValueOnce({ "test.external": "External" });
    const container = document.createElement("div");
    const root: Root = createRoot(container);

    function TestComponent() {
      useRegisterExternalResources(resources);
      return null;
    }

    await act(async () => {
      root.render(React.createElement(TestComponent));
    });
    await Promise.resolve();

    expect(resources).toHaveBeenCalledTimes(1);
    expect(warn).toHaveBeenCalled();

    await preloadExternalResources("en");

    expect(resources).toHaveBeenCalledTimes(2);

    await act(async () => {
      root.unmount();
    });
    warn.mockRestore();
  });
});
