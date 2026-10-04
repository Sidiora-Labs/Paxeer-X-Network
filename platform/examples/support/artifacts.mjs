import { createHash } from "node:crypto";
import { readFile, stat, lstat, realpath } from "node:fs/promises";
import { resolve, isAbsolute } from "node:path";
import { execFileSync } from "node:child_process";

export async function referenceArtifacts(path, root) {
  if (typeof path !== "string" || !isAbsolute(path) || resolve(path) !== path) throw new Error("missing_private_reference_artifacts");
  if (await realpath(path) !== path) throw new Error("reference_artifact_manifest_not_canonical");
  const info = await lstat(path);
  if (!info.isFile() || info.uid !== process.getuid() || (info.mode & 0o077) !== 0) throw new Error("unsafe_reference_artifacts");
  const value = JSON.parse(await readFile(path, "utf8"));
  const revision = execFileSync("git", ["rev-parse", "HEAD"], { cwd: root, encoding: "utf8" }).trim();
  if (value.schema !== "layerx.reference-app-artifacts.v1" || value.source_revision !== revision) throw new Error("reference_artifacts_source_mismatch");
  const directories = ["agent/sdk/typescript/dist", "platform/middleware/seller/dist", "platform/middleware/buyer/dist", "platform/middleware/merchant/dist"];
  if (!Array.isArray(value.node) || value.node.length !== directories.length) throw new Error("missing_reference_node_outputs");
  for (let index = 0; index < directories.length; index += 1) {
    const workspace = value.node[index];
    const directory = resolve(root, directories[index]);
    if (workspace.directory !== directories[index] || !Array.isArray(workspace.files) || workspace.files.length === 0) throw new Error("invalid_reference_node_workspace");
    const paths = new Set();
    for (const row of workspace.files) {
      if (typeof row.path !== "string" || !row.path.startsWith(`${directory}/`) || resolve(row.path) !== row.path
        || paths.has(row.path) || !/^[0-9a-f]{64}$/u.test(row.sha256)) throw new Error("invalid_reference_node_artifact");
      paths.add(row.path);
      if (await realpath(row.path) !== row.path || !(await lstat(row.path)).isFile()
        || createHash("sha256").update(await readFile(row.path)).digest("hex") !== row.sha256) throw new Error("reference_node_artifact_mismatch");
    }
  }
  for (const name of ["cli", "marketplace"]) {
    const row = value[name];
    if (row === null || typeof row !== "object" || typeof row.path !== "string" || !isAbsolute(row.path)
      || resolve(row.path) !== row.path || !/^[0-9a-f]{64}$/u.test(row.sha256)) throw new Error("invalid_reference_artifact");
    if (await realpath(row.path) !== row.path) throw new Error("reference_artifact_path_not_canonical");
    const bytes = await readFile(row.path);
    if (createHash("sha256").update(bytes).digest("hex") !== row.sha256) throw new Error("reference_artifact_digest_mismatch");
    if (name === "cli") {
      if (!bytes.subarray(0, 4).equals(Buffer.from([0x7f, 0x45, 0x4c, 0x46])) || ((await stat(row.path)).mode & 0o111) === 0) throw new Error("reference_cli_not_native_executable");
    } else if (!bytes.subarray(0, 4).equals(Buffer.from([0, 0x61, 0x73, 0x6d]))
      || row.code_hash !== row.sha256 || !Number.isSafeInteger(row.abi_version)) throw new Error("invalid_reference_guest_artifact");
  }
  return value;
}
