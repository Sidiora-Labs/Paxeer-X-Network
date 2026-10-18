import {
  adaptToStateStorage,
  readAttributedIndexedDBState,
} from "../persistIndexedDB";

const metadataConfig = {
  keyPath: "id",
  recordKey: "__metadata__",
  metadataField: "__source__",
};

describe("adaptToStateStorage", () => {
  const createStorage = () => ({
    getItem: jest.fn(),
    setItem: jest.fn().mockResolvedValue(undefined),
    removeItem: jest.fn().mockResolvedValue(undefined),
  });

  describe("setItem", () => {
    it("replaces data when state is an array", async () => {
      const storage = createStorage();
      const adapter = adaptToStateStorage(storage);

      await adapter.setItem(
        "paxeer-network-store",
        JSON.stringify({ state: [{ id: 1 }], version: 0 }),
      );

      expect(storage.setItem).toHaveBeenCalledWith([{ id: 1 }]);
      expect(storage.removeItem).not.toHaveBeenCalled();
    });

    it("clears storage when state is null", async () => {
      const storage = createStorage();
      const adapter = adaptToStateStorage(storage);

      await adapter.setItem(
        "paxeer-network-store",
        JSON.stringify({ state: null, version: 0 }),
      );

      expect(storage.removeItem).toHaveBeenCalled();
      expect(storage.setItem).not.toHaveBeenCalled();
    });

    it("skips the write when the state key is absent (error-path fallback)", async () => {
      const storage = createStorage();
      const adapter = adaptToStateStorage(storage);

      // zustand's JSON.stringify drops undefined values, so a partialized
      // state of undefined produces a payload without the `state` key
      await adapter.setItem("paxeer-network-store", JSON.stringify({ version: 0 }));

      expect(storage.setItem).not.toHaveBeenCalled();
      expect(storage.removeItem).not.toHaveBeenCalled();
    });

    it("writes data and source metadata atomically in attributed mode", async () => {
      const storage = createStorage();
      const adapter = adaptToStateStorage(storage, metadataConfig);

      await adapter.setItem(
        "paxeer-network-store",
        JSON.stringify({
          state: {
            data: [{ id: "chain" }],
            metadata: { dataOrigin: "broker" },
          },
          version: 0,
        }),
      );

      expect(storage.setItem).toHaveBeenCalledWith([
        { id: "chain" },
        {
          id: "__metadata__",
          __source__: { dataOrigin: "broker" },
        },
      ]);
    });
  });

  describe("attributed reads", () => {
    it("filters the metadata record from hydrated data", async () => {
      const storage = createStorage();
      storage.getItem.mockResolvedValue([
        { id: "chain" },
        {
          id: "__metadata__",
          __source__: { dataOrigin: "broker" },
        },
      ]);

      await expect(
        readAttributedIndexedDBState(storage, metadataConfig),
      ).resolves.toEqual({
        data: [{ id: "chain" }],
        metadata: { dataOrigin: "broker" },
      });
    });

    it("ignores legacy rows without source metadata", async () => {
      const storage = createStorage();
      storage.getItem.mockResolvedValue([{ id: "legacy" }]);

      await expect(
        readAttributedIndexedDBState(storage, metadataConfig),
      ).resolves.toBeNull();
    });
  });
});
