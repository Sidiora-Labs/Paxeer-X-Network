import pathOr from "ramda/es/pathOr";
import { create } from "zustand";
import {
  createIndexedDBStorage,
  persistIndexedDB,
  readAttributedIndexedDBState,
} from "../../middleware/persistIndexedDB";
import type {
  AttributedIndexedDBState,
  IndexedDBMetadataConfig,
} from "../../middleware/persistIndexedDB";
import { fetcher } from "../../utils/fetcher";
import {
  defaultSanitizeData,
  resolveFallbackData,
} from "./createDataStore.fallback";
import type { DataSanitizer } from "./createDataStore.fallback";

const DEFAULT_REQUEST_PRIORITY = 0;
const BROKER_REQUEST_PRIORITY = 1;
const DATA_ORIGIN_METADATA_KEY = "__paxeerNetwork_data_origin_metadata__";
const DATA_ORIGIN_METADATA_FIELD = "__paxeerNetwork_data_origin__";

export type DataOrigin = "broker" | "generic" | "fallback" | null;

type PersistedDataOrigin = Exclude<DataOrigin, "fallback" | null>;

// Deliberately omits the broker id: a domain serves a single broker at a
// time, so cache reuse across a broker change is accepted. After a broker
// switch the mount-time broker fetch rewrites the cache (a broker request is
// never blocked by previously committed broker data); only if that refresh
// fails do the previous broker's rows keep serving with broker authority
// until a later refresh succeeds.
type PersistedDataOriginMetadata = {
  dataOrigin: PersistedDataOrigin;
};

type ActiveRequest<T> = {
  generation: number;
  key: string;
  priority: number;
  promise: Promise<T[] | null>;
  cancel: (error: Error) => void;
};

const createAbortableRequest = <T>(
  url: string,
  formatter: (data: any) => T[],
  timeoutMs?: number,
) => {
  const controller = new AbortController();
  let timeoutId: ReturnType<typeof setTimeout> | undefined;
  let rejectCancellation: (error: Error) => void = () => {};

  const cancellationPromise = new Promise<never>((_, reject) => {
    rejectCancellation = reject;
  });

  const cancel = (error: Error) => {
    if (controller.signal.aborted) {
      return;
    }
    controller.abort();
    rejectCancellation(error);
  };

  if (typeof timeoutMs === "number" && timeoutMs > 0) {
    timeoutId = setTimeout(() => {
      cancel(new Error(`Request timed out after ${timeoutMs}ms: ${url}`));
    }, timeoutMs);
  }

  // fetcher is async in practice, but a synchronous throw must not escape
  // fetchData (it would skip the loading reset) — funnel it into the race
  let fetchPromise: Promise<T[]>;
  try {
    fetchPromise = fetcher(url, { signal: controller.signal }, { formatter });
  } catch (error) {
    fetchPromise = Promise.reject(error);
  }

  const promise = Promise.race([fetchPromise, cancellationPromise]).finally(
    () => {
      if (timeoutId !== undefined) {
        clearTimeout(timeoutId);
      }
    },
  ) as Promise<T[]>;

  return { cancel, promise };
};

/**
 * Generic store state for data fetching
 */
export interface DataStoreState<T> {
  data: T[] | null;
  /**
   * Provenance and authority level of `data`:
   * - `"broker"`: fetched with broker context (broker_id, incl. default "paxeer-network") — highest authority
   * - `"generic"`: fetched without broker context — cannot downgrade broker data
   * - `"fallback"`: static bundled fallback, never persisted
   * - `null`: no usable data yet
   *
   * Authority is sticky for the lifetime of the page: once broker data has
   * been committed, calls without a broker id reuse it instead of refetching.
   * Data stores have no automatic refetch — data refreshes only when a caller
   * invokes fetchData again.
   */
  dataOrigin: DataOrigin;
  loading: boolean;
  error: Error | null;
  name: string;
  /** Whether the store has been hydrated from IndexedDB */
  hydrated: boolean;
}

/**
 * Generic store actions for data fetching
 */
