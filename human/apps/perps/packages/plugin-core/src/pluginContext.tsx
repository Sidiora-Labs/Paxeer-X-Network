import React, {
  createContext,
  useContext,
  useMemo,
  useEffect,
  useRef,
  type FC,
  type PropsWithChildren,
} from "react";
import { getGlobalObject } from "@paxeer-network/utils";
import { createEventsFacade, NOOP_EVENTS } from "./apis/events";
import { getRegisteredInjectableTargets } from "./injectableTargetRegistry";
import { PaxeerNetworkPluginRegistry } from "./pluginRegistry";
import type {
  ApplicationState,
  InterceptorTargetDescriptor,
  PaxeerNetworkPlugin,
  PaxeerNetworkPluginAPI,
  PluginRegistrationFn,
} from "./types";

/** Global key for interceptor targets getter (same style as __PAXEER_NETWORK_EXTENSION_REGISTRY__) */
const PAXEER_NETWORK_INTERCEPTOR_TARGETS_REGISTRY_KEY =
  "__PAXEER_NETWORK_INTERCEPTOR_TARGETS_REGISTRY__" as const;

/** Collects unique interceptor targets from plugins (targets that have at least one interceptor). */
function collectTargetsFromPlugins(
  plugins: PaxeerNetworkPlugin[],
): InterceptorTargetDescriptor[] {
  const seen = new Set<string>();
  const list: InterceptorTargetDescriptor[] = [];
  for (const p of plugins) {
    for (const i of p.interceptors ?? []) {
      if (i.target && !seen.has(i.target)) {
        seen.add(i.target);
        list.push({ path: i.target });
      }
    }
  }
  return list;
}

/** Merges plugin interceptor targets with statically registered injectable paths (unique by path). */
function mergeInterceptorTargets(
  fromPlugins: InterceptorTargetDescriptor[],
): InterceptorTargetDescriptor[] {
  const byPath = new Map<string, InterceptorTargetDescriptor>();
  for (const t of fromPlugins) {
    byPath.set(t.path, t);
  }
  for (const path of getRegisteredInjectableTargets()) {
    if (!byPath.has(path)) {
      byPath.set(path, { path });
    }
  }
  return Array.from(byPath.values());
}

export type ExtensionContextState = Record<string, never>;

export const ExtensionContext = createContext<ExtensionContextState>({});

export const useExtensionContext = () =>
  useContext<ExtensionContextState>(ExtensionContext);

export const ExtensionProvider: React.FC<React.PropsWithChildren> = ({
  children,
}) => {
  const memoizedValue = useMemo<ExtensionContextState>(() => ({}), []);
  return (
    <ExtensionContext.Provider value={memoizedValue}>
      {children}
    </ExtensionContext.Provider>
  );
};

export interface PaxeerNetworkPluginContextValue {
  plugins: PaxeerNetworkPlugin[];
  apiFacade: PaxeerNetworkPluginAPI;
  /** List of interceptable targets (when provided by host); used for window registry and optional useInterceptorTargets */
  interceptorTargets: InterceptorTargetDescriptor[];
}

const DEFAULT_PLUGIN_CONTEXT: PaxeerNetworkPluginContextValue = {
  plugins: [],
  apiFacade: { events: NOOP_EVENTS },
  interceptorTargets: [],
};

export const PaxeerNetworkPluginContext =
  createContext<PaxeerNetworkPluginContextValue | null>(null);

export const usePaxeerNetworkPluginContext = (): PaxeerNetworkPluginContextValue => {
  const context = useContext(PaxeerNetworkPluginContext);
  return context ?? DEFAULT_PLUGIN_CONTEXT;
};

/** Returns the list of interceptable targets when provided via PaxeerNetworkPluginProvider (e.g. for in-app dev panel). */
export const useInterceptorTargets = (): InterceptorTargetDescriptor[] => {
  const { interceptorTargets } = usePaxeerNetworkPluginContext();
  return interceptorTargets;
};

export interface PaxeerNetworkPluginProviderProps extends PropsWithChildren {
  plugins: (PaxeerNetworkPlugin | PluginRegistrationFn)[];
  /** Optional state passed to plugin registration fns (e.g. AppState) */
  pluginState?: ApplicationState;
}

export const PaxeerNetworkPluginProvider: FC<PaxeerNetworkPluginProviderProps> = ({
  plugins,
  pluginState,
  children,
}) => {
  const pluginsRef = useRef<PaxeerNetworkPlugin[]>([]);

  const resolvedPlugins = useMemo(() => {
    const collected: PaxeerNetworkPlugin[] = [];
    const sdk = {
      registerPlugin: (descriptor: PaxeerNetworkPlugin) => collected.push(descriptor),
    };
    plugins.forEach((p) => {
      if (typeof p === "function") {
        p(sdk, pluginState);
      } else {
        collected.push(p);
      }
    });
    return [...collected, ...PaxeerNetworkPluginRegistry.getPlugins()];
  }, [plugins, pluginState]);

  const apiFacade = useMemo<PaxeerNetworkPluginAPI>(
    () => ({ events: createEventsFacade() }),
    [],
  );

  useEffect(() => {
    resolvedPlugins.forEach((plugin) => {
      if (typeof plugin.setup === "function") {
        try {
          plugin.setup(apiFacade);
        } catch (e) {
          console.error(`[Plugin ${plugin.id}] setup failed:`, e);
        }
      }
    });
  }, [resolvedPlugins, apiFacade]);

  pluginsRef.current = resolvedPlugins;

  // Lazy: only when user calls window.__PAXEER_NETWORK_INTERCEPTOR_TARGETS_REGISTRY__() we collect targets from current plugins
  useEffect(() => {
    const g = getGlobalObject() as Record<string, unknown>;
    g[PAXEER_NETWORK_INTERCEPTOR_TARGETS_REGISTRY_KEY] = () => {
      const fromPlugins = collectTargetsFromPlugins(pluginsRef.current);
      return mergeInterceptorTargets(fromPlugins);
    };
    return () => {
      delete (getGlobalObject() as Record<string, unknown>)[
        PAXEER_NETWORK_INTERCEPTOR_TARGETS_REGISTRY_KEY
      ];
    };
  }, []);

  const interceptorTargets = useMemo(
    () => mergeInterceptorTargets(collectTargetsFromPlugins(resolvedPlugins)),
    [resolvedPlugins],
  );
  const value = useMemo<PaxeerNetworkPluginContextValue>(
    () => ({ plugins: resolvedPlugins, apiFacade, interceptorTargets }),
    [resolvedPlugins, apiFacade, interceptorTargets],
  );

  return (
    <PaxeerNetworkPluginContext.Provider value={value}>
      {children}
    </PaxeerNetworkPluginContext.Provider>
  );
};
