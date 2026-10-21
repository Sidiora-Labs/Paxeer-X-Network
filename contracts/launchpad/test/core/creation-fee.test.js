const { expect } = require("chai");
const { ethers } = require("hardhat");
const { deployRealSidioraFixture } = require("../helpers/realSidioraFixture");

// A literal fee anchors these regressions independently of shared fee constants.
const ONE_USDL = 1_000_000n;
const ENTRYPOINTS = [
  { label: "Factory.createMarket", spender: "factory", permitFallback: false },
  { label: "Router.createMarket", spender: "router", permitFallback: false },
  {
    label: "Router.createMarketWithPermit (existing-allowance fallback; USDL fixture has no permit)",
    spender: "router",
    permitFallback: true,
  },
];

describe("Sidiora creation fee: exactly one six-decimal USDL", function () {
  let fx;

  beforeEach(async function () {
    fx = await deployRealSidioraFixture();
    // Remove fixture-wide approvals: each case proves its own exact allowance.
    await fx.usdl.connect(fx.alice).approve(fx.router.target, 0);
    await fx.usdl.connect(fx.alice).approve(fx.factory.target, 0);
  });

  async function snapshot(spender) {
    return {
      payerBalance: await fx.usdl.balanceOf(fx.alice.address),
      treasuryBalance: await fx.usdl.balanceOf(fx.treasury.target),
      treasuryLedger: await fx.treasury.getBalance(fx.usdl.target),
      routerBalance: await fx.usdl.balanceOf(fx.router.target),
      factoryBalance: await fx.usdl.balanceOf(fx.factory.target),
      creatorNonce: await fx.factory.getNonce(fx.alice.address),
      nextNftId: await fx.nft.nextTokenId(),
      nftSupply: await fx.nft.totalSupply(),
      poolCount: await fx.registry.getPoolCount(),
      allowance: await fx.usdl.allowance(fx.alice.address, spender),
    };
  }

  async function create(entrypoint) {
    const name = "One USDL Market";
    const symbol = "ONE";
    if (!entrypoint.permitFallback) {
      return fx[entrypoint.spender].connect(fx.alice).createMarket(name, symbol, 0, ethers.ZeroAddress);
    }
    // MockERC20 deliberately exposes no EIP-2612 permit. The router tolerates
    // this failure and must still consume only the explicit existing approval.
    const deadline = (await ethers.provider.getBlock("latest")).timestamp + 3600;
    return fx.router.connect(fx.alice).createMarketWithPermit(name, symbol, 0, ethers.ZeroAddress, {
      value: ONE_USDL,
      deadline,
      v: 27,
      r: ethers.ZeroHash,
      s: ethers.ZeroHash,
    });
  }

  it("initializes the fee to 1,000,000 raw units for six-decimal USDL", async function () {
    expect(await fx.usdl.decimals()).to.equal(6);
    expect(await fx.config.creationFee()).to.equal(ONE_USDL);
    expect(await fx.treasury.getBalance(fx.usdl.target)).to.equal(0);
  });

  for (const entrypoint of ENTRYPOINTS) {
    describe(entrypoint.label, function () {
      it("accepts exactly one USDL allowance and charges/credits exactly once", async function () {
        const spender = fx[entrypoint.spender].target;
        await fx.usdl.connect(fx.alice).approve(spender, ONE_USDL);
        const before = await snapshot(spender);
        const receipt = await (await create(entrypoint)).wait();
        const after = await snapshot(spender);

        expect(before.payerBalance - after.payerBalance).to.equal(ONE_USDL);
        expect(after.treasuryBalance - before.treasuryBalance).to.equal(ONE_USDL);
        expect(after.treasuryLedger - before.treasuryLedger).to.equal(ONE_USDL);
        expect(after.allowance).to.equal(0);
        expect(after.routerBalance).to.equal(before.routerBalance);
        expect(after.factoryBalance).to.equal(before.factoryBalance);
        expect(after.creatorNonce).to.equal(before.creatorNonce + 1n);
        expect(after.nextNftId).to.equal(before.nextNftId + 1n);
        expect(after.nftSupply).to.equal(before.nftSupply + 1n);
        expect(after.poolCount).to.equal(before.poolCount + 1n);
        expect(await fx.nft.ownerOf(before.nextNftId)).to.equal(fx.alice.address);

        const transfers = receipt.logs
          .filter(log => log.address.toLowerCase() === fx.usdl.target.toLowerCase())
          .map(log => fx.usdl.interface.parseLog(log))
          .filter(log => log.name === "Transfer");
        expect(transfers).to.have.length(1);
        expect(transfers[0].args.from).to.equal(fx.alice.address);
        expect(transfers[0].args.to).to.equal(fx.treasury.target);
        expect(transfers[0].args.value).to.equal(ONE_USDL);
      });

      it("rejects an allowance one raw unit short without changing balances, ledger or market state", async function () {
        const spender = fx[entrypoint.spender].target;
        await fx.usdl.connect(fx.alice).approve(spender, ONE_USDL - 1n);
        const before = await snapshot(spender);
        await expect(create(entrypoint)).to.be.revertedWithCustomError(fx[entrypoint.spender], "TransferFromFailed");
        expect(await snapshot(spender)).to.deep.equal(before);
      });
    });
  }

  it("does not expose the trusted-router createMarketFor fee bypass to ordinary creators or the admin", async function () {
    const role = await fx.factory.ROUTER_ROLE();
    expect(await fx.factory.hasRole(role, fx.router.target)).to.equal(true);
    for (const caller of [fx.alice, fx.deployer]) {
      expect(await fx.factory.hasRole(role, caller.address)).to.equal(false);
      await fx.usdl.connect(fx.alice).approve(fx.factory.target, ONE_USDL);
      const before = await snapshot(fx.factory.target);
      await expect(fx.factory.connect(caller).createMarketFor(
        fx.alice.address, "Unauthorized Free Market", "FREE", 0, ethers.ZeroAddress
      )).to.be.revertedWithCustomError(fx.factory, "MissingRole");
      expect(await snapshot(fx.factory.target)).to.deep.equal(before);
    }
  });
});
