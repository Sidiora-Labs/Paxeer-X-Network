"use strict";
const fs = require("fs");
const path = require("path");
function networkName() {
  const actual = require("hardhat").network.name;
  if (process.env.NETWORK_TYPE && process.env.NETWORK_TYPE !== actual) throw new Error("NETWORK_TYPE disagrees with connected Hardhat network");
  return actual;
}
function manifestPath() { return path.resolve(__dirname, "../../deployments", `${networkName()}-addresses.json`); }
function atomicWrite(file, value) {
  fs.mkdirSync(path.dirname(file), { recursive: true });
  const tmp = `${file}.${process.pid}.tmp`;
  const fd = fs.openSync(tmp, "w", 0o600);
  try { fs.writeFileSync(fd, JSON.stringify(value, null, 2) + "\n"); fs.fsyncSync(fd); } finally { fs.closeSync(fd); }
  fs.renameSync(tmp, file);
}
async function validateManifest(addresses, provider) {
  const network = await provider.getNetwork();
  if (String(addresses._meta?.network ?? addresses._meta?.chainId) !== String(network.chainId)) throw new Error("Manifest chain ID mismatch or absent");
  for (const [name, address] of Object.entries(addresses)) {
    if (name.startsWith("_") || typeof address !== "string" || !address.startsWith("0x")) continue;
    if (!require("ethers").isAddress(address)) throw new Error(`Invalid manifest address ${name}`);
    if ((await provider.getCode(address)) === "0x") throw new Error(`No contract code at ${name}`);
  }
}
module.exports = { networkName, manifestPath, atomicWrite, validateManifest };
async function checkpointDeployment(address, label) {
  const hre = require("hardhat");
  const file = path.resolve(__dirname, "../../deployments", `${networkName()}-operations.json`);
  const journal = fs.existsSync(file) ? JSON.parse(fs.readFileSync(file, "utf8")) : { chainId: String((await hre.ethers.provider.getNetwork()).chainId), events: [] };
  if (journal.chainId !== String((await hre.ethers.provider.getNetwork()).chainId)) throw new Error("Checkpoint chain mismatch");
  journal.events.push({ type: "deployment-confirmed", label, address, codeHash: hre.ethers.keccak256(await hre.ethers.provider.getCode(address)), at: new Date().toISOString() });
  atomicWrite(file, journal);
}
module.exports.checkpointDeployment = checkpointDeployment;
async function checkpointTransaction(tx, label, status, receipt) {
  const hre = require("hardhat");
  const file = path.resolve(__dirname, "../../deployments", `${networkName()}-operations.json`);
  const chainId = String((await hre.ethers.provider.getNetwork()).chainId);
  const journal = fs.existsSync(file) ? JSON.parse(fs.readFileSync(file, "utf8")) : { chainId, events: [] };
  if (journal.chainId !== chainId) throw new Error("Transaction checkpoint chain mismatch");
  journal.events.push({ type: "transaction", label, status, hash: tx.hash, nonce: tx.nonce, to: tx.to, data: tx.data, value: String(tx.value), blockNumber: receipt?.blockNumber, at: new Date().toISOString() });
  atomicWrite(file, journal);
}
module.exports.checkpointTransaction = checkpointTransaction;
