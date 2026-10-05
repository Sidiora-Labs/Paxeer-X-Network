import "server-only";

import { randomUUID } from "node:crypto";
import { constants, type Stats } from "node:fs";
import {
  lstat,
  mkdir,
  open,
  readFile,
  readdir,
  realpath,
  rename,
  unlink,
} from "node:fs/promises";
import path from "node:path";

import {
  normalizedBudget,
  percentile75,
  RUM_SERIES_CAPACITY,
  type PerformanceRoute,
  type WebVitalName,
} from "../perf/budgets";
import {
  parseRedactedWebVital,
  recordWebVital,
  rumSnapshot,
  type RedactedWebVital,
  type RumAggregate,
} from "../perf/rum-store";

const RECORD_LIMIT_BYTES = 512;
const RECORD_PATTERN =
  /^v1-(root|explorer|app|bridge|exchange|launchpad)-(lcp|inp|cls)-(\d{13})-([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\.json$/u;
const TEMPORARY_PATTERN =
  /^\.v1-(?:root|explorer|app|bridge|exchange|launchpad)-(?:lcp|inp|cls)-(\d{13})-[0-9a-f-]{36}\.json\.tmp$/u;
const ACTIVE_TEMPORARY_MS = 60_000;

type SinkMode = "durable-file" | "development-fallback" | "unconfigured";
type SinkDelivery = "unknown" | "healthy" | "degraded";
type SinkReason =
  | "awaiting-delivery"
  | "delivery-confirmed"
  | "delivery-failed"
  | "development-only"
  | "invalid-configuration"
  | "missing-configuration";

export interface PerformanceSinkHealth {
  readonly mode: SinkMode;
  readonly configured: boolean;
  readonly durable: boolean;
  readonly delivery: SinkDelivery;
  readonly reason: SinkReason;
}

export interface PerformanceSinkSnapshot {
  readonly health: PerformanceSinkHealth;
  readonly aggregates: readonly RumAggregate[];
}

export interface PerformanceSinkResult {
  readonly accepted: boolean;
  readonly durable: boolean;
  readonly health: PerformanceSinkHealth;
}

interface DurableConfiguration {
  readonly mode: "durable-file";
  readonly directory: string;
}

interface DevelopmentConfiguration {
  readonly mode: "development-fallback";
}

interface MissingConfiguration {
  readonly mode: "unconfigured";
  readonly reason: "invalid-configuration" | "missing-configuration";
}

type SinkConfiguration = DurableConfiguration | DevelopmentConfiguration | MissingConfiguration;

interface SinkRuntime {
  delivery: SinkDelivery;
}

type SinkGlobal = typeof globalThis & { __layerxPerformanceSink?: SinkRuntime };

const runtime = globalThis as SinkGlobal;
const sinkRuntime = runtime.__layerxPerformanceSink ?? { delivery: "unknown" as const };
runtime.__layerxPerformanceSink = sinkRuntime;

function storageDirectory(value: string): string | undefined {
  if (
    value.length === 0 ||
    value.includes("\0") ||
    !path.isAbsolute(value) ||
    path.resolve(value) !== value ||
    path.parse(value).root === value
  ) {
    return undefined;
  }
  return value;
}

function configuration(): SinkConfiguration {
  const configuredDirectory = process.env.LAYERX_RUM_STORAGE_DIRECTORY;
  if (configuredDirectory !== undefined) {
    const directory = storageDirectory(configuredDirectory);
    return directory === undefined
      ? { mode: "unconfigured", reason: "invalid-configuration" }
      : { mode: "durable-file", directory };
  }
  if (
    process.env.NODE_ENV === "development" &&
    process.env.LAYERX_RUM_DEVELOPMENT_FALLBACK === "1"
  ) {
    return { mode: "development-fallback" };
  }
  return { mode: "unconfigured", reason: "missing-configuration" };
}

function health(config: SinkConfiguration): PerformanceSinkHealth {
  if (config.mode === "durable-file") {
    const reason =
      sinkRuntime.delivery === "healthy"
        ? "delivery-confirmed"
        : sinkRuntime.delivery === "degraded"
          ? "delivery-failed"
          : "awaiting-delivery";
    return Object.freeze({
      mode: config.mode,
      configured: true,
      durable: true,
      delivery: sinkRuntime.delivery,
      reason,
    });
  }
  if (config.mode === "development-fallback") {
    return Object.freeze({
      mode: config.mode,
      configured: true,
      durable: false,
      delivery: "healthy",
      reason: "development-only",
    });
  }
  return Object.freeze({
    mode: config.mode,
    configured: false,
    durable: false,
    delivery: "degraded",
    reason: config.reason,
  });
}

async function ensureDirectory(directory: string): Promise<void> {
  await mkdir(directory, { recursive: true, mode: 0o700 });
  const [resolved, metadata] = await Promise.all([realpath(directory), lstat(directory)]);
  if (
    resolved !== directory ||
    !metadata.isDirectory() ||
    metadata.isSymbolicLink() ||
    (metadata.mode & 0o077) !== 0 ||
    (typeof process.getuid === "function" && metadata.uid !== process.getuid())
  ) {
    throw new Error("unsafe performance storage directory");
  }
}

async function syncDirectory(directory: string): Promise<void> {
  const handle = await open(directory, constants.O_RDONLY);
  try {
    await handle.sync();
  } finally {
    await handle.close();
  }
}

function seriesPrefix(observation: RedactedWebVital): string {
  return `v1-${observation.route}-${observation.metric.toLowerCase()}-`;
}

async function pruneSeries(directory: string, prefix: string): Promise<void> {
  const records = (await readdir(directory))
    .filter((name) => name.startsWith(prefix) && RECORD_PATTERN.test(name))
    .sort();
  const expired = records.slice(0, Math.max(0, records.length - RUM_SERIES_CAPACITY));
  if (expired.length === 0) {
    return;
  }
  for (const name of expired) {
    try {
      await unlink(path.join(directory, name));
    } catch (error) {
      if (
        typeof error !== "object" ||
        error === null ||
        !("code" in error) ||
        error.code !== "ENOENT"
      ) {
        throw error;
      }
    }
  }
  await syncDirectory(directory);
}

async function persistDurably(
  directory: string,
  observation: RedactedWebVital,
): Promise<void> {
  await ensureDirectory(directory);
  const timestamp = String(Date.now()).padStart(13, "0");
  const name = `${seriesPrefix(observation)}${timestamp}-${randomUUID()}.json`;
  const temporary = path.join(directory, `.${name}.tmp`);
  const destination = path.join(directory, name);
  const bytes = Buffer.from(`${JSON.stringify(observation)}\n`, "utf8");
  if (bytes.length > RECORD_LIMIT_BYTES) {
    throw new Error("performance observation exceeds its storage bound");
  }
  const handle = await open(
    temporary,
    constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW,
    0o600,
  );
  try {
    try {
      await handle.writeFile(bytes);
      await handle.sync();
    } finally {
      await handle.close();
    }
    await rename(temporary, destination);
  } catch (error) {
    await unlink(temporary).catch(() => undefined);
    throw error;
  }
  await syncDirectory(directory);
  await pruneSeries(directory, seriesPrefix(observation));
}

function fileIdentity(name: string):
  | Readonly<{ route: PerformanceRoute; metric: WebVitalName }>
  | undefined {
  const matched = RECORD_PATTERN.exec(name);
  if (matched === null) {
    return undefined;
  }
  const route = matched[1] as PerformanceRoute;
  const metric = matched[2]?.toUpperCase() as WebVitalName;
  return { route, metric };
}

async function readObservation(
  directory: string,
  name: string,
): Promise<RedactedWebVital | undefined> {
  const identity = fileIdentity(name);
  if (identity === undefined) {
    throw new Error("foreign durable performance store entry");
  }
  const location = path.join(directory, name);
  let metadata: Stats;
  try {
    metadata = await lstat(location);
  } catch (error) {
    if (
      typeof error === "object" &&
      error !== null &&
      "code" in error &&
      error.code === "ENOENT"
    ) {
      return undefined;
    }
    throw error;
  }
  if (
    !metadata.isFile() ||
    metadata.isSymbolicLink() ||
    metadata.nlink !== 1 ||
    (metadata.mode & 0o077) !== 0 ||
    (typeof process.getuid === "function" && metadata.uid !== process.getuid()) ||
    metadata.size > RECORD_LIMIT_BYTES
  ) {
    throw new Error("invalid durable performance record");
  }
  let source: string;
  try {
    source = await readFile(location, "utf8");
  } catch (error) {
    if (
      typeof error === "object" &&
      error !== null &&
      "code" in error &&
      error.code === "ENOENT"
    ) {
      return undefined;
    }
    throw error;
  }
  const observation = parseRedactedWebVital(JSON.parse(source) as unknown);
  if (
    observation === undefined ||
    observation.route !== identity.route ||
    observation.metric !== identity.metric
  ) {
    throw new Error("durable performance record failed validation");
  }
  return observation;
}

function aggregates(observations: readonly RedactedWebVital[]): readonly RumAggregate[] {
  const series = new Map<string, { route: PerformanceRoute; metric: WebVitalName; values: number[] }>();
  for (const observation of observations) {
    const key = `${observation.route}:${observation.metric}`;
    const current = series.get(key) ?? {
      route: observation.route,
      metric: observation.metric,
      values: [],
    };
    current.values.push(observation.observed);
    series.set(key, current);
  }
  return [...series.values()]
    .map(({ route, metric, values }): RumAggregate => {
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

async function durableSnapshot(directory: string): Promise<readonly RumAggregate[]> {
  await ensureDirectory(directory);
  const observations: RedactedWebVital[] = [];
  for (const name of (await readdir(directory)).sort()) {
    const temporary = TEMPORARY_PATTERN.exec(name);
    if (temporary !== null) {
      const createdAt = Number(temporary[1]);
      const age = Date.now() - createdAt;
      if (Number.isSafeInteger(createdAt) && age >= 0 && age <= ACTIVE_TEMPORARY_MS) {
        continue;
      }
      throw new Error("stale durable performance temporary record");
    }
    const observation = await readObservation(directory, name);
    if (observation !== undefined) {
      observations.push(observation);
    }
  }
  return aggregates(observations);
}

export async function persistPerformanceObservation(
  observation: RedactedWebVital,
): Promise<PerformanceSinkResult> {
  const config = configuration();
  if (config.mode === "durable-file") {
    try {
      await persistDurably(config.directory, observation);
      sinkRuntime.delivery = "healthy";
      return Object.freeze({ accepted: true, durable: true, health: health(config) });
    } catch {
      sinkRuntime.delivery = "degraded";
      return Object.freeze({ accepted: false, durable: false, health: health(config) });
    }
  }
  if (config.mode === "development-fallback") {
    recordWebVital(observation);
    return Object.freeze({ accepted: true, durable: false, health: health(config) });
  }
  return Object.freeze({ accepted: false, durable: false, health: health(config) });
}

export async function performanceSinkSnapshot(): Promise<PerformanceSinkSnapshot> {
  const config = configuration();
  if (config.mode === "durable-file") {
    try {
      return Object.freeze({ health: health(config), aggregates: await durableSnapshot(config.directory) });
    } catch {
      sinkRuntime.delivery = "degraded";
      return Object.freeze({ health: health(config), aggregates: [] });
    }
  }
  return Object.freeze({
    health: health(config),
    aggregates: config.mode === "development-fallback" ? rumSnapshot() : [],
  });
}
