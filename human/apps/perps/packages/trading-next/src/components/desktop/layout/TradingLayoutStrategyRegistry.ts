/**
 * Trading desktop layout strategy registry.
 * Strategies are provided by the consumer (e.g. split, grid); this module
 * exports resolveTradingLayoutStrategy for resolving by preferred ID.
 */
import type { LayoutStrategy } from "@paxeer-network/layout-core";
import { resolveStrategy } from "@paxeer-network/layout-core";

/** Strategies available for the trading desktop; consumer must pass their own (e.g. [splitStrategy, gridStrategy]) */
export const TRADING_LAYOUT_STRATEGIES: LayoutStrategy[] = [];

/**
 * Resolve the layout strategy to use for the trading desktop.
 * Caller must pass availableStrategies (e.g. from @paxeer-network/layout-split and layout-grid).
 *
 * @param options - preferredId and available strategies (and optional defaultStrategy)
 * @returns Resolved strategy and its ID
 */
export function resolveTradingLayoutStrategy(options: {
  preferredId?: string;
  availableStrategies: LayoutStrategy[];
  defaultStrategy?: LayoutStrategy;
}): { strategy: LayoutStrategy; id: string } {
  return resolveStrategy({
    preferredId: options.preferredId,
    availableStrategies: options.availableStrategies,
    defaultStrategy: options.defaultStrategy,
  });
}
