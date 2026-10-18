/**
 * Side-effect: augment InterceptorTargetPropsMap for typed interceptor props.
 */
import "./interceptorTargets";

/**
 * Re-export plugin system from @paxeer-network/plugin-core for backward compatibility.
 * @deprecated Import from @paxeer-network/plugin-core directly for new code
 */
export {
  installExtension,
  setExtensionBuilder,
  ExtensionSlot,
  PaxeerNetworkPluginProvider,
  usePaxeerNetworkPluginContext,
  useInjectedComponent,
  injectable,
  positionToPath,
  PaxeerNetworkPluginRegistry,
  createPaxeerNetworkSDK,
  PaxeerNetworkExtensionRegistry,
  PluginErrorBoundary,
  createInterceptor,
  PluginScopeContext,
  PluginScopeProvider,
  usePluginScope,
  ExtensionProvider,
  useExtensionContext,
  type ExtensionPosition,
  ExtensionPositionEnum,
  type PaxeerNetworkPlugin,
  type PaxeerNetworkPluginAPI,
  type PaxeerNetworkSDK,
  type PluginRegistrationFn,
  type PluginInterceptor,
  type PluginInterceptorComponent,
  type Extension,
  type ExtensionBuilder,
  type ExtensionOptions,
  type PaxeerNetworkPluginContextValue,
  type PluginScopeValue,
  type InterceptorTargetPropsMap,
  type KnownInterceptorTarget,
} from "@paxeer-network/plugin-core";

export type { DepositFormProps } from "./plugins/deposit";
