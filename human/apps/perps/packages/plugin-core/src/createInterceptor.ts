import type {
  InterceptorTargetPropsMap,
  KnownInterceptorTarget,
  PluginInterceptor,
  PluginInterceptorComponent,
} from "./types";

/**
 * String-target overload: use when `@paxeer-network/plugin-core` is the only dependency and
 * `InterceptorTargetPropsMap` is empty (ambient augmentation from UI packages not loaded).
 * Avoids DTS failures where keyof an empty map is `never`.
 */
export function createInterceptor(
  target: string,
  component: PluginInterceptorComponent<Record<string, unknown>>,
): PluginInterceptor<Record<string, unknown>>;
/** Typed overload when workspace packages augment `InterceptorTargetPropsMap`. */
export function createInterceptor<T extends KnownInterceptorTarget>(
  target: T,
  component: PluginInterceptorComponent<InterceptorTargetPropsMap[T]>,
): PluginInterceptor<InterceptorTargetPropsMap[T]>;
export function createInterceptor(
  target: string,
  component: PluginInterceptorComponent<Record<string, unknown>>,
): PluginInterceptor<Record<string, unknown>> {
  return { target, component };
}
