"use strict";
const { Interface, getAddress, isAddress, keccak256, ZeroHash } = require("ethers");
const IMPLEMENTATION_SLOT = "0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc";

/** Read-only snapshot. policy: maxTokens (1..10000), requiredAssets, expectedRoles
 * [{contract: manifest key, role: bytes32, account: address, present: boolean}],
 * requiredContracts (manifest keys), pausedIsCritical. Every required failure is critical.
 * No signer, secrets, or transaction submission is used. Raw amounts are decimal strings.
 */
async function inspectHealth({ provider, manifest, blockTag = "latest", policy = {} }) {
  const object = value => value !== null && typeof value === "object" && !Array.isArray(value);
  const bytes32 = value => typeof value === "string" && /^0x[0-9a-fA-F]{64}$/.test(value);
  if (!object(manifest) || !object(manifest._meta) || !object(policy)) throw new Error("Invalid manifest or policy object");
  const allowed = new Set(["maxTokens", "requiredContracts", "requiredAssets", "expectedRoles", "pausedIsCritical"]);
  if (Object.keys(policy).some(key => !allowed.has(key))) throw new Error("Unknown policy option");
  for (const field of ["requiredContracts", "requiredAssets", "expectedRoles"]) if (policy[field] !== undefined && (!Array.isArray(policy[field]) || policy[field].length > 10000)) throw new Error("Invalid or excessive policy inventory");
  if (policy.requiredContracts?.some(key => typeof key !== "string" || key.startsWith("_") || !/^[A-Za-z][A-Za-z0-9_]*$/.test(key))) throw new Error("Invalid required contract key");
  if (policy.requiredAssets?.some(token => !isAddress(token))) throw new Error("Invalid required asset address");
  if (policy.pausedIsCritical !== undefined && typeof policy.pausedIsCritical !== "boolean") throw new Error("Invalid pause policy");
  if (policy.expectedRoles?.some(e => !object(e) || typeof e.contract !== "string" || !isAddress(manifest[e.contract] || "") || (e.role !== undefined && !bytes32(e.role)) || !isAddress(e.account || "") || (e.present !== undefined && typeof e.present !== "boolean"))) throw new Error("Invalid expected role assertion");
  if (Object.keys(manifest).length > 2000) throw new Error("Manifest exceeds contract bound");
  for (const namespace of ["_deployment", "_metaAgDeployment"]) if (manifest[namespace] !== undefined && (!object(manifest[namespace]) || !object(manifest[namespace].records))) throw new Error("Invalid deployment records");
  const checks = [], assets = [];
  const add = (id, status, detail) => checks.push({ id, status, detail });
  const block = await provider.getBlock(blockTag);
  if (!block || !bytes32(block.hash) || !Number.isSafeInteger(block.number) || block.number < 0 || !Number.isSafeInteger(block.timestamp) || block.timestamp < 0) throw new Error("Snapshot block unavailable or invalid");
  const at = block.number;
  const read = async (address, signature, args = []) => {
    const iface = new Interface([signature]);
    const fn = iface.fragments[0];
    const data = await provider.call({ to: address, data: iface.encodeFunctionData(fn, args), blockTag: at });
    return iface.decodeFunctionResult(fn, data);
  };
  const probe = async (id, action) => {
    try { return await action(); } catch { add(id, "critical", "Required read failed or returned invalid data"); return undefined; }
  };
  const network = await provider.getNetwork();
  const expectedChain = manifest._meta?.network ?? manifest._meta?.chainId;
  add("chain", String(expectedChain) === String(network.chainId) ? "ok" : "critical", { expected: String(expectedChain), actual: String(network.chainId) });
  const maxTokens = policy.maxTokens ?? 1000;
  if (!Number.isInteger(maxTokens) || maxTokens < 1 || maxTokens > 10000) throw new Error("maxTokens must be an integer from 1 to 10000");
  const records = { ...manifest._deployment?.records, ...manifest._metaAgDeployment?.records };
  if (manifest._governance?.status && manifest._governance.status !== "finalized") add("governance:handoff", "warning", { status: manifest._governance.status });
  for (const key of policy.requiredContracts || ["PECORVault_proxy", "PECOROrders_proxy", "PriceOracle_proxy", "PECOR_proxy", "MetaAGRouter_proxy", "OracleHub_proxy"]) {
    if (!isAddress(manifest[key] || "")) add(`required:${key}`, "critical", "Required contract missing or invalid");
  }
  for (const [key, address] of Object.entries(manifest)) {
    if (key.startsWith("_") || typeof address !== "string") continue;
    if (!isAddress(address)) { add(`code:${key}`, "critical", "Invalid contract address"); continue; }
    await probe(`code:${key}`, async () => {
      const code = await provider.getCode(address, at);
      const record = records[key];
      if (code === "0x") { add(`code:${key}`, "critical", "No runtime code"); return; }
      if (!record?.runtimeHash || record.address?.toLowerCase() !== address.toLowerCase()) add(`identity:${key}`, "critical", "Missing matching pinned deployment runtime identity");
      else add(`identity:${key}`, keccak256(code) === record.runtimeHash ? "ok" : "critical", { runtimeHash: keccak256(code), expected: record.runtimeHash });
      if (key.endsWith("_proxy")) {
        if (!["UUPSProxy", "ERC1967Proxy"].includes(record?.name)) { add(`proxy:${key}`, "critical", "Unsupported proxy identity; independently qualify implementation"); return; }
        const word = await provider.getStorage(address, IMPLEMENTATION_SLOT, at);
        const actual = getAddress("0x" + word.slice(-40));
        const expected = manifest[key.replace(/_proxy$/, "_impl")] || record.args?.[0];
        add(`proxy:${key}`, actual.toLowerCase() === expected?.toLowerCase() ? "ok" : "critical", { actual, expected });
      }
      if (record?.name === "PoolBeacon") {
        const [actual] = await read(address, "function implementation() view returns(address)");
        add(`beacon:${key}`, actual.toLowerCase() === record.args?.[0]?.toLowerCase() ? "ok" : "critical", { actual, expected: record.args?.[0] });
      }
    });
  }
  const vault = manifest.PECORVault_proxy;
  const tokens = [];
  if (isAddress(vault || "")) {
    await probe("vault:ready", async () => {
      const [ready] = await read(vault, "function liabilitiesInitialized() view returns(bool)");
      add("vault:ready", ready ? "ok" : "critical", ready);
    });
    let cursor = 0;
    while (cursor <= maxTokens) {
      const page = await probe("vault:inventory", () => read(vault, "function getRegisteredTokensPage(uint256,uint256) view returns(address[],uint256)", [cursor, Math.min(100, maxTokens + 1 - tokens.length)]));
      if (!page) break;
      const [items, next] = page;
      if (tokens.length + items.length > maxTokens) { add("vault:inventory", "critical", "Inventory exceeds maxTokens; incomplete snapshot"); break; }
      if (items.length === 0) { if (Number(next) !== cursor) add("vault:inventory", "critical", "Invalid terminal cursor"); else add("vault:inventory", "ok", tokens.length); break; }
      if (Number(next) !== cursor + items.length || items.some(t => tokens.some(v => v.toLowerCase() === t.toLowerCase())) || new Set(items.map(t => t.toLowerCase())).size !== items.length) { add("vault:inventory", "critical", "Invalid or duplicate inventory page"); break; }
      tokens.push(...items); cursor = Number(next);
    }
    for (const token of tokens) await probe(`asset:${token}`, async () => {
      const [custody] = await read(token, "function balanceOf(address) view returns(uint256)", [vault]);
      const [decimals] = await read(token, "function decimals() view returns(uint8)");
      const [book] = await read(vault, "function getReserves(address) view returns(uint256)", [token]);
      const [reserved] = await read(vault, "function getReservedBalance(address) view returns(uint256)", [token]);
      const [quarantined] = await read(vault, "function quarantined(address) view returns(bool)", [token]);
      assets.push({ token, decimals: Number(decimals), custody: String(custody), book: String(book), reserved: String(reserved), quarantined });
      add(`asset:${token}:backing`, custody < book || custody < reserved || book < reserved ? "critical" : "ok", { donation: custody > book ? String(custody - book) : "0" });
      if (Number(decimals) > 36) add(`asset:${token}:decimals`, "critical", "Unsupported token decimals");
      if (quarantined) add(`asset:${token}:quarantine`, "warning", true);
    });
  }
  for (const token of policy.requiredAssets || []) if (!tokens.some(t => t.toLowerCase() === token.toLowerCase())) add(`requiredAsset:${token}`, "critical", "Required asset absent from vault inventory");
  if (isAddress(manifest.PECOROrders_proxy || "")) await probe("orders:ready", async () => {
    const [ready] = await read(manifest.PECOROrders_proxy, "function liabilitiesReady() view returns(bool)"); add("orders:ready", ready ? "ok" : "critical", ready);
  });
  if (isAddress(manifest.PriceOracle_proxy || "")) for (const token of new Set([...tokens, ...(policy.requiredAssets || [])])) await probe(`oracle:${token}`, async () => {
    const [price, timestamp, roundId] = await read(manifest.PriceOracle_proxy, "function getPriceWithTimestamp(address) view returns(uint256,uint256,uint256)", [token]);
    const [stale] = await read(manifest.PriceOracle_proxy, "function isPriceStale(address) view returns(bool)", [token]);
    add(`oracle:${token}`, stale || price === 0n || timestamp > BigInt(block.timestamp) ? "critical" : "ok", { price: String(price), timestamp: String(timestamp), roundId: String(roundId) });
  });
  for (const key of ["PECOROrders_proxy", "PECOR_proxy", "MetaAGRouter_proxy", "PriceOracle_proxy", "OracleHub_proxy"]) if (isAddress(manifest[key] || "")) await probe(`paused:${key}`, async () => {
    const signature = key === "PECOROrders_proxy" ? "function orderExecutionPaused() view returns(bool)" : "function emergencyPaused() view returns(bool)";
    const [paused] = await read(manifest[key], signature); add(`paused:${key}`, paused ? (policy.pausedIsCritical ? "critical" : "warning") : "ok", paused);
  });
  if (isAddress(vault || "")) add("vault:pause-policy", "ok", "Vault intentionally has no global pause; custody exits remain governed by liabilities, roles and per-asset quarantine");
  if (!policy.expectedRoles?.length) add("roles:qualification", "warning", "No expectedRoles policy supplied; privilege assignments unqualified");
  for (const expectation of policy.expectedRoles || []) await probe(`role:${expectation.contract}:${expectation.account}:${expectation.role}`, async () => {
    const [present] = await read(manifest[expectation.contract], "function hasRole(bytes32,address) view returns(bool)", [expectation.role || ZeroHash, expectation.account]);
    add(`role:${expectation.contract}:${expectation.account}:${expectation.role}`, present === (expectation.present ?? true) ? "ok" : "critical", { present, expected: expectation.present ?? true });
  });
  const finalBlock = await provider.getBlock(at);
  add("snapshot:canonical", finalBlock?.hash === block.hash ? "ok" : "critical", { hash: block.hash, finalHash: finalBlock?.hash });
  return { version: 1, scope: { mode: policy.requiredContracts ? "explicit-required-contracts" : "full-stack", requiredContracts: policy.requiredContracts || ["PECORVault_proxy", "PECOROrders_proxy", "PriceOracle_proxy", "PECOR_proxy", "MetaAGRouter_proxy", "OracleHub_proxy"], limitation: policy.requiredContracts ? "Explicit contract requirements qualify only this selected monitoring scope; all supplied manifest identities and discovered vault assets are still checked" : null }, chainId: String(network.chainId), blockNumber: at, blockHash: block.hash, timestamp: block.timestamp, status: checks.some(c => c.status === "critical") ? "critical" : checks.some(c => c.status === "warning") ? "warning" : "ok", checks, assets };
}
module.exports = { inspectHealth, IMPLEMENTATION_SLOT };
