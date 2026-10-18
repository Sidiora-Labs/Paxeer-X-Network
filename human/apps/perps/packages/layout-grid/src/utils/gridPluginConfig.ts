import type { GridLayoutModel, GridLayoutPreset } from "../types";
import { createTradingGridLayoutFromPreset } from "./tradingGridLayout";

export function createDefaultGetInitialLayout(
  presets: GridLayoutPreset[],
): () => GridLayoutModel {
  return () => {
    const preset = presets[0];
    return createTradingGridLayoutFromPreset(preset);
  };
}
