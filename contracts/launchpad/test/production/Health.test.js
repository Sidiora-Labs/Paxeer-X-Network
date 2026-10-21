"use strict";
const { expect } = require("chai");
const { Interface, keccak256, ZeroHash, zeroPadValue } = require("ethers");
const { inspectHealth, IMPLEMENTATION_SLOT } = require("../../scripts/lib/production-health");
describe("Production provider-only health snapshot", function () {
  const addr = n => "0x" + n.toString(16).padStart(40, "0");
  const abi = new Interface(["function liabilitiesInitialized() view returns(bool)", "function liabilitiesReady() view returns(bool)", "function getRegisteredTokensPage(uint256,uint256) view returns(address[],uint256)", "function balanceOf(address) view returns(uint256)", "function decimals() view returns(uint8)", "function getReserves(address) view returns(uint256)", "function getReservedBalance(address) view returns(uint256)", "function quarantined(address) view returns(bool)", "function getPriceWithTimestamp(address) view returns(uint256,uint256,uint256)", "function isPriceStale(address) view returns(bool)", "function emergencyPaused() view returns(bool)", "function orderExecutionPaused() view returns(bool)", "function hasRole(bytes32,address) view returns(bool)"]);
  function fixture(o = {}) {
    const tokens = Array.from({ length: o.count ?? 1 }, (_, i) => addr(100 + i)), calls = [];
    const provider = {
      async getNetwork() { return { chainId: 31337n }; },
      async getBlock(tag) { calls.push(["block", tag]); return { number: 50, timestamp: 1000, hash: o.reorg && calls.filter(c => c[0] === "block").length > 1 ? "0x" + "bb".repeat(32) : "0x" + "aa".repeat(32) }; },
      async getCode(a, block) { calls.push(["code", block]); return o.changedCode ? "0x6001" : "0x6000"; },
      async getStorage(a, slot, block) { expect(slot).to.equal(IMPLEMENTATION_SLOT); calls.push(["storage", block]); return zeroPadValue(o.changedImpl ? addr(9) : addr(4), 32); },
      async call(tx) {
        calls.push(["call", tx.blockTag]); const p = abi.parseTransaction(tx); if (tx.to === addr(1) && ["paused", "emergencyPaused"].includes(p.name)) throw new Error("Vault has no global pause API"); if (p.name === o.fail) throw new Error("secret credential"); let r;
        switch (p.name) {
        case "getRegisteredTokensPage": { const cursor = Number(p.args[0]); const page = tokens.slice(cursor, cursor + Number(p.args[1])); r = [page, cursor + page.length]; break; }
        case "balanceOf": r = [o.custody ?? 100n]; break;
        case "getReserves": r = [100n]; break;
        case "getReservedBalance": r = [20n]; break;
        case "decimals": r = [6]; break;
        case "getPriceWithTimestamp": r = [10n ** 18n, 990n, 1n]; break;
        case "isPriceStale": r = [!!o.stale]; break;
        case "emergencyPaused": case "orderExecutionPaused": r = [!!o.paused]; break;
        case "quarantined": r = [false]; break;
        case "hasRole": r = [!o.roleMissing]; break;
        default: r = [true];
        } return abi.encodeFunctionResult(p.fragment, r);
      }
    };
    const manifest = { _meta: { network: o.wrongChain ? "1" : "31337" }, PECORVault_proxy: addr(1), PECOROrders_proxy: addr(2), PriceOracle_proxy: addr(3), _deployment: { records: {} } };
    for (const key of ["PECORVault_proxy", "PECOROrders_proxy", "PriceOracle_proxy"]) manifest._deployment.records[key] = { address: manifest[key], runtimeHash: keccak256("0x6000"), name: "UUPSProxy", args: [addr(4)] };
    return { provider, manifest, calls, policy: { requiredContracts: ["PECORVault_proxy", "PECOROrders_proxy", "PriceOracle_proxy"], requiredAssets: [tokens[0]], expectedRoles: [{ contract: "PECORVault_proxy", role: ZeroHash, account: addr(8) }] } };
  }
  it("default full-stack scope fails closed when engine and hub are absent", async function () {
    const f = fixture(); delete f.policy.requiredContracts;
    const r = await inspectHealth(f);
    expect(r.status).to.equal("critical"); expect(r.scope.mode).to.equal("full-stack");
    for (const key of ["PECOR_proxy", "MetaAGRouter_proxy", "OracleHub_proxy"]) expect(r.checks.find(c => c.id === `required:${key}`).status).to.equal("critical");
    const partial = await inspectHealth(fixture()); expect(partial.scope.mode).to.equal("explicit-required-contracts"); expect(partial.scope.limitation).to.include("selected monitoring scope");
  });
  it("rejects malformed policies rather than silently ignoring misspelled requirements", async function () {
    for (const policy of [{ requiredAsset: [] }, { requiredAssets: ["bad"] }, { expectedRoles: [{}] }, { pausedIsCritical: "false" }]) {
      let error; try { await inspectHealth({ ...fixture(), policy }); } catch (e) { error = e; } expect(error).to.be.instanceOf(Error);
    }
  });
  it("pins raw accounting and actual decimals without signer or transaction API", async function () {
    const f = fixture(); const r = await inspectHealth(f); expect(r.status).to.equal("ok"); expect(r.assets[0]).to.include({ decimals: 6, custody: "100", book: "100", reserved: "20" }); expect(f.calls.filter(c => c[0] !== "block").every(c => c[1] === 50)).to.equal(true);
  });
  it("detects deficits and accepts inventory donation", async function () {
    expect((await inspectHealth(fixture({ custody: 19n }))).status).to.equal("critical"); const r = await inspectHealth(fixture({ custody: 110n })); expect(r.status).to.equal("ok"); expect(r.checks.find(c => c.id.endsWith(":backing")).detail.donation).to.equal("10");
  });
  for (const option of ["stale", "changedImpl", "changedCode", "wrongChain", "reorg", "roleMissing"]) it(`fails closed on ${option}`, async function () { expect((await inspectHealth(fixture({ [option]: true }))).status).to.equal("critical"); });
  it("does not leak failed RPC exception text", async function () { const r = await inspectHealth(fixture({ fail: "balanceOf" })); expect(r.status).to.equal("critical"); expect(JSON.stringify(r)).not.to.include("credential"); });
  it("traverses over 100 tokens and fails incomplete caps", async function () { const f = fixture({ count: 101 }); const r = await inspectHealth(f); expect(r.status).to.equal("ok"); expect(r.assets.length).to.equal(101); f.policy.maxTokens = 100; expect((await inspectHealth(f)).status).to.equal("critical"); });
  it("distinguishes operational pause from strict policy", async function () { const f = fixture({ paused: true }); expect((await inspectHealth(f)).status).to.equal("warning"); f.policy.pausedIsCritical = true; expect((await inspectHealth(f)).status).to.equal("critical"); });
  it("discloses privilege qualification absent explicit role policy", async function () { const f = fixture(); delete f.policy.expectedRoles; expect((await inspectHealth(f)).checks.find(c => c.id === "roles:qualification").status).to.equal("warning"); });
});
