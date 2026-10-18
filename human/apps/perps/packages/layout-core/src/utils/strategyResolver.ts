import type { LayoutStrategy, StrategyResolverOptions } from "../types";

/**
 * Resolve which strategy to use based on preferred ID and available strategies
 * Ensures we always return a valid strategy, falling back to default if preferred is not available
 *
 * @param options - Resolver options
 * @returns Resolved strategy and its ID
 */
export function resolveStrategy(options: StrategyResolverOptions): {
  strategy: LayoutStrategy;
  id: string;
} {
  const { preferredId, availableStrategies, defaultStrategy } = options;

  // If preferred ID is provided, try to find it
  if (preferredId) {
    const preferred = availableStrategies.find((s) => s.id === preferredId);
    if (preferred) {
      return { strategy: preferred, id: preferredId };
    }
  }

  // Fall back to default strategy if provided
  if (defaultStrategy) {
    return { strategy: defaultStrategy, id: defaultStrategy.id };
  }

  // Fall back to first available strategy
  if (availableStrategies.length > 0) {
    const first = availableStrategies[0];
    return { strategy: first, id: first.id };
  }

  // This should never happen if strategies are properly set up
  throw new Error(
    "No layout strategy available. Please ensure at least one strategy is provided.",
  );
}
