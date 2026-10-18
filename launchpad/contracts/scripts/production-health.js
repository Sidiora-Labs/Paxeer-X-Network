#!/usr/bin/env node
"use strict";
const fs = require("fs");
const { JsonRpcProvider } = require("ethers");
const { inspectHealth } = require("./lib/production-health");
/** Usage: RPC_URL=https://... node scripts/production-health.js manifest.json [policy.json]
 * RPC endpoint and exception details are never emitted (URLs may contain credentials).
 * Exit 0: ok/warning; 1: critical or incomplete. No Hardhat config/private key needed.
 */
async function main(args = process.argv.slice(2)) {
  if (!args[0] || !process.env.RPC_URL) throw new Error("Manifest path and RPC_URL required");
  const provider = new JsonRpcProvider(process.env.RPC_URL);
  try {
    const result = await inspectHealth({ provider, manifest: JSON.parse(fs.readFileSync(args[0], "utf8")), policy: args[1] ? JSON.parse(fs.readFileSync(args[1], "utf8")) : {} });
    console.log(JSON.stringify(result, null, 2));
    if (result.status === "critical") process.exitCode = 1;
    return result;
  } finally { provider.destroy(); }
}
if (require.main === module) main().catch(() => { console.error(JSON.stringify({ status: "critical", error: "Health snapshot failed; validate manifest, policy and RPC availability" })); process.exitCode = 1; });
module.exports = { main };
