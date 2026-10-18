/**
 * Shape of a locale JSON file loaded via dynamic import / Vite `import.meta.glob`.
 */
export type LocaleJsonModule = {
  default?: Record<string, string> | string | null;
};

export function asMessageRecord(
  value: Record<string, string> | string | null | undefined,
): Record<string, string> {
  if (!value) {
    return {};
  }
  if (typeof value !== "string") {
    return value;
  }
  try {
    return JSON.parse(value) as Record<string, string>;
  } catch {
    console.warn("Failed to parse locale JSON string, returning empty record");
    return {};
  }
}

/**
 * Loads a locale JSON module via a Vite `import.meta.glob` loader or a dynamic `import()` thunk,
 * and normalizes `default` to a flat message record.
 * Missing `loader`, rejected `loader()`, or missing/invalid `default` yields `{}` (see {@link asMessageRecord}).
 */
export async function importLocaleJsonModule(
  loader: (() => Promise<LocaleJsonModule>) | undefined,
): Promise<Record<string, string>> {
  if (!loader) {
    return asMessageRecord(undefined);
  }
  try {
    const mod = await loader();
    return asMessageRecord(mod.default);
  } catch {
    return {};
  }
}
