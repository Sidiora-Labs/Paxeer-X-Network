/**
 * Jest setup: isolate PaxeerNetworkExtensionRegistry singleton per test
 * by mocking getGlobalObject to a shared object we clear in tests that use the registry.
 * Mock @paxeer-network/core to avoid loading @solana/web3.js (ESM) in Jest.
 */
jest.mock("@paxeer-network/core", () => ({
  SimpleDI: { get: () => null },
  EventEmitter: jest.fn(),
}));

const __globalStorage: Record<string, unknown> = {};

jest.mock("@paxeer-network/utils", () => ({
  getGlobalObject: () => __globalStorage,
}));

/** Call in beforeEach to get a fresh PaxeerNetworkExtensionRegistry in the next getInstance() */
export function resetExtensionRegistry(): void {
  delete (__globalStorage as Record<string, unknown>)
    .__PAXEER_NETWORK_EXTENSION_REGISTRY__;
}
