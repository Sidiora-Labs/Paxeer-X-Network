/**
 * SplitInlinedLayout - Legacy component kept for backward compatibility.
 *
 * The split plugin no longer uses this component (plugin.tsx now renders
 * SplitTradingLayout directly). Kept exported so external consumers are not broken.
 */
import React from "react";
import type { LayoutStrategy } from "@paxeer-network/layout-core";
import { splitStrategy } from "../splitStrategy";
import type { DesktopLayoutProps } from "../types";
import { SplitTradingDesktopChrome } from "./SplitTradingDesktopChrome";

/** @deprecated Use SplitTradingLayout directly. */
export function SplitInlinedLayout({
  Original,
  props,
}: {
  Original: React.ComponentType<DesktopLayoutProps>;
  props: DesktopLayoutProps;
}): React.ReactElement {
  const layoutStrategy =
    (props.layoutStrategy as LayoutStrategy | undefined) ??
    (splitStrategy as unknown as LayoutStrategy);

  return (
    <SplitTradingDesktopChrome {...props}>
      <Original {...props} layoutStrategy={layoutStrategy} />
    </SplitTradingDesktopChrome>
  );
}
