import { StateCreator, StoreMutatorIdentifier } from "zustand";
import {
  PersistOptions,
  StateStorage,
  persist,
  createJSONStorage,
} from "zustand/middleware";
import { indexedDBManager } from "./indexedDBManager";

/**
 * Configuration for IndexedDB storage
 */
export interface IndexedDBMetadataConfig {
  /**
   * Object-store keyPath used by the reserved metadata record. The metadata
   * record lives in the SAME object store as the data rows under the
   * reserved `recordKey`: any code reading this store directly must filter
   * it out (see readAttributedIndexedDBState). Data-store sanitizers also
   * drop it as an invalid row as a safety net.
   */
  keyPath: string;
  /** Reserved key that cannot collide with a real data row. */
  recordKey: string;
  /** Field containing the metadata payload. */
  metadataField: string;
}

interface IndexedDBStorageConfig {
  /** Database name */
  dbName: string;
  /** Object store name */
  storeName: string;
  /** Optional metadata record stored atomically with the data rows. */
  metadata?: IndexedDBMetadataConfig;
}

/**
 * IndexedDB storage interface for array-based data operations
 */
export interface IndexedDBStorage<T = unknown> {
  /** Get all stored data */
  getItem: () => Promise<Array<T> | null>;
  /** Replace all data (clear and insert) */
  setItem: (value: Array<T>) => Promise<void>;
  /** Clear all data */
  removeItem: () => Promise<void>;
}

export interface AttributedIndexedDBState<T, M = unknown> {
  data: T[];
  metadata: M;
}

/**
 * Splits the reserved metadata record from data rows. A missing metadata record
 * is treated as a cache miss so legacy, unattributed data is never published.
 */
export const readAttributedIndexedDBState = async <T, M = unknown>(
  indexedDBStorage: IndexedDBStorage<T>,
  config: IndexedDBMetadataConfig,
): Promise<AttributedIndexedDBState<T, M> | null> => {
  const rows = await indexedDBStorage.getItem();
  if (!rows) {
    return null;
  }

  const metadataRecord = rows.find(
    (row) =>
      row !== null &&
      typeof row === "object" &&
      (row as Record<string, unknown>)[config.keyPath] === config.recordKey,
  ) as Record<string, unknown> | undefined;
  const metadata = metadataRecord?.[config.metadataField];
  if (typeof metadata === "undefined") {
    return null;
  }

  return {
    data: rows.filter((row) => row !== metadataRecord),
    metadata: metadata as M,
  };
};

/**
 * Creates an IndexedDB storage instance using simple connection manager
 */
export const createIndexedDBStorage = <T = unknown>(
  config: IndexedDBStorageConfig,
): IndexedDBStorage<T> => {
  const { dbName, storeName } = config;

  /**
   * Gets database connection from manager with automatic initialization
   * Uses Promise-based initialization to prevent race conditions
   */
  const getDB = async (): Promise<IDBDatabase> => {
    // The manager now handles initialization automatically with Promise caching
    return indexedDBManager.getConnection(dbName, storeName);
  };

  /**
   * Executes a transaction with proper error handling
   */
  const executeTransaction = async <R>(
    operation: (store: IDBObjectStore) => R,
    mode: IDBTransactionMode = "readwrite",
  ): Promise<R> => {
    const dbInstance = await getDB();
    const transaction = dbInstance.transaction(storeName, mode);
    const store = transaction.objectStore(storeName);

    return new Promise((resolve, reject) => {
      const result = operation(store);

      transaction.oncomplete = () => resolve(result);
      transaction.onerror = () => {
        console.error(
          `Transaction failed for ${storeName}:`,
          transaction.error,
        );
        reject(transaction.error);
      };
      transaction.onabort = () => {
        console.error(`Transaction aborted for ${storeName}`);
        reject(new Error("Transaction was aborted"));
      };
    });
  };

  /**
   * Retrieves all data from the store
   */
  const getItem = async (): Promise<Array<T> | null> => {
    try {
      const allValues = await executeTransaction((store) => {
        return new Promise<Array<T>>((resolve, reject) => {
          const request = store.getAll();
          request.onsuccess = () => {
            const result = request.result as Array<T>;
            resolve(result.length > 0 ? result : []);
          };
          request.onerror = () => reject(request.error);
        });
      }, "readonly");

      return allValues.length > 0 ? allValues : null;
    } catch (error) {
      console.error(`Failed to get items from ${storeName}:`, error);
      return null;
    }
  };

  /**
   * Replaces all data in the store (clear + insert)
   */
  const setItem = async (value: Array<T>): Promise<void> => {
    try {
      await executeTransaction((store) => {
        store.clear();
        value.forEach((item) => store.put(item));
        return undefined;
      }, "readwrite");
    } catch (error) {
      console.error(`Failed to set items in ${storeName}:`, error);
      throw error;
    }
  };

  /**
   * Clears all data from the store
   */
  const removeItem = async (): Promise<void> => {
    try {
      await executeTransaction((store) => {
        store.clear();
        return undefined;
      }, "readwrite");
    } catch (error) {
      console.error(`Failed to remove items from ${storeName}:`, error);
      throw error;
    }
  };

  return {
    getItem,
    setItem,
    removeItem,
  };
};

