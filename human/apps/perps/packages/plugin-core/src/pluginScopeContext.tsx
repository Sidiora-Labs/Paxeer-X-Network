import React, {
  createContext,
  useContext,
  useMemo,
  type FC,
  type PropsWithChildren,
} from "react";

/**
 * Plugin scope value: identifies the current plugin for API request attribution.
 * When useQuery etc. run inside PluginScopeProvider, they add X-paxeer-network-Plugin-Id header.
 */
export interface PluginScopeValue {
  pluginId: string;
  pluginName?: string;
  pluginVersion?: string;
}

export const PluginScopeContext = createContext<PluginScopeValue | null>(null);

export const usePluginScope = (): PluginScopeValue | null =>
  useContext(PluginScopeContext);

export interface PluginScopeProviderProps
  extends PropsWithChildren, PluginScopeValue {}

/**
 * Wraps plugin output so useQuery/useMutation etc. can inject X-paxeer-network-Plugin-Id header.
 */
export const PluginScopeProvider: FC<PluginScopeProviderProps> = ({
  pluginId,
  pluginName,
  pluginVersion,
  children,
}) => {
  const value = useMemo<PluginScopeValue>(
    () => ({ pluginId, pluginName, pluginVersion }),
    [pluginId, pluginName, pluginVersion],
  );
  return (
    <PluginScopeContext.Provider value={value}>
      {children}
    </PluginScopeContext.Provider>
  );
};
