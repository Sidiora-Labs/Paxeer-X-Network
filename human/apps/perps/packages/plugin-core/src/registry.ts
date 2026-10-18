import { getGlobalObject } from "@paxeer-network/utils";
import { Extension, ExtensionBuilder, ExtensionPosition } from "./types";

/**
 * Legacy extension registry (position-based, for installExtension backward compat).
 * @deprecated Use registerPlugin via PaxeerNetworkPluginProvider plugins prop instead
 */
export class PaxeerNetworkExtensionRegistry {
  static getInstance(): PaxeerNetworkExtensionRegistry {
    const globalObject = getGlobalObject();
    if (!(globalObject as any).__PAXEER_NETWORK_EXTENSION_REGISTRY__) {
      (globalObject as any).__PAXEER_NETWORK_EXTENSION_REGISTRY__ =
        new PaxeerNetworkExtensionRegistry();
    }
    return (globalObject as any).__PAXEER_NETWORK_EXTENSION_REGISTRY__;
  }

  private extensionMap: Map<
    ExtensionPosition,
    Extension<Record<string, unknown>>
  > = new Map();

  register<Props extends object>(
    plugin: Omit<Extension<Props>, "builder"> & {
      builder?: (props: any) => Props;
    },
  ) {
    for (let index = 0; index < plugin.positions.length; index++) {
      if (typeof plugin.builder !== "function") {
        const builder = this.extensionMap.get(plugin.positions[index])?.builder;
        plugin.builder =
          typeof builder === "undefined"
            ? undefined
            : (builder as (props?: any) => Props);
      }
      const pos = plugin.positions[index];
      this.registerToPosition<Props>(pos, plugin as Extension<Props>);
    }
  }

  private registerToPosition<Props extends object>(
    position: ExtensionPosition,
    plugin: Extension<Props>,
  ) {
    if (this.extensionMap.has(position)) {
      const existingPlugin = this.extensionMap.get(position);
      if (!existingPlugin?.__isInternal) {
        if (!existingPlugin?.builder && plugin.__isInternal && plugin.builder) {
          this.setBuilder(position, plugin.builder);
        }
        return;
      }
      if (!plugin.builder) {
        plugin.builder = existingPlugin.builder as ExtensionBuilder<Props>;
      }
    }
    this.extensionMap.set(position, plugin as Extension<any>);
  }

  setBuilder<Props extends object>(
    position: ExtensionPosition,
    builder: ExtensionBuilder<Props>,
  ) {
    const plugin = this.extensionMap.get(position);
    if (plugin) {
      plugin.builder = builder as ExtensionBuilder<Record<string, unknown>>;
    }
  }

  getPluginsByPosition(position: ExtensionPosition) {
    return this.extensionMap.get(position);
  }
}
