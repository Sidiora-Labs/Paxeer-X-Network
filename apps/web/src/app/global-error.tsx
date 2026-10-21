"use client";

import { RouteErrorBoundary } from "../states";

export default function GlobalError({
  error,
  reset,
}: Readonly<{ error: Error & { digest?: string }; reset: () => void }>) {
  return (
    <html lang="en">
      <body><RouteErrorBoundary error={error} reset={reset} route="/" /></body>
    </html>
  );
}
