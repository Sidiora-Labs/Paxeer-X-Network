import type {} from "@paxeer-network/trading-next";
import type { LayoutStrategy } from "@paxeer-network/layout-core";
import type { PaxeerNetworkSDK } from "@paxeer-network/ui";
import { createInterceptor } from "@paxeer-network/ui";
import { SymbolBarLayoutSwitcher } from "./components/SymbolBarLayoutSwitcher";
import { splitTradingStrategy } from "./splitTradingStrategy";

/** Plugin registration options */
export interface LayoutSplitPluginOptions {
  gap?: number;
  classNames?: {
    panelGroup?: string;
    panel?: string;
    handle?: string;
    container?: string;
  };
}

const PLUGIN_ID = "paxeer-network-layout-split";
const PLUGIN_NAME = "Split Layout";
const PLUGIN_VERSION = "1.0.0";

/**
 * Registers the split trading layout plugin.
 *
 * Intercepts `Trading.Layout.Desktop` and injects `layoutStrategy` into the
 * original DesktopLayout component — exactly like GridDesktopInjector.
 * DesktopLayout calls LayoutHost, which calls splitTradingStrategy.Renderer
 * (SplitTradingLayout) with the PanelRegistry built by trading-next.
 */
export function registerLayoutSplitPlugin(
  options: LayoutSplitPluginOptions = {},
): (SDK: PaxeerNetworkSDK) => void {
  void options;
  return (SDK: PaxeerNetworkSDK) => {
    SDK.registerPlugin({
      id: PLUGIN_ID,
      name: PLUGIN_NAME,
      version: PLUGIN_VERSION,
      paxeerNetworkVersion: ">=1.0.0",
      interceptors: [
        createInterceptor("Trading.Layout.Desktop", (Original, props) => (
          <Original
            {...props}
            layoutStrategy={splitTradingStrategy as unknown as LayoutStrategy}
            // State is managed internally by SplitTradingLayout — no model to persist here.
            getInitialLayout={() => ({})}
            disableLayoutPersistence={true}
          />
        )),
        createInterceptor(
          "Trading.SymbolInfoBar.Desktop",
          (Original, props) => (
            <Original
              {...props}
              trailing={
                <>
                  {props.trailing}
                  <SymbolBarLayoutSwitcher />
                </>
              }
            />
          ),
        ),
      ],
    });
  };
}
