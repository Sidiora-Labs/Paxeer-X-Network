const { expect } = require("chai");
const { ethers, network } = require("hardhat");
const { deployPriceOracle } = require("../meta-ag/helpers/fixtures");
const now = async () => (await ethers.provider.getBlock("latest")).timestamp;
async function mine(t) { await network.provider.send("evm_setNextBlockTimestamp", [t]); await network.provider.send("evm_mine"); }

describe("production oracle readiness", function () {
  let oracle, admin, relayer, token, other;
  beforeEach(async function () {
    [admin, relayer, token, other] = await ethers.getSigners();
    oracle = await deployPriceOracle({ admin: admin.address });
    await oracle.setRelayer(relayer.address, true);
    await oracle.registerToken(token.address, 60, 100, 1, ethers.MaxUint256, 3600);
  });
  it("preserves disabled publication metadata and enforces exact upward and downward boundaries", async function () {
    await oracle.connect(relayer).updatePrice(token.address, 10000);
    await oracle.connect(relayer).updatePrice(token.address, 20000);
    await expect(oracle.setMaxPriceJumpBps(token.address, 500)).to.emit(oracle, "MaxPriceJumpUpdated").withArgs(token.address, 0, 500);
    await oracle.connect(relayer).updatePrice(token.address, 21000);
    await expect(oracle.connect(relayer).updatePrice(token.address, 22051)).to.be.revertedWithCustomError(oracle, "PriceJumpExceeded");
    await oracle.connect(relayer).updatePrice(token.address, 19950);
    await expect(oracle.connect(relayer).updatePrice(token.address, 18952)).to.be.revertedWithCustomError(oracle, "PriceJumpExceeded");
    expect((await oracle.getTokenConfig(token.address)).deviationThresholdBps).to.equal(100);
    await oracle.setMaxPriceJumpBps(token.address, 0);
    await oracle.connect(relayer).updatePrice(token.address, 100);
  });
  it("handles full precision, first publication, stale anchors and governed recovery", async function () {
    await oracle.setMaxPriceJumpBps(token.address, 100);
    const large = 1n << 220n;
    await oracle.connect(relayer).updatePrice(token.address, large);
    await oracle.connect(relayer).updatePrice(token.address, large + large / 100n);
    const anchor = (await oracle.getLatestRound(token.address)).price;
    await mine((await now()) + 3601);
    await expect(oracle.connect(relayer).updatePrice(token.address, anchor * 2n)).to.be.revertedWithCustomError(oracle, "PriceJumpExceeded");
    await oracle.setMaxPriceJumpBps(token.address, 0);
    await oracle.connect(relayer).updatePrice(token.address, anchor * 2n);
    await expect(oracle.setMaxPriceJumpBps(token.address, 10001)).to.be.revertedWithCustomError(oracle, "InvalidConfig");
    await expect(oracle.connect(other).setMaxPriceJumpBps(token.address, 500)).to.be.revertedWithCustomError(oracle, "MissingRole");
  });
  it("rolls back every publication and TWAP write in a rejected batch", async function () {
    await oracle.registerToken(other.address, 60, 100, 1, 1000000, 3600);
    await oracle.connect(relayer).batchUpdatePrices([token.address, other.address], [10000, 10000]);
    await oracle.setMaxPriceJumpBps(other.address, 100);
    const before = await oracle.getLatestRound(token.address);
    const status = await oracle.getPriceStatus(token.address, 1);
    await expect(oracle.connect(relayer).batchUpdatePrices([token.address, other.address], [12000, 15000])).to.be.revertedWithCustomError(oracle, "PriceJumpExceeded");
    expect(await oracle.getLatestRound(token.address)).to.deep.equal(before);
    expect((await oracle.getPriceStatus(token.address, 1)).retainedObservationCount).to.equal(status.retainedObservationCount);
    await oracle.connect(relayer).updatePrice(token.address, 12000);
    const end = await now();
    expect(await oracle.getTWAP(token.address, end - Number(before.timestamp))).to.equal(10000);
  });
  it("reports missing data, exact heartbeat/expiry, full coverage and publication-only pause", async function () {
    expect((await oracle.getPriceStatus(other.address, 1)).registered).to.equal(false);
    let s = await oracle.getPriceStatus(token.address, 1);
    expect(s.fresh).to.equal(false); expect(s.heartbeatOverdue).to.equal(true);
    await oracle.connect(relayer).updatePrice(token.address, 10000);
    const first = await now();
    await mine(first + 60);
    s = await oracle.getPriceStatus(token.address, 60);
    expect(s.fresh).to.equal(true); expect(s.heartbeatOverdue).to.equal(false); expect(s.twapReady).to.equal(true);
    expect((await oracle.getPriceStatus(token.address, 61)).twapReady).to.equal(false);
    expect((await oracle.getPriceStatus(token.address, 0)).twapReady).to.equal(false);
    await oracle.grantRole(await oracle.PAUSER_ROLE(), other.address);
    await oracle.connect(other).pause();
    expect((await oracle.getPriceStatus(token.address, 60)).publishingPaused).to.equal(true);
    expect(await oracle.getPrice(token.address)).to.equal(10000);
    await expect(oracle.connect(other).unpause()).to.be.revertedWithCustomError(oracle, "MissingRole");
    await expect(oracle.connect(relayer).updatePrice(token.address, 10000)).to.be.revertedWithCustomError(oracle, "Paused");
    await mine(first + 3600); expect((await oracle.getPriceStatus(token.address, 60)).fresh).to.equal(true);
    await mine(first + 3601); s = await oracle.getPriceStatus(token.address, 60);
    expect(s.fresh).to.equal(false); expect(s.twapReady).to.equal(false); expect(s.heartbeatOverdue).to.equal(true);
  });
  it("bounds discovery and traverses all registered tokens without gaps", async function () {
    for (let i = 1; i <= 101; ++i) await oracle.registerToken(ethers.getAddress(ethers.zeroPadValue(ethers.toBeHex(i), 20)), 60, 100, 1, 1000000, 3600);
    expect(await oracle.registeredTokenCount()).to.equal(102);
    let [page, cursor] = await oracle.getRegisteredTokensPage(0, ethers.MaxUint256);
    expect(page.length).to.equal(100);
    const [tail, end] = await oracle.getRegisteredTokensPage(cursor, 100);
    expect([...page, ...tail]).to.deep.equal(await oracle.getRegisteredTokens());
    expect(end).to.equal(102);
    expect((await oracle.getRegisteredTokensPage(ethers.MaxUint256, 100))[1]).to.equal(102);
    expect((await oracle.getRegisteredTokensPage(0, 0))[0].length).to.equal(0);
  });
  it("retains conservative coverage after the observation ring wraps", async function () {
    this.timeout(120000);
    const times = [];
    for (let i = 0; i < 1001; ++i) { await oracle.connect(relayer).updatePrice(token.address, 10000); if (i < 2) times.push(await now()); }
    const s = await oracle.getPriceStatus(token.address, (await now()) - times[1]);
    expect(s.retainedObservationCount).to.equal(1000); expect(s.oldestObservationTimestamp).to.equal(times[1]); expect(s.twapReady).to.equal(true);
    expect((await oracle.getPriceStatus(token.address, (await now()) - times[0])).twapReady).to.equal(false);
  });
  it("upgrades a populated actual prior implementation preserving configuration, history and roles", async function () {
    const old = require("./baselines/PriceOracle.json");
    const impl = await new ethers.ContractFactory(old.abi, old.bytecode, admin).deploy();
    const Proxy = await ethers.getContractFactory("ERC1967Proxy");
    const proxy = await Proxy.deploy(impl.target, impl.interface.encodeFunctionData("initialize", [admin.address]));
    const previous = new ethers.Contract(proxy.target, old.abi, admin);
    await previous.setRelayer(relayer.address, true);
    await previous.registerToken(token.address, 60, 100, 1, 1000000, 3600);
    await previous.connect(relayer).updatePrice(token.address, 10000);
    const config = await previous.getTokenConfig(token.address), round = await previous.getLatestRound(token.address);
    const Current = await ethers.getContractFactory("PriceOracle"), upgraded = await Current.deploy();
    await previous.upgradeToAndCall(upgraded.target, "0x");
    const current = Current.attach(proxy.target);
    expect(await current.getTokenConfig(token.address)).to.deep.equal(config);
    expect(await current.getLatestRound(token.address)).to.deep.equal(round);
    expect((await current.getPriceHistory(token.address, 1))[0]).to.deep.equal(round);
    expect(await current.isAuthorizedRelayer(relayer.address)).to.equal(true);
    expect(await current.maxPriceJumpBps(token.address)).to.equal(0);
    expect((await current.getPriceStatus(token.address, 1)).retainedObservationCount).to.equal(1);
    await current.setMaxPriceJumpBps(token.address, 100);
    await expect(current.connect(relayer).updatePrice(token.address, 20000)).to.be.revertedWithCustomError(current, "PriceJumpExceeded");
  });
});
