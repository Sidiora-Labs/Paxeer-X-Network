import React, {
  createContext,
  useContext,
  useMemo,
  type ComponentType,
} from "react";
import { PluginErrorBoundary } from "./PluginErrorBoundary";
import { usePaxeerNetworkPluginContext } from "./pluginContext";
import { PluginScopeProvider } from "./pluginScopeContext";
import type { PaxeerNetworkPluginAPI, PluginInterceptorComponent } from "./types";

type ResolvedInterceptor<P extends object> = {
  identity: string;
  pluginId: string;
  pluginName?: string;
  pluginVersion?: string;
  component: PluginInterceptorComponent<P>;
  onError?: (error: Error) => void;
  onFallback?: () => React.ReactNode;
};

type InterceptorChainProps<P extends object> = {
  interceptors: ResolvedInterceptor<P>[];
  index: number;
  DefaultComponent: ComponentType<P>;
  componentProps: P;
  apiFacade: PaxeerNetworkPluginAPI;
};

const InterceptorChainContext = createContext<InterceptorChainProps<
  Record<string, unknown>
> | null>(null);

const OriginalBridge = (props: Record<string, unknown>) => {
  const chain = useContext(InterceptorChainContext);

  if (!chain) {
    return null;
  }

  return <InterceptorChain {...chain} componentProps={props} />;
};

const InterceptorExecutor = <P extends object>({
  interceptor,
  componentProps,
  apiFacade,
}: {
  interceptor: ResolvedInterceptor<P>;
  componentProps: P;
  apiFacade: PaxeerNetworkPluginAPI;
}) => {
  return (
    <>
      {interceptor.component(
        OriginalBridge as ComponentType<P>,
        componentProps,
        apiFacade,
      )}
    </>
  );
};

const InterceptorChain = <P extends object>({
  interceptors,
  index,
  DefaultComponent,
  componentProps,
  apiFacade,
}: InterceptorChainProps<P>) => {
  if (index < 0) {
    return <DefaultComponent {...componentProps} />;
  }

  const interceptor = interceptors[index];
  const nextChain = {
    interceptors,
    index: index - 1,
    DefaultComponent,
    componentProps,
    apiFacade,
  } as InterceptorChainProps<Record<string, unknown>>;

  return (
    <PluginScopeProvider
      key={interceptor.identity}
      pluginId={interceptor.pluginId}
      pluginName={interceptor.pluginName}
      pluginVersion={interceptor.pluginVersion}
    >
      <PluginErrorBoundary
        pluginId={interceptor.pluginId}
        onError={interceptor.onError}
        onFallback={interceptor.onFallback}
      >
        <InterceptorChainContext.Provider value={nextChain}>
          <InterceptorExecutor
            interceptor={interceptor}
            componentProps={componentProps}
            apiFacade={apiFacade}
          />
        </InterceptorChainContext.Provider>
      </PluginErrorBoundary>
    </PluginScopeProvider>
  );
};

const InjectedRenderer = <P extends object>({
  name,
  DefaultComponent,
  componentProps,
}: {
  name: string;
  DefaultComponent: ComponentType<P>;
  componentProps: P;
}) => {
  const { plugins, apiFacade } = usePaxeerNetworkPluginContext();

  const interceptors = useMemo(
    () =>
      plugins.flatMap((plugin) =>
        (plugin.interceptors ?? [])
          .filter((interceptor) => interceptor.target === name)
          .map((interceptor, index) => ({
            identity: `${plugin.id}:${name}:${index}`,
            pluginId: plugin.id,
            pluginName: plugin.name,
            pluginVersion: plugin.version,
            component: interceptor.component as PluginInterceptorComponent<P>,
            onError: plugin.onError,
            onFallback: plugin.onFallback,
          })),
      ),
    [plugins, name],
  );

  return (
    <InterceptorChain
      interceptors={interceptors}
      index={interceptors.length - 1}
      DefaultComponent={DefaultComponent}
      componentProps={componentProps}
      apiFacade={apiFacade}
    />
  );
};

/**
 * Returns a stable component that chains all interceptors for the given path.
 * Plugin context updates refresh the chain data without replacing the component
 * type, so stateful injected components are not remounted unnecessarily.
 */
export function useInjectedComponent<P extends object>(
  name: string,
  DefaultComponent: ComponentType<P>,
): ComponentType<P> {
  return useMemo(() => {
    const StableInjectedComponent = (props: P) => (
      <InjectedRenderer
        name={name}
        DefaultComponent={DefaultComponent}
        componentProps={props}
      />
    );

    StableInjectedComponent.displayName = `Injected(${name})`;
    return StableInjectedComponent;
  }, [name, DefaultComponent]);
}
