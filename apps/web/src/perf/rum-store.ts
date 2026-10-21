import "server-only";

import {
  isPerformanceRoute,
  isWebVitalName,
  normalizedBudget,
  percentile75,
  RUM_SERIES_CAPACITY,
  type PerformanceRoute,
  type WebVitalName,
} from "./budgets";

export interface RedactedWebVital {
  readonly version: 1;
  readonly route: PerformanceRoute;
  readonly metric: WebVitalName;
  readonly observed: number;
}

export interface RumAggregate {
  readonly route: PerformanceRoute;
  readonly metric: WebVitalName;
  readonly samples: number;
  readonly percentile75: number;
  readonly withinBudget: boolean;
}

interface RumState {
  readonly series: Map<string, number[]>;
}

type RumGlobal = typeof globalThis & { __layerxDevelopmentRumState?: RumState };

const runtime = globalThis as RumGlobal;
const state = runtime.__layerxDevelopmentRumState ?? { series: new Map<string, number[]>() };
runtime.__layerxDevelopmentRumState = state;

function isObject(value: unknown): value is Readonly<Record<string, unknown>> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function parseRedactedWebVital(value: unknown): RedactedWebVital | undefined {
  if (!isObject(value)) {
    return undefined;
  }
  const keys = Object.keys(value).sort();
  if (keys.join(",") !== "metric,observed,route,version") {
    return undefined;
  }
  const { version, route, metric, observed } = value;
  if (
    version !== 1 ||
    typeof route !== "string" ||
    !isPerformanceRoute(route) ||
    typeof metric !== "string" ||
    !isWebVitalName(metric) ||
    typeof observed !== "number" ||
    !Number.isSafeInteger(observed) ||
    observed < 0 ||
    observed > (metric === "CLS" ? 10_000_000 : 120_000_000)
  ) {
    return undefined;
  }
  return Object.freeze({ version, route, metric, observed });
}

export function recordWebVital(observation: RedactedWebVital): void {
  const key = `${observation.route}:${observation.metric}`;
  const series = state.series.get(key) ?? [];
  series.push(observation.observed);
  if (series.length > RUM_SERIES_CAPACITY) {
    series.splice(0, series.length - RUM_SERIES_CAPACITY);
  }
  state.series.set(key, series);
}

export function rumSnapshot(): readonly RumAggregate[] {
  return [...state.series.entries()]
    .map(([key, values]): RumAggregate => {
      const [route, metric] = key.split(":");
      if (
        route === undefined ||
        metric === undefined ||
        !isPerformanceRoute(route) ||
        !isWebVitalName(metric)
      ) {
        throw new TypeError("The development RUM fallback contains an invalid key");
      }
      const observed = percentile75(values);
      return Object.freeze({
        route,
        metric,
        samples: values.length,
        percentile75: observed,
        withinBudget: observed <= normalizedBudget(metric),
      });
    })
    .sort((left, right) =>
      `${left.route}:${left.metric}`.localeCompare(`${right.route}:${right.metric}`),
    );
}
