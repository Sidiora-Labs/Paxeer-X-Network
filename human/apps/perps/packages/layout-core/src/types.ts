import { ComponentType, ReactNode } from "react";

/**
 * Base layout model type - each strategy defines its own concrete type
 * This is a marker type to ensure type safety in serialization
 */
export type LayoutModel = Record<string, unknown>;

/**
 * Panel registry entry: maps panel IDs to React component with optional props
 */
export type PanelRegistryEntry = {
  node: ReactNode;
  props?: Record<string, unknown>;
};

/**
 * Panel registry: maps panel IDs to React components/nodes
 */
export type PanelRegistry = Map<string, PanelRegistryEntry>;

/**
 * Layout change callback (generic version)
 * @param layout - The new layout model (strategy-specific)
 */
export type OnLayoutChange<TLayout extends LayoutModel = LayoutModel> = (
  layout: TLayout,
) => void;

/**
 * Layout persist callback - called when layout should be persisted (e.g., on drag stop)
 * @param layout - The layout model to persist
 */
export type OnLayoutPersist<TLayout extends LayoutModel = LayoutModel> = (
  layout: TLayout,
) => void;

/**
 * Layout strategy interface
 * Each layout strategy (split, grid, etc.) must implement this interface
 */
export interface LayoutStrategy<TLayout extends LayoutModel = LayoutModel> {
  /** Unique identifier for this strategy */
  id: string;
  /** Human-readable display name */
  displayName: string;
  /** Create default layout model for given panel IDs */
  defaultLayout: (panelIds: string[]) => TLayout;
  /** Serialize layout model to JSON for persistence */
  serialize: (layout: TLayout) => string;
  /** Deserialize JSON string back to layout model */
  deserialize: (json: string) => TLayout;
  /** Renderer component that takes layout model and panels and renders them */
  Renderer: ComponentType<LayoutRendererProps<TLayout>>;
}

/**
 * Props for the strategy renderer component
 */
export interface LayoutRendererProps<
  TLayout extends LayoutModel = LayoutModel,
> {
  /** Current layout model */
  layout: TLayout;
  /** Panel registry containing all available panels */
  panels: PanelRegistry;
  /** Callback when layout changes (typed to the specific layout model) */
  onLayoutChange: OnLayoutChange<TLayout>;
  /** Callback when layout should be persisted (e.g., on drag stop or resize stop) */
  onLayoutPersist?: OnLayoutPersist<TLayout>;
  /** Optional className for the root container */
  className?: string;
  /** Optional style for the root container */
  style?: React.CSSProperties;
}

/**
 * Props for LayoutHost component
 */
export interface LayoutHostProps<TLayout extends LayoutModel = LayoutModel> {
  /** The layout strategy to use */
  strategy: LayoutStrategy<TLayout>;
  /** Panel registry: map of panel ID to ReactNode or PanelRegistryEntry */
  panels: PanelRegistry | Record<string, PanelRegistryEntry | ReactNode>;
  /** Initial layout model (optional, will use defaultLayout if not provided) */
  initialLayout?: TLayout;
  /** Callback when layout changes (typed to the specific layout model) */
  onLayoutChange?: OnLayoutChange<TLayout>;
  /** Storage key for persistence (optional, if not provided layout won't be persisted) */
  storageKey?: string;
  /** Optional className for the root container */
  className?: string;
  /** Optional style for the root container */
  style?: React.CSSProperties;
}

/**
 * Strategy resolver options
 */
export interface StrategyResolverOptions {
  /** Preferred strategy ID (may not be available) */
  preferredId?: string;
  /** Available strategies to choose from */
  availableStrategies: LayoutStrategy[];
  /** Default strategy to use if preferred is not available */
  defaultStrategy?: LayoutStrategy;
}
