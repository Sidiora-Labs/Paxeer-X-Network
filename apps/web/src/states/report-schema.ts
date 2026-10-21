import { errorCodeVariants } from "../api/generated/index.ts";

export type ReportShell = "mobile" | "desktop";

export interface SupportReportRequest {
  readonly traceId: string;
  readonly machineCode: string;
  readonly route: string;
  readonly shell: ReportShell;
}

export interface StoredSupportReport extends SupportReportRequest {
  readonly reportId: string;
  readonly receivedAt: string;
}

export interface SavedReportReceipt {
  readonly traceId: string;
  readonly status: "saved";
}

function exactObject(
  value: unknown,
  fields: readonly string[],
  label: string,
): Readonly<Record<string, unknown>> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error(`${label} must be an object`);
  }
  const object = value as Readonly<Record<string, unknown>>;
  const actual = Object.keys(object).sort();
  const expected = [...fields].sort();
  if (actual.length !== expected.length || actual.some((field, index) => field !== expected[index])) {
    throw new Error(`${label} fields are invalid`);
  }
  return object;
}

function stringField(object: Readonly<Record<string, unknown>>, field: string): string {
  const value = object[field];
  if (typeof value !== "string") {
    throw new Error(`${field} must be a string`);
  }
  return value;
}

export function supportReportTrace(value: string): string {
  const normalized = value.trim();
  if (normalized.length === 0 || normalized.length > 120 || !/^[A-Za-z0-9][A-Za-z0-9._:-]*$/u.test(normalized)) {
    throw new Error("Invalid trace identifier");
  }
  return normalized;
}

function supportReportMachineCode(value: string): string {
  const normalized = value.trim();
  if (normalized.length === 0 || normalized.length > 120
    || (!errorCodeVariants.some((code) => code === normalized) && !/^[A-Z][A-Z0-9._:-]*$/u.test(normalized))) {
    throw new Error("Invalid machine code");
  }
  return normalized;
}

function supportReportRoute(value: string): string {
  const normalized = value.trim();
  const segments = normalized.split("/");
  if (
    !/^\/[A-Za-z0-9/_-]*$/u.test(normalized)
    || normalized.length > 160
    || segments.includes("..")
    || segments.includes(".")
  ) {
    throw new Error("Invalid report route");
  }
  return normalized;
}

function supportReportShell(value: string): ReportShell {
  if (value !== "mobile" && value !== "desktop") {
    throw new Error("Invalid report shell");
  }
  return value;
}

export function parseSupportReportRequest(value: unknown): SupportReportRequest {
  const object = exactObject(value, ["traceId", "machineCode", "route", "shell"], "Support report");
  return Object.freeze({
    traceId: supportReportTrace(stringField(object, "traceId")),
    machineCode: supportReportMachineCode(stringField(object, "machineCode")),
    route: supportReportRoute(stringField(object, "route")),
    shell: supportReportShell(stringField(object, "shell")),
  });
}

export function parseStoredSupportReport(value: unknown): StoredSupportReport {
  const object = exactObject(
    value,
    ["traceId", "machineCode", "route", "shell", "reportId", "receivedAt"],
    "Stored support report",
  );
  const report = parseSupportReportRequest({
    traceId: object.traceId,
    machineCode: object.machineCode,
    route: object.route,
    shell: object.shell,
  });
  const reportId = stringField(object, "reportId");
  const receivedAt = stringField(object, "receivedAt");
  if (!/^[a-f0-9]{64}$/u.test(reportId) || !Number.isFinite(Date.parse(receivedAt))) {
    throw new Error("Stored support report metadata is invalid");
  }
  return Object.freeze({ ...report, reportId, receivedAt });
}

export function parseSavedReportReceipt(value: unknown): SavedReportReceipt {
  const object = exactObject(value, ["traceId", "status"], "Support report response");
  if (object.status !== "saved") {
    throw new Error("Support report was not saved");
  }
  return Object.freeze({ traceId: supportReportTrace(stringField(object, "traceId")), status: "saved" });
}
