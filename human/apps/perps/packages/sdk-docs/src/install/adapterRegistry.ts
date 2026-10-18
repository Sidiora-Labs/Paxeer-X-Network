import type {
  ClientAdapter,
  ClientAdapterRegistry,
  InstallClient,
} from "./types.js";

/** Creates a mutable adapter registry so install targets can be extended safely. */
export function createClientAdapterRegistry(
  initialAdapters: ClientAdapter[] = [],
): ClientAdapterRegistry {
  const adapters = new Map<InstallClient, ClientAdapter>();

  for (const adapter of initialAdapters) {
    adapters.set(adapter.client, adapter);
  }

  return {
    get(client: InstallClient): ClientAdapter | undefined {
      return adapters.get(client);
    },
    register(adapter: ClientAdapter): void {
      adapters.set(adapter.client, adapter);
    },
    list(): ClientAdapter[] {
      return Array.from(adapters.values());
    },
  };
}
