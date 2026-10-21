/**
 * Sidiora Meta-AG — Phase 2 gas snapshot (oracle layer).
 *
 * With explicit opt-in, updates `test/meta-ag/gas/oracle.gas.json`. Default runs write temporary observations. This is the Phase 2 baseline
 * that Phase 7 will extend. The suite runs under the standard mocha harness
 * so the snapshot stays reproducible across CI shards.
 *
 * To refresh the baseline:
 *   UPDATE_GAS_SNAPSHOTS=true node node_modules/hardhat/internal/cli/cli.js test test/meta-ag/gas/oracle.gas.js
 *
 * The file it writes is intentionally checked in — diffs flag gas regressions.
 */

const fs = require("fs");
const path = require("path");
const { ethers } = require("hardhat");

const {
  deployPriceOracle,
  deployOracleHub,
  deployPriceOracleAdapter,
} = require("../helpers/fixtures");

const ONE = 10n ** 18n;

async function deployERC20(label) {
  const F = await ethers.getContractFactory("MockERC20");
  const t = await F.deploy(label, label.slice(0, 4).toUpperCase(), 18);
  await t.waitForDeployment();
  return t;
}

async function measure(tx) {
  const r = await (await tx).wait();
  return Number(r.gasUsed);
}

describe("meta-ag/gas/oracle", function () {
  const outPath = process.env.UPDATE_GAS_SNAPSHOTS === "true"
    ? path.resolve(__dirname, "oracle.gas.json")
    : path.join(require("os").tmpdir(), `oracle.gas.${process.pid}.json`);
  const snapshot = {};

  after(function () {
    fs.mkdirSync(path.dirname(outPath), { recursive: true });
    fs.writeFileSync(outPath, JSON.stringify(snapshot, null, 2) + "\n");
    // eslint-disable-next-line no-console
    console.log(`\n[meta-ag:gas] wrote ${path.relative(process.cwd(), outPath)}`);
  });

  it("captures PriceOracle + OracleHub hot-path gas", async function () {
    this.timeout(120_000);
    const [admin, relayer] = await ethers.getSigners();

    const oracle = await deployPriceOracle({ admin: admin.address });
    await oracle.connect(admin).setRelayer(relayer.address, true);

    const counts = [5, 20, 50];
    const tokens = [];
    for (let i = 0; i < Math.max(...counts); i++) {
      const t = await deployERC20(`T${i}`);
      await oracle.connect(admin).registerToken(
        t.target,
        60,
        100,
        ONE / 100n,
        ONE * 1_000_000n,
        3600
      );
      tokens.push(t.target);
    }

    // updatePrice (single)
    snapshot.updatePrice_first = await measure(
      oracle.connect(relayer).updatePrice(tokens[0], ONE)
    );
    snapshot.updatePrice_followup = await measure(
      oracle.connect(relayer).updatePrice(tokens[0], ONE + 1n)
    );

    // batchUpdatePrices (5 / 20 / 50)
    for (const n of counts) {
      const slice = tokens.slice(0, n);
      const prices = slice.map((_, i) => ONE + BigInt(i));
      snapshot[`batchUpdatePrices_${n}`] = await measure(
        oracle.connect(relayer).batchUpdatePrices(slice, prices)
      );
    }

    // getPrice (view — use gas estimate)
    snapshot.getPrice_view = Number(
      await oracle.getPrice.estimateGas(tokens[0])
    );

    // getTWAP — seed a few samples then measure
    for (let i = 0; i < 3; i++) {
      await oracle.connect(relayer).updatePrice(tokens[0], ONE);
      await ethers.provider.send("evm_increaseTime", [60]);
      await ethers.provider.send("evm_mine", []);
    }
    snapshot.getTWAP_view = Number(
      await oracle.getTWAP.estimateGas(tokens[0], 60 * 3)
    );

    // OracleHub hot-paths
    const hub = await deployOracleHub({
      admin: admin.address,
      primaryOracle: oracle.target,
      deviationBps: 500,
      minConfidence: 3000,
    });
    const pocAdapter = await deployPriceOracleAdapter({ priceOracle: oracle.target });
    snapshot.oracleHub_registerAdapter = await measure(
      hub.connect(admin).registerAdapter(pocAdapter.target, 10)
    );
    snapshot.oracleHub_getPrice_view = Number(
      await hub.getPrice.estimateGas(tokens[0])
    );
    snapshot.oracleHub_getAggregatedPrice_view = Number(
      await hub.getAggregatedPrice.estimateGas(tokens[0])
    );
  });
});
