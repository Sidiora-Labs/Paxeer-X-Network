"use client";

import type { PerformanceRoute, WebVitalName } from "../perf/budgets";

export function submitWebVital(
  route: PerformanceRoute,
  metric: WebVitalName,
  observed: number,
): void {
  const body = JSON.stringify({ version: 1, route, metric, observed });
  if (
    navigator.sendBeacon(
      "/api/performance/vitals",
      new Blob([body], { type: "application/json" }),
    )
  ) {
    return;
  }
  void fetch("/api/performance/vitals", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body,
    credentials: "same-origin",
    keepalive: true,
  }).catch(() => undefined);
}
