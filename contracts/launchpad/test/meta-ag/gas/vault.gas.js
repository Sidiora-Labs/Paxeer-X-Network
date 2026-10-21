/**
 * Sidiora Meta-AG — Phase 3 gas snapshot (vault layer).
 *
 * Produces `test/meta-ag/gas/vault.gas.json` — the Phase 3 baseline tracking
 * the four hot paths the plan pinned for PECORVault (see Task 3.1):
 *   - deposit (single token)
 *   - pullTokens (operator path)
 *   - pushTokens (operator path)
 *   - depositBatch with 10 registered tokens
 *
 * The companion file is committed under version control so diffs surface
 * gas regressions in CI. Rebuild via:
 *   node_modules/.bin/hardhat test test/meta-ag/gas/vault.gas.js
 */

const fs = require("fs");
const path = require("path");
const { ethers } = require("hardhat");

const { deployPECORVault } = require("../helpers/fixtures");

const ONE = 10n ** 18n;

async function deployStandardERC20(name, symbol, decimals = 18) {
  const F = await ethers.getContractFactory("MockStandardERC20");
  const t = await F.deploy(name, symbol, decimals);
  await t.waitForDeployment();
  return t;
}

async function deployMockWETH() {
  const F = await ethers.getContractFactory("MockWETH9");
  const w = await F.deploy();
  await w.waitForDeployment();
  return w;
}

async function deployMockTxTracker() {
  const F = await ethers.getContractFactory("MockTxTracker");
  const t = await F.deploy();
  await t.waitForDeployment();
  return t;
}

async function measure(tx) {
  const r = await (await tx).wait();
  return Number(r.gasUsed);
}

describe("meta-ag/gas/vault", function () {
  const outPath = process.env.UPDATE_GAS_SNAPSHOTS === "true"
    ? path.resolve(__dirname, "vault.gas.json")
    : path.join(require("os").tmpdir(), `vault.gas.${process.pid}.json`);
  const snapshot = {};

  after(function () {
    fs.mkdirSync(path.dirname(outPath), { recursive: true });
    fs.writeFileSync(outPath, JSON.stringify(snapshot, null, 2) + "\n");
    // eslint-disable-next-line no-console
    console.log(`\n[meta-ag:gas] wrote ${path.relative(process.cwd(), outPath)}`);
  });

  it("captures PECORVault hot-path gas", async function () {
    this.timeout(120_000);
    const [admin, operator, user, recipient] = await ethers.getSigners();

    const weth = await deployMockWETH();
    const tracker = await deployMockTxTracker();
    const vault = await deployPECORVault({
      weth: weth.target,
      tracker: tracker.target,
      admin: admin.address,
    });
    await vault.connect(admin).setOperator(operator.address, true);

    // -------- deposit (single token) --------
    const token = await deployStandardERC20("GasTok", "GAS", 18);
    await vault.connect(admin).registerToken(token.target, false);
    await token.mint(user.address, ONE * 100n);
    await token.connect(user).approve(vault.target, ONE * 100n);

    snapshot.deposit_first = await measure(
      vault.connect(user).deposit(token.target, ONE)
    );
    snapshot.deposit_followup = await measure(
      vault.connect(user).deposit(token.target, ONE)
    );

    // -------- pullTokens (operator path) --------
    snapshot.pullTokens_first = await measure(
      vault.connect(operator).pullTokens(token.target, user.address, ONE)
    );
    snapshot.pullTokens_followup = await measure(
      vault.connect(operator).pullTokens(token.target, user.address, ONE)
    );

    // -------- pushTokens (operator path) --------
    snapshot.pushTokens_first = await measure(
      vault.connect(operator).pushTokens(token.target, recipient.address, ONE)
    );
    snapshot.pushTokens_followup = await measure(
      vault.connect(operator).pushTokens(token.target, recipient.address, ONE)
    );

    // -------- depositBatch (10 tokens) --------
    const batchTokens = [];
    const batchAmounts = [];
    for (let i = 0; i < 10; i++) {
      const t = await deployStandardERC20(`Batch${i}`, `B${i}`, 18);
      await vault.connect(admin).registerToken(t.target, false);
      await t.mint(user.address, ONE * 10n);
      await t.connect(user).approve(vault.target, ONE * 10n);
      batchTokens.push(t.target);
      batchAmounts.push(ONE);
    }

    snapshot.depositBatch_10 = await measure(
      vault.connect(user).depositBatch(batchTokens, batchAmounts)
    );
  });
});
