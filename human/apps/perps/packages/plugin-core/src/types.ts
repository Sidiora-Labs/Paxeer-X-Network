import { ElementType, ReactElement, ReactNode } from "react";
import { NetworkId } from "@paxeer-network/types";
import type { PluginEventsAPI } from "./apis/events";

/* ========== New Plugin System (path-based interceptor) ========== */

/** API Facade for plugins (placeholder, extend later) */
export interface PaxeerNetworkPluginAPI {
  data?: Record<string, unknown>;
  actions?: Record<string, unknown>;
  utils?: Record<string, unknown>;
  /** Event subscription - forwards to global EventEmitter (SimpleDI "EE") */
  events: PluginEventsAPI;
}

/**
 * Mapping from interceptor target path to component props type.
 * Extend via module augmentation in UI packages (e.g. @paxeer-network/trading)
 * to enable typed props in createInterceptor(target, (Original, props, api) => ...).
 * No index signature so that augmented entries keep their exact prop types.
 */
export interface InterceptorTargetPropsMap {}

/** Union of known interceptor target paths (keys of InterceptorTargetPropsMap) */
export type KnownInterceptorTarget = keyof InterceptorTargetPropsMap;

/**
 * Descriptor for one interceptable target (path + optional props type name for docs).
 * Returned lazily by window.__PAXEER_NETWORK_INTERCEPTOR_TARGETS_REGISTRY__() when called.
 */
export interface InterceptorTargetDescriptor {
  path: string;
  /** Props type name (e.g. 'DepositFormProps') for documentation / Inspector */
  propsType?: string;
  description?: string;
}

declare global {
  interface Window {
    /** Lazy getter: when called, collects supported targets from current plugins' interceptors. Set by PaxeerNetworkPluginProvider. Same style as __PAXEER_NETWORK_EXTENSION_REGISTRY__. */
    __PAXEER_NETWORK_INTERCEPTOR_TARGETS_REGISTRY__?: () => InterceptorTargetDescriptor[];
  }
}

/**
 * Interceptor component signature: (Original, props, api) => ReactNode.
 * Use generic TProps to get typed props when targeting a known path.
 */
export type PluginInterceptorComponent<
  TProps extends object = Record<string, unknown>,
> = (
  Original: React.ComponentType<TProps>,
  props: TProps,
  api: PaxeerNetworkPluginAPI,
) => ReactNode;

/** Single interceptor targeting a component path. TProps defaults to Record<string, unknown> for backward compat. */
export interface PluginInterceptor<
  TProps extends object = Record<string, unknown>,
> {
  target: string;
  component: PluginInterceptorComponent<TProps>;
}

/** Plugin descriptor (per GUIDE.md) */
export interface PaxeerNetworkPlugin {
  id: string;
  name?: string;
  version?: string;
  paxeerNetworkVersion?: string;
  /** Interceptors targeting component paths (e.g. Trading.OrderEntry.SubmitButton). Uses array of any to accept interceptors whose props are augmented (and may not have index signature required by Record<string, unknown>). */
  interceptors?: Array<PluginInterceptor<any>>;
  setup?: (api: PaxeerNetworkPluginAPI) => void;
  onInitialize?: () => void;
  onInstall?: () => void | Promise<void>;
  onError?: (error: Error) => void;
  onFallback?: () => ReactNode | ReactElement;
}

/** SDK instance passed to plugin registration fn */
export interface PaxeerNetworkSDK {
  registerPlugin: (descriptor: PaxeerNetworkPlugin) => void;
}

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

export interface ApplicationState {
  config: {
    appIcons?: AppLogos;
    brokerName: string;
    dateFormatting?: string;
  };
  networkId: NetworkId;
}

/** Plugin registration function: (SDK, state?) => void; calls SDK.registerPlugin internally */
export type PluginRegistrationFn<TState = ApplicationState> = (
  SDK: PaxeerNetworkSDK,
  state?: TState,
) => void;

/* ========== Legacy Extension System (position-based) ========== */

/** @deprecated Use PluginInterceptor with path-based target instead */
export type ExtensionBuilder<Props = Record<string, unknown>> = (
  props?: Partial<Props> & Record<string, unknown>,
) => Props;

/** @deprecated Use path strings (e.g. 'Deposit.DepositForm') instead */
export type ExtensionPosition = ExtensionPositionEnum | string;

/** @deprecated Use PaxeerNetworkPlugin with interceptors instead */
export interface Extension<Props extends object = object> {
  __isInternal: boolean;
  name: string;
  positions: ExtensionPosition[];
  initialize?: () => void;
  builder?: ExtensionBuilder<Props>;
  render: ElementType<Props> | ((props: Props) => ReactElement);
}

/** @deprecated Use path strings (e.g. 'Deposit.DepositForm') instead */
export enum ExtensionPositionEnum {
  DepositForm = "depositForm",
  WithdrawForm = "withdrawForm",
  AccountMenu = "accountMenu",
  MobileAccountMenu = "mobileAccountMenu",
  MainMenus = "mainMenus",
  EmptyDataIdentifier = "emptyDataIdentifier",
  /** @deprecated Use EmptyDataIdentifier - same value for backward compatibility */
  // eslint-disable-next-line @typescript-eslint/no-duplicate-enum-values -- intentional alias for backward compat
  EmptyDataState = "emptyDataIdentifier",
}
