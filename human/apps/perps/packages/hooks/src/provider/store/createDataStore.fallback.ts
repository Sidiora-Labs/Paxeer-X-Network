type IndexedDBStorageLike<T> = {
  getItem: () => Promise<Array<T> | null>;
};

export type DataSanitizer<T> = (data: unknown) => T[] | null;

export const hasUsableData = <T>(data: unknown): data is T[] =>
  Array.isArray(data) && data.length > 0;

export const defaultSanitizeData = <T>(data: unknown): T[] | null =>
  hasUsableData<T>(data) ? data : null;

/**
 * Resolves data on fetch failure: in-memory cache → IndexedDB → fallback.
 *
 * Always returns the original array reference (never a copy) so callers can
 * attribute the result by identity. `sanitizeData` is used as a gate only:
 * data reaching this point was already sanitized when it was committed or
 * hydrated, and re-filtering here would break identity attribution.
 */
export const resolveFallbackData = async <T>(
  currentData: T[] | null,
  fallbackData: T[] | null | undefined,
  indexedDBStorage: IndexedDBStorageLike<T>,
  sanitizeData: DataSanitizer<T> = defaultSanitizeData,
): Promise<T[] | null> => {
  if (sanitizeData(currentData) !== null) {
    return currentData;
  }

  try {
    const cached = await indexedDBStorage.getItem();
    if (sanitizeData(cached) !== null) {
      return cached;
    }
  } catch {
    // IndexedDB read failed; fall through to fallback data
  }

  if (fallbackData === null) {
    return null;
  }

  if (fallbackData !== undefined && sanitizeData(fallbackData) !== null) {
    return fallbackData;
  }

  return currentData;
};
