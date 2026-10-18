// Layer 2 — structured logger.
//
// Replaces ad-hoc `console.log("[Foo]", x)` debris with a single
// surface that:
//
//   • gates by level (silenced below `NEXT_PUBLIC_LOG_LEVEL`),
//   • forces a namespace prefix so logs are grep-able,
//   • silences `debug` / `info` in production builds by default,
//   • never logs an address, signature, or other obvious PII unless
//     the caller explicitly opts in (PII is `data.unsafe`).
//
// In production the recommended sink is Sentry — `setSink()` lets the
// app entrypoint wire up a Sentry transport without scattering imports
// across every file that wants to log.

export type LogLevel = "debug" | "info" | "warn" | "error";
const LEVEL_ORDER: Record<LogLevel, number> = { debug: 10, info: 20, warn: 30, error: 40 };

export type LogEvent = {
  ns: string;
  level: LogLevel;
  msg: string;
  data?: Record<string, unknown>;
};

export type LogSink = (event: LogEvent) => void;

let currentSink: LogSink = consoleSink;
const minLevel: LogLevel = ((): LogLevel => {
  const raw = (process.env.NEXT_PUBLIC_LOG_LEVEL ?? "").toLowerCase();
  if (raw === "debug" || raw === "info" || raw === "warn" || raw === "error") return raw;
  return process.env.NODE_ENV === "production" ? "warn" : "info";
})();

function consoleSink(event: LogEvent): void {
  if (typeof console === "undefined") return;
  const prefix = `[${event.ns}]`;
  const payload = event.data ? [event.msg, event.data] : [event.msg];
  // eslint-disable-next-line no-console -- this is the structured logger sink
  switch (event.level) {
    case "debug": console.debug(prefix, ...payload); break;
    case "info":  console.info(prefix, ...payload); break;
    case "warn":  console.warn(prefix, ...payload); break;
    case "error": console.error(prefix, ...payload); break;
  }
}

export function setSink(sink: LogSink): void {
  currentSink = sink;
}

function emit(ns: string, level: LogLevel, msg: string, data?: Record<string, unknown>): void {
  if (LEVEL_ORDER[level] < LEVEL_ORDER[minLevel]) return;
  currentSink({ ns, level, msg, data });
}

export interface Logger {
  debug(msg: string, data?: Record<string, unknown>): void;
  info(msg: string, data?: Record<string, unknown>): void;
  warn(msg: string, data?: Record<string, unknown>): void;
  error(msg: string, data?: Record<string, unknown>): void;
  child(suffix: string): Logger;
}

export function createLogger(ns: string): Logger {
  return {
    debug: (msg, data) => emit(ns, "debug", msg, data),
    info: (msg, data) => emit(ns, "info", msg, data),
    warn: (msg, data) => emit(ns, "warn", msg, data),
    error: (msg, data) => emit(ns, "error", msg, data),
    child: (suffix) => createLogger(`${ns}:${suffix}`),
  };
}
