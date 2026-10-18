import {
  hasUsableData,
  resolveFallbackData,
} from "../createDataStore.fallback";

describe("createDataStore fallback", () => {
  describe("hasUsableData", () => {
    it("returns true for non-empty arrays", () => {
      expect(hasUsableData([{ id: 1 }])).toBe(true);
    });

    it("returns false for null, undefined, and empty arrays", () => {
      expect(hasUsableData(null)).toBe(false);
      expect(hasUsableData(undefined)).toBe(false);
      expect(hasUsableData([])).toBe(false);
    });
  });

  describe("resolveFallbackData", () => {
    const initChains = [{ chain_id: "init" }] as { chain_id: string }[];
    const cachedChains = [{ chain_id: "cached" }] as { chain_id: string }[];
    const memoryChains = [{ chain_id: "memory" }] as { chain_id: string }[];

    const createStorage = (cached: { chain_id: string }[] | null) => ({
      getItem: jest.fn().mockResolvedValue(cached),
      setItem: jest.fn(),
      removeItem: jest.fn(),
    });

    it("keeps in-memory data when non-empty (API cache)", async () => {
      const storage = createStorage(cachedChains);
      const result = await resolveFallbackData(
        memoryChains,
        initChains,
        storage,
      );
      expect(result).toBe(memoryChains);
      expect(storage.getItem).not.toHaveBeenCalled();
    });

    it("reads IndexedDB when memory is empty", async () => {
      const storage = createStorage(cachedChains);
      const result = await resolveFallbackData([], initChains, storage);
      expect(result).toEqual(cachedChains);
      expect(storage.getItem).toHaveBeenCalled();
    });

    it("falls back to configured data when memory and IDB are empty", async () => {
      const storage = createStorage(null);
      const result = await resolveFallbackData(null, initChains, storage);
      expect(result).toEqual(initChains);
    });

    it("falls back to null when configured", async () => {
      const storage = createStorage(null);
      const result = await resolveFallbackData(null, null, storage);
      expect(result).toBe(null);
    });

    it("keeps current data when no fallback and no cache", async () => {
      const storage = createStorage(null);
      const result = await resolveFallbackData([], undefined, storage);
      expect(result).toEqual([]);
    });

    it("falls back to configured data when IndexedDB read fails", async () => {
      const storage = {
        getItem: jest.fn().mockRejectedValue(new Error("idb error")),
        setItem: jest.fn(),
        removeItem: jest.fn(),
      };
      const result = await resolveFallbackData(null, initChains, storage);
      expect(result).toEqual(initChains);
    });

    it("ignores invalid in-memory and IndexedDB data", async () => {
      const storage = createStorage([{ chain_id: "" }]);
      const sanitizer = (data: unknown) =>
        Array.isArray(data) &&
        data.length > 0 &&
        data.every((item) => !!item?.chain_id)
          ? (data as { chain_id: string }[])
          : null;

      const result = await resolveFallbackData(
        [{ chain_id: "" }],
        initChains,
        storage,
        sanitizer,
      );

      expect(result).toBe(initChains);
      expect(storage.getItem).toHaveBeenCalled();
    });
  });
});
