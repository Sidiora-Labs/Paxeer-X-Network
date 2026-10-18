import fs from "node:fs";
import path from "node:path";

/**
 * Read JSON config if it exists; otherwise return empty object.
 */
export function readJsonConfig(configPath: string): Record<string, unknown> {
  if (!fs.existsSync(configPath)) {
    return {};
  }
  const rawText = fs.readFileSync(configPath, "utf8").trim();
  if (!rawText) return {};
  try {
    const parsed = JSON.parse(rawText);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      throw new Error("Root value must be an object");
    }
    return parsed as Record<string, unknown>;
  } catch (error) {
    throw new Error(
      `Invalid JSON at ${configPath}: ${error instanceof Error ? error.message : String(error)}`,
    );
  }
}

/**
 * Write JSON atomically via temp-file + rename.
 */
export function writeJsonAtomic(
  configPath: string,
  data: Record<string, unknown>,
): void {
  const dir = path.dirname(configPath);
  fs.mkdirSync(dir, { recursive: true });
  const tempPath = `${configPath}.tmp`;
  fs.writeFileSync(tempPath, `${JSON.stringify(data, null, 2)}\n`, "utf8");
  fs.renameSync(tempPath, configPath);
}

/**
 * Create timestamped backup when target already exists.
 */
export function backupIfExists(configPath: string): string | undefined {
  if (!fs.existsSync(configPath)) return undefined;
  const stamp = new Date().toISOString().replace(/[:.]/g, "-");
  const backupPath = `${configPath}.${stamp}.bak`;
  fs.copyFileSync(configPath, backupPath);
  return backupPath;
}
