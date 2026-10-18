import { ComponentType, ReactNode } from "react";
import {
  ExclusiveConfigProviderProps,
  ConfigProviderProps,
} from "@paxeer-network/hooks";
import {
  ExtensionPosition,
  type PaxeerNetworkPlugin,
  type PluginRegistrationFn,
} from "@paxeer-network/ui";

type Logo = {
  // the logo image url
  img?: string;
  // also can use React component
  component?: ReactNode;
  className?: string;
};

export type AppLogos = Partial<{
  // logo for top navigation bar
  main: Logo;
  // logo for popover/dialog header
  secondary: Logo;
}>;

export type PaxeerNetworkAppConfig = {
  appIcons?: AppLogos;
  dateFormatting?: string;
  /** Plugins for the new interceptor system (registerPlugin descriptors or registration fns) */
  plugins?: (PaxeerNetworkPlugin | PluginRegistrationFn)[];
  components?: {
    [position in ExtensionPosition]: ComponentType;
  };
} & Partial<
  Omit<
    ConfigProviderProps,
    "brokerId" | "brokerName" | "configStore" | "networkId"
  >
> &
  ExclusiveConfigProviderProps;
