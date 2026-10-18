import { getTradingPanelIds } from "@paxeer-network/layout-core";
import type { GridLayoutModel, GridLayoutPreset } from "../types";
import { DEFAULT_GRID_PRESETS } from "./defaultPresets";
import { createDefaultGridLayout } from "./gridLayoutUtils";

export function createTradingGridLayoutFromPreset(
  preset?: GridLayoutPreset | null,
): GridLayoutModel {
  const panelIds = getTradingPanelIds();
  const p = preset ?? DEFAULT_GRID_PRESETS[0];
  if (!p) throw new Error("Grid layout: no default presets");
  return createDefaultGridLayout(panelIds as string[], p.rule, p.rowHeight);
}

export function createTradingGridLayout(): GridLayoutModel {
  return createTradingGridLayoutFromPreset();
}
