"use strict";

const fs = require("fs");
const path = require("path");
const crypto = require("crypto");
const { compareLayouts, readRequiredBaseline } = require("./storage-layout");
const { TRACKED } = require("../meta-ag/check-storage-layout");

function sha256(data) { return crypto.createHash("sha256").update(data).digest("hex"); }
function filesBelow(directory) {
  if (!fs.existsSync(directory)) return [];
  return fs.readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const file = path.join(directory, entry.name);
    return entry.isDirectory() ? filesBelow(file) : entry.isFile() ? [file] : [];
  }).sort();
}

// Check the exact build behind each artifact, including all transitive imports.
function verifyRelease({ root = path.resolve(__dirname, "../.."), tracked = TRACKED, compiler = "0.8.27" } = {}) {
  const errors = [], artifacts = [], layouts = [], sourceHashes = {};
  const builds = new Map(), outputs = new Map();
  const directory = path.join(root, "artifacts/contracts");
  const debugFiles = filesBelow(directory).filter(file => file.endsWith(".dbg.json"));
  if (!debugFiles.length) errors.push("No compiled artifacts; compile before release qualification");
  for (const debugFile of debugFiles) {
    try {
      const pointer = JSON.parse(fs.readFileSync(debugFile, "utf8"));
      const buildPath = path.resolve(path.dirname(debugFile), pointer.buildInfo);
      if (!buildPath.startsWith(path.join(root, "artifacts") + path.sep)) throw new Error("Build pointer outside artifacts");
      if (!builds.has(buildPath)) builds.set(buildPath, JSON.parse(fs.readFileSync(buildPath, "utf8")));
      const build = builds.get(buildPath);
      if (build.solcVersion !== compiler) throw new Error(`Unexpected compiler ${build.solcVersion}`);
      const artifactFile = debugFile.replace(/\.dbg\.json$/, ".json");
      const artifact = JSON.parse(fs.readFileSync(artifactFile, "utf8"));
      const fq = `${artifact.sourceName}:${artifact.contractName}`;
      const output = build.output.contracts[artifact.sourceName][artifact.contractName];
      for (const key of ["bytecode", "deployedBytecode"]) {
        if (artifact[key] !== "0x" + output.evm[key].object) throw new Error(`${fq}: ${key} differs from compiler output`);
      }
      if (JSON.stringify(artifact.abi) !== JSON.stringify(output.abi)) throw new Error(`${fq}: ABI differs from compiler output`);
      const pending = [artifact.sourceName], visited = new Set();
      while (pending.length) {
        const name = pending.pop();
        if (visited.has(name)) continue;
        visited.add(name);
        const input = build.input.sources[name];
        const sourcePath = path.resolve(root, name);
        if (!sourcePath.startsWith(root + path.sep)) throw new Error(`Source outside project: ${name}`);
        const physical = fs.existsSync(sourcePath) ? sourcePath : path.resolve(root, "node_modules", name);
        const content = fs.readFileSync(physical, "utf8");
        if (!input || input.content !== content) throw new Error(`${fq}: stale compiler input ${name}`);
        const ast = build.output.sources[name].ast;
        if (!ast || !Array.isArray(ast.nodes)) throw new Error(`Missing import AST: ${name}`);
        for (const node of ast.nodes) if (node.nodeType === "ImportDirective") pending.push(node.absolutePath);
        sourceHashes[name] = sha256(content);
      }
      const runtimeBytes = (artifact.deployedBytecode.length - 2) / 2;
      const production = !/(?:^|\/)(?:test|mocks)\//.test(artifact.sourceName);
      if (production && runtimeBytes > 24576) errors.push(`${fq}: runtime ${runtimeBytes} exceeds EIP-170`);
      artifacts.push({ source: artifact.sourceName, contract: artifact.contractName, runtime_bytes: runtimeBytes,
        production, sha256: sha256(fs.readFileSync(artifactFile)), compiler: build.solcLongVersion || build.solcVersion });
      outputs.set(fq, output);
    } catch (error) { errors.push(`${path.relative(root, debugFile)}: ${error.message}`); }
  }
  for (const file of filesBelow(path.join(root, "contracts")).filter(file => file.endsWith(".sol"))) {
    const name = path.relative(root, file).split(path.sep).join("/");
    if (!sourceHashes[name]) errors.push(`Source not covered by current artifacts: ${name}`);
  }
  for (const fq of tracked) {
    const [source, name] = fq.split(":");
    try {
      const relativeBaseline = `${source.includes("/meta-ag/") ? "meta-ag" : "core"}/${name}.json`;
      const baselineDirectory = path.join(root, "storage-layout", "production");
      const baselineFile = path.join(baselineDirectory, relativeBaseline);
      const manifest = JSON.parse(fs.readFileSync(path.join(baselineDirectory, "manifest.json"), "utf8"));
      const baseline = readRequiredBaseline(baselineFile);
      if (sha256(fs.readFileSync(baselineFile)) !== manifest.files?.[relativeBaseline]) throw new Error("Frozen layout digest mismatch");
      const output = outputs.get(fq);
      if (!output || !output.storageLayout) throw new Error("Missing current layout");
      const drift = compareLayouts(baseline, output.storageLayout);
      errors.push(...drift.map(error => `${fq}: ${error}`));
      layouts.push({ contract: fq, baseline_sha256: sha256(fs.readFileSync(baselineFile)), passed: !drift.length });
    } catch (error) { errors.push(`${fq}: ${error.message}`); }
  }
  return { schema_version: 1, status: errors.length ? "failed" : "passed", errors, source_sha256: sourceHashes,
    artifacts, storage_layouts: layouts, limits: { deployed_runtime_bytes: 24576 },
    scope: "Local compiler input, artifact identity, standard runtime size and frozen storage baselines; no live-state migration certification." };
}

module.exports = { verifyRelease, filesBelow, sha256 };
