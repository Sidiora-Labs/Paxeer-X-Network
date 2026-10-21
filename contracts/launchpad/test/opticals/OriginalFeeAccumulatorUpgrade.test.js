const { expect } = require("chai");
const { ethers } = require("hardhat");
const original = require("./baselines/FeeAccumulator.original.json");
const { registryFixture } = require("./fixture");

describe("Pinned original production FeeAccumulator upgrade", function () {
  it("preserves real recorded liabilities and prior legacy claims through reconstruction settlement", async function () {
    const [admin, alice, bob] = await ethers.getSigners();
    async function proxy(name, args) {
      const F = await ethers.getContractFactory(name);
      const impl = await F.deploy();
      const p = await (await ethers.getContractFactory("UUPSProxy")).deploy(await impl.getAddress(), F.interface.encodeFunctionData("initialize", args));
      return F.attach(await p.getAddress());
    }
    const registry = await registryFixture(admin);
    const usdl = await (await ethers.getContractFactory("MockERC20")).deploy("USD", "USD", 6);
    const token = await (await ethers.getContractFactory("MockERC20Base")).deploy("Token", "TOK", 6);
    const config = await proxy("ProtocolConfig", [await usdl.getAddress(), ethers.ZeroAddress, admin.address]);
    const treasury = await proxy("Treasury", [ethers.ZeroAddress, admin.address]);
    const Original = new ethers.ContractFactory(original.abi, original.bytecode, admin);
    const impl = await Original.deploy();
    const p = await (await ethers.getContractFactory("UUPSProxy")).deploy(await impl.getAddress(), Original.interface.encodeFunctionData("initialize", [await config.getAddress(), await treasury.getAddress(), await registry.getAddress(), ethers.ZeroAddress, await usdl.getAddress(), admin.address]));
    const old = Original.attach(await p.getAddress());
    const pool = await (await ethers.getContractFactory("OpticalAuditHarness")).deploy(await token.getAddress(), await old.getAddress());
    const poolAddress = await pool.getAddress();
    await registry.register(poolAddress, await token.getAddress(), admin.address, ethers.ZeroAddress, 1);
    await old.grantRole(ethers.id("POOL_ROLE"), poolAddress);
    await old.grantRole(ethers.id("FEES_ROUTER_ROLE"), admin.address);
    await treasury.grantRole(ethers.id("DEPOSITOR_ROLE"), await old.getAddress());
    async function record(amount) {
      await usdl.mint(await old.getAddress(), amount);
      await pool.callTarget(await old.getAddress(), old.interface.encodeFunctionData("recordFee", [poolAddress, amount]));
    }
    await token.mint(alice.address, 600);
    await token.mint(bob.address, 400);
    await record(1000);
    const epochBudget = await old.getAccumulatedFees(poolAddress);
    await old.triggerAirdrop(poolAddress);
    await old.claimAirdrop(poolAddress, alice.address);
    const remaining = await old.getAirdropBalance(poolAddress);
    expect(remaining).to.equal(epochBudget * 400n / 1000n);
    await record(100);
    const fees = await old.getAccumulatedFees(poolAddress);
    await usdl.mint(await old.getAddress(), 100);
    await pool.callTarget(await old.getAddress(), old.interface.encodeFunctionData("recordOpticalSurplus", [poolAddress, 100]));
    expect(await old.getProtocolFeesPending()).to.be.greaterThan(0); // Original cumulative counter.
    const New = await ethers.getContractFactory("FeeAccumulator");
    const next = await New.deploy();
    await old.upgradeToAndCall(await next.getAddress(), "0x");
    const upgraded = New.attach(await old.getAddress());
    expect(await upgraded.hasClaimedAirdrop(poolAddress, alice.address, 1)).to.equal(true);
    await expect(upgraded.claim(poolAddress, admin.address)).to.be.revertedWithCustomError(upgraded, "Unauthorized");
    await upgraded.migrateAccounting(1);
    expect(await upgraded.totalLiabilities()).to.equal(remaining + fees + 100n);
    expect(await upgraded.getProtocolFeesPending()).to.equal(0);
    await upgraded.claim(poolAddress, admin.address);
    const leaf = ethers.keccak256(ethers.AbiCoder.defaultAbiCoder().encode(["address", "uint256", "address", "uint256"], [poolAddress, 1, bob.address, remaining]));
    await upgraded.allocateLegacyAirdrop(poolAddress, 1, leaf, remaining);
    await expect(upgraded.claimLegacyAirdrop(poolAddress, 1, alice.address, remaining, [])).to.be.revertedWithCustomError(upgraded, "AlreadyClaimed");
    await upgraded.claimLegacyAirdrop(poolAddress, 1, bob.address, remaining, []);
    expect(await upgraded.getAirdropBalance(poolAddress)).to.equal(0);
    expect(await upgraded.totalLiabilities()).to.equal(100);
    expect(await usdl.balanceOf(await upgraded.getAddress())).to.equal(100);
    expect(await upgraded.getOpticalSurplus(poolAddress)).to.equal(100);
    await expect(upgraded.claimLegacyAirdrop(poolAddress, 1, bob.address, remaining, [])).to.be.revertedWithCustomError(upgraded, "AlreadyClaimed");
  });
});
