/**
 * Sidiora Meta-AG — SidioraAdapter unit tests (Phase 5 / Task 5.2 — deferred to Session 8)
 *
 * Spec: docs/architecture/pecor-sidiora-aggregator-spec.md §7.10 (FROZEN 2026-04-24)
 * Interface: contracts/meta-ag/interfaces/IProtocolAdapter.sol
 * Contract: contracts/meta-ag/adapters/SidioraAdapter.sol
 *
 * Regressions exercised:
 *   - I1  getQuote never reverts (returns available=false on unsupported/revert)
 *   - I2  executeSwap enforces min-out (SlippageExceeded revert)
 *   - I3  executeSwap honors deadline (Expired revert) / 0 = no-deadline
 *   - I4  Adapter pulls tokenIn from `from`, sends tokenOut to recipient
 *   - I5  adapterId = keccak256("SidioraAMM.v1")
 *   - I6  adapterData round-trips from getQuote to executeSwap
 *   - S9  Zero-first approval reset on the live Sidiora Router
 *
 * Port-adaptation regression guards (vs dev/ ancestor):
 *   - swapTokenToToken → swapTokenForToken (live Sidiora IRouter selector)
 *   - swapTokenForToken returns (amountOut, intermediateUsdl) — adapter
 *     unpacks 2-tuple and DISCARDS the second value
 *   - OZ forceApprove → TransferHelper.safeApprove with zero-first reset + cleanup
 */

const { expect } = require("chai");
const { ethers } = require("hardhat");

const {
  ADAPTER_IDS,
  PECOR_ROLES,
  ZERO_ADDRESS,
} = require("../helpers/constants");
const { ERRORS } = require("../helpers/errors");
const { deploySidioraAdapter } = require("../helpers/fixtures");

const ONE = 10n ** 18n;
const MAX_UINT256 = 2n ** 256n - 1n;

const MODE_BUY = 0;
const MODE_SELL = 1;
const MODE_MULTIHOP = 2;

// --------------------------------------------------------------------------- //
// Local mock deployers                                                        //
// --------------------------------------------------------------------------- //

async function deployMockToken(name, symbol, decimals = 18) {
  const F = await ethers.getContractFactory("MockStandardERC20");
  const t = await F.deploy(name, symbol, decimals);
  await t.waitForDeployment();
  return t;
}

async function deployMockRegistry() {
  const F = await ethers.getContractFactory("MockSidioraPoolRegistry");
  const r = await F.deploy();
  await r.waitForDeployment();
  return r;
}

async function deployMockRouter() {
  const F = await ethers.getContractFactory("MockSidioraRouter");
  const r = await F.deploy();
  await r.waitForDeployment();
  return r;
}

async function deployMockQuoter() {
  const F = await ethers.getContractFactory("MockSidioraQuoter");
  const q = await F.deploy();
  await q.waitForDeployment();
  return q;
}

/** Encode adapterData as `(uint8 mode, address poolA, address poolB)` */
function encodeAdapterData(mode, poolA, poolB) {
  return ethers.AbiCoder.defaultAbiCoder().encode(
    ["uint8", "address", "address"],
    [mode, poolA, poolB]
  );
}

/** Get the latest block timestamp on the active provider. */
async function nowTs() {
  const b = await ethers.provider.getBlock("latest");
  return b.timestamp;
}

