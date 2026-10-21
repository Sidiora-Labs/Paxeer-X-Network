const { expect } = require("chai");
const { ethers, network } = require("hardhat");
const { deployPriceOracle, deployOracleHub, deployPriceOracleAdapter, deployTransactionTracker } = require("../helpers/fixtures");
const ONE = 10n ** 18n;
const timestamp = async () => (await ethers.provider.getBlock("latest")).timestamp;
async function at(t) { await network.provider.send("evm_setNextBlockTimestamp", [t]); }
async function mine(t) { await at(t); await network.provider.send("evm_mine"); }

describe("oracle audit remediation", function () {
  let admin, relayer, token, oracle;
  beforeEach(async function () {
    [admin, relayer, token] = await ethers.getSigners();
    oracle = await deployPriceOracle({ admin: admin.address });
    await oracle.setRelayer(relayer.address, true);
    await oracle.registerToken(token.address, 60, 100, 1, ONE * 1000000n, 7200);
  });
  it("ORACLE-01/02/13: exact sparse TWAP, full coverage, stale rejection and no spot downgrade", async function () {
    await oracle.connect(relayer).updatePrice(token.address, 100n * ONE);
    const first = await timestamp();
    await at(first + 1000); await oracle.connect(relayer).updatePrice(token.address, 200n * ONE);
    await mine(first + 1100);
    expect(await oracle.getTWAP(token.address, 50)).to.equal(200n * ONE);
    expect(await oracle.getTWAP(token.address, 200)).to.equal(150n * ONE);
    await expect(oracle.getTWAP(token.address, 1101)).to.be.revertedWithCustomError(oracle, "TwapWindowInvalid");
    const hub = await deployOracleHub({ admin: admin.address, primaryOracle: oracle.target });
    await expect(hub.getTWAP(token.address, 0)).to.be.revertedWithCustomError(hub, "InvalidConfig");
    await mine(first + 8201);
    await expect(oracle.getTWAP(token.address, 50)).to.be.revertedWithCustomError(oracle, "StalePrice");
    await expect(hub.getTWAP(token.address, 50)).to.be.revertedWithCustomError(oracle, "StalePrice");
  });
  it("ORACLE-02/08: same-block observations coalesce and deviation remains trusted relayer metadata", async function () {
    await network.provider.send("evm_setAutomine", [false]);
    let tx1, tx2;
    try {
      const nonce = await relayer.getNonce();
      tx1 = await oracle.connect(relayer).updatePrice(token.address, ONE, { nonce, gasLimit: 1000000 });
      tx2 = await oracle.connect(relayer).updatePrice(token.address, 200n * ONE, { nonce: nonce + 1, gasLimit: 1000000 });
      await network.provider.send("evm_mine");
    } finally { await network.provider.send("evm_setAutomine", [true]); }
    await tx1.wait(); await tx2.wait();
    const last = await timestamp();
    await mine(last + 20);
    expect(await oracle.getTWAP(token.address, 20)).to.equal(200n * ONE);
  });
  it("ORACLE-03: retains at most 1000 new rounds and bounds history allocations", async function () {
    this.timeout(120000);
    let first;
    for (let i = 0; i < 1002; ++i) {
      await oracle.connect(relayer).updatePrice(token.address, ONE + BigInt(i));
      if (i === 0) first = await timestamp();
    }
    const history = await oracle.getPriceHistory(token.address, ethers.MaxUint256);
    expect(history.length).to.equal(1000);
    expect(history[0].roundId).to.equal(1002n);
    expect(history[999].roundId).to.equal(3n);
    await expect(oracle.getRoundData(token.address, 1)).to.be.revertedWithCustomError(oracle, "TokenNotConfigured");
    await expect(oracle.getTWAP(token.address, (await timestamp()) - first)).to.be.revertedWithCustomError(oracle, "TwapWindowInvalid");
    expect(await oracle.getTWAP(token.address, 10)).to.be.gt(0);
  });
  it("ORACLE-09: inherited generic role operations synchronize relayer mirrors", async function () {
    const role = await oracle.RELAYER_ROLE();
    await oracle.connect(relayer).renounceRole(role, relayer.address);
    expect(await oracle.authorizedRelayers(relayer.address)).to.equal(false);
    expect(await oracle.isAuthorizedRelayer(relayer.address)).to.equal(false);
    await oracle.grantRole(role, relayer.address);
    expect(await oracle.authorizedRelayers(relayer.address)).to.equal(true);
    await oracle.revokeRole(role, relayer.address);
    expect(await oracle.authorizedRelayers(relayer.address)).to.equal(false);
  });
  it("ORACLE-10/15: long token expiry survives hub and batch count measures applied updates", async function () {
    const tx = await oracle.connect(relayer).batchUpdatePrices([token.address, ethers.ZeroAddress], [ONE, ONE]);
    const logs = (await tx.wait()).logs.map(l => { try { return oracle.interface.parseLog(l); } catch {} });
    expect(logs.find(l => l?.name === "BatchPriceUpdate").args[1]).to.equal(1n);
    const adapter = await deployPriceOracleAdapter({ priceOracle: oracle.target });
    const hub = await deployOracleHub({ admin: admin.address, primaryOracle: oracle.target, minConfidence: 3000 });
    await hub.registerAdapter(adapter.target, 1);
    await mine((await timestamp()) + 4000);
    expect(await hub.getPrice(token.address)).to.equal(ONE);
  });
  it("ORACLE-04/05/06/07/12: invalid metadata and future sources isolate; median and metadata use accepted sources", async function () {
    const hub = await deployOracleHub({ admin: admin.address, primaryOracle: oracle.target, minConfidence: 3000, deviationBps: 500 });
    const F = await ethers.getContractFactory("MockOracleMetadataAdapter");
    const adapters = [];
    for (let i = 0; i < 5; ++i) { const a = await F.deploy(ethers.id(`audit-${i}`)); adapters.push(a); await hub.registerAdapter(a.target, i); }
    const now = await timestamp();
    await adapters[0].setFeed(ONE, now, 9000); await adapters[0].setMetadataReverts(true);
    await adapters[1].setFeed(ONE, now + 10000, 9000);
    await adapters[2].setFeed(ONE, now - 100, 9000);
    await adapters[3].setFeed(ONE, now - 50, 8000);
    await adapters[4].setFeed(2n * ONE, now, 9000);
    expect(await hub.getPrice(token.address)).to.equal(ONE);
    expect((await hub.getPricesBatch([token.address]))[0][0]).to.equal(ONE);
    expect((await hub.isPriceAvailable(token.address))[0]).to.equal(true);
    const result = await hub.getAggregatedPrice(token.address);
    expect(result.price).to.equal(ONE); expect(result.sourceCount).to.equal(2n); expect(result.timestamp).to.equal(BigInt(now - 100));
    await adapters[2].setFeed(ONE, now, 1500); await adapters[3].setFeed(ONE, now, 1500); await adapters[4].setFeed(ONE, now, 1500);
    expect((await hub.isPriceAvailable(token.address))[0]).to.equal(false);
    await expect(hub.getPrice(token.address)).to.be.revertedWithCustomError(hub, "NoActiveAdapters");
    for (let i = 5; i < 20; ++i) { const a = await F.deploy(ethers.id(`capacity-${i}`)); await hub.registerAdapter(a.target, i); }
    expect(await hub.adapterCount()).to.equal(20n);
    await hub.removeAdapter(await adapters[0].sourceId());
    expect(await hub.adapterCount()).to.equal(19n);
    await hub.registerAdapter(adapters[0].target, 0); expect(await hub.adapterCount()).to.equal(20n);
  });
  it("ORACLE-09/14: emitter mirrors track generic roles and idle-day analytics is empty", async function () {
    const tracker = await deployTransactionTracker({ admin: admin.address });
    const role = await tracker.EMITTER_ROLE();
    await tracker.grantRole(role, relayer.address);
    expect(await tracker.authorizedEmitters(relayer.address)).to.equal(true);
    await tracker.connect(relayer).recordTrade(admin.address, token.address, admin.address, 10, 10, 10);
    const initial = await tracker.getCurrentDayStats();
    expect(initial.timestamp).to.equal(await tracker.currentDayTimestamp());
    expect(initial.totalVolume).to.equal(10n);
    const today = Math.floor((await timestamp()) / 86400) * 86400;
    await mine(today + 86400 * 3 + 1);
    const current = await tracker.getCurrentDayStats();
    expect(current.timestamp).to.equal(BigInt(today + 86400 * 3));
    expect(current.totalVolume).to.equal(0n); expect(current.totalTrades).to.equal(0n);
    await tracker.connect(relayer).renounceRole(role, relayer.address);
    expect(await tracker.authorizedEmitters(relayer.address)).to.equal(false);
  });
  it("ORACLE-11/16: spot references default off, require explicit opt-in and normalize USDL depeg", async function () {
    const registry = await (await ethers.getContractFactory("MockSidioraPoolRegistry")).deploy();
    const pool = await (await ethers.getContractFactory("MockSidioraPool")).deploy();
    await registry.setPoolByToken(token.address, pool.target);
    await pool.setPrice(2n * ONE); await pool.setReserves(ONE, 1000n * ONE, ONE);
    const F = await ethers.getContractFactory("SidioraFeedAdapter");
    await expect(F.deploy(registry.target, 0, admin.address)).to.be.revertedWithCustomError(F, "InvalidConfig");
    const adapter = await F.deploy(registry.target, ONE, admin.address);
    expect((await adapter.getFeedPrice(token.address)).price).to.equal(0n);
    const reference = await (await ethers.getContractFactory("MockUsdReference")).deploy();
    await reference.setPrice(ONE / 2n);
    await expect(adapter.configureSpotReference(true, reference.target, admin.address)).to.emit(adapter, "SpotReferenceConfigured");
    expect((await adapter.getFeedPrice(token.address)).price).to.equal(ONE);
    // Reserve movement remains instantaneous, explicitly enabled risk, not TWAP.
    await pool.setPrice(4n * ONE);
    expect((await adapter.getFeedPrice(token.address)).price).to.equal(2n * ONE);
    await reference.setStale(true);
    expect((await adapter.getFeedPrice(token.address)).price).to.equal(0n);
    await expect(adapter.setMinLiquidityThreshold(0)).to.be.revertedWithCustomError(adapter, "InvalidConfig");
    await expect(adapter.setMinLiquidityThreshold(2n * ONE)).to.emit(adapter, "MinLiquidityThresholdUpdated").withArgs(ONE, 2n * ONE);
    await expect(adapter.registerKnownToken(token.address)).to.emit(adapter, "KnownTokenRegistered").withArgs(token.address);
    await expect(adapter.setPoolRegistry(registry.target)).to.emit(adapter, "PoolRegistryUpdated").withArgs(registry.target, registry.target);
  });

});
