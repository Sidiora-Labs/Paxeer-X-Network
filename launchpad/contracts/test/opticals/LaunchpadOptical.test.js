const { expect } = require("chai");
const { ethers } = require("hardhat");
const { registryFixture, poolSigner } = require("./fixture");

describe("LaunchpadOptical transfer-wide vesting", function () {
  let admin, creator, team, buyer, registry, pool, token, optical, created;
  async function advance(seconds) { await ethers.provider.send("evm_increaseTime", [seconds]); await ethers.provider.send("evm_mine", []); }
  beforeEach(async function () {
    [admin, creator, team, buyer] = await ethers.getSigners();
    registry = await registryFixture(admin);
    token = await (await ethers.getContractFactory("SidioraERC20")).deploy("Token", "TOK", 10000, creator.address);
    pool = await (await ethers.getContractFactory("MockPoolForOptical")).deploy();
    await pool.setTokenAddress(await token.getAddress());
    await pool.setFeeAccumulator(admin.address);
    await token.connect(creator).transfer(await pool.getAddress(), 8000);
    await token.connect(creator).transfer(team.address, 1000);
    optical = await (await ethers.getContractFactory("LaunchpadOptical")).deploy(await registry.getAddress(), admin.address, creator.address, [team.address], 100, 1000, 500, 2000, creator.address, admin.address);
    await pool.setOpticalAddress(await optical.getAddress());
    await registry.register(await pool.getAddress(), await token.getAddress(), creator.address, await optical.getAddress(), 1);
    created = Number((await registry.getPoolMetadata(await pool.getAddress())).createdAt);
    await token.bindPool(await pool.getAddress());
  });
  it("advertises transfer policy and uses the authoritative creation timestamp", async function () {
    expect(await optical.getFlags()).to.equal(23);
    const info = await optical.getVestingInfo(await pool.getAddress(), creator.address);
    expect(info.cliffEnd).to.equal(created + 100);
    expect(info.vestEnd).to.equal(created + 1100);
  });
  it("blocks direct transfers during cliff", async function () {
    await expect(token.connect(creator).transfer(buyer.address, 1)).to.be.revertedWithCustomError(optical, "CliffActive");
    expect(await optical.tokensSold(await pool.getAddress(), creator.address)).to.equal(0);
  });
  it("blocks delegated transfers during cliff", async function () {
    await token.connect(team).approve(buyer.address, 1000);
    await expect(token.connect(buyer).transferFrom(team.address, buyer.address, 1)).to.be.revertedWithCustomError(optical, "CliffActive");
  });
  it("blocks team sells during cliff at token debit, before swap", async function () {
    await expect(token.connect(team).transfer(await pool.getAddress(), 1)).to.be.revertedWithCustomError(optical, "CliffActive");
  });
  it("releases only the vested fraction and counts each transfer once", async function () {
    await ethers.provider.send("evm_setNextBlockTimestamp", [created + 600]);
    await token.connect(creator).transfer(buyer.address, 500);
    expect(await optical.tokensSold(await pool.getAddress(), creator.address)).to.equal(500);
    await expect(token.connect(creator).transfer(buyer.address, 10)).to.be.revertedWithCustomError(optical, "VestingExceeded");
    const attached = optical.connect(await poolSigner(pool));
    await attached.afterSwap(await pool.getAddress(), creator.address, false, 500, 10);
    expect(await optical.tokensSold(await pool.getAddress(), creator.address)).to.equal(500);
  });
  it("permits all remaining tokens after full vesting", async function () {
    await advance(1100);
    await token.connect(team).transfer(buyer.address, 1000);
    expect(await token.balanceOf(buyer.address)).to.equal(1000);
  });
  it("does not lock unrelated holders or pool outputs", async function () {
    const signer = await poolSigner(pool);
    await token.connect(signer).transfer(buyer.address, 100);
    await token.connect(buyer).transfer(admin.address, 100);
    expect(await token.balanceOf(admin.address)).to.equal(100);
  });
  it("does not consume vesting on self-transfers", async function () {
    await token.connect(team).transfer(team.address, 1000);
    expect(await optical.tokensSold(await pool.getAddress(), team.address)).to.equal(0);
  });
  it("rejects spoofed swap and token callbacks", async function () {
    const p = await pool.getAddress();
    await expect(optical.beforeSwap(p, creator.address, false, 1)).to.be.revertedWithCustomError(optical, "NotPool");
    await expect(optical.beforeTokenTransfer(p, creator.address, buyer.address, 1)).to.be.revertedWithCustomError(optical, "NotPool");
    await expect(optical.afterSwap(p, creator.address, false, 1, 1)).to.be.revertedWithCustomError(optical, "NotPool");
  });
  it("restricts fee accounting to configured accumulator and expires from creation", async function () {
    const p = await pool.getAddress();
    await expect(optical.connect(buyer).beforeFeeDistribution(p, 1000)).to.be.revertedWithCustomError(optical, "NotPool");
    await optical.beforeFeeDistribution(p, 1000);
    expect(await optical.accumulatedUsdl(p)).to.equal(50);
    await advance(2000);
    expect(await optical.beforeFeeDistribution.staticCall(p, 1000)).to.equal(1000);
    expect(await optical.accumulatedUsdl(p)).to.equal(50);
  });
  it("rejects invalid constructor configurations", async function () {
    const F = await ethers.getContractFactory("LaunchpadOptical");
    await expect(F.deploy(await registry.getAddress(), admin.address, creator.address, [], 100, 1000, 1001, 1000, creator.address, admin.address)).to.be.revertedWithCustomError(optical, "CapitalRaiseTooHigh");
    await expect(F.deploy(await registry.getAddress(), admin.address, creator.address, [], 366 * 86400, 1000, 100, 1000, creator.address, admin.address)).to.be.revertedWithCustomError(optical, "DurationTooLong");
  });
});
