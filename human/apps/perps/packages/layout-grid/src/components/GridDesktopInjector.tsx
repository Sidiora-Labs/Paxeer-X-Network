import React from "react";
import type { LayoutStrategy } from "@paxeer-network/layout-core";
import type { DesktopLayoutInitialOptions } from "@paxeer-network/trading-next";
import { gridStrategy } from "../gridStrategy";
import type { GridLayoutModel, LayoutGridPluginOptions } from "../types";
import { getDefaultGridPresets } from "../utils/defaultPresets";
import { mergeGridConfig } from "../utils/gridLayoutUtils";
import { createDefaultGetInitialLayout } from "../utils/gridPluginConfig";

export interface GridDesktopInjectorProps {
  Original: React.ComponentType<Record<string, unknown>>;
  props: Record<string, unknown>;
  options: LayoutGridPluginOptions;
}

export function GridDesktopInjector({
  Original,
  props,
  options,
}: GridDesktopInjectorProps): React.ReactElement {
  const presets = options.presets ?? getDefaultGridPresets();
  const presetRowHeight = presets[0]?.rowHeight;
  const gridConfig = mergeGridConfig(options.grid, presetRowHeight);

  const { persistLayout = false } = options;

  const getInitialLayout = React.useCallback(() => {
    const baseLayout =
      options.getInitialLayout?.() ?? createDefaultGetInitialLayout(presets)();
    return {
      ...baseLayout,
      rowHeight: gridConfig.rowHeight,
      margin: gridConfig.margin,
      containerPadding: gridConfig.containerPadding,
      compactType: gridConfig.compactType,
      isDraggable: gridConfig.isDraggable,
      isResizable: gridConfig.isResizable,
    };
  }, [
    options.getInitialLayout,
    presets,
    gridConfig.rowHeight,
    gridConfig.margin,
    gridConfig.containerPadding,
    gridConfig.compactType,
    gridConfig.isDraggable,
    gridConfig.isResizable,
  ]);

  const storageKey = persistLayout ? "paxeerNetwork_trading_grid" : undefined;

  return (
    <Original
      {...props}
      layoutStrategy={gridStrategy as unknown as LayoutStrategy}
      getInitialLayout={
        (props.getInitialLayout as
          | ((opts: DesktopLayoutInitialOptions) => GridLayoutModel | undefined)
          | undefined) ??
        ((_opts: DesktopLayoutInitialOptions) => getInitialLayout())
      }
      storageKey={storageKey}
      disableLayoutPersistence={!persistLayout}
    />
  );
}
