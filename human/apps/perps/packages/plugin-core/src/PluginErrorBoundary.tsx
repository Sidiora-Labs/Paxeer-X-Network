import React, { type ReactNode } from "react";
import { ErrorBoundary } from "react-error-boundary";

/** Default fallback when plugin has no onFallback */
const DEFAULT_FALLBACK = (pluginId: string) => (
  <div style={{ color: "#ef4444", fontSize: "12px" }}>
    [Plugin {pluginId} error]
  </div>
);

/**
 * Error boundary for plugin interceptors.
 * Prevents a single plugin crash from taking down the whole page.
 * Uses plugin's onError/onFallback when provided, else SDK defaults.
 */
export const PluginErrorBoundary: React.FC<{
  children: ReactNode;
  /** Override fallback (legacy); prefer onFallback from plugin */
  fallback?: ReactNode;
  pluginId?: string;
  /** Plugin's error handler; called when error is caught */
  onError?: (error: Error) => void;
  /** Plugin's fallback renderer; used when error is caught */
  onFallback?: () => ReactNode;
}> = ({ children, fallback, pluginId, onError, onFallback }) => {
  const resolvedFallback =
    fallback ??
    (onFallback ? onFallback() : DEFAULT_FALLBACK(pluginId ?? "unknown"));

  return (
    <ErrorBoundary
      fallback={resolvedFallback}
      onError={(error) => onError?.(error)}
    >
      {children}
    </ErrorBoundary>
  );
};
