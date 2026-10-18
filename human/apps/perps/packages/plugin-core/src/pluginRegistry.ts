import type { PaxeerNetworkPlugin, PaxeerNetworkSDK } from "./types";

/**
 * Global plugin registry for installExtension adapter and SDK.registerPlugin.
 * PaxeerNetworkPluginProvider merges plugins from props with this registry.
 */
const PLUGINS: PaxeerNetworkPlugin[] = [];

export const PaxeerNetworkPluginRegistry = {
  register(descriptor: PaxeerNetworkPlugin): void {
    PLUGINS.push(descriptor);
  },
  getPlugins(): PaxeerNetworkPlugin[] {
    return [...PLUGINS];
  },
  clear(): void {
    PLUGINS.length = 0;
  },
};

/** SDK instance for plugin registration (used by registration fns) */
export const createPaxeerNetworkSDK = (): PaxeerNetworkSDK => ({
  registerPlugin: (descriptor) => PaxeerNetworkPluginRegistry.register(descriptor),
});
