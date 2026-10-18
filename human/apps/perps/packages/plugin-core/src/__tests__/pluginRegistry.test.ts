import { PaxeerNetworkPluginRegistry, createPaxeerNetworkSDK } from "../pluginRegistry";
import type { PaxeerNetworkPlugin, PaxeerNetworkSDK } from "../types";

describe("PaxeerNetworkPluginRegistry", () => {
  afterEach(() => {
    PaxeerNetworkPluginRegistry.clear();
  });

  it("starts empty", () => {
    expect(PaxeerNetworkPluginRegistry.getPlugins()).toEqual([]);
  });

  it("registers a plugin and returns a copy via getPlugins", () => {
    const plugin: PaxeerNetworkPlugin = {
      id: "test-plugin",
      name: "Test",
    };
    PaxeerNetworkPluginRegistry.register(plugin);
    const list = PaxeerNetworkPluginRegistry.getPlugins();
    expect(list).toHaveLength(1);
    expect(list[0]).toBe(plugin);
    expect(list).not.toBe(PaxeerNetworkPluginRegistry.getPlugins());
  });

  it("accumulates multiple plugins", () => {
    PaxeerNetworkPluginRegistry.register({ id: "a" });
    PaxeerNetworkPluginRegistry.register({ id: "b" });
    expect(PaxeerNetworkPluginRegistry.getPlugins().map((p) => p.id)).toEqual([
      "a",
      "b",
    ]);
  });

  it("clear removes all plugins", () => {
    PaxeerNetworkPluginRegistry.register({ id: "x" });
    PaxeerNetworkPluginRegistry.clear();
    expect(PaxeerNetworkPluginRegistry.getPlugins()).toEqual([]);
  });
});

describe("createPaxeerNetworkSDK", () => {
  afterEach(() => {
    PaxeerNetworkPluginRegistry.clear();
  });

  it("returns SDK with registerPlugin that registers to PaxeerNetworkPluginRegistry", () => {
    const sdk: PaxeerNetworkSDK = createPaxeerNetworkSDK();
    const plugin: PaxeerNetworkPlugin = { id: "sdk-plugin", name: "SDK" };
    sdk.registerPlugin(plugin);
    expect(PaxeerNetworkPluginRegistry.getPlugins()).toContain(plugin);
  });
});
