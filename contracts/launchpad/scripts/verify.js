/**
 * Verify the deployed Sidiora launchpad on Paxscan using its recorded build.
 * NETWORK_TYPE=paxeer-x-network npx hardhat run --no-compile scripts/verify.js --network paxeer-x-network
 * VERIFY_DRY_RUN=true performs the artifact/onchain checks without publishing.
 */
"use strict";
const fs = require("fs");
const path = require("path");
const { Interface, keccak256 } = require("ethers");
const { atomicWrite } = require("./lib/operations");
const PAXSCAN_API = "https://api.paxscan.io/api/v2";

const CONTRACT_SOURCES = {
  ProtocolConfig: "contracts/protocol/ProtocolConfig.sol",
  Treasury: "contracts/protocol/Treasury.sol",
  GovernanceModule: "contracts/protocol/GovernanceModule.sol",
  EventEmitter: "contracts/data/EventEmitter.sol",
  PoolRegistry: "contracts/data/PoolRegistry.sol",
  FeeAccumulator: "contracts/data/FeeAccumulator.sol",
  SidioraNFT: "contracts/core/SidioraNFT.sol",
  SidioraFactory: "contracts/core/SidioraFactory.sol",
  SidioraPool: "contracts/core/SidioraPool.sol",
  OpticalRegistry: "contracts/opticals/OpticalRegistry.sol",
  Router: "contracts/periphery/Router.sol",
  Quoter: "contracts/periphery/Quoter.sol",
  FeesRouter: "contracts/periphery/FeesRouter.sol",
  AntiSnipeOptical: "contracts/opticals/presets/AntiSnipeOptical.sol",
  MaxWalletOptical: "contracts/opticals/presets/MaxWalletOptical.sol",
  TaxOptical: "contracts/opticals/presets/TaxOptical.sol",
  CooldownOptical: "contracts/opticals/presets/CooldownOptical.sol",
  BuybackBurnOptical: "contracts/opticals/presets/BuybackBurnOptical.sol",
  PoolBeacon: "contracts/core/PoolBeacon.sol",
  Timelock: "contracts/protocol/Timelock.sol",
  UUPSProxy: "contracts/test/MockUUPS.sol",
};

function buildQueue(addresses) {
  const records = addresses._deployment?.records;
  if (!records || Object.keys(records).length === 0) throw new Error("Deployment creation records not found in manifest");
  return Object.entries(records).map(([key, record]) => {
    if (record.status !== "confirmed" || record.address?.toLowerCase() !== addresses[key]?.toLowerCase()) {
      throw new Error(`Unconfirmed or inconsistent deployment record: ${key}`);
    }
    if (!CONTRACT_SOURCES[record.name]) throw new Error(`No source mapped for ${record.name}`);
    return { label: key, address: record.address, contractName: record.name, record };
  });
}

async function prepareVerification(item, { artifacts, provider }) {
  const sourcePath = CONTRACT_SOURCES[item.contractName];
  const contractPath = `${sourcePath}:${item.contractName}`;
  const artifact = await artifacts.readArtifact(contractPath);
  // getBuildInfo follows this artifact's .dbg.json, not a stale build directory match.
  const bi = await artifacts.getBuildInfo(contractPath);
  const output = bi?.output?.contracts?.[sourcePath]?.[item.contractName];
  if (!output || !bi.solcLongVersion) throw new Error(`Artifact-linked build info not found: ${item.label}`);
  if (`0x${output.evm.bytecode.object}` !== artifact.bytecode ||
      `0x${output.evm.deployedBytecode.object}` !== artifact.deployedBytecode) {
    throw new Error(`Compiler output differs from artifact: ${item.label}`);
  }
  const constructorArgs = new Interface(artifact.abi).encodeDeploy(item.record.args);
  if (keccak256(artifact.bytecode + constructorArgs.slice(2)) !== item.record.creationHash) {
    throw new Error(`Creation artifact or constructor arguments differ from deployment: ${item.label}`);
  }
  const code = await provider.getCode(item.address);
  if (code === "0x" || keccak256(code) !== item.record.runtimeHash) {
    throw new Error(`Onchain runtime differs from deployment: ${item.label}`);
  }
  return { ...item, contractPath, compilerVersion: `v${bi.solcLongVersion.replace(/^v/, "")}`, input: bi.input, constructorArgs };
}

const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));

