const { expect } = require("chai");
const { ethers } = require("hardhat");
const { poolSigner } = require("./fixture");

describe("Optical and fee remediation", function () {
  let admin, alice, bob, registry, pool, otherPool, token, usdl, accumulator;
  const role = (name) => ethers.id(name);
  async function proxy(name, args) {
    const factory = await ethers.getContractFactory(name);
    const implementation = await factory.deploy();
    const p = await (await ethers.getContractFactory("UUPSProxy")).deploy(
      await implementation.getAddress(), factory.interface.encodeFunctionData("initialize", args));
    return factory.attach(await p.getAddress());
  }
  async function dispatch(caller, target, fn, args) {
    return caller.callTarget(await target.getAddress(), target.interface.encodeFunctionData(fn, args));
  }
  async function register(p, optical = ethers.ZeroAddress, nft = 1) {
    await registry.register(await p.getAddress(), await p.tokenAddress(), admin.address, optical, nft);
  }
  beforeEach(async function () {
    [admin, alice, bob] = await ethers.getSigners();
    registry = await proxy("PoolRegistry", [ethers.ZeroAddress, admin.address]);
    await registry.grantRole(role("FACTORY_ROLE"), admin.address);
    token = await (await ethers.getContractFactory("MockERC20Base")).deploy("Token", "TOK", 18);
    usdl = await (await ethers.getContractFactory("MockERC20")).deploy("USD", "USD", 6);
    const treasury = await proxy("Treasury", [ethers.ZeroAddress, admin.address]);
    const config = await proxy("ProtocolConfig", [await usdl.getAddress(), ethers.ZeroAddress, admin.address]);
    accumulator = await proxy("FeeAccumulator", [await config.getAddress(), await treasury.getAddress(), await registry.getAddress(), ethers.ZeroAddress, await usdl.getAddress(), admin.address]);
    await treasury.grantRole(role("DEPOSITOR_ROLE"), await accumulator.getAddress());
    await accumulator.grantRole(role("FEES_ROUTER_ROLE"), admin.address);
    pool = await (await ethers.getContractFactory("OpticalAuditHarness")).deploy(await token.getAddress(), await accumulator.getAddress());
    otherPool = await (await ethers.getContractFactory("OpticalAuditHarness")).deploy(ethers.ZeroAddress, await accumulator.getAddress());
    await accumulator.grantRole(role("POOL_ROLE"), await pool.getAddress());
  });
  it("authenticates the exact attached pool before cooldown writes", async function () {
    const optical = await (await ethers.getContractFactory("CooldownOptical")).deploy(await registry.getAddress(), admin.address, 60);
    const p = await pool.getAddress();
    await register(pool, await optical.getAddress());
    await expect(optical.beforeSwap(p, alice.address, true, 100)).to.be.revertedWithCustomError(optical, "NotPool");
    await expect(dispatch(otherPool, optical, "beforeSwap", [p, alice.address, true, 100])).to.be.revertedWithCustomError(optical, "NotPool");
    expect(await optical.lastTradeTime(p, alice.address)).to.equal(0);
    await dispatch(pool, optical, "beforeSwap", [p, alice.address, true, 100]);
    expect(await optical.lastTradeTime(p, alice.address)).to.be.greaterThan(0);
  });
  it("rejects unbacked and cross-pool fee recording", async function () {
    await register(pool);
    const p = await pool.getAddress();
    await expect(dispatch(pool, accumulator, "recordFee", [p, 100])).to.be.revertedWithCustomError(accumulator, "InsufficientSurplus");
    await usdl.mint(await accumulator.getAddress(), 100);
    await expect(dispatch(pool, accumulator, "recordFee", [await otherPool.getAddress(), 100])).to.be.revertedWithCustomError(accumulator, "Unauthorized");
    await dispatch(pool, accumulator, "recordFee", [p, 100]);
    expect(await accumulator.getProtocolFeesPending()).to.equal(0);
    expect(await accumulator.totalLiabilities()).to.equal(await accumulator.getAccumulatedFees(p));
  });
  it("preserves historical snapshot entitlement across transfers and newer epochs", async function () {
    await register(pool);
    const p = await pool.getAddress();
    await token.mint(alice.address, 50);
    await token.mint(bob.address, 50);
    await token.mint(p, 900); // Liquidity inventory must not dilute holders.
    await usdl.mint(await accumulator.getAddress(), 2000);
    await dispatch(pool, accumulator, "recordFee", [p, 1000]);
    const reward = await accumulator.getAccumulatedFees(p);
    await accumulator.triggerAirdrop(p);
    await token.connect(alice).transfer(bob.address, 50);
    await dispatch(pool, accumulator, "recordFee", [p, 1000]);
    await accumulator.triggerAirdrop(p);
    await accumulator.claimAirdropEpoch(p, alice.address, 1);
    expect(await usdl.balanceOf(alice.address)).to.equal(reward / 2n);
    await accumulator.claimAirdropEpoch(p, bob.address, 1);
    expect(await accumulator.airdropEpochRemaining(p, 1)).to.equal(0);
    await expect(accumulator.claimAirdropEpoch(p, bob.address, 1)).to.be.revertedWithCustomError(accumulator, "AlreadyClaimed");
    await accumulator.claimAirdropEpoch(p, bob.address, 2);
    expect(await accumulator.getAirdropBalance(p)).to.equal(0);
  });
  it("prevents globally authorized opticals claiming another optical's surplus", async function () {
    const attached = await (await ethers.getContractFactory("OpticalAuditHarness")).deploy(ethers.ZeroAddress, ethers.ZeroAddress);
    await register(pool, await attached.getAddress());
    const p = await pool.getAddress();
    await usdl.mint(await accumulator.getAddress(), 100);
    await dispatch(pool, accumulator, "recordOpticalSurplus", [p, 100]);
    await accumulator.grantRole(role("OPTICAL_CLAIM_ROLE"), await otherPool.getAddress());
    await expect(dispatch(otherPool, accumulator, "claimOpticalSurplus", [p, 100, bob.address])).to.be.revertedWithCustomError(accumulator, "Unauthorized");
    await dispatch(attached, accumulator, "claimOpticalSurplus", [p, 100, alice.address]);
    expect(await usdl.balanceOf(alice.address)).to.equal(100);
  });
  it("rejects duplicate pool identities and overflow-size pagination", async function () {
    await register(pool);
    await expect(registry.register(await pool.getAddress(), bob.address, admin.address, ethers.ZeroAddress, 2)).to.be.revertedWithCustomError(registry, "DuplicatePool");
    expect(await registry.getAllPools(0, ethers.MaxUint256)).to.deep.equal([await pool.getAddress()]);
    expect(await registry.getPoolCountByCreator(admin.address)).to.equal(1);
  });
  it("executes funded buyback through the real AMM and retires actual output", async function () {
    const optical = await (await ethers.getContractFactory("BuybackBurnOptical")).deploy(await registry.getAddress(), admin.address, 2000);
    const config = await accumulator.protocolConfig();
    const realPool = await proxy("SidioraPool", [await token.getAddress(), await usdl.getAddress(), await optical.getAddress(), await accumulator.getAddress(), ethers.ZeroAddress, config, admin.address, 1000000, 1000000]);
    const p = await realPool.getAddress();
    await token.mint(p, 1000000);
    await registry.register(p, await token.getAddress(), admin.address, await optical.getAddress(), 1);
    await accumulator.grantRole(role("POOL_ROLE"), p);
    await usdl.mint(await accumulator.getAddress(), 10000);
    await accumulator.connect(await poolSigner(realPool)).recordFee(p, 10000);
    const capital = await optical.accumulatedUsdl(p);
    expect(capital).to.be.greaterThan(0);
    const deadline = (await ethers.provider.getBlock("latest")).timestamp + 100;
    await expect(optical.connect(bob).executeBuybackBurn(p, 1, deadline)).to.be.revertedWithCustomError(optical, "NotOwner");
    expect(await optical.accumulatedUsdl(p)).to.equal(capital);
    expect(await accumulator.getOpticalSurplus(p)).to.equal(capital);
    await expect(optical.executeBuybackBurn(p, 1, deadline)).to.emit(optical, "BuybackExecuted").withArgs(p, capital);
    expect(await token.balanceOf("0x000000000000000000000000000000000000dEaD")).to.be.greaterThan(0);
    expect(await usdl.balanceOf(await optical.getAddress())).to.equal(0);
    // Executing the swap can earn a small new buyback share; it is separately backed.
    expect(await optical.accumulatedUsdl(p)).to.equal(await accumulator.getOpticalSurplus(p));
  });

  it("migrates populated proxy liabilities and preserves legacy airdrop custody", async function () {
    await register(pool);
    const p = await pool.getAddress();
    const legacy = await proxy("LegacyFeeAccumulatorHarness", [await registry.getAddress(), await usdl.getAddress(), admin.address]);
    await legacy.seed(p, 100, 200, 300);
    await legacy.seed(bob.address, 50, 0, 0); // Legacy bug allowed liability keys outside registry.
    await usdl.mint(await legacy.getAddress(), 650);
    const implementation = await (await ethers.getContractFactory("FeeAccumulator")).deploy();
    await legacy.upgradeToAndCall(await implementation.getAddress(), "0x");
    const migrated = (await ethers.getContractFactory("FeeAccumulator")).attach(await legacy.getAddress());
    await migrated.grantRole(role("FEES_ROUTER_ROLE"), admin.address);
    await expect(migrated.claim(p, alice.address)).to.be.revertedWithCustomError(migrated, "Unauthorized");
    await migrated.migrateAdditionalAccounting([bob.address, bob.address]);
    await migrated.migrateAccounting(1);
    expect(await migrated.totalLiabilities()).to.equal(650);
    await migrated.claim(p, alice.address);
    const leaf = ethers.keccak256(ethers.AbiCoder.defaultAbiCoder().encode(["address", "uint256", "address", "uint256"], [p, 1, bob.address, 200]));
    await migrated.allocateLegacyAirdrop(p, 1, leaf, 200);
    await migrated.claimLegacyAirdrop(p, 1, bob.address, 200, []);
    expect(await usdl.balanceOf(bob.address)).to.equal(200);
    expect(await migrated.totalLiabilities()).to.equal(350);
    expect(await migrated.legacyAirdropUnallocated(p)).to.equal(0);
    await expect(migrated.claimLegacyAirdrop(p, 1, bob.address, 200, [])).to.be.revertedWithCustomError(migrated, "AlreadyClaimed");
    await expect(migrated.migrateAccounting(1)).to.be.revertedWithCustomError(migrated, "Unauthorized");
  });

  it("mirrors authenticated source events and preserves local logs on mirror failure", async function () {
    const hub = await proxy("EventEmitter", [admin.address]);
    const r = await proxy("PoolRegistry", [await hub.getAddress(), admin.address]);
    await r.grantRole(role("FACTORY_ROLE"), admin.address);
    await hub.setPoolRegistry(await r.getAddress());
    await expect(r.register(await pool.getAddress(), await token.getAddress(), admin.address, ethers.ZeroAddress, 1))
      .to.emit(r, "PoolRegistered").and.to.emit(hub, "PoolRegistered");
    const a = await proxy("FeeAccumulator", [await accumulator.protocolConfig(), await accumulator.treasury(), await r.getAddress(), await hub.getAddress(), await usdl.getAddress(), admin.address]);
    const treasury = (await ethers.getContractFactory("Treasury")).attach(await a.treasury());
    await treasury.grantRole(role("DEPOSITOR_ROLE"), await a.getAddress());
    await a.grantRole(role("POOL_ROLE"), await pool.getAddress());
    await usdl.mint(await a.getAddress(), 1000);
    await expect(dispatch(pool, a, "recordFee", [await pool.getAddress(), 500])).to.emit(a, "FeeRecorded").and.to.emit(a, "MirrorFailed");
    await hub.setAuthorizedEmitter(await a.getAddress(), true);
    await hub.setTypedEmitter(await a.getAddress(), hub.interface.getFunction("emitFeeFlow").selector, true);
    await expect(dispatch(pool, a, "recordFee", [await pool.getAddress(), 500])).to.emit(a, "FeeRecorded").and.to.emit(hub, "FeeFlow");
  });

  it("records and claims Launchpad capital only through authenticated fee custody", async function () {
    const optical = await (await ethers.getContractFactory("LaunchpadOptical")).deploy(await registry.getAddress(), admin.address, admin.address, [], 0, 0, 1000, 1000, alice.address, await accumulator.getAddress());
    const p = await pool.getAddress();
    await register(pool, await optical.getAddress());
    await expect(optical.beforeFeeDistribution(p, 1000)).to.be.revertedWithCustomError(optical, "NotPool");
    await usdl.mint(await accumulator.getAddress(), 1000);
    await dispatch(pool, accumulator, "recordFee", [p, 1000]);
    const raised = await optical.accumulatedUsdl(p);
    expect(raised).to.equal(await accumulator.getOpticalSurplus(p));
    expect(raised).to.be.greaterThan(0);
    await expect(optical.connect(bob).claimCapital(p)).to.be.revertedWithCustomError(optical, "NotTeam");
    await optical.connect(alice).claimCapital(p);
    expect(await usdl.balanceOf(alice.address)).to.equal(raised);
    expect(await optical.accumulatedUsdl(p)).to.equal(0);
    expect(await accumulator.getOpticalSurplus(p)).to.equal(0);
  });

  it("rejects new airdrops for legacy immutable tokens while preserving ordinary fee claims", async function () {
    const oldToken = await (await ethers.getContractFactory("MockERC20")).deploy("Legacy", "OLD", 6);
    const oldPool = await (await ethers.getContractFactory("OpticalAuditHarness")).deploy(await oldToken.getAddress(), await accumulator.getAddress());
    const p = await oldPool.getAddress();
    await registry.register(p, await oldToken.getAddress(), admin.address, ethers.ZeroAddress, 2);
    await accumulator.grantRole(role("POOL_ROLE"), p);
    await usdl.mint(await accumulator.getAddress(), 1000);
    await dispatch(oldPool, accumulator, "recordFee", [p, 1000]);
    expect(await accumulator.supportsAirdrop(p)).to.equal(false);
    await expect(accumulator.triggerAirdrop(p)).to.be.revertedWithCustomError(accumulator, "UnsupportedSnapshotToken");
    const fees = await accumulator.getAccumulatedFees(p);
    await accumulator.claim(p, alice.address);
    expect(await usdl.balanceOf(alice.address)).to.equal(fees);
  });

});
