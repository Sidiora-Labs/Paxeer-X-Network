const { expect } = require("chai");
const { ethers } = require("hardhat");
const fs = require("fs");
const path = require("path");
const crypto = require("crypto");
// Immutable bytecode captured before remediation qualifies only the local audit
// baseline. No unknown deployed previous release is inferred from this test.
const BASELINE = path.resolve(__dirname, "../../../audit/2026-10-09/remediation/baseline-artifacts");
async function oldProxy(source, name, args, signer) {
  const artifactPath = path.join(BASELINE, source, `${name}.sol`, `${name}.json`);
  const bytes = fs.readFileSync(artifactPath);
  const manifest = JSON.parse(fs.readFileSync(path.join(BASELINE, "manifest.json"), "utf8"));
  const entry = manifest.artifacts.find(a => a.path === path.relative(BASELINE, artifactPath));
  expect(entry, "baseline artifact is pinned").to.exist;
  expect(crypto.createHash("sha256").update(bytes).digest("hex")).to.equal(entry.sha256);
  const artifact = JSON.parse(bytes);
  const factory = new ethers.ContractFactory(artifact.abi, artifact.bytecode, signer);
  const implementation = await factory.deploy();
  const Proxy = await ethers.getContractFactory("UUPSProxy", signer);
  const proxy = await Proxy.deploy(implementation.target, factory.interface.encodeFunctionData("initialize", args));
  return { instance: new ethers.Contract(proxy.target, artifact.abi, signer),
    implementation, hash: crypto.createHash("sha256").update(artifact.bytecode).digest("hex") };
}
describe("Audit remediation: populated immutable baseline UUPS migration", function () {
  // Missing immutable baseline is an explicit failure, never a silent skip.
  it("preserves Treasury ledger, roles and custody; rejected migration rolls back implementation", async function () {
    const [admin, depositor, recipient, outsider] = await ethers.getSigners();
    const Token = await ethers.getContractFactory("MockERC20");
    const token = await Token.deploy("USD Ledger", "USDL", 6);
    const prior = await oldProxy("protocol", "Treasury", [ethers.ZeroAddress, admin.address], admin);
    const treasury = prior.instance;
    const role = ethers.id("DEPOSITOR_ROLE");
    await treasury.grantRole(role, depositor.address);
    await token.mint(depositor.address, 100000000n);
    await token.connect(depositor).approve(treasury.target, 100000000n);
    await treasury.connect(depositor).deposit(token.target, 100000000n);
    const New = await ethers.getContractFactory("Treasury");
    const next = await New.deploy();
    const slot = "0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc";
    const oldSlot = await ethers.provider.getStorage(treasury.target, slot);
    await expect(treasury.connect(outsider).upgradeToAndCall(next.target, "0x")).to.be.reverted;
    expect(await ethers.provider.getStorage(treasury.target, slot)).to.equal(oldSlot);
    await expect(treasury.upgradeToAndCall(next.target, "0xdeadbeef")).to.be.reverted;
    expect(await ethers.provider.getStorage(treasury.target, slot)).to.equal(oldSlot);
    await treasury.upgradeToAndCall(next.target, "0x");
    const upgraded = New.attach(treasury.target);
    expect(await upgraded.getBalance(token.target)).to.equal(100000000n);
    expect(await token.balanceOf(upgraded.target)).to.equal(100000000n);
    expect(await upgraded.hasRole(role, depositor.address)).to.equal(true);
    await upgraded.withdraw(token.target, recipient.address, 20000000n);
    expect(await upgraded.getBalance(token.target)).to.equal(80000000n);
    expect(await token.balanceOf(recipient.address)).to.equal(20000000n);
    expect(prior.hash).to.have.length(64);
  });
  it("preserves populated vault reserves, cumulative flows, registration and operator rights", async function () {
    const [admin, operator, recipient] = await ethers.getSigners();
    const Weth = await ethers.getContractFactory("MockWETH9");
    const weth = await Weth.deploy();
    const Token = await ethers.getContractFactory("MockStandardERC20");
    const token = await Token.deploy("Six decimal inventory", "INV", 6);
    const prior = await oldProxy("meta-ag/vault", "PECORVault", [weth.target, ethers.ZeroAddress, admin.address], admin);
    const vault = prior.instance;
    await vault.registerToken(token.target, true);
    await vault.setOperator(operator.address, true);
    await token.mint(admin.address, 700000000n);
    await token.approve(vault.target, 700000000n);
    await vault.deposit(token.target, 700000000n);
    const before = await vault.getTokenInfo(token.target);
    const New = await ethers.getContractFactory("PECORVault");
    const next = await New.deploy();
    await vault.upgradeToAndCall(next.target, "0x");
    const upgraded = New.attach(vault.target);
    expect(await upgraded.getTokenInfo(token.target)).to.deep.equal(before);
    expect(await upgraded.getAvailableReserves(token.target)).to.equal(0);
    await expect(upgraded.connect(operator).pushTokens(token.target, recipient.address, 1)).to.be.reverted;
    // This fixture has no historical fee/order operators or liabilities.
    await upgraded.finalizeLiabilityMigration([]);
    expect(await upgraded.getAvailableReserves(token.target)).to.equal(700000000n);
    expect(await upgraded.getReservedBalance(token.target)).to.equal(0);
    expect(await upgraded.hasRole(ethers.id("OPERATOR_ROLE"), operator.address)).to.equal(true);
    await upgraded.connect(operator).pushTokens(token.target, recipient.address, 100000000n);
    expect(await token.balanceOf(recipient.address)).to.equal(100000000n);
    expect(await upgraded.getAvailableReserves(token.target)).to.equal(600000000n);
  });
  it("migrates pre-existing pending order escrow and accrued engine fees before custody can reopen", async function () {
    const [admin, user] = await ethers.getSigners();
    const { deployPriceOracle } = require("../meta-ag/helpers/fixtures");
    const oracle = await deployPriceOracle();
    await oracle.setRelayer(admin.address, true);
    const Weth = await ethers.getContractFactory("MockWETH9");
    const weth = await Weth.deploy();
    const Token = await ethers.getContractFactory("MockStandardERC20");
    const stable = await Token.deploy("USD Ledger", "USDL", 6);
    const asset = await Token.deploy("Inventory", "INV", 6);
    const ONE = 10n ** 18n;
    for (const token of [stable, asset]) {
      await oracle.registerToken(token.target, 60, 100, 1, ONE * 1000000n, 3600);
      await oracle.updatePrice(token.target, ONE);
    }
    const priorVault = await oldProxy("meta-ag/vault", "PECORVault", [weth.target, ethers.ZeroAddress, admin.address], admin);
    const vault = priorVault.instance;
    const priorEngine = await oldProxy("meta-ag/engine", "PECOR", [oracle.target, vault.target, weth.target, ethers.ZeroAddress, admin.address], admin);
    const priorOrders = await oldProxy("meta-ag/engine", "PECOROrders", [oracle.target, vault.target, ethers.ZeroAddress, admin.address], admin);
    const engine = priorEngine.instance, orders = priorOrders.instance;
    await vault.setOperator(engine.target, true);
    await vault.setOperator(orders.target, true);
    await vault.setOperator(admin.address, true);
    for (const token of [stable, asset]) {
      await vault.registerToken(token.target, token === stable);
      await token.mint(admin.address, 10000000000n);
      await token.approve(vault.target, ethers.MaxUint256);
      await vault.deposit(token.target, 10000000000n);
    }
    await stable.mint(user.address, 200000000n);
    await stable.connect(user).approve(vault.target, ethers.MaxUint256);
    const deadline = (await ethers.provider.getBlock("latest")).timestamp + 3600;
    await engine.setSwapFee(30);
    await engine.setPriceImpact(false, 0);
    await engine.connect(user).swapExactIn(stable.target, asset.target, 10000000n, 0, deadline);
    const fee = await engine.accruedFees(asset.target);
    expect(fee).to.be.gt(0);
    await orders.connect(user).placeLimitBuy(stable.target, asset.target, 100000000n, ONE, deadline);
    const pending = await orders.getLimitOrder(1);
    const userBalance = await stable.balanceOf(user.address);
    await engine.pause(); await orders.pause();
    const Vault = await ethers.getContractFactory("PECORVault"), Engine = await ethers.getContractFactory("PECOR"), Orders = await ethers.getContractFactory("PECOROrders");
    const newVault = await Vault.deploy(), newEngine = await Engine.deploy(), newOrders = await Orders.deploy();
    await vault.upgradeToAndCall(newVault.target, "0x");
    await engine.upgradeToAndCall(newEngine.target, "0x");
    await orders.upgradeToAndCall(newOrders.target, "0x");
    const v = Vault.attach(vault.target), e = Engine.attach(engine.target), o = Orders.attach(orders.target);
    expect(await o.getLimitOrder(1)).to.deep.equal(pending);
    expect(await e.accruedFees(asset.target)).to.equal(fee);
    expect(await v.getAvailableReserves(stable.target)).to.equal(0);
    await expect(o.connect(user).cancelLimitOrder(1)).to.be.reverted;
    await expect(v.finalizeLiabilityMigration([e.target, o.target])).to.be.reverted;
    await e.migrateFeeReservations(100);
    await o.migrateEscrowReservations(100);
    expect(await v.operatorReservations(e.target, asset.target)).to.equal(fee);
    expect(await v.operatorReservations(o.target, stable.target)).to.equal(100000000n);
    await e.setFeeCollector(admin.address);
    await expect(e.collectFees(asset.target)).to.be.reverted;
    expect(await e.accruedFees(asset.target)).to.equal(fee);
    expect(await v.operatorReservations(e.target, asset.target)).to.equal(fee);
    await v.finalizeLiabilityMigration([e.target, o.target]);
    const freeStable = await v.getAvailableReserves(stable.target);
    const freeAsset = await v.getAvailableReserves(asset.target);
    await expect(v.pushTokens(stable.target, admin.address, freeStable + 1n)).to.be.reverted;
    await expect(v.pushTokens(asset.target, admin.address, freeAsset + 1n)).to.be.reverted;
    expect(await v.operatorReservations(o.target, stable.target)).to.equal(100000000n);
    expect(await v.operatorReservations(e.target, asset.target)).to.equal(fee);
    await o.connect(user).cancelLimitOrder(1);
    expect(await stable.balanceOf(user.address)).to.equal(userBalance + 100000000n);
    expect(await v.getReservedBalance(stable.target)).to.equal(0);
    await e.setFeeCollector(admin.address);
    const collectedBefore = await asset.balanceOf(admin.address);
    await e.collectFees(asset.target);
    expect(await asset.balanceOf(admin.address)).to.equal(collectedBefore + fee);
    expect(await v.getReservedBalance(asset.target)).to.equal(0);
  });

});
