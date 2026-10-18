const { expect } = require("chai");
const { ethers } = require("hardhat");
const { registryFixture, poolSigner } = require("./fixture");
describe("MaxWalletOptical actual recipient policy", function () {
  let admin, alice, bob, registry, pool, token, optical;
  beforeEach(async function () {
    [admin, alice, bob] = await ethers.getSigners();
    registry = await registryFixture(admin);
    token = await (await ethers.getContractFactory("SidioraERC20")).deploy("Token", "TOK", 10000, admin.address);
    pool = await (await ethers.getContractFactory("MockPoolForOptical")).deploy();
    await pool.setTokenAddress(await token.getAddress());
    optical = await (await ethers.getContractFactory("MaxWalletOptical")).deploy(await registry.getAddress(), admin.address, 200);
    await pool.setOpticalAddress(await optical.getAddress());
    await registry.register(await pool.getAddress(), await token.getAddress(), admin.address, await optical.getAddress(), 1);
    await token.transfer(await pool.getAddress(), 10000);
    await token.bindPool(await pool.getAddress());
  });
  it("uses the token callback", async function () { expect(await optical.getFlags()).to.equal(16); expect(await optical.maxWalletBps()).to.equal(200); });
  it("permits output up to recipient cap", async function () {
    await token.connect(await poolSigner(pool)).transfer(alice.address, 200);
    expect(await token.balanceOf(alice.address)).to.equal(200);
  });
  it("rejects recipient output above cap regardless of swap trader", async function () {
    await expect(token.connect(await poolSigner(pool)).transfer(bob.address, 201)).to.be.revertedWithCustomError(optical, "MaxWalletExceeded");
  });
  it("exempting liquidity pool does not exempt recipients", async function () {
    await optical.setExempt(await pool.getAddress(), true);
    await expect(token.connect(await poolSigner(pool)).transfer(alice.address, 201)).to.be.revertedWithCustomError(optical, "MaxWalletExceeded");
  });
  it("owner can explicitly exempt recipient with an observable event", async function () {
    await expect(optical.setExempt(alice.address, true)).to.emit(optical, "ExemptionUpdated").withArgs(alice.address, true);
    await token.connect(await poolSigner(pool)).transfer(alice.address, 1000);
  });
  it("rejects non-owner policy changes and forged callbacks", async function () {
    await expect(optical.connect(alice).setExempt(alice.address, true)).to.be.revertedWithCustomError(optical, "NotOwner");
    await expect(optical.beforeTokenTransfer(await pool.getAddress(), await pool.getAddress(), alice.address, 1)).to.be.revertedWithCustomError(optical, "NotPool");
  });
  it("documents buy-time scope: wallet transfers may exceed cap", async function () {
    const signer = await poolSigner(pool);
    await token.connect(signer).transfer(alice.address, 200);
    await token.connect(signer).transfer(bob.address, 200);
    await token.connect(bob).transfer(alice.address, 200);
    expect(await token.balanceOf(alice.address)).to.equal(400);
  });
});
