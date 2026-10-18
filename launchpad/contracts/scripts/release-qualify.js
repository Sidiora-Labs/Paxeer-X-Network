#!/usr/bin/env node
"use strict";
const fs = require("fs");
const path = require("path");
const { spawnSync } = require("child_process");
const { filesBelow, sha256 } = require("./lib/release-check");
const root = path.resolve(__dirname, "..");

function snapshot() {
  const files = ["contracts", "test", "scripts", "storage-layout"].flatMap(name => filesBelow(path.join(root, name)));
  files.push(...["package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", "hardhat.config.js", ".eslintrc.js"].map(name => path.join(root, name)));
  return Object.fromEntries(files.sort().map(file => [path.relative(root, file), sha256(fs.readFileSync(file))]));
}
function artifactSnapshot() {
  return Object.fromEntries(filesBelow(path.join(root, "artifacts")).filter(file => file.endsWith(".json"))
    .map(file => [path.relative(root, file), sha256(fs.readFileSync(file))]));
}
function main(args = process.argv.slice(2)) {
  if (args.length && (args.length !== 2 || args[0] !== "--output")) throw new Error("Usage: node scripts/release-qualify.js [--output DIRECTORY]");
  const output = path.resolve(args[1] || path.join(root, ".release"));
  // Evidence must not become an input to the same qualification run.
  for (const name of ["contracts", "test", "scripts", "storage-layout"]) {
    const dir = path.join(root, name);
    if (output === dir || output.startsWith(dir + path.sep)) throw new Error("Output must be outside source input directories");
  }
  fs.mkdirSync(output, { recursive: true });
  const runDirectory = fs.mkdtempSync(path.join(output, "run-"));
  const before = snapshot(), started = new Date().toISOString(), steps = [];
  const resultPath = path.join(output, "qualification.json");
  // In-progress evidence replaces the prior result before any fallible command.
  fs.writeFileSync(resultPath, JSON.stringify({ schema_version: 1, status: "running", started_utc: started,
    run_directory: path.basename(runDirectory) }, null, 2) + "\n");
  let compiledArtifacts, buildReport;
  const node = process.execPath, cli = "node_modules/hardhat/internal/cli/cli.js";
  const commands = [
    ["compile", node, [cli, "compile", "--force"]],
    ["lint", node, ["node_modules/eslint/bin/eslint.js", "scripts/**/*.js", "test/**/*.js", "--ext", ".js"]],
    ["build", node, ["scripts/release-check.js", "--report", path.join(runDirectory, "build.json")]],
    ["tests", node, [cli, "test", "--no-compile", "--network", "hardhat"]],
  ];
  for (const [name, command, args_] of commands) {
    const logPath = path.join(runDirectory, `${name}.log`), fd = fs.openSync(logPath, "w");
    const startedAt = Date.now();
    let result;
    try { result = spawnSync(command, args_, { cwd: root, stdio: ["ignore", fd, fd], env: { ...process.env, UPDATE_GAS_SNAPSHOTS: "false", RUN_DOCGEN: "false", PECOR_COVERAGE_OUTPUT: path.join(runDirectory, "pecor-observations.json") } }); }
    finally { fs.closeSync(fd); }
    steps.push({ name, command: [command, ...args_], exit_code: result.status,
      error: result.error ? result.error.message : null, signal: result.signal,
      seconds: (Date.now() - startedAt) / 1000, log: path.relative(output, logPath), log_sha256: sha256(fs.readFileSync(logPath)) });
    console.log(`${name}: ${result.status === 0 ? "passed" : "failed"} (${steps.at(-1).seconds}s)`);
    if (result.status !== 0) break;
    if (name === "build") {
      compiledArtifacts = artifactSnapshot();
      buildReport = { path: path.relative(output, path.join(runDirectory, "build.json")),
        sha256: sha256(fs.readFileSync(path.join(runDirectory, "build.json"))) };
    }
  }
  const after = snapshot(), unchanged = JSON.stringify(before) === JSON.stringify(after);
  const artifactsUnchanged = !!compiledArtifacts && JSON.stringify(compiledArtifacts) === JSON.stringify(artifactSnapshot());
  const reportUnchanged = !!buildReport && sha256(fs.readFileSync(path.join(output, buildReport.path))) === buildReport.sha256;
  const result = { schema_version: 1, started_utc: started, finished_utc: new Date().toISOString(),
    status: unchanged && artifactsUnchanged && reportUnchanged && steps.length === commands.length && steps.every(step => step.exit_code === 0) ? "passed" : "failed",
    source_unchanged: unchanged, source_sha256: before, artifact_unchanged: artifactsUnchanged,
    artifact_sha256: compiledArtifacts || {}, build_report_unchanged: reportUnchanged, build_report: buildReport || null, steps,
    dependency_install: "Run pnpm install --frozen-lockfile before this command; CI enforces this prerequisite.",
    scope: "Local release qualification only; deployment, live network and external migration inputs require their own evidence." };
  const observation = path.join(runDirectory, "pecor-observations.json");
  result.observations = fs.existsSync(observation)
    ? [{ path: path.relative(output, observation), sha256: sha256(fs.readFileSync(observation)) }] : [];
  fs.writeFileSync(path.join(runDirectory, "qualification.json"), JSON.stringify(result, null, 2) + "\n");
  fs.writeFileSync(resultPath, JSON.stringify(result, null, 2) + "\n");
  if (result.status !== "passed") process.exitCode = 1;
  return result;
}
if (require.main === module) { try { main(); } catch (error) { console.error(error.message); process.exitCode = 1; } }
module.exports = { snapshot, artifactSnapshot, main };
