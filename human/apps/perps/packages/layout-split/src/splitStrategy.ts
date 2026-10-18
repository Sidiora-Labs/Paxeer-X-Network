import type { LayoutStrategy } from "@paxeer-network/layout-core";
import { SplitRenderer } from "./SplitRenderer";
import type { SplitLayoutModel } from "./types";
import {
  createDefaultSplitLayout,
  serializeSplitLayout,
  deserializeSplitLayout,
} from "./utils/splitLayoutUtils";

/**
 * Split layout strategy implementation
 * Provides split-pane layout with resizable panels
 */
export const splitStrategy: LayoutStrategy<SplitLayoutModel> = {
  id: "split",
  displayName: "Split Layout",
  defaultLayout: createDefaultSplitLayout,
  serialize: serializeSplitLayout,
  deserialize: deserializeSplitLayout,
  Renderer: SplitRenderer,
};
