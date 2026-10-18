import type { InstallReport } from "./types.js";

/**
 * Render a concise install summary for terminal output.
 */
export function printInstallReport(
  report: InstallReport,
  dryRun: boolean,
): void {
  const prefix = dryRun ? "[dry-run]" : "[install]";

  for (const item of report.results) {
    const backup = item.backupPath ? ` (backup: ${item.backupPath})` : "";
    console.warn(
      `${prefix} ${item.client}: ${item.action} -> ${item.path}${backup}`,
    );
  }

  if (report.errors.length > 0) {
    for (const msg of report.errors) {
      console.error(`${prefix} error: ${msg}`);
    }
  }
}
