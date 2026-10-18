/** SSR-safe check for localStorage availability. */
function hasStorage(): boolean {
  return typeof window !== "undefined" && !!window.localStorage;
}

/**
 * Generic preset shape: id, name, and strategy-specific rule.
 */
export interface LayoutPreset<TRule = unknown> {
  id: string;
  name: string;
  rule: TRule;
}

/**
 * Storage key configuration per strategy (split vs grid use different keys).
 */
export interface LayoutRuleManagerOptions {
  /** localStorage key for persisting the selected preset id. */
  presetIdStorageKey: string;
  /** Base key for layout persistence; actual key is `${layoutStorageKeyPrefix}_${presetId}`. */
  layoutStorageKeyPrefix: string;
  /**
   * When false, no localStorage read/write; preset selection kept in memory only.
   * Default true for backward compatibility.
   */
  persist?: boolean;
}

/**
 * Manages layout presets, selected preset persistence, and reset.
 * Persistence of the user-selected rule (preset id) is done via presetIdStorageKey.
 * When persist is false, selection is kept in memory only (no localStorage).
 */
export class LayoutRuleManager<TRule = unknown> {
  private readonly _presets: readonly LayoutPreset<TRule>[];
  private readonly options: LayoutRuleManagerOptions;
  /** In-memory selected preset id when persist is false. */
  private _selectedId?: string;

  constructor(
    presets: LayoutPreset<TRule>[],
    options: LayoutRuleManagerOptions,
  ) {
    this._presets = presets.length > 0 ? [...presets] : [];
    this.options = { ...options };
  }

  /** Whether persistence to localStorage is enabled. */
  private get persist(): boolean {
    return this.options.persist !== false;
  }

  /** Read-only preset list. */
  get presets(): readonly LayoutPreset<TRule>[] {
    return this._presets;
  }

  /**
   * Reads selected preset id from localStorage (presetIdStorageKey).
   * When persist is false, returns in-memory selection or first preset.
   * Validates against presets; falls back to first preset id when missing or invalid.
   */
  getSelectedPresetId(): string {
    if (this._presets.length === 0) {
      return "";
    }
    if (!this.persist) {
      return this._selectedId ?? this._presets[0]?.id ?? "";
    }
    if (!hasStorage()) {
      return this._presets[0]?.id ?? "";
    }
    try {
      const stored = window.localStorage.getItem(
        this.options.presetIdStorageKey,
      );
      const valid =
        stored && this._presets.some((p) => p.id === stored)
          ? stored
          : (this._presets[0]?.id ?? "");
      return valid;
    } catch {
      return this._presets[0]?.id ?? "";
    }
  }

  /**
   * Writes selected preset id to localStorage (presetIdStorageKey).
   * When persist is false, only updates in-memory selection.
   * Validates id exists in presets before writing.
   */
  setSelectedPresetId(id: string): void {
    if (!this._presets.some((p) => p.id === id)) return;
    if (!this.persist) {
      this._selectedId = id;
      return;
    }
    if (!hasStorage()) return;
    try {
      window.localStorage.setItem(this.options.presetIdStorageKey, id);
    } catch {
      // ignore
    }
  }

  /** Returns the preset for the currently selected id. */
  getSelectedPreset(): LayoutPreset<TRule> | undefined {
    const id = this.getSelectedPresetId();
    return this._presets.find((p) => p.id === id);
  }

  /**
   * Storage key for layout persistence for the current preset.
   * Used by LayoutHost: `${layoutStorageKeyPrefix}_${selectedPresetId}`.
   */
  getLayoutStorageKey(): string {
    const id = this.getSelectedPresetId();
    return `${this.options.layoutStorageKeyPrefix}_${id}`;
  }

  /**
   * Clears persisted layout data for the current preset (reset to preset rule).
   * After this, next load will use getInitialLayout() from preset rule.
   * No-op when persist is false (nothing to clear).
   */
  reset(): void {
    if (!this.persist || !hasStorage()) return;
    try {
      window.localStorage.removeItem(this.getLayoutStorageKey());
    } catch {
      // ignore
    }
  }

  /**
   * Returns whether the current preset has any persisted layout data.
   * Useful for UI "custom" badge or "Reset to default" visibility.
   * Returns false when persist is false.
   */
  hasPersistedLayout(): boolean {
    if (!this.persist || !hasStorage()) return false;
    try {
      const key = this.getLayoutStorageKey();
      const value = window.localStorage.getItem(key);
      return value != null && value.length > 0;
    } catch {
      return false;
    }
  }
}
