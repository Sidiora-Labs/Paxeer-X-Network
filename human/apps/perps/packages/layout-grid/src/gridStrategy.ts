import type { LayoutStrategy } from "@paxeer-network/layout-core";
import { GridRenderer } from "./GridRenderer";
import type { GridLayoutModel } from "./types";
import {
  createDefaultGridLayout,
  serializeGridLayout,
  deserializeGridLayout,
} from "./utils/gridLayoutUtils";

export const gridStrategy: LayoutStrategy<GridLayoutModel> = {
  id: "grid",
  displayName: "Grid Layout",
  defaultLayout: createDefaultGridLayout,
  serialize: serializeGridLayout,
  deserialize: deserializeGridLayout,
  Renderer: GridRenderer,
};