describe("meta-ag/adapters/SidioraAdapter", function () {
  let admin, user, recipient, other;
  let usdl, tokenA, tokenB;
  let registry, router, quoter;
  let adapter;
  /** Synthetic pool addresses — only used as PoolRegistry keys. */
  let poolA, poolB;

  beforeEach(async function () {
    [admin, user, recipient, other] = await ethers.getSigners();

    usdl = await deployMockToken("USDL", "USDL", 18);
    tokenA = await deployMockToken("Alpha", "A", 18);
    tokenB = await deployMockToken("Beta", "B", 18);

    // Quotes inspect executable liquidity and optical restrictions.
    const Pool = await ethers.getContractFactory("MockSidioraPool");
    const pa = await Pool.deploy();
    const pb = await Pool.deploy();
    await pa.setReserves(1000n * ONE, 5000n * ONE, 1000n * ONE);
    await pb.setReserves(1000n * ONE, 5000n * ONE, 1000n * ONE);
    poolA = pa.target;
    poolB = pb.target;

    registry = await deployMockRegistry();
    router = await deployMockRouter();
    quoter = await deployMockQuoter();

    await registry.setPoolByToken(tokenA.target, poolA);
    await registry.setPoolByToken(tokenB.target, poolB);

    await router.setUsdl(usdl.target);

    adapter = await deploySidioraAdapter({
      poolRegistry: registry.target,
      quoter: quoter.target,
      sidioraRouter: router.target,
      usdl: usdl.target,
      admin: admin.address,
    });
  });

  // ===================================================================== //
  // METADATA (I5)                                                         //
  // ===================================================================== //
  describe("metadata", function () {
    it("adapterId == keccak256('SidioraAMM.v1')", async function () {
      expect(await adapter.adapterId()).to.equal(ADAPTER_IDS.SIDIORA);
    });

    it("adapterName == 'SidioraAMM.v1'", async function () {
      expect(await adapter.adapterName()).to.equal("SidioraAMM.v1");
    });

    it("adapterVersion == '1.0.0'", async function () {
      expect(await adapter.adapterVersion()).to.equal("1.0.0");
    });
  });

  // ===================================================================== //
  // CONSTRUCTOR                                                            //
  // ===================================================================== //
  describe("constructor", function () {
    it("stores all five args and grants admin DEFAULT_ADMIN_ROLE", async function () {
      expect(await adapter.poolRegistry()).to.equal(registry.target);
      expect(await adapter.quoter()).to.equal(quoter.target);
      expect(await adapter.sidioraRouter()).to.equal(router.target);
      expect(await adapter.usdl()).to.equal(usdl.target);
      expect(await adapter.hasRole(PECOR_ROLES.DEFAULT_ADMIN_ROLE, admin.address)).to.equal(true);
    });

    it("admin != deployer: role is granted to admin, NOT deployer", async function () {
      const Factory = await ethers.getContractFactory("SidioraAdapter", other);
      const a = await Factory.deploy(
        registry.target,
        quoter.target,
        router.target,
        usdl.target,
        admin.address
      );
      await a.waitForDeployment();
      expect(await a.hasRole(PECOR_ROLES.DEFAULT_ADMIN_ROLE, admin.address)).to.equal(true);
      expect(await a.hasRole(PECOR_ROLES.DEFAULT_ADMIN_ROLE, other.address)).to.equal(false);
    });

    it("rejects zero poolRegistry", async function () {
      const F = await ethers.getContractFactory("SidioraAdapter");
      await expect(
        F.deploy(ZERO_ADDRESS, quoter.target, router.target, usdl.target, admin.address)
      ).to.be.revertedWithCustomError(F, "ZeroAddress");
    });

    it("rejects zero quoter", async function () {
      const F = await ethers.getContractFactory("SidioraAdapter");
      await expect(
        F.deploy(registry.target, ZERO_ADDRESS, router.target, usdl.target, admin.address)
      ).to.be.revertedWithCustomError(F, "ZeroAddress");
    });

    it("rejects zero sidioraRouter", async function () {
      const F = await ethers.getContractFactory("SidioraAdapter");
      await expect(
        F.deploy(registry.target, quoter.target, ZERO_ADDRESS, usdl.target, admin.address)
      ).to.be.revertedWithCustomError(F, "ZeroAddress");
    });

    it("rejects zero usdl", async function () {
      const F = await ethers.getContractFactory("SidioraAdapter");
      await expect(
        F.deploy(registry.target, quoter.target, router.target, ZERO_ADDRESS, admin.address)
      ).to.be.revertedWithCustomError(F, "ZeroAddress");
    });

    it("rejects zero admin", async function () {
      const F = await ethers.getContractFactory("SidioraAdapter");
      await expect(
        F.deploy(registry.target, quoter.target, router.target, usdl.target, ZERO_ADDRESS)
      ).to.be.revertedWithCustomError(F, "ZeroAddress");
    });
  });

  // ===================================================================== //
  // ADMIN SETTERS (DEFAULT_ADMIN_ROLE → Timelock on mainnet)              //
  // ===================================================================== //
  describe("admin setters", function () {
    it("setPoolRegistry: admin succeeds; non-admin reverts; zero reverts", async function () {
      const next = await deployMockRegistry();
      await adapter.connect(admin).setPoolRegistry(next.target);
      expect(await adapter.poolRegistry()).to.equal(next.target);

      await expect(
        adapter.connect(other).setPoolRegistry(next.target)
      ).to.be.revertedWithCustomError(adapter, ERRORS.common.Unauthorized);

      await expect(
        adapter.connect(admin).setPoolRegistry(ZERO_ADDRESS)
      ).to.be.revertedWithCustomError(adapter, "ZeroAddress");
    });

    it("setQuoter: admin succeeds; non-admin reverts; zero reverts", async function () {
      const next = await deployMockQuoter();
      await adapter.connect(admin).setQuoter(next.target);
      expect(await adapter.quoter()).to.equal(next.target);

      await expect(
        adapter.connect(other).setQuoter(next.target)
      ).to.be.revertedWithCustomError(adapter, ERRORS.common.Unauthorized);

      await expect(
        adapter.connect(admin).setQuoter(ZERO_ADDRESS)
      ).to.be.revertedWithCustomError(adapter, "ZeroAddress");
    });

    it("setSidioraRouter: admin succeeds; non-admin reverts; zero reverts", async function () {
      const next = await deployMockRouter();
      await adapter.connect(admin).setSidioraRouter(next.target);
      expect(await adapter.sidioraRouter()).to.equal(next.target);

      await expect(
        adapter.connect(other).setSidioraRouter(next.target)
      ).to.be.revertedWithCustomError(adapter, ERRORS.common.Unauthorized);

      await expect(
        adapter.connect(admin).setSidioraRouter(ZERO_ADDRESS)
      ).to.be.revertedWithCustomError(adapter, "ZeroAddress");
    });

    it("setUsdl: admin succeeds; non-admin reverts; zero reverts", async function () {
      const next = await deployMockToken("USDL-v2", "USDL2", 18);
      await adapter.connect(admin).setUsdl(next.target);
      expect(await adapter.usdl()).to.equal(next.target);

      await expect(
        adapter.connect(other).setUsdl(next.target)
      ).to.be.revertedWithCustomError(adapter, ERRORS.common.Unauthorized);

      await expect(
        adapter.connect(admin).setUsdl(ZERO_ADDRESS)
      ).to.be.revertedWithCustomError(adapter, "ZeroAddress");
    });
  });

  // ===================================================================== //
  // supportsSwap                                                          //
  // ===================================================================== //
  describe("supportsSwap", function () {
    it("false when tokenIn == tokenOut", async function () {
      expect(await adapter.supportsSwap(tokenA.target, tokenA.target)).to.equal(false);
      expect(await adapter.supportsSwap(usdl.target, usdl.target)).to.equal(false);
    });

    it("true for USDL → token with registered pool", async function () {
      expect(await adapter.supportsSwap(usdl.target, tokenA.target)).to.equal(true);
    });

    it("false for USDL → token without pool", async function () {
      const unreg = await deployMockToken("No", "N", 18);
      expect(await adapter.supportsSwap(usdl.target, unreg.target)).to.equal(false);
    });

    it("true for token → USDL with registered pool", async function () {
      expect(await adapter.supportsSwap(tokenA.target, usdl.target)).to.equal(true);
    });

    it("false for token → USDL without pool", async function () {
      const unreg = await deployMockToken("No", "N", 18);
      expect(await adapter.supportsSwap(unreg.target, usdl.target)).to.equal(false);
    });

    it("true for token → token when both have pools (multihop)", async function () {
      expect(await adapter.supportsSwap(tokenA.target, tokenB.target)).to.equal(true);
      expect(await adapter.supportsSwap(tokenB.target, tokenA.target)).to.equal(true);
    });

    it("false for token → token when one side is missing a pool", async function () {
      const unreg = await deployMockToken("No", "N", 18);
      expect(await adapter.supportsSwap(tokenA.target, unreg.target)).to.equal(false);
      expect(await adapter.supportsSwap(unreg.target, tokenA.target)).to.equal(false);
    });
  });

  // ===================================================================== //
  // getSupportedPairs + getMaxInput                                       //
  // ===================================================================== //
  describe("authorization and execution availability", function () {
    it("does not let strangers spend user approvals", async function () {
      await usdl.mint(user.address, ONE);
      await usdl.connect(user).approve(adapter.target, ONE);
      await expect(adapter.connect(other).executeSwap(usdl.target, tokenA.target, ONE, 0, user.address, other.address, 0, encodeAdapterData(0, poolA, ZERO_ADDRESS)))
        .to.be.revertedWithCustomError(adapter, "UnauthorizedPayer");
      expect(await usdl.balanceOf(user.address)).to.equal(ONE);
    });
    it("filters pools with trader-dependent opticals", async function () {
      const pool = await ethers.getContractAt("MockSidioraPool", poolA);
      await pool.setOpticalAddress(other.address);
      expect((await adapter.getQuote(usdl.target, tokenA.target, ONE)).available).to.equal(false);
    });
    it("returns zero sell capacity when the real USDL reserve is empty", async function () {
      const pool = await ethers.getContractAt("MockSidioraPool", poolA);
      await pool.setReserves(1000n * ONE, 0, 1000n * ONE);
      expect(await adapter.getMaxInput(tokenA.target, usdl.target)).to.equal(0n);
    });
  });

  describe("getSupportedPairs / getMaxInput", function () {
    it("getSupportedPairs returns empty arrays (dynamic discovery)", async function () {
      const [ins, outs] = await adapter.getSupportedPairs();
      expect(ins.length).to.equal(0);
      expect(outs.length).to.equal(0);
    });

    it("getMaxInput returns 0 for unsupported pair", async function () {
      const unreg = await deployMockToken("No", "N", 18);
      expect(await adapter.getMaxInput(usdl.target, unreg.target)).to.equal(0n);
      expect(await adapter.getMaxInput(tokenA.target, tokenA.target)).to.equal(0n);
    });

    it("getMaxInput uses a conservative real-reserve sell bound and unbounded buys", async function () {
      // Seed poolA (a synthetic address) with tokenA balance.
      await tokenA.mint(poolA, 5000n * ONE);
      expect(await adapter.getMaxInput(tokenA.target, usdl.target)).to.equal(5000n * ONE);

      // USDL → tokenA uses the tokenOut's pool for balance lookup.
      await usdl.mint(poolA, 3_000n * ONE);
      expect(await adapter.getMaxInput(usdl.target, tokenA.target)).to.equal(ethers.MaxUint256);
    });
  });

  // ===================================================================== //
  // getQuote — I1 never-reverts                                           //
  // ===================================================================== //
  describe("getQuote — I1 never-reverts", function () {
    it("tokenIn == tokenOut returns available=false", async function () {
      const r = await adapter.getQuote(tokenA.target, tokenA.target, 100n);
      expect(r.available).to.equal(false);
      expect(r.amountOut).to.equal(0n);
    });

    it("amountIn == 0 returns available=false", async function () {
      const r = await adapter.getQuote(usdl.target, tokenA.target, 0n);
      expect(r.available).to.equal(false);
    });

    it("unsupported pair (no pool) returns available=false", async function () {
      const unreg = await deployMockToken("No", "N", 18);
      const r = await adapter.getQuote(usdl.target, unreg.target, 100n);
      expect(r.available).to.equal(false);
    });

    it("quoter reverts on buy → available=false (adapter catches)", async function () {
      await quoter.setRevertOnBuyQuote(true);
      const r = await adapter.getQuote(usdl.target, tokenA.target, 100n);
      expect(r.available).to.equal(false);
      expect(r.amountOut).to.equal(0n);
    });

    it("quoter reverts on sell → available=false", async function () {
      await quoter.setRevertOnSellQuote(true);
      const r = await adapter.getQuote(tokenA.target, usdl.target, 100n);
      expect(r.available).to.equal(false);
    });

    it("quoter reverts on multihop → available=false", async function () {
      await quoter.setRevertOnMultihopQuote(true);
      const r = await adapter.getQuote(tokenA.target, tokenB.target, 100n);
      expect(r.available).to.equal(false);
    });
  });

  // ===================================================================== //
  // getQuote — correctness (3 modes)                                      //
  // ===================================================================== //
  describe("getQuote — correctness", function () {
    it("BUY: returns buy quote + _MODE_BUY adapterData(pool, 0)", async function () {
      await quoter.setBuyQuote(500n * ONE, 5n * ONE, 50n); // amtOut=500, fee=5, impact=50 bps
      const r = await adapter.getQuote(usdl.target, tokenA.target, 100n * ONE);

      expect(r.available).to.equal(true);
      expect(r.amountOut).to.equal(500n * ONE);
      expect(r.priceImpactBps).to.equal(50n);
      expect(r.feeAmount).to.equal(5n * ONE);
      expect(r.feeBps).to.equal(0n); // Sidiora fees not surfaced as bps

      const decoded = ethers.AbiCoder.defaultAbiCoder().decode(
        ["uint8", "address", "address"],
        r.adapterData
      );
      expect(decoded[0]).to.equal(BigInt(MODE_BUY));
      expect(decoded[1]).to.equal(poolA);
      expect(decoded[2]).to.equal(ZERO_ADDRESS);
    });

    it("SELL: returns sell quote + _MODE_SELL adapterData(pool, 0)", async function () {
      await quoter.setSellQuote(300n * ONE, 3n * ONE, 30n);
      const r = await adapter.getQuote(tokenA.target, usdl.target, 100n * ONE);

      expect(r.available).to.equal(true);
      expect(r.amountOut).to.equal(300n * ONE);
      expect(r.priceImpactBps).to.equal(30n);
      expect(r.feeAmount).to.equal(3n * ONE);

      const decoded = ethers.AbiCoder.defaultAbiCoder().decode(
        ["uint8", "address", "address"],
        r.adapterData
      );
      expect(decoded[0]).to.equal(BigInt(MODE_SELL));
      expect(decoded[1]).to.equal(poolA);
      expect(decoded[2]).to.equal(ZERO_ADDRESS);
    });

    it("MULTIHOP: combines sell+buy fees and encodes (_MODE_MULTIHOP, poolA, poolB)", async function () {
      // amountOut=700, intermediate=350, sellFee=4, buyFee=6, combinedImpact=100
      await quoter.setMultihopQuote(700n * ONE, 350n * ONE, 4n * ONE, 6n * ONE, 100n, poolA, poolB);
      const r = await adapter.getQuote(tokenA.target, tokenB.target, 100n * ONE);

      expect(r.available).to.equal(true);
      expect(r.amountOut).to.equal(700n * ONE);
      expect(r.priceImpactBps).to.equal(100n);
      expect(r.feeAmount).to.equal(6n * ONE + (4n * ONE * 350n * ONE) / (96n * ONE)); // both legs expressed in USDL

      const decoded = ethers.AbiCoder.defaultAbiCoder().decode(
        ["uint8", "address", "address"],
        r.adapterData
      );
      expect(decoded[0]).to.equal(BigInt(MODE_MULTIHOP));
      expect(decoded[1]).to.equal(poolA);
      expect(decoded[2]).to.equal(poolB);
    });
  });

  // ===================================================================== //
  // executeSwap — invariants (I2 / I3)                                    //
  // ===================================================================== //
  describe("executeSwap — invariants", function () {
    beforeEach(async function () {
      // Router pre-funded for BUY scenarios (USDL → tokenA).
      await router.setBuyTokenOut(tokenA.target);
      await tokenA.mint(router.target, 1_000_000n * ONE);

      // User gets USDL and approves adapter for unlimited.
      await usdl.mint(user.address, 1_000_000n * ONE);
      await usdl.connect(user).approve(adapter.target, MAX_UINT256);
    });

    it("reverts Expired when block.timestamp > deadline", async function () {
      const past = (await nowTs()) - 100;
      const data = encodeAdapterData(MODE_BUY, poolA, ZERO_ADDRESS);
      await expect(
        adapter.connect(user).executeSwap(
          usdl.target, tokenA.target,
          100n * ONE, 1n,
          user.address, recipient.address,
          past,
          data
        )
      ).to.be.revertedWithCustomError(adapter, "Expired");
    });

    it("accepts deadline == 0 as no-deadline (passes type(uint256).max to router)", async function () {
      await router.setBuyReturn(50n * ONE);
      const data = encodeAdapterData(MODE_BUY, poolA, ZERO_ADDRESS);
      await adapter.connect(user).executeSwap(
        usdl.target, tokenA.target,
        10n * ONE, 1n,
        user.address, recipient.address,
        0n,
        data
      );
      const lastBuy = await router.lastBuy();
      expect(lastBuy.deadline).to.equal(MAX_UINT256);
    });

    it("reverts ZeroAmount when amountIn == 0", async function () {
      const data = encodeAdapterData(MODE_BUY, poolA, ZERO_ADDRESS);
      await expect(
        adapter.connect(user).executeSwap(
          usdl.target, tokenA.target,
          0n, 0n,
          user.address, recipient.address,
          0n,
          data
        )
      ).to.be.revertedWithCustomError(adapter, "ZeroAmount");
    });

    it("reverts PairNotSupported for unregistered pair", async function () {
      const unreg = await deployMockToken("No", "N", 18);
      const data = encodeAdapterData(MODE_BUY, poolA, ZERO_ADDRESS);
      await expect(
        adapter.connect(user).executeSwap(
          usdl.target, unreg.target,
          100n * ONE, 1n,
          user.address, recipient.address,
          0n,
          data
        )
      ).to.be.revertedWithCustomError(adapter, "PairNotSupported");
    });

    it("reverts SlippageExceeded when router.amountOut < minAmountOut", async function () {
      await router.setBuyReturn(10n); // tiny
      const data = encodeAdapterData(MODE_BUY, poolA, ZERO_ADDRESS);
      await expect(
        adapter.connect(user).executeSwap(
          usdl.target, tokenA.target,
          100n * ONE, 1000n * ONE, // minOut >> what router returns
          user.address, recipient.address,
          0n,
          data
        )
      ).to.be.revertedWithCustomError(adapter, "SlippageExceeded");
    });
  });

  // ===================================================================== //
  // executeSwap — mode flows (BUY / SELL / MULTIHOP)                      //
  // ===================================================================== //
  describe("executeSwap — mode flows", function () {
    beforeEach(async function () {
      // Pre-fund router with all tokens so any flow can pay out.
      await usdl.mint(router.target, 10_000_000n * ONE);
      await tokenA.mint(router.target, 10_000_000n * ONE);
      await tokenB.mint(router.target, 10_000_000n * ONE);

      // Fund user balances.
      await usdl.mint(user.address, 10_000_000n * ONE);
      await tokenA.mint(user.address, 10_000_000n * ONE);
      await tokenB.mint(user.address, 10_000_000n * ONE);

      // User approvals to adapter (I4: adapter pulls from user).
      await usdl.connect(user).approve(adapter.target, MAX_UINT256);
      await tokenA.connect(user).approve(adapter.target, MAX_UINT256);
      await tokenB.connect(user).approve(adapter.target, MAX_UINT256);
    });

    it("BUY: calls router.buy(pool,amountIn,minOut,maxDeadline); recipient gets tokenA", async function () {
      await router.setBuyTokenOut(tokenA.target);
      await router.setBuyReturn(750n * ONE);

      const data = encodeAdapterData(MODE_BUY, poolA, ZERO_ADDRESS);
      const balBefore = await tokenA.balanceOf(recipient.address);

      await adapter.connect(user).executeSwap(
        usdl.target, tokenA.target,
        100n * ONE, 700n * ONE,
        user.address, recipient.address,
        0n,
        data
      );

      const lastBuy = await router.lastBuy();
      expect(lastBuy.pool).to.equal(poolA);
      expect(lastBuy.usdlAmountIn).to.equal(100n * ONE);
      expect(lastBuy.minTokensOut).to.equal(700n * ONE);
      expect(lastBuy.deadline).to.equal(MAX_UINT256);
      // S9: adapter had the router approved for exactly amountIn at buy() entry.
      expect(lastBuy.observedAllowance).to.equal(100n * ONE);
      expect(lastBuy.caller).to.equal(adapter.target);

      expect(await tokenA.balanceOf(recipient.address)).to.equal(balBefore + 750n * ONE);
      expect(await router.buyCallCount()).to.equal(1n);
    });

    it("SELL: calls router.sell(pool,amountIn,minOut,maxDeadline); recipient gets USDL", async function () {
      await router.setSellTokenIn(tokenA.target);
      await router.setSellReturn(90n * ONE);

      const data = encodeAdapterData(MODE_SELL, poolA, ZERO_ADDRESS);
      const balBefore = await usdl.balanceOf(recipient.address);

      await adapter.connect(user).executeSwap(
        tokenA.target, usdl.target,
        100n * ONE, 80n * ONE,
        user.address, recipient.address,
        0n,
        data
      );

      const lastSell = await router.lastSell();
      expect(lastSell.pool).to.equal(poolA);
      expect(lastSell.tokenAmountIn).to.equal(100n * ONE);
      expect(lastSell.minUsdlOut).to.equal(80n * ONE);
      expect(lastSell.deadline).to.equal(MAX_UINT256);
      expect(lastSell.observedAllowance).to.equal(100n * ONE); // S9 witness
      expect(lastSell.caller).to.equal(adapter.target);

      expect(await usdl.balanceOf(recipient.address)).to.equal(balBefore + 90n * ONE);
      expect(await router.sellCallCount()).to.equal(1n);
    });

    it("MULTIHOP: unpacks (amountOut, intermediateUsdl) 2-tuple; delivers first element to recipient [PORT REGRESSION]", async function () {
      // PORT-ADAPTATION REGRESSION GUARD:
      //   - Live IRouter.swapTokenForToken returns (amountOut, intermediateUsdl) — 2-tuple.
      //   - dev/ ancestor assumed 1-tuple (amountOut).
      //   - SidioraAdapter must unpack correctly and ONLY forward the first element.
      //   - If adapter accidentally forwarded intermediateUsdl (50) instead of
      //     amountOut (420), this test would fail — which is exactly the signal we want.
      await router.setMultihopReturn(420n * ONE, 50n * ONE);

      const data = encodeAdapterData(MODE_MULTIHOP, poolA, poolB);
      const balBefore = await tokenB.balanceOf(recipient.address);

      await adapter.connect(user).executeSwap(
        tokenA.target, tokenB.target,
        100n * ONE, 400n * ONE,
        user.address, recipient.address,
        0n,
        data
      );

      const lastMh = await router.lastMultihop();
      expect(lastMh.tokenIn).to.equal(tokenA.target);
      expect(lastMh.tokenOut).to.equal(tokenB.target);
      expect(lastMh.amountIn).to.equal(100n * ONE);
      expect(lastMh.minAmountOut).to.equal(400n * ONE);
      expect(lastMh.deadline).to.equal(MAX_UINT256);
      expect(lastMh.observedAllowance).to.equal(100n * ONE); // S9
      expect(lastMh.caller).to.equal(adapter.target);

      // First element of 2-tuple (420) is the adapter's amountOut. Second (50)
      // is discarded per §7.10.
      expect(await tokenB.balanceOf(recipient.address)).to.equal(balBefore + 420n * ONE);
      expect(await router.multihopCallCount()).to.equal(1n);
    });

    it("honors explicit future deadline (passes it through to router)", async function () {
      await router.setBuyTokenOut(tokenA.target);
      await router.setBuyReturn(10n * ONE);
      const future = (await nowTs()) + 1000;

      const data = encodeAdapterData(MODE_BUY, poolA, ZERO_ADDRESS);
      await adapter.connect(user).executeSwap(
        usdl.target, tokenA.target,
        1n * ONE, 1n,
        user.address, recipient.address,
        future,
        data
      );

      const lastBuy = await router.lastBuy();
      expect(lastBuy.deadline).to.equal(BigInt(future));
    });

    it("emits SwapExecuted(adapterId, tokenIn, tokenOut, amountIn, amountOut, recipient)", async function () {
      await router.setBuyTokenOut(tokenA.target);
      await router.setBuyReturn(500n * ONE);

      const data = encodeAdapterData(MODE_BUY, poolA, ZERO_ADDRESS);

      await expect(
        adapter.connect(user).executeSwap(
          usdl.target, tokenA.target,
          100n * ONE, 1n,
          user.address, recipient.address,
          0n,
          data
        )
      )
        .to.emit(adapter, "SwapExecuted")
        .withArgs(
          ADAPTER_IDS.SIDIORA,
          usdl.target,
          tokenA.target,
          100n * ONE,
          500n * ONE,
          recipient.address
        );
    });
  });

  // ===================================================================== //
  // S9 approval dance (zero-first reset + cleanup)                        //
  // ===================================================================== //
  describe("executeSwap — S9 approval dance", function () {
    beforeEach(async function () {
      await router.setBuyTokenOut(tokenA.target);
      await router.setBuyReturn(100n * ONE);
      await tokenA.mint(router.target, 1_000_000n * ONE);
      await usdl.mint(user.address, 1_000_000n * ONE);
      await usdl.connect(user).approve(adapter.target, MAX_UINT256);
    });

    it("emits three Approval events on tokenIn: [0, amountIn, 0]", async function () {
      const data = encodeAdapterData(MODE_BUY, poolA, ZERO_ADDRESS);
      const amountIn = 50n * ONE;

      const tx = await adapter.connect(user).executeSwap(
        usdl.target, tokenA.target,
        amountIn, 1n,
        user.address, recipient.address,
        0n,
        data
      );
      const rcpt = await tx.wait();

      const iface = new ethers.Interface([
        "event Approval(address indexed owner, address indexed spender, uint256 value)",
      ]);
      const approvals = rcpt.logs
        .filter((l) => l.address === usdl.target)
        .map((l) => {
          try {
            return iface.parseLog(l);
          } catch {
            return null;
          }
        })
        .filter(
          (p) =>
            p &&
            p.name === "Approval" &&
            p.args.owner === adapter.target &&
            p.args.spender === router.target
        );

      expect(approvals.length).to.equal(3);
      expect(approvals[0].args.value).to.equal(0n); // zero-first reset
      expect(approvals[1].args.value).to.equal(amountIn); // set exact amount
      expect(approvals[2].args.value).to.equal(0n); // cleanup
    });

    it("leaves zero allowance between adapter and router after swap", async function () {
      const data = encodeAdapterData(MODE_BUY, poolA, ZERO_ADDRESS);
      await adapter.connect(user).executeSwap(
        usdl.target, tokenA.target,
        10n * ONE, 1n,
        user.address, recipient.address,
        0n,
        data
      );

      expect(await usdl.allowance(adapter.target, router.target)).to.equal(0n);
    });
  });
});
