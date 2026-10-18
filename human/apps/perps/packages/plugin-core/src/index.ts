export {
  PluginScopeContext,
  PluginScopeProvider,
  usePluginScope,
  type PluginScopeValue,
} from "./pluginScopeContext";

export {
  PaxeerNetworkPluginProvider,
  usePaxeerNetworkPluginContext,
  useInterceptorTargets,
  ExtensionProvider,
  useExtensionContext,
  type PaxeerNetworkPluginContextValue,
} from "./pluginContext";

export {
  installExtension,
  setExtensionBuilder,
  type ExtensionOptions,
} from "./install";
export { ExtensionSlot } from "./slot";
export { useInjectedComponent } from "./useInjectedComponent";
export { injectable } from "./injectable";
export { positionToPath } from "./pathMap";
export { PaxeerNetworkPluginRegistry, createPaxeerNetworkSDK } from "./pluginRegistry";
export { createInterceptor } from "./createInterceptor";
export { PaxeerNetworkExtensionRegistry } from "./registry";
export { PluginErrorBoundary } from "./PluginErrorBoundary";

export {
  type ExtensionPosition,
  ExtensionPositionEnum,
  type PaxeerNetworkPlugin,
  type PaxeerNetworkPluginAPI,
  type PaxeerNetworkSDK,
  type PluginRegistrationFn,
  type PluginInterceptor,
  type PluginInterceptorComponent,
  type InterceptorTargetDescriptor,
  type InterceptorTargetPropsMap,
  type KnownInterceptorTarget,
  type Extension,
  type ExtensionBuilder,
  type ApplicationState,
} from "./types";

export { createEventsFacade, type PluginEventsAPI } from "./apis/events";
