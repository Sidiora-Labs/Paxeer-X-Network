"use client";

import { RouteErrorBoundary } from "../../states";

export default function ExplorerRouteError({
  error,
  reset,
}: Readonly<{ error: Error & { digest?: string }; reset: () => void }>) {
  return <RouteErrorBoundary error={error} reset={reset} route="/explorer" />;
}
