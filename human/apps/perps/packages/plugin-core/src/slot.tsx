import React from "react";
import { PluginErrorBoundary } from "./PluginErrorBoundary";
import { NotFound } from "./notFound";
import { positionToPath } from "./pathMap";
import { ExtensionPosition } from "./types";
import { useInjectedComponent } from "./useInjectedComponent";

interface SlotProps {
  position: ExtensionPosition;
  defaultWidget?: React.ComponentType<any>;
  scope?: string[];
  [key: string]: any;
}

/**
 * Renders the component for the given position, resolving interceptors via useInjectedComponent.
 * Requires PaxeerNetworkPluginProvider in the tree (can have plugins=[]).
 * @deprecated Prefer useInjectedComponent with path string directly (e.g. 'Deposit.DepositForm')
 */
export const ExtensionSlot: React.FC<SlotProps> = (props) => {
  const { position, defaultWidget: defaultValue, ...rest } = props;
  const path = positionToPath(position);
  const DefaultComponent =
    defaultValue ?? (() => <NotFound position={String(position)} />);
  const Injected = useInjectedComponent(path, DefaultComponent);

  return (
    <PluginErrorBoundary pluginId={path}>
      <Injected {...rest} />
    </PluginErrorBoundary>
  );
};
