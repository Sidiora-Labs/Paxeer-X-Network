"use client";

import { usePathname } from "next/navigation";
import { useReportWebVitals } from "next/web-vitals";
import { useCallback, useRef } from "react";

import { submitWebVital } from "../api/performance-client";
import {
  classifyPerformanceRoute,
  isWebVitalName,
  normalizeWebVital,
} from "./budgets";

type ReportedWebVital = Readonly<{ name: string; value: number }>;

export function WebVitalsRum() {
  const pathname = usePathname();
  const route = useRef(classifyPerformanceRoute(pathname));
  route.current = classifyPerformanceRoute(pathname);
  const report = useCallback((metric: ReportedWebVital) => {
    if (route.current !== undefined && isWebVitalName(metric.name)) {
      const observed = normalizeWebVital(metric.name, metric.value);
      if (observed !== undefined) {
        submitWebVital(route.current, metric.name, observed);
      }
    }
  }, []);
  useReportWebVitals(report);
  return null;
}
