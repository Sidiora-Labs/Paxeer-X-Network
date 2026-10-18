import type {} from "@paxeer-network/trading-next";
import React from "react";
import type { PaxeerNetworkSDK } from "@paxeer-network/ui";
import { createInterceptor } from "@paxeer-network/ui";
import { GridDesktopInjector } from "./components/GridDesktopInjector";
import type { LayoutGridPluginOptions } from "./types";
import { getDefaultGridPresets } from "./utils/defaultPresets";

const PLUGIN_ID = "paxeer-network-layout-grid";
const PLUGIN_NAME = "Grid Layout";
const PLUGIN_VERSION = "1.0.0";

export type { LayoutGridPluginOptions } from "./types";

/** Registers the grid layout plugin (Trading.Layout.Desktop interceptor). */
export function registerLayoutGridPlugin(
  options?: LayoutGridPluginOptions,
): (SDK: PaxeerNetworkSDK) => void {
  const resolvedPresets = options?.presets ?? getDefaultGridPresets();

  return (SDK: PaxeerNetworkSDK) => {
    SDK.registerPlugin({
      id: PLUGIN_ID,
      name: PLUGIN_NAME,
      version: PLUGIN_VERSION,
      paxeerNetworkVersion: ">=1.0.0",
      interceptors: [
        createInterceptor("Trading.Layout.Desktop", (Original, props) => (
          <GridDesktopInjector
            Original={
              Original as unknown as React.ComponentType<
                Record<string, unknown>
              >
            }
            props={props as unknown as Record<string, unknown>}
            options={{
              ...options,
              presets: resolvedPresets,
            }}
          />
        )),
      ],
    });
  };
}