export interface DataStoreActions<T> {
  fetchData: (
    baseUrl?: string,
    options?: { brokerId?: string },
  ) => Promise<T[] | null>;
  setHydrated: (hydrated: boolean) => void;
}

/**
 * Configuration for creating a data store
 */
export interface DataStoreConfig<T> {
  /** Store name for persistence */
  name: string;
  /** Database name */
  dbName: string;
  /** Store name in IndexedDB */
  storeName: string;
  /** Key path for IndexedDB */
  keyPath: string;
  /** API endpoint path */
  endpoint: string;
  /** Optional base URL */
  baseUrl?: string;
  /** Optional data transformer */
  formatter?: (data: any) => T[];
  /** Initial data used when the store is created. */
  initData?: T[] | null;
  /**
   * Fetch-failure fallback when neither memory nor IndexedDB has usable data.
   * Defaults to `initData` for backward compatibility. Fallback data is never
   * persisted to IndexedDB; only successful responses are written to cache.
   */
  fallbackData?: T[] | null;
  /** Optional request timeout. Requests without a value keep existing behavior. */
  timeoutMs?: number;
  /**
   * Optional per-row sanitize before data is published or restored. Invalid
   * rows are dropped; returning null rejects the whole payload.
   */
  sanitizeData?: DataSanitizer<T>;
  /** Persist API data together with its broker/generic authority. */
  persistDataOrigin?: boolean;

  // brokerId: string;
}

/**
 * Creates a Zustand store with IndexedDB persistence for data fetching
 *
 * @param config Store configuration
 * @returns A Zustand store hook
 *
 * @example
 * ```typescript
 * const useChainInfoStore = createDataStore<API.Chain>({
 *   name: "paxeer-network-chain-info",
 *   dbName: "PAXEER_NETWORK_STORE",
 *   storeName: "PAXEER_NETWORK_CHAIN_INFO",
 *   keyPath: "chain_id",
 *   endpoint: "/v1/public/chain_info",
 *   baseUrl: "https://api.paxeer-network.org"
 * });
 * ```
 */
