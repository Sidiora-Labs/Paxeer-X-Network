const { expect } = require("chai");
const fs = require("fs");
const os = require("os");
const path = require("path");
const { atomicWrite, validateManifest, networkName } = require("../../scripts/lib/operations");
const { upgradePreflight } = require("../../scripts/lib/upgrade-preflight");
describe("OPS operator safety", function () {
  afterEach(() => { delete process.env.NETWORK_TYPE; delete process.env.UPGRADE_PLAN; });
  it("rejects caller-supplied wrong network labels", () => { process.env.NETWORK_TYPE = "paxeer-network"; expect(() => networkName()).to.throw("disagrees"); });
  it("rejects absent or mismatched manifest chains", async () => {
    const provider = { getNetwork: async () => ({ chainId: 125n }) };
    for (const manifest of [{}, { _meta: { network: "31337" } }]) {
      let error; try { await validateManifest(manifest, provider); } catch (e) { error = e; }
      expect(error.message).to.include("chain ID");
    }
  });
  it("rejects manifest addresses without code", async () => {
    const provider = { getNetwork: async () => ({ chainId: 125n }), getCode: async () => "0x" };
    let error; try { await validateManifest({ _meta: { network: "125" }, Router_proxy: "0x1111111111111111111111111111111111111111" }, provider); } catch (e) { error = e; }
    expect(error.message).to.include("No contract code");
  });
  it("writes recoverable complete atomic checkpoints", () => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "ops-checkpoint-")); const file = path.join(dir, "manifest.json");
    try { atomicWrite(file, { stage: 1 }); atomicWrite(file, { stage: 2 }); expect(JSON.parse(fs.readFileSync(file))).to.deep.equal({ stage: 2 }); expect(fs.readdirSync(dir)).to.deep.equal(["manifest.json"]); } finally { fs.rmSync(dir, { recursive: true }); }
  });
  it("rejects upgrades before transactions when no reviewed plan exists", async () => {
    let error; try { await upgradePreflight(["Router"]); } catch (e) { error = e; }
    expect(error.message).to.include("UPGRADE_PLAN required");
  });
});
