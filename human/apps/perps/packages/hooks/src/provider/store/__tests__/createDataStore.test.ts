import { fetcher } from "../../../utils/fetcher";
import { createDataStore } from "../createDataStore";
import type { DataStoreConfig } from "../createDataStore";

jest.mock("ramda/es/pathOr", () => ({
  __esModule: true,
  default: (def: any, path: string[], obj: any) =>
    path.reduce((acc: any, key) => (acc == null ? def : acc[key]), obj),
}));
jest.mock("../../../utils/fetcher", () => ({ fetcher: jest.fn() }));

let mockDB: any;
jest.mock("../../../middleware/indexedDBManager", () => ({
  indexedDBManager: {
    getConnection: async () => mockDB,
  },
}));

const mockFetcher = fetcher as jest.Mock;

/**
 * Minimal fake IDBDatabase backed by an in-memory array. Emulates the
 * transaction/request callback flow expected by createIndexedDBStorage.
 */
const createFakeDB = (initialRows: any[] = []) => {
  let rows: any[] = [...initialRows];
  return {
    transaction: () => {
      const tx: any = {
        objectStore: () => ({
          getAll: () => {
            const req: any = {};
            Promise.resolve().then(() => {
              req.result = [...rows];
              req.onsuccess?.();
            });
            return req;
          },
          clear: () => {
            rows = [];
          },
          put: (item: any) => {
            rows.push(item);
          },
        }),
      };
      Promise.resolve().then(() => tx.oncomplete?.());
      return tx;
    },
    getRows: () => rows,
  };
};

const createStore = (
  initData?: any[] | null,
  fallbackData?: any[] | null,
  config: Partial<DataStoreConfig<any>> = {},
) =>
  createDataStore<any>({
    name: "test-store",
    dbName: "PAXEER_NETWORK_TEST_STORE",
    storeName: "TEST_STORE",
    keyPath: "id",
    endpoint: "/v1/public/test",
    initData,
    fallbackData,
    ...config,
  });

const sanitizeRows = (data: unknown): any[] | null => {
  if (!Array.isArray(data)) {
    return null;
  }
  const rows = data.filter(
    (item) => typeof item?.id === "string" && item.id.length > 0,
  );
  return rows.length > 0 ? rows : null;
};

const dataOriginMetadata = (dataOrigin: "broker" | "generic") => ({
  id: "__paxeerNetwork_data_origin_metadata__",
  __paxeerNetwork_data_origin__: { dataOrigin },
});

const createDeferred = <T>() => {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((promiseResolve, promiseReject) => {
    resolve = promiseResolve;
    reject = promiseReject;
  });
  return { promise, reject, resolve };
};

/**
 * Fake IndexedDB whose first read snapshots the cache but waits until released.
 * Later transactions continue normally so a fetch can persist before the stale
 * hydration snapshot is delivered.
 */
const createDelayedHydrationDB = (
  initialRows: any[] = [],
  subsequentReadRows?: any[],
) => {
  let rows: any[] = [...initialRows];
  let delayNextRead = true;
  const hydrationStarted = createDeferred<void>();
  const hydrationRelease = createDeferred<void>();

  const db = {
    transaction: () => {
      const tx: any = {
        objectStore: () => ({
          getAll: () => {
            const req: any = {};
            const isHydrationRead = delayNextRead;
            const snapshot =
              !isHydrationRead && subsequentReadRows
                ? [...subsequentReadRows]
                : [...rows];
            const completeRead = () => {
              req.result = snapshot;
              req.onsuccess?.();
            };

            if (delayNextRead) {
              delayNextRead = false;
              hydrationStarted.resolve(undefined);
              void hydrationRelease.promise.then(completeRead);
            } else {
              Promise.resolve().then(completeRead);
            }
            return req;
          },
          clear: () => {
            rows = [];
          },
          put: (item: any) => {
            rows.push(item);
          },
        }),
      };
      Promise.resolve().then(() => tx.oncomplete?.());
      return tx;
    },
    getRows: () => rows,
  };

  return {
    db,
    hydrationStarted: hydrationStarted.promise,
    releaseHydration: () => hydrationRelease.resolve(undefined),
  };
};

