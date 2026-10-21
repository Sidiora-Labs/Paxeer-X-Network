const { expect } = require("chai");
const { ethers } = require("hardhat");
const { deployPecorFixture } = require("../meta-ag/helpers/fixtures");

const incident = ethers.id("incident-2026");
const pauseGas = { gasLimit: 9_000_000 };
async function controller(admin, guardian, targets = []) {
  return (await ethers.getContractFactory("EmergencyPauseController")).deploy(admin.address, guardian.address, targets);
}

describe("Production emergency containment", function () {
  it("isolates bounded failures, verifies postconditions, and handles repeated targets", async function () {
    const [admin, guardian, outsider] = await ethers.getSigners();
    const factory = await ethers.getContractFactory("EmergencyPauseTarget");
    const targets = [];
    for (let i = 0; i <= 6; i++) targets.push(await factory.deploy(i));
    const ctl = await controller(admin, guardian, targets.map(t => t.target));
    const order = [targets[1], targets[0], targets[2], targets[3], targets[4], targets[5], targets[6], targets[0]];
    const tx = await ctl.connect(guardian).pauseTargets([...order.map(t => t.target), outsider.address], incident, pauseGas);
    const events = (await tx.wait()).logs.map(l => { try { return ctl.interface.parseLog(l); } catch { return null; } })
      .filter(l => l && l.name === "IncidentTargetResult");
    expect(events.map(l => Number(l.args.status))).to.deep.equal([4, 0, 5, 3, 3, 4, 4, 1, 2]);
    expect(await targets[0].emergencyPaused()).to.equal(true);
    await expect(ctl.connect(outsider).pauseTargets([targets[0].target], incident, pauseGas)).to.be.revertedWithCustomError(ctl, "MissingRole");
    await expect(ctl.connect(guardian).setTarget(targets[0].target, false)).to.be.revertedWithCustomError(ctl, "MissingRole");
    await expect(ctl.connect(guardian).grantRole(ethers.ZeroHash, guardian.address)).to.be.revertedWithCustomError(ctl, "MissingRole");
    await ctl.setTarget(targets[0].target, false);
    expect(await ctl.allowedTargets(targets[0].target)).to.equal(false);
  });

  it("processes the final healthy target after 31 distinct gas-exhausting targets at the declared budget", async function () {
    const [admin, guardian] = await ethers.getSigners();
    const factory = await ethers.getContractFactory("EmergencyPauseTarget");
    const targets = [];
    for (let i = 0; i < 31; i++) targets.push(await factory.deploy(5));
    targets.push(await factory.deploy(0));
    const ctl = await controller(admin, guardian, targets.map(t => t.target));
    const tx = await ctl.connect(guardian).pauseTargets(targets.map(t => t.target), incident,
      { gasLimit: 32 * 250000 + 150000 });
    const receipt = await tx.wait();
    const results = receipt.logs.map(l => { try { return ctl.interface.parseLog(l); } catch { return null; } })
      .filter(l => l && l.name === "IncidentTargetResult");
    expect(results).to.have.length(32);
    expect(results.slice(0, 31).every(l => Number(l.args.status) === 4)).to.equal(true);
    expect(results[31].args.target).to.equal(targets[31].target);
    expect(Number(results[31].args.status)).to.equal(0);
    expect(await targets[31].emergencyPaused()).to.equal(true);
    expect(receipt.gasUsed).to.be.lessThan(8150000n);
  });

  it("rejects invalid registration and bounded batch/gas violations", async function () {
    const [admin, guardian] = await ethers.getSigners();
    const target = await (await ethers.getContractFactory("EmergencyPauseTarget")).deploy(0);
    const ctl = await controller(admin, guardian, [target.target]);
    await expect(ctl.setTarget(admin.address, true)).to.be.revertedWithCustomError(ctl, "InvalidTarget");
    await expect(ctl.connect(guardian).pauseTargets([], incident)).to.be.revertedWithCustomError(ctl, "InvalidBatchLength");
    await expect(ctl.connect(guardian).pauseTargets(Array(33).fill(target.target), incident)).to.be.revertedWithCustomError(ctl, "InvalidBatchLength");
    await expect(ctl.connect(guardian).pauseTargets([target.target], incident, { gasLimit: 150000 })).to.be.revertedWithCustomError(ctl, "InsufficientIncidentGas");
    expect(await target.emergencyPaused()).to.equal(false);
  });

  it("halts real proxies while preserving cancellation, cached prices and admin-only recovery", async function () {
    const f = await deployPecorFixture();
    const e = { ...f, ...f.signers, ...f.tokens, orders: f.pecorOrders, oracleHub: f.hub, priceOracle: f.oracle, tokenA: f.tokens.sidA };
    await e.vault.registerToken(e.tokenA.target, false);
    await e.usdl.mint(e.user.address, 2n * 10n ** 18n);
    await e.usdl.connect(e.user).approve(e.vault.target, ethers.MaxUint256);
    const [admin, guardian] = await ethers.getSigners();
    // Fixture names are asserted below so this test always uses actual proxies.
    const targets = [e.pecor, e.orders, e.router, e.oracleHub, e.priceOracle];
    const ctl = await controller(admin, guardian, targets.map(t => t.target));
    for (const target of targets) await target.grantRole(await target.PAUSER_ROLE(), ctl.target);
    const unit = 10n ** 18n;
    await e.orders.connect(e.user).placeLimitBuy(e.usdl.target, e.tokenA.target, unit, unit, 0);
    const expiry = (await ethers.provider.getBlock("latest")).timestamp + 100;
    await e.orders.connect(e.user).placeLimitBuy(e.usdl.target, e.tokenA.target, unit, unit, expiry);
    const before = await e.usdl.balanceOf(e.user.address);
    await ctl.connect(guardian).pauseTargets(targets.map(t => t.target), incident, pauseGas);
    for (const target of targets) {
      expect(await target.emergencyPaused()).to.equal(true);
      await expect(target.connect(guardian).unpause()).to.be.revertedWithCustomError(target, "MissingRole");
      await expect(target.connect(guardian).grantRole(ethers.ZeroHash, guardian.address)).to.be.revertedWithCustomError(target, "MissingRole");
    }
    await expect(e.orders.connect(e.keeper).executeLimitOrder(1)).to.be.revertedWithCustomError(e.orders, "Paused");
    await expect(e.pecor.connect(e.user).swapExactIn(e.usdl.target, e.tokenA.target, unit, 0, 0)).to.be.revertedWithCustomError(e.pecor, "Paused");
    expect(await e.priceOracle.getPrice(e.tokenA.target)).to.be.greaterThan(0);
    await e.vault.setTokenQuarantine(e.usdl.target, true);
    await e.orders.connect(e.user).cancelLimitOrder(1);
    expect(await e.usdl.balanceOf(e.user.address)).to.equal(before + unit);
    expect(await e.vault.getReservedBalance(e.usdl.target)).to.equal(unit);
    await ethers.provider.send("evm_increaseTime", [101]);
    await ethers.provider.send("evm_mine", []);
    await e.orders.connect(e.other).expireLimitOrder(2);
    expect(await e.usdl.balanceOf(e.user.address)).to.equal(before + 2n * unit);
    expect(await e.vault.getReservedBalance(e.usdl.target)).to.equal(0);
    for (const target of targets) { await target.unpause(); expect(await target.emergencyPaused()).to.equal(false); }
  });
});
