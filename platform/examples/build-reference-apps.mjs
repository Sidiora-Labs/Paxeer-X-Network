import { spawn } from "node:child_process";
import { createHash } from "node:crypto";
import { readFile, open, stat, readdir } from "node:fs/promises";
import { dirname, isAbsolute, resolve } from "node:path";
import { execFileSync } from "node:child_process";

const root = resolve(import.meta.dirname, "../..");
const output = process.env.PAXEER_X_REFERENCE_APP_ARTIFACTS;
if (typeof output !== "string" || !isAbsolute(output) || resolve(output) !== output || output.startsWith(`${root}/`)) throw new Error("private_reference_artifacts_path_required");
const parent = await stat(dirname(output));
if (!parent.isDirectory() || parent.uid !== process.getuid() || (parent.mode & 0o077) !== 0) throw new Error("private_reference_artifacts_directory_required");
const revision = execFileSync("git", ["rev-parse", "HEAD"], { cwd: root, encoding: "utf8" }).trim();
if (execFileSync("git", ["status", "--porcelain"], { cwd: root, encoding: "utf8" }).trim() !== "") throw new Error("reference_build_requires_clean_candidate");

async function command(executable, arguments_, cwd = root) {
  return new Promise((accept, reject) => {
    const child = spawn(executable, arguments_, { cwd, env: { ...process.env, CARGO_BUILD_JOBS: "4" }, stdio: ["ignore", "pipe", "inherit"] });
    const chunks = [];
    let size = 0;
    child.stdout.on("data", (chunk) => {
      size += chunk.length;
      if (size > 16 * 1024 * 1024) { child.kill("SIGTERM"); reject(new Error("reference_build_output_bound")); return; }
      chunks.push(chunk);
    });
    child.once("error", reject);
    child.once("close", (code, signal) => code === 0 ? accept(Buffer.concat(chunks).toString("utf8"))
      : reject(new Error(`reference_build_failed_${executable}_${signal ?? code}`)));
  });
}

await command("npm", ["run", "build", "--workspace", "@sidiora/layerx-sdk", "--workspace", "@sidiora/layerx-seller-middleware", "--workspace", "@sidiora/layerx-buyer-middleware", "--workspace", "@sidiora/layerx-merchant-middleware"]);
const compiled = await command(process.env.CARGO ?? "cargo", ["build", "--locked", "--manifest-path", "platform/Cargo.toml", "-p", "layerx-platform-cli", "--bin", "layerx", "--message-format=json"]);
const executables = compiled.split("\n").filter((line) => line.startsWith("{")).map((line) => JSON.parse(line))
  .filter((item) => item.reason === "compiler-artifact" && item.target?.name === "layerx" && typeof item.executable === "string");
if (executables.length !== 1) throw new Error("compiler_omitted_unique_reference_cli");
const cli = resolve(executables[0].executable);
const directory = resolve(root, "platform/examples/marketplace");
const built = JSON.parse((await command(cli, ["--json", "program", "build", "--manifest-path", resolve(directory, "program/Cargo.toml")], directory)).trim());
if (built.ok !== true || typeof built.data?.artifact !== "string" || !Number.isSafeInteger(built.data?.abi_version)) throw new Error("actual_cli_omitted_reference_guest");
const guest = resolve(directory, built.data.artifact);
const hash = async (path) => createHash("sha256").update(await readFile(path)).digest("hex");
const guestHash = await hash(guest);
if (guestHash !== built.data.code_hash) throw new Error("actual_guest_code_hash_mismatch");
if (execFileSync("git", ["rev-parse", "HEAD"], { cwd: root, encoding: "utf8" }).trim() !== revision
  || execFileSync("git", ["status", "--porcelain"], { cwd: root, encoding: "utf8" }).trim() !== "") throw new Error("reference_source_changed_during_build");
async function compiledFiles(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const files = [];
  for (const entry of entries.sort((a, b) => a.name.localeCompare(b.name))) {
    const path = resolve(directory, entry.name);
    if (entry.isDirectory()) files.push(...await compiledFiles(path));
    else if (entry.isFile()) files.push({ path, sha256: await hash(path) });
    else throw new Error("reference_node_output_not_regular");
  }
  return files;
}
const node = [];
for (const path of ["agent/sdk/typescript/dist", "platform/middleware/seller/dist", "platform/middleware/buyer/dist", "platform/middleware/merchant/dist"]) {
  const files = await compiledFiles(resolve(root, path));
  if (files.length === 0) throw new Error("missing_compiled_reference_node_workspace");
  node.push({ directory: path, files });
}
const manifest = { schema: "layerx.reference-app-artifacts.v1", source_revision: revision, node,
  cli: { path: cli, sha256: await hash(cli) },
  marketplace: { path: guest, sha256: guestHash, code_hash: guestHash, abi_version: built.data.abi_version } };
const file = await open(output, "wx", 0o600);
try { await file.writeFile(`${JSON.stringify(manifest, null, 2)}\n`); await file.sync(); } finally { await file.close(); }
process.stdout.write(`${JSON.stringify({ source_revision: revision, artifacts: ["cli", "marketplace"], build: "complete" })}\n`);