const awaitHydration = async (store: any) => {
  for (let i = 0; i < 100 && !store.getState().hydrated; i++) {
    await new Promise((resolve) => setTimeout(resolve, 0));
  }
  expect(store.getState().hydrated).toBe(true);
};

/** Lets the async IndexedDB write chain flushed by persist middleware settle */
const flushWrites = async () => {
  await new Promise((resolve) => setTimeout(resolve, 0));
};

describe("createDataStore persistence", () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockDB = createFakeDB();
  });

  it("does not persist initData to IndexedDB when the first fetch fails", async () => {
    mockFetcher.mockRejectedValue(new Error("network down"));
    const initData = [{ id: "init" }];
    const store = createStore(initData);

    await awaitHydration(store);
    await store.getState().fetchData();
    await flushWrites();

    expect(store.getState().error).toBeInstanceOf(Error);
    expect(store.getState().data).toEqual(initData);
    expect(mockDB.getRows()).toEqual([]);
  });

  it("does not expose fallbackData before a fetch fails", async () => {
    const fallbackData = [{ id: "fallback" }];
    const store = createStore(null, fallbackData);

    await awaitHydration(store);

    expect(store.getState().data).toBeNull();
    expect(mockDB.getRows()).toEqual([]);
  });

  it("uses fallbackData after a failed fetch without persisting it", async () => {
    mockFetcher.mockRejectedValue(new Error("network down"));
    const fallbackData = [{ id: "fallback" }];
    const store = createStore(null, fallbackData);

    await awaitHydration(store);
    const result = await store.getState().fetchData();
    await flushWrites();

    expect(result).toBe(fallbackData);
    expect(store.getState().data).toBe(fallbackData);
    expect(store.getState().error).toBeInstanceOf(Error);
    expect(mockDB.getRows()).toEqual([]);
  });

  it("does not persist initData when fetch starts before hydration completes", async () => {
    mockFetcher.mockRejectedValue(new Error("network down"));
    const initData = [{ id: "init" }];
    const store = createStore(initData);

    // no awaitHydration: fetch races ahead of the async IndexedDB
    // rehydration, so state.data is still the static initData
    await Promise.all([awaitHydration(store), store.getState().fetchData()]);
    await flushWrites();

    expect(store.getState().data).toEqual(initData);
    expect(mockDB.getRows()).toEqual([]);
  });

  it("persists data to IndexedDB after a successful fetch", async () => {
    mockFetcher.mockResolvedValue([{ id: "api" }]);
    const store = createStore([{ id: "init" }]);

    await awaitHydration(store);
    await store.getState().fetchData();
    await flushWrites();

    expect(store.getState().error).toBeNull();
    expect(mockDB.getRows()).toEqual([{ id: "api" }]);
  });

  it("commits and persists only the valid rows of a partially invalid response", async () => {
    const warnSpy = jest.spyOn(console, "warn").mockImplementation(() => {});
    mockFetcher.mockResolvedValue([{ id: "api" }, { id: "" }]);
    const store = createStore([{ id: "init" }], undefined, {
      sanitizeData: sanitizeRows,
    });

    await awaitHydration(store);
    const result = await store.getState().fetchData();
    await flushWrites();

    expect(result).toEqual([{ id: "api" }]);
    expect(store.getState().data).toEqual([{ id: "api" }]);
    expect(store.getState().error).toBeNull();
    expect(mockDB.getRows()).toEqual([{ id: "api" }]);
    expect(warnSpy).toHaveBeenCalled();
    warnSpy.mockRestore();
  });

  it("keeps a generic response committed before stale hydration completes", async () => {
    const delayedDB = createDelayedHydrationDB([{ id: "cached" }]);
    mockDB = delayedDB.db;
    mockFetcher.mockResolvedValue([{ id: "api" }]);
    const store = createStore([{ id: "init" }], undefined, {
      sanitizeData: sanitizeRows,
    });

    await delayedDB.hydrationStarted;
    await store.getState().fetchData();
    await flushWrites();

    expect(store.getState().data).toEqual([{ id: "api" }]);
    expect(mockDB.getRows()).toEqual([{ id: "api" }]);

    delayedDB.releaseHydration();
    await awaitHydration(store);
    await flushWrites();

    expect(store.getState()).toMatchObject({
      data: [{ id: "api" }],
      error: null,
      hydrated: true,
      loading: false,
    });
    expect(mockDB.getRows()).toEqual([{ id: "api" }]);
  });

  it("keeps a broker response committed before stale hydration completes", async () => {
    const delayedDB = createDelayedHydrationDB([{ id: "cached" }]);
    mockDB = delayedDB.db;
    mockFetcher.mockResolvedValue([{ id: "broker" }]);
    const store = createStore([{ id: "init" }], undefined, {
      sanitizeData: sanitizeRows,
    });

    await delayedDB.hydrationStarted;
    await store.getState().fetchData(undefined, { brokerId: "broker-a" });
    await flushWrites();

    delayedDB.releaseHydration();
    await awaitHydration(store);
    await flushWrites();

    expect(store.getState()).toMatchObject({
      data: [{ id: "broker" }],
      error: null,
      hydrated: true,
      loading: false,
    });
    expect(mockDB.getRows()).toEqual([{ id: "broker" }]);
  });

  it("publishes hydrated cache while a request is still pending", async () => {
    const delayedDB = createDelayedHydrationDB([{ id: "cached" }]);
    const request = createDeferred<any[]>();
    mockDB = delayedDB.db;
    mockFetcher.mockReturnValue(request.promise);
    const store = createStore([{ id: "init" }], undefined, {
      sanitizeData: sanitizeRows,
    });

    await delayedDB.hydrationStarted;
    const fetchPromise = store.getState().fetchData();
    delayedDB.releaseHydration();
    await awaitHydration(store);

    expect(store.getState()).toMatchObject({
      data: [{ id: "cached" }],
      error: null,
      hydrated: true,
      loading: true,
    });

    request.resolve([{ id: "api" }]);
    await fetchPromise;
    await flushWrites();

    expect(store.getState()).toMatchObject({
      data: [{ id: "api" }],
      error: null,
      hydrated: true,
      loading: false,
    });
    expect(mockDB.getRows()).toEqual([{ id: "api" }]);
  });

  it("keeps the failure state when valid cache hydrates after a rejection", async () => {
    const delayedDB = createDelayedHydrationDB([{ id: "cached" }]);
    const error = new Error("network down");
    mockDB = delayedDB.db;
    mockFetcher.mockRejectedValue(error);
    const store = createStore(null, undefined, {
      sanitizeData: sanitizeRows,
    });

    await delayedDB.hydrationStarted;
    const fetchPromise = store.getState().fetchData();
    delayedDB.releaseHydration();
    await Promise.all([fetchPromise, awaitHydration(store)]);
    await flushWrites();

    expect(store.getState()).toMatchObject({
      data: [{ id: "cached" }],
      error,
      hydrated: true,
      loading: false,
    });
    expect(mockDB.getRows()).toEqual([{ id: "cached" }]);
  });

  it("allows valid cache to replace fallback after an invalid response", async () => {
    const delayedDB = createDelayedHydrationDB([{ id: "cached" }], []);
    const fallbackData = [{ id: "fallback" }];
    mockDB = delayedDB.db;
    mockFetcher.mockResolvedValue([]);
    const store = createStore(null, fallbackData, {
      sanitizeData: sanitizeRows,
    });

    await delayedDB.hydrationStarted;
    await store.getState().fetchData();

    expect(store.getState().data).toBe(fallbackData);
    expect(store.getState().error?.message).toContain("Invalid data");

    delayedDB.releaseHydration();
    await awaitHydration(store);
    await flushWrites();

    expect(store.getState()).toMatchObject({
      data: [{ id: "cached" }],
      hydrated: true,
      loading: false,
    });
    expect(store.getState().error?.message).toContain("Invalid data");
    expect(mockDB.getRows()).toEqual([{ id: "cached" }]);
  });

  it("keeps previous cache in IndexedDB when a later fetch fails", async () => {
    mockFetcher.mockResolvedValueOnce([{ id: "api" }]);
    const store = createStore();

    await awaitHydration(store);
    await store.getState().fetchData();

    mockFetcher.mockRejectedValue(new Error("network down"));
    await store.getState().fetchData();
    await flushWrites();

    expect(store.getState().error).toBeInstanceOf(Error);
    expect(mockDB.getRows()).toEqual([{ id: "api" }]);
  });

  it("keeps initial data when IndexedDB is empty (hydration must not wipe it)", async () => {
    const initData = [{ id: "init" }];
    const store = createStore(initData);

    await awaitHydration(store);

    expect(store.getState().data).toEqual(initData);
  });

  it("hydrates persisted data over initial data when IndexedDB has a cache", async () => {
    mockDB = createFakeDB();
    mockDB.transaction = () => {
      const tx: any = {
        objectStore: () => ({
          getAll: () => {
            const req: any = {};
            Promise.resolve().then(() => {
              req.result = [{ id: "cached" }];
              req.onsuccess?.();
            });
            return req;
          },
          clear: () => {},
          put: () => {},
        }),
      };
      Promise.resolve().then(() => tx.oncomplete?.());
      return tx;
    };
    const store = createStore([{ id: "init" }]);

    await awaitHydration(store);

    expect(store.getState().data).toEqual([{ id: "cached" }]);
  });

  it("times out a permanently pending request and uses fallback data", async () => {
    mockFetcher.mockReturnValue(new Promise(() => {}));
    const fallbackData = [{ id: "fallback" }];
    const store = createStore(null, fallbackData, {
      timeoutMs: 5,
      sanitizeData: sanitizeRows,
    });

    await awaitHydration(store);
    const result = await store.getState().fetchData();

    expect(result).toBe(fallbackData);
    expect(store.getState().data).toBe(fallbackData);
    expect(store.getState().loading).toBe(false);
    expect(store.getState().error?.message).toContain("timed out");
    expect(mockFetcher.mock.calls[0][1].signal.aborted).toBe(true);
    expect(mockDB.getRows()).toEqual([]);
  });

  it.each([
    [
      "mainnet CORS",
      "https://api.paxeer-network.org",
      new TypeError("Failed to fetch"),
    ],
    [
      "staging CORS",
      "https://staging-api.paxeer-network.org",
      new TypeError("Failed to fetch"),
    ],
    ["502", "https://api.paxeer-network.org", new Error("Bad Gateway")],
    [
      "invalid JSON",
      "https://api.paxeer-network.org",
      new SyntaxError("Unexpected token"),
    ],
    ["success false", "https://api.paxeer-network.org", new Error("success false")],
    ["offline", "https://api.paxeer-network.org", new Error("Network offline")],
  ])("uses fallback data for %s", async (_name, baseUrl, error) => {
    mockFetcher.mockRejectedValue(error);
    const fallbackData = [{ id: "fallback" }];
    const store = createStore(null, fallbackData, {
      sanitizeData: sanitizeRows,
    });

    await awaitHydration(store);
    await expect(store.getState().fetchData(baseUrl)).resolves.toBe(
      fallbackData,
    );

    expect(store.getState().data).toBe(fallbackData);
    expect(store.getState().error).toBe(error);
    expect(mockDB.getRows()).toEqual([]);
  });

  it("keeps last-known-good data and IndexedDB when success data is invalid", async () => {
    mockFetcher
      .mockResolvedValueOnce([{ id: "valid" }])
      .mockResolvedValueOnce([]);
    const store = createStore(undefined, undefined, {
      sanitizeData: sanitizeRows,
    });

    await awaitHydration(store);
    await store.getState().fetchData();
    await flushWrites();
    const result = await store.getState().fetchData();
    await flushWrites();

    expect(result).toEqual([{ id: "valid" }]);
    expect(store.getState().data).toEqual([{ id: "valid" }]);
    expect(store.getState().error?.message).toContain("Invalid data");
    expect(mockDB.getRows()).toEqual([{ id: "valid" }]);
  });

  it("ignores corrupted IndexedDB data during hydration", async () => {
    mockDB = createFakeDB([{ id: "" }]);
    const initData = [{ id: "init" }];
    const store = createStore(initData, undefined, {
      sanitizeData: sanitizeRows,
    });

    await awaitHydration(store);

    expect(store.getState().data).toBe(initData);
  });

  it("lets a broker request supersede a generic request", async () => {
    const genericRequest = createDeferred<any[]>();
    const brokerRequest = createDeferred<any[]>();
    mockFetcher
      .mockReturnValueOnce(genericRequest.promise)
      .mockReturnValueOnce(brokerRequest.promise);
    const store = createStore(undefined, undefined, {
      sanitizeData: sanitizeRows,
    });

    await awaitHydration(store);
    const genericPromise = store.getState().fetchData();
    const genericSignal = mockFetcher.mock.calls[0][1].signal;
    const brokerPromise = store
      .getState()
      .fetchData(undefined, { brokerId: "broker-a" });

    expect(genericSignal.aborted).toBe(true);
    brokerRequest.resolve([{ id: "broker" }]);
    await brokerPromise;
    genericRequest.resolve([{ id: "late-generic" }]);
    await genericPromise;
    await flushWrites();

    expect(store.getState().data).toEqual([{ id: "broker" }]);
    expect(mockDB.getRows()).toEqual([{ id: "broker" }]);

    await expect(store.getState().fetchData()).resolves.toEqual([
      { id: "broker" },
    ]);
    expect(mockFetcher).toHaveBeenCalledTimes(2);
  });

  it("reuses an active broker request for a lower-priority generic caller", async () => {
    const brokerRequest = createDeferred<any[]>();
    mockFetcher.mockReturnValueOnce(brokerRequest.promise);
    const store = createStore(undefined, undefined, {
      sanitizeData: sanitizeRows,
    });

    await awaitHydration(store);
    const brokerPromise = store
      .getState()
      .fetchData(undefined, { brokerId: "broker-a" });
    const genericPromise = store.getState().fetchData();

    expect(genericPromise).toBe(brokerPromise);
    expect(mockFetcher).toHaveBeenCalledTimes(1);

    brokerRequest.resolve([{ id: "broker" }]);
    await expect(genericPromise).resolves.toEqual([{ id: "broker" }]);
    expect(store.getState().data).toEqual([{ id: "broker" }]);
  });

  it("ignores a late response from an older same-priority request", async () => {
    const olderRequest = createDeferred<any[]>();
    const newerRequest = createDeferred<any[]>();
    mockFetcher
      .mockReturnValueOnce(olderRequest.promise)
      .mockReturnValueOnce(newerRequest.promise);
    const store = createStore(undefined, undefined, {
      sanitizeData: sanitizeRows,
    });

    await awaitHydration(store);
    const olderPromise = store.getState().fetchData("https://old.example");
    const newerPromise = store.getState().fetchData("https://new.example");

    newerRequest.resolve([{ id: "newer" }]);
    await newerPromise;
    olderRequest.resolve([{ id: "older" }]);
    await olderPromise;
    await flushWrites();

    expect(store.getState().data).toEqual([{ id: "newer" }]);
    expect(mockDB.getRows()).toEqual([{ id: "newer" }]);
  });

  it("hydrates broker authority and blocks a generic refresh", async () => {
    mockDB = createFakeDB([
      { id: "broker-cache" },
      dataOriginMetadata("broker"),
    ]);
    const store = createStore(null, [{ id: "fallback" }], {
      sanitizeData: sanitizeRows,
      persistDataOrigin: true,
    });

    await awaitHydration(store);

    expect(store.getState()).toMatchObject({
      data: [{ id: "broker-cache" }],
      dataOrigin: "broker",
      hydrated: true,
    });

    await expect(store.getState().fetchData()).resolves.toEqual([
      { id: "broker-cache" },
    ]);
    expect(mockFetcher).not.toHaveBeenCalled();
    expect(mockDB.getRows()).toEqual([
      { id: "broker-cache" },
      dataOriginMetadata("broker"),
    ]);
  });

  it("drops corrupted rows from a partially invalid persisted cache on hydration", async () => {
    mockDB = createFakeDB([
      { id: "valid-cache" },
      { id: "" },
      dataOriginMetadata("broker"),
    ]);
    const store = createStore(null, [{ id: "fallback" }], {
      sanitizeData: sanitizeRows,
      persistDataOrigin: true,
    });

    await awaitHydration(store);

    expect(store.getState()).toMatchObject({
      data: [{ id: "valid-cache" }],
      dataOrigin: "broker",
      hydrated: true,
    });
  });

  it("sanitizes a partially invalid cache when serving it after a fetch failure", async () => {
    const delayedDB = createDelayedHydrationDB([
      { id: "valid-cache" },
      { id: "" },
      dataOriginMetadata("broker"),
    ]);
    mockDB = delayedDB.db;
    mockFetcher.mockRejectedValue(new Error("network down"));
    const store = createStore(null, [{ id: "fallback" }], {
      sanitizeData: sanitizeRows,
      persistDataOrigin: true,
    });

    await delayedDB.hydrationStarted;
    await store.getState().fetchData();

    expect(store.getState().data).toEqual([{ id: "valid-cache" }]);
    expect(store.getState().dataOrigin).toBe("broker");
    expect(store.getState().error).toBeInstanceOf(Error);

    delayedDB.releaseHydration();
    await awaitHydration(store);

    expect(store.getState().data).toEqual([{ id: "valid-cache" }]);
  });

  it("treats a synchronously throwing fetcher as a failed request", async () => {
    mockFetcher.mockImplementation(() => {
      throw new Error("sync throw");
    });
    const initData = [{ id: "init" }];
    const store = createStore(initData);

    const result = await store.getState().fetchData();

    expect(result).toEqual(initData);
    expect(store.getState().loading).toBe(false);
    expect(store.getState().error?.message).toBe("sync throw");
  });

  it("upgrades a generic cache to broker data and metadata", async () => {
    mockDB = createFakeDB([
      { id: "generic-cache" },
      dataOriginMetadata("generic"),
    ]);
    mockFetcher.mockResolvedValue([{ id: "broker-api" }]);
    const store = createStore(null, [{ id: "fallback" }], {
      sanitizeData: sanitizeRows,
      persistDataOrigin: true,
    });

    await awaitHydration(store);
    expect(store.getState().dataOrigin).toBe("generic");

    await store.getState().fetchData(undefined, { brokerId: "broker-a" });
    await flushWrites();

    expect(store.getState()).toMatchObject({
      data: [{ id: "broker-api" }],
      dataOrigin: "broker",
      error: null,
    });
    expect(mockDB.getRows()).toEqual([
      { id: "broker-api" },
      dataOriginMetadata("broker"),
    ]);
  });

  it("cancels an in-flight generic request when broker cache hydrates", async () => {
    const delayedDB = createDelayedHydrationDB([
      { id: "broker-cache" },
      dataOriginMetadata("broker"),
    ]);
    const genericRequest = createDeferred<any[]>();
    mockDB = delayedDB.db;
    mockFetcher.mockReturnValue(genericRequest.promise);
    const store = createStore(null, [{ id: "fallback" }], {
      sanitizeData: sanitizeRows,
      persistDataOrigin: true,
    });

    await delayedDB.hydrationStarted;
    const genericPromise = store.getState().fetchData();
    const genericSignal = mockFetcher.mock.calls[0][1].signal;

    delayedDB.releaseHydration();
    await Promise.all([genericPromise, awaitHydration(store)]);
    await flushWrites();

    expect(genericSignal.aborted).toBe(true);
    expect(store.getState()).toMatchObject({
      data: [{ id: "broker-cache" }],
      dataOrigin: "broker",
      hydrated: true,
      loading: false,
    });
    expect(mockDB.getRows()).toEqual([
      { id: "broker-cache" },
      dataOriginMetadata("broker"),
    ]);
  });

  it("restores broker authority from fallback cache before hydration finishes", async () => {
    const delayedDB = createDelayedHydrationDB([
      { id: "broker-cache" },
      dataOriginMetadata("broker"),
    ]);
    mockDB = delayedDB.db;
    mockFetcher.mockRejectedValue(new Error("network down"));
    const store = createStore(null, [{ id: "fallback" }], {
      sanitizeData: sanitizeRows,
      persistDataOrigin: true,
    });

    await delayedDB.hydrationStarted;
    await store.getState().fetchData();

    expect(store.getState()).toMatchObject({
      data: [{ id: "broker-cache" }],
      dataOrigin: "broker",
      loading: false,
    });

    await expect(store.getState().fetchData()).resolves.toEqual([
      { id: "broker-cache" },
    ]);
    expect(mockFetcher).toHaveBeenCalledTimes(1);

    delayedDB.releaseHydration();
    await awaitHydration(store);
  });

  it("keeps generic data for wallets when a broker request fails", async () => {
    mockDB = createFakeDB([
      { id: "generic-cache" },
      dataOriginMetadata("generic"),
    ]);
    const error = new Error("broker unavailable");
    mockFetcher.mockRejectedValue(error);
    const store = createStore(null, [{ id: "fallback" }], {
      sanitizeData: sanitizeRows,
      persistDataOrigin: true,
    });

    await awaitHydration(store);
    await store.getState().fetchData(undefined, { brokerId: "broker-a" });
    await flushWrites();

    expect(store.getState()).toMatchObject({
      data: [{ id: "generic-cache" }],
      dataOrigin: "generic",
      error,
      loading: false,
    });
    expect(mockDB.getRows()).toEqual([
      { id: "generic-cache" },
      dataOriginMetadata("generic"),
    ]);
  });

  it("ignores legacy attributed-store rows without metadata", async () => {
    mockDB = createFakeDB([{ id: "legacy" }]);
    const fallbackData = [{ id: "fallback" }];
    const store = createStore(null, fallbackData, {
      sanitizeData: sanitizeRows,
      persistDataOrigin: true,
    });

    await awaitHydration(store);

    expect(store.getState()).toMatchObject({
      data: null,
      dataOrigin: null,
      hydrated: true,
    });
  });

  it("treats the paxeer-network broker context as broker authority without a query", async () => {
    mockFetcher.mockResolvedValue([{ id: "paxeer-network" }]);
    const store = createStore(null, [{ id: "fallback" }], {
      sanitizeData: sanitizeRows,
      persistDataOrigin: true,
    });

    await awaitHydration(store);
    await store.getState().fetchData(undefined, { brokerId: "paxeer-network" });

    expect(mockFetcher.mock.calls[0][0]).toBe("/v1/public/test");
    expect(store.getState().dataOrigin).toBe("broker");
  });
});
