import React, { PropsWithChildren, useMemo } from "react";
import { PaxeerNetworkConfigProvider, useTrack } from "@paxeer-network/hooks";
import type { NetworkId } from "@paxeer-network/types";
import {
  LocaleProvider as UILocaleProvider,
  ModalProvider,
  PaxeerNetworkPluginProvider,
  Toaster,
  TooltipProvider,
  type PaxeerNetworkPlugin,
  type PaxeerNetworkThemeProviderProps,
  type PluginRegistrationFn,
} from "@paxeer-network/ui";
import { useBootstrap } from "../hooks/useBootstrap";
import { useExecutionReport } from "../hooks/useExecutionReport";
import { useUILocale } from "../hooks/useUILocale";
import { PaxeerNetworkAppConfig } from "../types";
import { AppConfigProvider } from "./appConfigProvider";
import { AppStateProvider, AppStateProviderProps } from "./appStateProvider";
import { AppThemeProvider } from "./appThemeProvider";

export type PaxeerNetworkAppProviderProps = PropsWithChildren<
  PaxeerNetworkAppConfig & AppStateProviderProps & PaxeerNetworkThemeProviderProps
>;

const EMPTY_PLUGINS: (PaxeerNetworkPlugin | PluginRegistrationFn)[] = [];

// Cannot be called outside of Provider because useExecutionReport requires usePaxeerNetworkContext.
const ExecutionReportListener: React.FC = () => {
  useExecutionReport();
  return null;
};

const PaxeerNetworkAppProvider: React.FC<PaxeerNetworkAppProviderProps> = (props) => {
  const {
    // dateFormatting,
    components,
    appIcons,
    plugins,
    themes,
    onChainChanged,
    defaultChain,
    widgetConfigs,
    ...configProps
  } = props;

  const configStore = configProps.configStore;
  const brokerName =
    props.brokerName ?? configStore?.get<string>("brokerName") ?? "paxeer-network";
  const networkId =
    configProps.networkId ??
    configStore?.get<NetworkId>("networkId") ??
    "testnet";

  useTrack();
  useBootstrap();

  const uiLocale = useUILocale();

  /**
   * Keep referential identity stable while inputs are unchanged so `PaxeerNetworkPluginProvider`
   * memo/effects (plugin list, `setup`, context) are not invalidated every parent render.
   */
  const pluginState = useMemo(
    () => ({
      config: { appIcons, brokerName },
      networkId,
    }),
    [appIcons, brokerName, networkId],
  );

  const pluginsList = plugins ?? EMPTY_PLUGINS;

  return (
    <AppConfigProvider appIcons={appIcons} brokerName={brokerName}>
      <AppThemeProvider
        // dateFormatting={dateFormatting}
        components={components}
        themes={themes}
        overrides={props.overrides}
      >
        <PaxeerNetworkConfigProvider {...configProps}>
          <PaxeerNetworkPluginProvider
            plugins={pluginsList}
            pluginState={pluginState}
          >
            <ExecutionReportListener />
            <AppStateProvider
              onChainChanged={onChainChanged}
              defaultChain={defaultChain}
              restrictedInfo={props.restrictedInfo}
              onRouteChange={props.onRouteChange}
              widgetConfigs={widgetConfigs}
            >
              <UILocaleProvider locale={uiLocale}>
                <TooltipProvider delayDuration={300}>
                  <ModalProvider>{props.children}</ModalProvider>
                </TooltipProvider>
              </UILocaleProvider>
            </AppStateProvider>
          </PaxeerNetworkPluginProvider>
          <Toaster />
        </PaxeerNetworkConfigProvider>
      </AppThemeProvider>
    </AppConfigProvider>
  );
};

if (process.env.NODE_ENV !== "production") {
  PaxeerNetworkAppProvider.displayName = "PaxeerNetworkAppProvider";
}

export { PaxeerNetworkAppProvider };