async function verifyContract(item, {
  fetchImpl = globalThis.fetch,
  sleepImpl = sleep,
  pollAttempts = 8,
  pollIntervalMs = 1500,
} = {}) {
  async function request(url, options = {}) {
    const response = await fetchImpl(url, { ...options, signal: globalThis.AbortSignal.timeout(30000) });
    const text = await response.text();
    let data;
    try { data = JSON.parse(text); } catch { throw new Error(`Explorer returned non-JSON (HTTP ${response.status})`); }
    return { response, data };
  }
  async function isVerified() {
    const { response, data } = await request(`${PAXSCAN_API}/addresses/${item.address}`);
    if (response.status === 404) return false;
    if (!response.ok) throw new Error(`Explorer status check failed (HTTP ${response.status}): ${data.message || "unknown error"}`);
    // An unverified smart-contract detail also returns HTTP 200; use the explicit address flag.
    return data.is_verified === true;
  }
  try {
    if (await isVerified()) return { status: "verified", alreadyVerified: true };
    const form = new globalThis.FormData();
    form.set("compiler_version", item.compilerVersion);
    form.set("contract_name", item.contractPath);
    form.set("autodetect_constructor_args", "false");
    form.set("constructor_args", item.constructorArgs.slice(2));
    form.set("files[0]", new globalThis.Blob([JSON.stringify(item.input)], { type: "application/json" }), "input.json");
    const { response, data } = await request(`${PAXSCAN_API}/smart-contracts/${item.address}/verification/via/standard-input`, {
      method: "POST", body: form,
    });
    const message = String(data.message || data.error || JSON.stringify(data));
    const alreadyVerified = /already.*verified/i.test(message);
    if (!alreadyVerified && (!response.ok || data.status === "0" || data.errors || data.error || /error|failed|invalid/i.test(message))) {
      throw new Error(`Verification submission failed (HTTP ${response.status}): ${message}`);
    }
    // Blockscout's successful POST only queues a job. Confirm completion separately.
    for (let attempt = 0; attempt < pollAttempts; attempt++) {
      if (await isVerified()) return { status: "verified" };
      if (attempt + 1 < pollAttempts) await sleepImpl(pollIntervalMs);
    }
    return { status: "pending", error: "Explorer has not confirmed verification; rerun to check or retry" };
  } catch (error) {
    return { status: "failed", error: error.message };
  }
}

function verificationExitCode(results) {
  return results.length > 0 && results.every(result => result.status === "verified") ? 0 : 1;
}

async function main() {
  const hre = require("hardhat");
  const network = require("./lib/operations").networkName();
  if (String((await hre.ethers.provider.getNetwork()).chainId) !== "125") throw new Error("Paxscan verification requires Paxeer mainnet (chain ID 125)");
  const addrPath = path.join(__dirname, "..", "deployments", `${network}-addresses.json`);
  const addresses = JSON.parse(fs.readFileSync(addrPath, "utf8"));
  if (String(addresses._meta?.network ?? addresses._meta?.chainId) !== "125") throw new Error("Manifest chain ID mismatch");
  const queue = buildQueue(addresses);
  console.log(`\nVerifying ${queue.length} Sidiora contracts on Paxscan (${network}, chain ID 125)`);
  console.log("Checking recorded creation artifacts and onchain runtime hashes before submission...");
  const prepared = [];
  // Finish every identity check before publishing any sources.
  for (const item of queue) prepared.push(await prepareVerification(item, { artifacts: hre.artifacts, provider: hre.ethers.provider }));
  console.log(`Deployment identity matched for all ${prepared.length} contracts.`);
  if (process.env.VERIFY_DRY_RUN === "true") {
    console.log("Dry run complete; no verification requests submitted.");
    return { dryRun: true, count: prepared.length, exitCode: 0 };
  }
  const reportPath = path.join(__dirname, "..", "deployments", `${network}-verification.json`);
  const report = { network, chainId: "125", startedAt: new Date().toISOString(), status: "running", results: [] };
  atomicWrite(reportPath, report);
  for (const [index, item] of prepared.entries()) {
    console.log(`[${index + 1}/${prepared.length}] ${item.label} at ${item.address}`);
    const result = await verifyContract(item);
    report.results.push({ label: item.label, address: item.address, compilerVersion: item.compilerVersion, ...result });
    atomicWrite(reportPath, report);
    console.log(`  ${result.status}${result.alreadyVerified ? " (already verified)" : ""}${result.error ? `: ${result.error}` : ""}`);
    if (index + 1 < prepared.length) await sleep(1500);
  }
  report.finishedAt = new Date().toISOString();
  report.status = verificationExitCode(report.results) === 0 ? "verified" : "incomplete";
  atomicWrite(reportPath, report);
  const count = status => report.results.filter(result => result.status === status).length;
  console.log(`\nVerified: ${count("verified")}; pending: ${count("pending")}; failed: ${count("failed")}. Report: ${reportPath}`);
  return { ...report, exitCode: verificationExitCode(report.results) };
}

module.exports = { CONTRACT_SOURCES, buildQueue, prepareVerification, verifyContract, verificationExitCode, main };
if (require.main === module) main().then(result => { process.exitCode = result.exitCode; }).catch(error => {
  console.error("Verification failed:", error.message);
  process.exitCode = 1;
});
