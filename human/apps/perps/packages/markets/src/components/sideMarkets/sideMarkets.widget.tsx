import React from "react";
import { pick } from "ramda";
import { MarketsProvider, MarketsProviderProps } from "../marketsProvider";
import { useSideMarketsScript } from "./sideMarkets.script";
import type { SideMarketsProps } from "./sideMarkets.ui";

const LazySideMarkets = React.lazy(() =>
  import("./sideMarkets.ui").then((mod) => {
    return { default: mod.SideMarkets };
  }),
);

export type SideMarketsWidgetProps = MarketsProviderProps &
  Partial<Pick<SideMarketsProps, "className">> & {
    panelSize?: "small" | "middle" | "large";
    collapsed?: boolean;
  };

export const SideMarketsWidget: React.FC<SideMarketsWidgetProps> = (props) => {
  const state = useSideMarketsScript(props);

  return (
    <MarketsProvider {...pick(["symbol", "onSymbolChange"], props)}>
      <React.Suspense fallback={null}>
        <LazySideMarkets {...state} className={props.className} />
      </React.Suspense>
    </MarketsProvider>
  );
};