export const createDataStore = <T>(config: DataStoreConfig<T>) => {
  const {
    name,
    dbName,
    storeName,
    keyPath,
    endpoint,
    baseUrl,
    formatter = (data: any) => pathOr([], ["rows"], data),
    initData,
    fallbackData,
    timeoutMs,
    sanitizeData,
    persistDataOrigin = false,
    // brokerId,
  } = config;

  const indexedDBStorage = createIndexedDBStorage<T>({
    dbName,
    storeName,
  });
  const indexedDBMetadata: IndexedDBMetadataConfig | undefined =
    persistDataOrigin
      ? {
          keyPath,
          recordKey: DATA_ORIGIN_METADATA_KEY,
          metadataField: DATA_ORIGIN_METADATA_FIELD,
        }
      : undefined;

  // Captured by reference so `partialize` can recognize data that was never
  // fetched (initial state) and skip persisting it
  const initialData = typeof initData === "undefined" ? [] : initData;
  const fetchFailureFallback =
    typeof fallbackData === "undefined" ? initData : fallbackData;
  const sanitizeFn = sanitizeData ?? defaultSanitizeData<T>;
  const initialDataOrigin: DataOrigin =
    sanitizeFn(initialData) !== null ? "fallback" : null;
  let requestGeneration = 0;
  let hasCommittedRequest = false;
  let activeRequest: ActiveRequest<T> | null = null;
  let committedRequestPriority = DEFAULT_REQUEST_PRIORITY;

  return create(
    persistIndexedDB<DataStoreState<T> & DataStoreActions<T>>(
      (set, get) => {
        const store = {
          name: storeName,
          data: initialData,
          dataOrigin: initialDataOrigin,
          loading: false,
          error: null,
          hydrated: false,
          setHydrated: (hydrated: boolean) => set({ hydrated }),
          fetchData: (
            dynamicBaseUrl?: string,
            options?: { brokerId?: string },
          ) => {
            const brokerId = options?.brokerId?.trim();
            const hasBrokerContext = !!brokerId;
            const priority = hasBrokerContext
              ? BROKER_REQUEST_PRIORITY
              : DEFAULT_REQUEST_PRIORITY;
            const brokerIdQuery =
              hasBrokerContext && brokerId !== "paxeer-network"
                ? `?broker_id=${encodeURIComponent(brokerId)}`
                : "";
            const url = `${dynamicBaseUrl || baseUrl || ""}${endpoint}${brokerIdQuery}`;

            if (
              activeRequest?.key === url &&
              activeRequest.priority === priority
            ) {
              return activeRequest.promise;
            }

            // A generic provider request must not replace an authoritative
            // broker request, regardless of React effect ordering.
            if (activeRequest && activeRequest.priority > priority) {
              return activeRequest.promise;
            }

            // Once valid broker data has been committed in this store session,
            // generic refreshes may use it but cannot downgrade it.
            if (committedRequestPriority > priority) {
              return Promise.resolve(get().data);
            }

            const generation = ++requestGeneration;
            activeRequest?.cancel(new Error(`Request superseded: ${url}`));
            set({ loading: true, error: null });

            const request = createAbortableRequest(url, formatter, timeoutMs);
            const promise = (async (): Promise<T[] | null> => {
              try {
                const data = await request.promise;

                if (generation !== requestGeneration) {
                  return get().data;
                }

                if (committedRequestPriority > priority) {
                  set({ loading: false });
                  return get().data;
                }

                const sanitized = sanitizeFn(data);
                if (sanitized === null) {
                  throw new Error(`Invalid data received for store: ${name}`);
                }

                if (sanitized.length < data.length) {
                  console.warn(
                    `[createDataStore] ${name}: dropped ${
                      data.length - sanitized.length
                    } invalid rows from the response`,
                  );
                }

                // Mark the sanitized network response before publishing it so
                // a hydration that completes later cannot restore stale data.
                hasCommittedRequest = true;
                set({
                  data: sanitized,
                  dataOrigin: hasBrokerContext ? "broker" : "generic",
                  loading: false,
                  error: null,
                });
                committedRequestPriority = Math.max(
                  committedRequestPriority,
                  priority,
                );
                return sanitized;
              } catch (error) {
                if (generation !== requestGeneration) {
                  return get().data;
                }

                const currentState = get();
                const currentData = currentState.data;
                const cacheResult: {
                  state: AttributedIndexedDBState<
                    T,
                    PersistedDataOriginMetadata
                  > | null;
                } = { state: null };
                const fallbackStorage = {
                  getItem: async (): Promise<T[] | null> => {
                    if (!persistDataOrigin) {
                      const cached = await indexedDBStorage.getItem();
                      return cached === null ? null : sanitizeFn(cached);
                    }

                    if (!indexedDBMetadata) {
                      return null;
                    }
                    const attributed = await readAttributedIndexedDBState<
                      T,
                      PersistedDataOriginMetadata
                    >(indexedDBStorage, indexedDBMetadata);
                    const cachedSource = attributed?.metadata?.dataOrigin;
                    if (
                      !attributed ||
                      (cachedSource !== "broker" && cachedSource !== "generic")
                    ) {
                      return null;
                    }
                    // Sanitize before publishing so a cache holding corrupted
                    // rows (e.g. written by an older release) never leaks them
                    // into state on the fetch-failure path.
                    const sanitizedCache = sanitizeFn(attributed.data);
                    if (sanitizedCache === null) {
                      return null;
                    }
                    cacheResult.state = {
                      ...attributed,
                      data: sanitizedCache,
                    };
                    return sanitizedCache;
                  },
                };
                const data = await resolveFallbackData(
                  currentData,
                  fetchFailureFallback,
                  fallbackStorage,
                  sanitizeFn,
                );

                if (generation !== requestGeneration) {
                  return get().data;
                }

                const fallbackUpdate = {
                  error: error as Error,
                  loading: false,
                };
                // Attribution relies on reference identity: resolveFallbackData
                // returns the original current/cached/fallback array, so each
                // comparison maps the result back to its source.
                const resolvedDataOrigin: DataOrigin =
                  data === currentData
                    ? currentState.dataOrigin
                    : cacheResult.state?.data === data
                      ? cacheResult.state.metadata.dataOrigin
                      : data === fetchFailureFallback
                        ? "fallback"
                        : data === null
                          ? null
                          : currentState.dataOrigin;

                if (resolvedDataOrigin === "broker") {
                  committedRequestPriority = BROKER_REQUEST_PRIORITY;
                }

                if (data !== currentData) {
                  set({
                    ...fallbackUpdate,
                    data,
                    dataOrigin: resolvedDataOrigin,
                  });
                } else {
                  set(fallbackUpdate);
                }
                return data;
              } finally {
                if (activeRequest?.generation === generation) {
                  activeRequest = null;
                }
              }
            })();

            activeRequest = {
              generation,
              key: url,
              priority,
              promise,
              cancel: request.cancel,
            };

            return promise;
          },
        };
        return store;
      },
      {
        name,
        indexedDBConfig: {
          dbName,
          storeName,
          metadata: indexedDBMetadata,
        },
        //@ts-ignore
        // Only persist data obtained from a successful fetch. zustand's
        // persist middleware writes on every set(), so both guards are
        // needed: `error` skips error-path writes, and the `initialData`
        // reference check skips the pristine state (e.g. the initial
        // `loading: true` set) whose data is still the static initData —
        // a persisted copy would shadow newer initData in future releases.
        partialize: (state) => {
          if (
            state.error ||
            state.data === initialData ||
            state.dataOrigin === "fallback" ||
            state.dataOrigin === null
          ) {
            return undefined;
          }

          return persistDataOrigin
            ? {
                data: state.data as T[],
                metadata: { dataOrigin: state.dataOrigin },
              }
            : (state.data as T[]);
        },
        merge: (persisted, current) => {
          const attributed = persistDataOrigin
            ? (persisted as
                | AttributedIndexedDBState<T, PersistedDataOriginMetadata>
                | undefined)
            : undefined;
          const persistedData = persistDataOrigin
            ? attributed?.data
            : (persisted as T[] | undefined);
          const persistedDataOrigin: DataOrigin = persistDataOrigin
            ? (attributed?.metadata?.dataOrigin ?? null)
            : persistedData
              ? "generic"
              : null;
          const persistedPriority =
            persistedDataOrigin === "broker"
              ? BROKER_REQUEST_PRIORITY
              : DEFAULT_REQUEST_PRIORITY;
          const sanitizedPersisted =
            persistedData !== undefined ? sanitizeFn(persistedData) : null;
          const canUsePersistedData =
            sanitizedPersisted !== null &&
            (persistedDataOrigin === "broker" ||
              persistedDataOrigin === "generic") &&
            (!hasCommittedRequest ||
              persistedPriority > committedRequestPriority);

          let loading = current.loading;
          if (canUsePersistedData) {
            committedRequestPriority = Math.max(
              committedRequestPriority,
              persistedPriority,
            );

            if (
              persistedPriority === BROKER_REQUEST_PRIORITY &&
              activeRequest &&
              activeRequest.priority < persistedPriority
            ) {
              requestGeneration += 1;
              activeRequest.cancel(
                new Error("Generic request superseded by broker cache"),
              );
              activeRequest = null;
              loading = false;
            }
          }

          return {
            ...current,
            // When nothing was stored, zustand calls merge with
            // `undefined` — keep the initial data instead of wiping it. A
            // configured sanitizer prevents corrupted cache hydration, while
            // a committed request prevents late hydration from restoring an
            // older cache snapshot.
            data: (canUsePersistedData ? sanitizedPersisted : current.data) as
              | T[]
              | null,
            dataOrigin: canUsePersistedData
              ? persistedDataOrigin
              : current.dataOrigin,
            loading,
          };
        },
        /**
         * Callback executed after rehydration from IndexedDB completes
         * Sets hydrated flag to true to indicate store is ready
         */
        onRehydrateStorage: () => (state, error) => {
          if (state && !error) {
            // state.hydrated = true;
            state.setHydrated(true);
          }
        },
      },
    ),
  );
};
