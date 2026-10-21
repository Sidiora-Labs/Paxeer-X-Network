"use strict";
const fs = require("fs");
const path = require("path");
const { atomicWrite, checkpointDeployment } = require("./operations");
const serializable = value => JSON.parse(JSON.stringify(value, (_, v) => typeof v === "bigint" ? v.toString() : v));
async function deploymentSession({ file, signer, config, resume, afterCheckpoint, namespace = "_deployment", inherited = {} }) {
  const hre = require("hardhat");
  const { ethers } = hre;
  const provider = signer.provider;
  const chainId = String((await provider.getNetwork()).chainId);
  const hash = data => ethers.keccak256(ethers.toUtf8Bytes(JSON.stringify(serializable(data))));
  const configHash = hash(config);
  const exists = fs.existsSync(file);
  const addresses = exists ? JSON.parse(fs.readFileSync(file, "utf8")) : {
    _meta: { network: chainId, deployer: signer.address, status: "bootstrap", usdl: config.usdl, sid: config.sid }
  };
  if (exists && addresses[namespace] && !resume) throw new Error("Deployment manifest exists; refusing to overwrite. Set RESUME_DEPLOYMENT=true only for the matching checkpoint.");
  if (namespace === "_deployment" && exists && !addresses[namespace]) throw new Error("Existing core manifest lacks bound resume provenance; operator qualification required");
  if (namespace !== "_deployment" && !exists) throw new Error("Layered deployment requires an existing core manifest");
  if (String(addresses._meta?.network) !== chainId) throw new Error("Resume checkpoint chain identity mismatch");
  if (!addresses[namespace]) addresses[namespace] = { version: 1, configHash, config: serializable(config), deployer: signer.address, inherited: serializable(inherited), records: {}, steps: {} };
  const state = addresses[namespace];
  if (state.version !== 1 || state.deployer?.toLowerCase() !== signer.address.toLowerCase() || state.configHash !== configHash || hash(state.inherited || {}) !== hash(inherited)) throw new Error("Resume checkpoint chain, deployer or configuration identity mismatch");
  for (const [key, address] of Object.entries(inherited)) {
    if (addresses[key] && addresses[key].toLowerCase() !== address.toLowerCase()) throw new Error(`Inherited deployment identity mismatch: ${key}`);
    if (await provider.getCode(address) === "0x") throw new Error(`Inherited deployment has no code: ${key}`);
    addresses[key] = address;
  }
  function checkpoint(stage) {
    atomicWrite(file, addresses);
    if (afterCheckpoint) {
      const result = afterCheckpoint(stage, addresses);
      if (result && typeof result.then === "function") throw new Error("afterCheckpoint must be synchronous");
    }
  }
  const compilerCache = new Map();
  async function runtimeMatches(name, address) {
    const code = await provider.getCode(address);
    if (code === "0x") throw new Error(`Resume missing deployed code: ${name}`);
    const artifact = await hre.artifacts.readArtifact(name);
    const debugPath = path.join(hre.config.paths.artifacts, artifact.sourceName, `${artifact.contractName}.dbg.json`);
    const debug = JSON.parse(fs.readFileSync(debugPath, "utf8"));
    const buildPath = path.resolve(path.dirname(debugPath), debug.buildInfo);
    if (!compilerCache.has(buildPath)) compilerCache.set(buildPath, JSON.parse(fs.readFileSync(buildPath, "utf8")));
    const info = compilerCache.get(buildPath);
    if (!info) throw new Error(`Missing compiler identity for ${name}`);
    const evm = info.output.contracts[artifact.sourceName][artifact.contractName].evm.deployedBytecode;
    function mask(value) {
      let chars = value.replace(/^0x/, "").split("");
      for (const refs of Object.values(evm.immutableReferences || {})) for (const ref of refs) chars.splice(ref.start * 2, ref.length * 2, ..."0".repeat(ref.length * 2));
      return chars.join("");
    }
    if (mask(code) !== mask(artifact.deployedBytecode)) throw new Error(`Resume runtime artifact identity mismatch: ${name}`);
    return ethers.keccak256(code);
  }
  async function findCreation(record, creator = signer.address) {
    if (record.txHash) return provider.getTransaction(record.txHash);
    const latest = await provider.getBlockNumber();
    if (latest - record.preparedBlock > 1024) throw new Error("Ambiguous creation exceeds reconciliation block window; operator receipt reconciliation required");
    for (let block = record.preparedBlock; block <= latest; block++) {
      const full = await provider.getBlock(block, true);
      for (const tx of full.prefetchedTransactions) if (tx.from.toLowerCase() === creator.toLowerCase() && tx.nonce === record.nonce) return tx;
    }
    return null;
  }
  async function verifyRecord(key, record, expectedData, creator = signer.address) {
    const Factory = await ethers.getContractFactory(record.name, signer);
    const transaction = await Factory.getDeployTransaction(...record.args);
    if (ethers.keccak256(transaction.data) !== record.creationHash || (expectedData && ethers.keccak256(expectedData) !== record.creationHash)) throw new Error(`Resume creation/artifact identity mismatch: ${key}`);
    const tx = await findCreation(record, creator);
    if (!tx) throw new Error(`Ambiguous deployment ${key}: no confirmed creation; do not rebroadcast until receipt reconciliation`);
    if (tx.to !== null || tx.from.toLowerCase() !== creator.toLowerCase() || tx.nonce !== record.nonce || String(tx.value) !== (record.value || "0") || ethers.keccak256(tx.data) !== record.creationHash) throw new Error(`Resume creation transaction identity mismatch: ${key}`);
    const receipt = await provider.getTransactionReceipt(tx.hash);
    if (!receipt || Number(receipt.status) !== 1 || receipt.contractAddress?.toLowerCase() !== record.address.toLowerCase()) throw new Error(`Ambiguous or failed deployment ${key}: receipt reconciliation required`);
    const codeHash = await runtimeMatches(record.name, record.address);
    if (record.runtimeHash && record.runtimeHash !== codeHash) throw new Error(`Resume runtime code hash mismatch: ${key}`);
    if (["UUPSProxy", "ERC1967Proxy"].includes(record.name)) {
      const slot = "0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc";
      const implementation = ethers.getAddress("0x" + (await provider.getStorage(record.address, slot)).slice(-40));
      if (implementation.toLowerCase() !== record.args[0].toLowerCase()) throw new Error(`Resume proxy implementation drift: ${key}`);
    }
    if (record.name === "PoolBeacon") {
      const beacon = await ethers.getContractAt("PoolBeacon", record.address);
      if ((await beacon.implementation()).toLowerCase() !== record.args[0].toLowerCase()) throw new Error(`Resume beacon implementation drift: ${key}`);
    }
    record.txHash = tx.hash; record.runtimeHash = codeHash; record.status = "confirmed"; record.blockNumber = receipt.blockNumber;
    addresses[key] = record.address;
  }
  // Validate every recorded creation and transaction before any new broadcast.
  if (namespace !== "_deployment" && addresses._deployment?.records) {
    const coreCreator = addresses._deployment.deployer || addresses._meta.deployer;
    for (const [key, record] of Object.entries(addresses._deployment.records)) await verifyRecord(key, record, undefined, coreCreator);
  }
  for (const [key, record] of Object.entries(state.records)) await verifyRecord(key, record);
  for (const [key, value] of Object.entries(addresses)) if (!key.startsWith("_") && typeof value === "string" && value.startsWith("0x") && !state.records[key] && !inherited[key]) throw new Error(`Untracked deployment address ${key}; explicit operator migration required`);
  checkpoint("session");
  async function deploy(name, args, key, stage) {
    const Factory = await ethers.getContractFactory(name, signer);
    const creation = await Factory.getDeployTransaction(...args);
    let record = state.records[key];
    if (record) {
      await verifyRecord(key, record, creation.data);
      console.log(`  Reusing ${key}: ${record.address}`);
      return Factory.attach(record.address);
    }
    // Persist an intent before sending: even loss of the RPC response cannot silently trigger duplicate creation.
    const request = await signer.populateTransaction(creation);
    request.nonce = request.nonce ?? await signer.getNonce("pending");
    request.chainId = BigInt(chainId);
    request.gasLimit = request.gasLimit ?? await provider.estimateGas({ ...request, from: signer.address });
    record = { name, args: serializable(args), creationHash: ethers.keccak256(creation.data), address: ethers.getCreateAddress({ from: signer.address, nonce: request.nonce }), nonce: request.nonce, value: String(request.value || 0), preparedBlock: await provider.getBlockNumber(), status: "prepared" };
    state.records[key] = record;
    checkpoint(`prepared:${key}`);
    let tx;
    try { tx = await signer.sendTransaction(request); }
    catch (e) { throw new Error(`Ambiguous creation broadcast ${key}; resume reconciles recorded nonce before any further creation`, { cause: e }); }
    record.txHash = tx.hash; record.status = "broadcast";
    checkpoint(`broadcast:${key}`);
    const receipt = await tx.wait();
    if (!receipt || Number(receipt.status) !== 1) throw new Error(`Creation failed ${key}`);
    await verifyRecord(key, record, creation.data);
    await checkpointDeployment(record.address, key);
    checkpoint(stage || `deployment:${key}`);
    return Factory.attach(record.address);
  }
  async function ensure(label, condition, send) {
    const previous = state.steps[label];
    if (await condition()) {
      if (previous?.status !== "confirmed") { state.steps[label] = { ...previous, status: "confirmed", reconciledByPostcondition: true }; checkpoint(label); }
      return;
    }
    if (previous && previous.status !== "confirmed") {
      const receipt = previous.txHash && await provider.getTransactionReceipt(previous.txHash);
      if (!receipt || Number(receipt.status) === 1) throw new Error(`Ambiguous wiring ${label}; unmet postcondition requires operator reconciliation`);
    }
    // A previously confirmed postcondition drifting later is not permission to rewrite finalized state.
    if (previous?.status === "confirmed") throw new Error(`Deployment postcondition drift: ${label}`);
    state.steps[label] = { status: "prepared" };
    checkpoint(`prepared:${label}`);
    let tx;
    try { tx = await send(); }
    catch (e) { throw new Error(`Ambiguous wiring broadcast ${label}; reconcile before retrying`, { cause: e }); }
    state.steps[label] = { status: "broadcast", txHash: tx.hash, nonce: tx.nonce, to: tx.to, data: tx.data };
    checkpoint(`broadcast:${label}`);
    const receipt = await tx.wait();
    if (!receipt || Number(receipt.status) !== 1 || !await condition()) throw new Error(`Deployment postcondition failed: ${label}`);
    state.steps[label].status = "confirmed";
    checkpoint(label);
  }
  return { addresses, deploy, ensure, checkpoint };
}
module.exports = { deploymentSession };
