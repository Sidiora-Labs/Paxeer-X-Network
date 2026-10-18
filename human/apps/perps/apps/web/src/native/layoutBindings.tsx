import type { GridLayoutItemSpec } from "@paxeer-network/layout-grid/types";
import React, { useCallback } from "react";
import {
  GridPresetProvider,
  useGridPresetContext,
} from "@paxeer-network/layout-grid/GridPresetContext";
import { gridStrategy } from "@paxeer-network/layout-grid/gridStrategy";
import { getDefaultGridPresets } from "@paxeer-network/layout-grid/utils/defaultPresets";
import { createTradingGridLayoutFromPreset } from "@paxeer-network/layout-grid/utils/tradingGridLayout";
import { splitTradingStrategy } from "@paxeer-network/layout-split/splitTradingStrategy";
import { createInterceptor, type PaxeerNetworkSDK } from "@paxeer-network/ui";
import { GridLayoutSwitcher } from "@paxeer-network/layout-grid/GridLayoutSwitcher";
import { useSplitLayout } from "@paxeer-network/layout-split/hooks/useSplitLayout";
import { SymbolBarLayoutSwitcher } from "@paxeer-network/layout-split/components/SymbolBarLayoutSwitcher";
function NativeSplitPicker() {
  const controls = useSplitLayout({ canTrade: false });
  return (
    <SymbolBarLayoutSwitcher
      layout={controls.layout}
      marketLayout={controls.marketLayout}
      onLayoutChange={controls.setLayout}
      onMarketLayoutChange={controls.setMarketLayout}
    />
  );
}
export function nativeLayoutBindings(grid: boolean) {
  return (SDK: PaxeerNetworkSDK) => {
    SDK.registerPlugin({
      id: "paxeer-network-layout-controls",
      name: "paxeer-network layout controls",
      version: "1.0.0",
      paxeerNetworkVersion: ">=1.0.0",
      interceptors: [
        createInterceptor(
          "Trading.SymbolInfoBar.Desktop",
          (Original, props) => (
            <Original
              {...props}
              trailing={
                <>
                  {props.trailing}
                  {grid ? (
                    <GridLayoutSwitcher className="oui-rounded-lg oui-bg-base-7 oui-px-3 oui-py-2 oui-text-xs" />
                  ) : (
                    <NativeSplitPicker />
                  )}
                </>
              }
            />
          ),
        ),
      ],
    });
  };
}

export function registerNativeSplitLayout(SDK: PaxeerNetworkSDK) {
  SDK.registerPlugin({
    id: "paxeer-network-layout-split",
    name: "paxeer-network split layout",
    version: "1.0.0",
    paxeerNetworkVersion: ">=1.0.0",
    interceptors: [
      createInterceptor("Trading.Layout.Desktop", (Original, props) => (
        <Original
          {...props}
          layoutStrategy={splitTradingStrategy}
          getInitialLayout={() => ({})}
          disableLayoutPersistence={true}
        />
      )),
      createInterceptor("Trading.SymbolInfoBar.Desktop", (Original, props) => (
        <Original
          {...props}
          trailing={
            <>
              {props.trailing}
              <NativeSplitPicker />
            </>
          }
        />
      )),
    ],
  });
}

const sourcePresets = getDefaultGridPresets();
const nativeGridPresets = [
  ...sourcePresets,
  {
    ...sourcePresets[0],
    id: "order-entry-left",
    name: "Order Entry Left",
    rule: Object.fromEntries(
      Object.entries(sourcePresets[0].rule).map(([breakpoint, items]) => [
        breakpoint,
        items?.map((item: GridLayoutItemSpec) => ({
          ...item,
          x: 24 - item.x - item.w,
        })),
      ]),
    ),
  },
];
function NativeGridDesktop({
  Original,
  props,
}: {
  Original: React.ComponentType<any>;
  props: Record<string, unknown>;
}) {
  const context = useGridPresetContext()!;
  const preset = context.presets.find(
    (item) => item.id === context.selectedPresetId,
  );
  const initial = useCallback(
    () => createTradingGridLayoutFromPreset(preset),
    [preset],
  );
  return (
    <Original
      {...props}
      layoutStrategy={gridStrategy}
      getInitialLayout={initial}
      storageKey={context.layoutStorageKey}
      disableLayoutPersistence={false}
    />
  );
}
export function registerNativeGridLayout(SDK: PaxeerNetworkSDK) {
  SDK.registerPlugin({
    id: "paxeer-network-layout-grid",
    name: "paxeer-network grid layout",
    version: "1.0.0",
    paxeerNetworkVersion: ">=1.0.0",
    interceptors: [
      createInterceptor("Trading.Layout.Desktop", (Original, props) => (
        <GridPresetProvider presets={nativeGridPresets} persistLayout>
          <NativeGridDesktop Original={Original} props={props} />
        </GridPresetProvider>
      )),
      createInterceptor("Trading.SymbolInfoBar.Desktop", (Original, props) => (
        <Original
          {...props}
          trailing={
            <>
              {props.trailing}
              <GridLayoutSwitcher className="oui-rounded-lg oui-bg-base-7 oui-px-3 oui-py-2 oui-text-xs" />
            </>
          }
        />
      )),
    ],
  });
}