/**
 * Configuration options for IndexedDB persistence middleware
 */
type IndexedDBPersistOptions<T, U = T> = Omit<
  PersistOptions<T, U>,
  "storage"
> & {
  /** IndexedDB configuration */
  indexedDBConfig: IndexedDBStorageConfig;
};

/**
 * Adapts IndexedDBStorage to Zustand's StateStorage interface
 */
export const adaptToStateStorage = <T>(
  indexedDBStorage: IndexedDBStorage<T>,
  metadataConfig?: IndexedDBMetadataConfig,
): StateStorage => ({
  getItem: async (): Promise<string | null> => {
    try {
      const result = metadataConfig
        ? await readAttributedIndexedDBState(indexedDBStorage, metadataConfig)
        : await indexedDBStorage.getItem();

      return result &&
        (Array.isArray(result)
          ? result.length > 0
          : Array.isArray(result.data) && result.data.length > 0)
        ? JSON.stringify({
            state: result,
            version: 0,
          })
        : null;
    } catch (error) {
      console.error("Failed to get item from IndexedDB storage:", error);
      return null;
    }
  },

  setItem: async (_name: string, value: string): Promise<void> => {
    try {
      const parsed = JSON.parse(value);
      const stateData = (
        parsed as {
          state?: Array<T> | AttributedIndexedDBState<T> | null;
        }
      )?.state;

      if (
        metadataConfig &&
        stateData !== null &&
        typeof stateData === "object" &&
        !Array.isArray(stateData) &&
        Array.isArray(stateData.data) &&
        typeof stateData.metadata !== "undefined"
      ) {
        const metadataRecord = {
          [metadataConfig.keyPath]: metadataConfig.recordKey,
          [metadataConfig.metadataField]: stateData.metadata,
        } as T;
        await indexedDBStorage.setItem([...stateData.data, metadataRecord]);
      } else if (Array.isArray(stateData) && !metadataConfig) {
        // Replace all data with the persisted array
        await indexedDBStorage.setItem(stateData);
      } else if (stateData === null) {
        // Explicitly clear IndexedDB when state is null
        await indexedDBStorage.removeItem();
      }
      // If the `state` key is absent (undefined), do nothing and preserve the
      // existing data. Stores use this to skip persisting error-path fallback
      // data so IndexedDB only ever holds successful responses.
    } catch (error) {
      console.error("Failed to set item in IndexedDB storage:", error);
      console.warn("Raw value that failed to parse:", _name, value);
    }
  },

  removeItem: async (): Promise<void> => {
    try {
      await indexedDBStorage.removeItem();
    } catch (error) {
      console.error("Failed to remove item from IndexedDB storage:", error);
    }
  },
});

/**
 * Creates a Zustand store with IndexedDB persistence
 *
 * @param initializer - The state creator function
 * @param options - Persistence options including IndexedDB configuration
 * @returns A state creator with IndexedDB persistence middleware applied
 *
 * @example
 * ```typescript
 * const useStore = create(
 *   persistIndexedDB(
 *     (set) => ({
 *       items: [],
 *       addItem: (item) => set((state) => ({ items: [...state.items, item] })),
 *     }),
 *     {
 *       name: 'my-store',
 *       indexedDBConfig: {
 *         dbName: 'PAXEER_NETWORK_STORE',
 *         storeName: 'ITEMS_STORE',
 *       },
 *     }
 *   )
 * );
 * ```
 */
export const persistIndexedDB = <
  T,
  Mps extends [StoreMutatorIdentifier, unknown][] = [],
  Mcs extends [StoreMutatorIdentifier, unknown][] = [],
  U = T,
>(
  initializer: StateCreator<T, [...Mps, ["zustand/persist", unknown]], Mcs>,
  options: IndexedDBPersistOptions<T, U>,
): StateCreator<T, Mps, [["zustand/persist", U], ...Mcs]> => {
  const { indexedDBConfig, ...rest } = options;
  // Create IndexedDB storage instance
  const indexedDBStorage = createIndexedDBStorage<U>(indexedDBConfig);

  // Create JSON storage wrapper for IndexedDB
  const jsonStorage = createJSONStorage(() =>
    adaptToStateStorage(indexedDBStorage, indexedDBConfig.metadata),
  );

  // Apply persist middleware with IndexedDB storage
  return persist(initializer, {
    ...rest,
    storage: jsonStorage,
  }) as StateCreator<T, Mps, [["zustand/persist", U], ...Mcs]>;
};
