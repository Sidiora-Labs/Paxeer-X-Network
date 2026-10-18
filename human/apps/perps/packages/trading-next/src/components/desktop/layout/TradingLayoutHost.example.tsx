/**
 * Example: How to use LayoutHost with strategy pattern in Trading
 *
 * This file demonstrates how to migrate from manual split tree construction
 * to using the LayoutHost + strategy pattern.
 *
 * NOTE: This is an example file. The actual DesktopLayout still uses
 * the traditional SplitLayout approach for backward compatibility.
 * To fully migrate, gradually replace split tree construction with
 * LayoutHost usage.
 */
import React, { useMemo } from "react";
import type {
  LayoutStrategy,
  PanelRegistry,
} from "@paxeer-network/layout-core";
import { LayoutHost } from "@paxeer-network/layout-core";

/** Strategy must be provided by the consumer, e.g. splitStrategy from @paxeer-network/layout-split or gridStrategy from @paxeer-network/layout-grid */

/**
 * Example: TradingLayoutHost component
 *
 * This component shows how to use LayoutHost with a strategy.
 * The strategy is passed as a prop (not read from localStorage),
 * ensuring type safety and explicit control.
 *
 * @example
 * ```tsx
 * <TradingLayoutHost
 *   strategy={splitStrategy}
 *   panels={{
 *     tradingview: <TradingviewWidget />,
 *     orderbook: <OrderBookWidget />,
 *     orderEntry: <OrderEntryWidget />,
 *   }}
 *   storageKey="paxeerNetwork_trading_layout_state__split"
 * />
 * ```
 */
export interface TradingLayoutHostProps {
  /** Layout strategy to use (must be explicitly provided) */
  strategy: LayoutStrategy;
  /** Panels to render (map of panel ID to ReactNode) */
  panels: Record<string, React.ReactNode>;
  /** Storage key for persistence (optional) */
  storageKey?: string;
  /** Optional className */
  className?: string;
  /** Optional style */
  style?: React.CSSProperties;
}

export function TradingLayoutHost(props: TradingLayoutHostProps) {
  const { strategy, panels, storageKey, className, style } = props;

  // Convert panels Record to PanelRegistry (Map)
  const panelRegistry: PanelRegistry = useMemo(() => {
    const map = new Map<string, React.ReactNode>();
    Object.entries(panels).forEach(([id, node]) => {
      map.set(id, node);
    });
    return map;
  }, [panels]);

  return (
    <LayoutHost
      strategy={strategy}
      panels={panelRegistry}
      storageKey={storageKey}
      className={className}
      style={style}
    />
  );
}

/**
 * Example: How to use strategy resolver to safely select a strategy
 *
 * This shows how to handle strategy selection at a higher level,
 * ensuring we always have a valid strategy even if preferred is not available.
 */
export function useTradingLayoutStrategy(options: {
  preferredStrategyId?: string;
  availableStrategies: LayoutStrategy[];
  defaultStrategy?: LayoutStrategy;
}): LayoutStrategy {
  const { preferredStrategyId, availableStrategies, defaultStrategy } = options;

  return useMemo(() => {
    // Try to find preferred strategy
    if (preferredStrategyId) {
      const preferred = availableStrategies.find(
        (s) => s.id === preferredStrategyId,
      );
      if (preferred) {
        return preferred;
      }
    }

    // Fall back to default or first available
    return defaultStrategy || availableStrategies[0];
  }, [preferredStrategyId, availableStrategies, defaultStrategy]);
}

/**
 * Example usage in a parent component:
 *
 * ```tsx
 * function TradingPage() {
 *   // Strategy is explicitly provided (not read from localStorage)
 *   const strategy = splitStrategy; // or gridStrategy, etc.
 *
 *   // Or use resolver if you want to support preference with fallback:
 *   // const strategy = useTradingLayoutStrategy({
 *   //   preferredStrategyId: preferredId, // from props or localStorage
 *   //   availableStrategies: [splitStrategy, gridStrategy],
 *   //   defaultStrategy: splitStrategy,
 *   // });
 *
 *   return (
 *     <TradingLayoutHost
 *       strategy={strategy}
 *       panels={{
 *         tradingview: <TradingviewWidget />,
 *         orderbook: <OrderBookWidget />,
 *         orderEntry: <OrderEntryWidget />,
 *       }}
 *       storageKey={`paxeerNetwork_trading_layout_state__${strategy.id}`}
 *     />
 *   );
 * }
 * ```
 */
