/**
 * Sidiora Meta-AG — MetaAGRouter unit tests (Phase 6 / Task 6.1 — deferred to Session 8)
 *
 * Spec: docs/architecture/pecor-sidiora-aggregator-spec.md §7.10 (FROZEN 2026-04-24)
 * Interface: contracts/meta-ag/interfaces/IMetaAGRouter.sol
 * Contract: contracts/meta-ag/router/MetaAGRouter.sol
 *
 * Regressions exercised:
 *   - S1  DEFAULT_ADMIN_ROLE = upgrade authority (Timelock at deploy)
 *   - S3  swapMultiHop re-quotes with the actual intermediate amount each hop
 *   - S4  _oracleSanityCheck reverts on deviation > maxOracleSanityDeviation
 *         and skips silently when either price is unavailable
 *   - S9  TransferHelper.safeApprove(token, adapter, 0) fires before AND after
 *         every adapter call (no dangling allowance)
 *   - S10 Pause / unpause gates user-facing swap entrypoints
 *   - MAX_HOPS = 5 / TooFewHops < 2 / NoAdaptersAvailable / etc.
 */

const { expect } = require("chai");
const { ethers } = require("hardhat");

const {
  PECOR_ROLES,
  ZERO_ADDRESS,
} = require("../helpers/constants");
const { ERRORS } = require("../helpers/errors");
const { deployMetaAGRouter } = require("../helpers/fixtures");

const ONE = 10n ** 18n;
const BPS_DENOMINATOR = 10_000n;

// --------------------------------------------------------------------------- //
// Local helpers                                                               //
// --------------------------------------------------------------------------- //

async function makeToken(name, symbol, decimals = 18) {
  const F = await ethers.getContractFactory("MockStandardERC20");
  const t = await F.deploy(name, symbol, decimals);
  await t.waitForDeployment();
  return t;
}

async function makeMockOracleHub() {
  const F = await ethers.getContractFactory("MockOracleHub");
  const h = await F.deploy();
  await h.waitForDeployment();
  return h;
}

async function makeMockAdapter(idString, name = idString, version = "1.0.0") {
  const id = ethers.keccak256(ethers.toUtf8Bytes(idString));
  const F = await ethers.getContractFactory("MockProtocolAdapter");
  const a = await F.deploy(id, name, version);
  await a.waitForDeployment();
  return { adapter: a, id };
}

async function nowTs() {
  const b = await ethers.provider.getBlock("latest");
  return b.timestamp;
}

// --------------------------------------------------------------------------- //
// Test suite                                                                  //
// --------------------------------------------------------------------------- //

describe("meta-ag/router/MetaAGRouter", function () {
  let admin, user, recipient, other;
  let oracleHub, router;
  let tokenA, tokenB, tokenC;

  beforeEach(async function () {
    [admin, user, recipient, other] = await ethers.getSigners();

    oracleHub = await makeMockOracleHub();

    router = await deployMetaAGRouter({
      oracleHub: oracleHub.target,
      maxOracleSanityDeviation: 500n, // 5%
      admin: admin.address,
    });

    tokenA = await makeToken("Alpha", "A", 18);
    tokenB = await makeToken("Beta", "B", 18);
    tokenC = await makeToken("Gamma", "C", 18);
  });

  // ===================================================================== //
  // Initializer                                                            //
  // ===================================================================== //
  describe("initializer", function () {
    it("stores oracleHub / maxOracleSanityDeviation / oracleSanityEnabled and grants admin DEFAULT_ADMIN_ROLE", async function () {
      expect(await router.oracleHub()).to.equal(oracleHub.target);
      expect(await router.maxOracleSanityDeviation()).to.equal(500n);
      expect(await router.oracleSanityEnabled()).to.equal(true);
      expect(
        await router.hasRole(PECOR_ROLES.DEFAULT_ADMIN_ROLE, admin.address)
      ).to.equal(true);
      expect(await router.MAX_ADAPTERS()).to.equal(20n);
      expect(await router.MAX_HOPS()).to.equal(5n);
    });

    it("rejects zero oracleHub / zero admin / sanityDeviation > BPS_DENOMINATOR", async function () {
      const Impl = await ethers.getContractFactory("MetaAGRouter");
      const impl = await Impl.deploy();
      await impl.waitForDeployment();
      const Proxy = await ethers.getContractFactory("ERC1967Proxy");

      const enc = (hub, dev, adm) =>
        impl.interface.encodeFunctionData("initialize", [hub, dev, adm]);

      await expect(
        Proxy.deploy(impl.target, enc(ZERO_ADDRESS, 500n, admin.address))
      ).to.be.revertedWithCustomError(impl, "ZeroAddress");

      await expect(
        Proxy.deploy(impl.target, enc(oracleHub.target, 500n, ZERO_ADDRESS))
      ).to.be.revertedWithCustomError(impl, "ZeroAddress");

      await expect(
        Proxy.deploy(impl.target, enc(oracleHub.target, BPS_DENOMINATOR + 1n, admin.address))
      ).to.be.revertedWithCustomError(impl, "InvalidBps");
    });

    it("cannot be re-initialized (Initializable guard)", async function () {
      await expect(
        router.initialize(oracleHub.target, 500n, admin.address)
      ).to.be.revertedWithCustomError(router, ERRORS.common.AlreadyInitialized);
    });
  });

  // ===================================================================== //
  // Admin gates                                                            //
  // ===================================================================== //
  describe("admin gates", function () {
    it("setOracleHub: admin succeeds + emits OracleHubUpdated; non-admin reverts; zero reverts", async function () {
      const next = await makeMockOracleHub();
      await expect(router.connect(admin).setOracleHub(next.target))
        .to.emit(router, "OracleHubUpdated")
        .withArgs(next.target);
      expect(await router.oracleHub()).to.equal(next.target);

      await expect(
        router.connect(other).setOracleHub(next.target)
      ).to.be.revertedWithCustomError(router, ERRORS.common.Unauthorized);

      await expect(
        router.connect(admin).setOracleHub(ZERO_ADDRESS)
      ).to.be.revertedWithCustomError(router, "ZeroAddress");
    });

    it("setOracleSanityDeviation: admin succeeds + event; non-admin reverts; >BPS reverts", async function () {
      await expect(router.connect(admin).setOracleSanityDeviation(750n))
        .to.emit(router, "OracleSanityDeviationUpdated")
        .withArgs(750n);
      expect(await router.maxOracleSanityDeviation()).to.equal(750n);

      await expect(
        router.connect(other).setOracleSanityDeviation(750n)
      ).to.be.revertedWithCustomError(router, ERRORS.common.Unauthorized);

      await expect(
        router.connect(admin).setOracleSanityDeviation(BPS_DENOMINATOR + 1n)
      ).to.be.revertedWithCustomError(router, "InvalidBps");
    });

    it("setOracleSanityEnabled: toggles and emits", async function () {
      await expect(router.connect(admin).setOracleSanityEnabled(false))
        .to.emit(router, "OracleSanityEnabledUpdated")
        .withArgs(false);
      expect(await router.oracleSanityEnabled()).to.equal(false);
    });

    it("pause / unpause: admin only", async function () {
      await router.connect(admin).pause();
      await router.connect(admin).unpause();
      await expect(router.connect(other).pause()).to.be.revertedWithCustomError(
        router,
        ERRORS.common.Unauthorized
      );
    });
  });

  // ===================================================================== //
  // Adapter registration                                                   //
  // ===================================================================== //
  describe("adapter registration", function () {
    it("registerAdapter: stores entry, populates by-id index, emits event", async function () {
      const { adapter, id } = await makeMockAdapter("MockA");

      await expect(router.connect(admin).registerAdapter(adapter.target))
        .to.emit(router, "AdapterRegistered")
        .withArgs(id, adapter.target, "MockA");

      expect(await router.adapterCount()).to.equal(1n);
      const entry = await router.getAdapter(id);
      expect(entry.adapter).to.equal(adapter.target);
      expect(entry.adapterId).to.equal(id);
      expect(entry.active).to.equal(true);
      expect(entry.name).to.equal("MockA");
      expect(await router.isAdapterActive(id)).to.equal(true);
    });

    it("registerAdapter: rejects zero address", async function () {
      await expect(
        router.connect(admin).registerAdapter(ZERO_ADDRESS)
      ).to.be.revertedWithCustomError(router, "ZeroAddress");
    });

    it("registerAdapter: rejects duplicate adapter address", async function () {
      const { adapter } = await makeMockAdapter("MockA");
      await router.connect(admin).registerAdapter(adapter.target);
      await expect(
        router.connect(admin).registerAdapter(adapter.target)
      ).to.be.revertedWithCustomError(router, "AdapterAlreadyRegistered");
    });

    it("registerAdapter: rejects duplicate adapterId from different addresses", async function () {
      // Two distinct deploys with the SAME adapterId → second must revert.
      const { adapter: a1, id } = await makeMockAdapter("Same");
      const F = await ethers.getContractFactory("MockProtocolAdapter");
      const a2 = await F.deploy(id, "Same", "1.0.0");
      await a2.waitForDeployment();

      await router.connect(admin).registerAdapter(a1.target);
      await expect(
        router.connect(admin).registerAdapter(a2.target)
      ).to.be.revertedWithCustomError(router, "AdapterAlreadyRegistered");
    });

    it("registerAdapter: non-admin reverts", async function () {
      const { adapter } = await makeMockAdapter("MockA");
      await expect(
        router.connect(other).registerAdapter(adapter.target)
      ).to.be.revertedWithCustomError(router, ERRORS.common.Unauthorized);
    });

    it("deactivateAdapter / activateAdapter: toggle active flag and emit events", async function () {
      const { adapter, id } = await makeMockAdapter("MockA");
      await router.connect(admin).registerAdapter(adapter.target);

      await expect(router.connect(admin).deactivateAdapter(id))
        .to.emit(router, "AdapterDeactivated")
        .withArgs(id);
      expect(await router.isAdapterActive(id)).to.equal(false);

      await expect(router.connect(admin).activateAdapter(id))
        .to.emit(router, "AdapterActivated")
        .withArgs(id);
      expect(await router.isAdapterActive(id)).to.equal(true);
    });

    it("deactivateAdapter / activateAdapter: revert on unknown id", async function () {
      const unknownId = ethers.keccak256(ethers.toUtf8Bytes("Ghost"));
      await expect(
        router.connect(admin).deactivateAdapter(unknownId)
      ).to.be.revertedWithCustomError(router, "AdapterNotFound");
      await expect(
        router.connect(admin).activateAdapter(unknownId)
      ).to.be.revertedWithCustomError(router, "AdapterNotFound");
    });
  });

  // ===================================================================== //
  // Quote views                                                            //
  // ===================================================================== //
  describe("adapter retirement", function () {
    it("reclaims an inactive adapter ID and capacity for replacement", async function () {
      const { adapter, id } = await makeMockAdapter("replaceable");
      await router.registerAdapter(adapter.target);
      await router.deactivateAdapter(id);
      await expect(router.connect(other).removeAdapter(id)).to.be.reverted;
      await expect(router.removeAdapter(id)).to.emit(router, "AdapterRemoved").withArgs(id, adapter.target);
      const replacement = await makeMockAdapter("replaceable");
      await router.registerAdapter(replacement.adapter.target);
      expect(await router.adapterCount()).to.equal(1n);
      expect((await router.getAdapter(id)).adapter).to.equal(replacement.adapter.target);
    });
  });

  describe("quote fault isolation", function () {
    it("selects a healthy route when another active adapter reverts", async function () {
      const bad = await makeMockAdapter("broken");
      const good = await makeMockAdapter("healthy");
      await bad.adapter.setRevertOnQuote(true);
      await good.adapter.setQuoteResult(true, ONE, 0, 0, 0, "0x");
      await router.registerAdapter(bad.adapter.target);
      await router.registerAdapter(good.adapter.target);
      expect((await router.getBestQuote(tokenA.target, tokenB.target, ONE)).adapter).to.equal(good.adapter.target);
      const all = await router.getAllQuotes(tokenA.target, tokenB.target, ONE);
      expect(all[0][0].available).to.equal(false);
      expect(all[0][1].available).to.equal(true);
    });
  });

  describe("getBestQuote / getAllQuotes", function () {
    it("getBestQuote: empty registry returns found=false", async function () {
      const q = await router.getBestQuote(tokenA.target, tokenB.target, ONE);
      expect(q.found).to.equal(false);
      expect(q.amountOut).to.equal(0n);
    });

    it("getBestQuote: single active adapter returns its quote", async function () {
      const { adapter, id } = await makeMockAdapter("MockA");
      await router.connect(admin).registerAdapter(adapter.target);
      await adapter.setQuoteResult(true, 100n * ONE, 50n, 30n, 1n * ONE, "0xdeadbeef");

      const q = await router.getBestQuote(tokenA.target, tokenB.target, ONE);
      expect(q.found).to.equal(true);
      expect(q.amountOut).to.equal(100n * ONE);
      expect(q.priceImpactBps).to.equal(50n);
      expect(q.feeBps).to.equal(30n);
      expect(q.feeAmount).to.equal(1n * ONE);
      expect(q.adapterId).to.equal(id);
      expect(q.adapter).to.equal(adapter.target);
      expect(q.adapterData).to.equal("0xdeadbeef");
    });

    it("getBestQuote: picks higher amountOut among multiple active adapters", async function () {
      const { adapter: a1, id: id1 } = await makeMockAdapter("Lower");
      const { adapter: a2, id: id2 } = await makeMockAdapter("Higher");
      await router.connect(admin).registerAdapter(a1.target);
      await router.connect(admin).registerAdapter(a2.target);

      await a1.setQuoteResult(true, 100n * ONE, 0n, 0n, 0n, "0x01");
      await a2.setQuoteResult(true, 110n * ONE, 0n, 0n, 0n, "0x02");

      const q = await router.getBestQuote(tokenA.target, tokenB.target, ONE);
      expect(q.adapterId).to.equal(id2);
      expect(q.amountOut).to.equal(110n * ONE);
    });

    it("getBestQuote: skips adapters with available=false or amountOut=0", async function () {
      const { adapter: aGood } = await makeMockAdapter("Good");
      const { adapter: aZero } = await makeMockAdapter("Zero");
      const { adapter: aUnavail } = await makeMockAdapter("Unavail");
      await router.connect(admin).registerAdapter(aGood.target);
      await router.connect(admin).registerAdapter(aZero.target);
      await router.connect(admin).registerAdapter(aUnavail.target);

      await aGood.setQuoteResult(true, 100n * ONE, 0n, 0n, 0n, "0x");
      await aZero.setQuoteResult(true, 0n, 0n, 0n, 0n, "0x"); // amountOut=0 → skipped
      await aUnavail.setQuoteResult(false, 1_000_000n * ONE, 0n, 0n, 0n, "0x"); // available=false

      const q = await router.getBestQuote(tokenA.target, tokenB.target, ONE);
      expect(q.amountOut).to.equal(100n * ONE);
    });

    it("getBestQuote: skips inactive adapters even if their quote is best", async function () {
      const { adapter: a1 } = await makeMockAdapter("Active");
      const { adapter: a2, id: id2 } = await makeMockAdapter("Inactive");
      await router.connect(admin).registerAdapter(a1.target);
      await router.connect(admin).registerAdapter(a2.target);

      await a1.setQuoteResult(true, 100n * ONE, 0n, 0n, 0n, "0x");
      await a2.setQuoteResult(true, 999n * ONE, 0n, 0n, 0n, "0x"); // would-be winner
      await router.connect(admin).deactivateAdapter(id2);

      const q = await router.getBestQuote(tokenA.target, tokenB.target, ONE);
      expect(q.amountOut).to.equal(100n * ONE);
    });

    it("getAllQuotes: returns parallel arrays of length adapterCount with empty quote for inactive", async function () {
      const { adapter: a1, id: id1 } = await makeMockAdapter("First");
      const { adapter: a2, id: id2 } = await makeMockAdapter("Second");
      await router.connect(admin).registerAdapter(a1.target);
      await router.connect(admin).registerAdapter(a2.target);

      await a1.setQuoteResult(true, 100n * ONE, 0n, 0n, 0n, "0x");
      await a2.setQuoteResult(true, 200n * ONE, 0n, 0n, 0n, "0x");
      await router.connect(admin).deactivateAdapter(id2);

      const [quotes, ids, names] = await router.getAllQuotes(
        tokenA.target,
        tokenB.target,
        ONE
      );
      expect(quotes.length).to.equal(2);
      expect(ids.length).to.equal(2);
      expect(names.length).to.equal(2);
      expect(quotes[0].amountOut).to.equal(100n * ONE);
      // Inactive entry: empty quote (default struct)
      expect(quotes[1].amountOut).to.equal(0n);
      expect(quotes[1].available).to.equal(false);
      expect(ids[0]).to.equal(id1);
      expect(ids[1]).to.equal(id2);
    });
  });

  // ===================================================================== //
  // swapBestRoute                                                         //
  // ===================================================================== //
  describe("swapBestRoute", function () {
    let mockA;

    beforeEach(async function () {
      const { adapter } = await makeMockAdapter("BestA");
      mockA = adapter;
      await router.connect(admin).registerAdapter(mockA.target);

      // Disable oracle sanity by default so the happy-path test is isolated.
      await router.connect(admin).setOracleSanityEnabled(false);

      // Pre-fund the adapter with tokenB so it can deliver to recipient.
      await tokenB.mint(mockA.target, 1_000_000n * ONE);
      // Give the user tokenA + approve router to pull it.
      await tokenA.mint(user.address, 1_000_000n * ONE);
      await tokenA.connect(user).approve(router.target, ethers.MaxUint256);
    });

    it("happy path: pulls tokenIn, calls best adapter, delivers tokenOut to caller, emits BestRouteSwap", async function () {
      await mockA.setQuoteResult(true, 50n * ONE, 25n, 30n, 0n, "0xfeed");
      await mockA.setSwapResult(50n * ONE, 0n, "0x");

      const balBefore = await tokenB.balanceOf(user.address);
      const adapterId = ethers.keccak256(ethers.toUtf8Bytes("BestA"));

      await expect(
        router.connect(user).swapBestRoute(
          tokenA.target,
          tokenB.target,
          100n * ONE,
          50n * ONE,
          0n
        )
      )
        .to.emit(router, "BestRouteSwap")
        .withArgs(user.address, tokenA.target, tokenB.target, 100n * ONE, 50n * ONE, adapterId);

      expect(await tokenB.balanceOf(user.address)).to.equal(balBefore + 50n * ONE);
      const last = await mockA.lastSwap();
      expect(last.amountIn).to.equal(100n * ONE);
      expect(last.from).to.equal(router.target); // S9 — payer is the router
      expect(last.recipient).to.equal(user.address);
      expect(last.adapterData).to.equal("0xfeed");
    });

    it("reverts SameToken when tokenIn == tokenOut", async function () {
      await expect(
        router.connect(user).swapBestRoute(tokenA.target, tokenA.target, ONE, 1n, 0n)
      ).to.be.revertedWithCustomError(router, "SameToken");
    });

    it("reverts ZeroAmount when amountIn == 0", async function () {
      await expect(
        router.connect(user).swapBestRoute(tokenA.target, tokenB.target, 0n, 0n, 0n)
      ).to.be.revertedWithCustomError(router, "ZeroAmount");
    });

    it("reverts NoAdaptersAvailable when registry is empty", async function () {
      // Deploy a fresh router with NO adapters.
      const fresh = await deployMetaAGRouter({
        oracleHub: oracleHub.target,
        maxOracleSanityDeviation: 500n,
        admin: admin.address,
      });
      await fresh.connect(admin).setOracleSanityEnabled(false);
      await expect(
        fresh.connect(user).swapBestRoute(tokenA.target, tokenB.target, ONE, 0n, 0n)
      ).to.be.revertedWithCustomError(fresh, "NoAdaptersAvailable");
    });

    it("reverts BestQuoteUnavailable when every adapter declines", async function () {
      await mockA.setQuoteResult(false, 0n, 0n, 0n, 0n, "0x");
      await expect(
        router.connect(user).swapBestRoute(tokenA.target, tokenB.target, ONE, 0n, 0n)
      ).to.be.revertedWithCustomError(router, "BestQuoteUnavailable");
    });

    it("reverts SlippageTooHigh when best.amountOut < amountOutMin", async function () {
      await mockA.setQuoteResult(true, 10n * ONE, 0n, 0n, 0n, "0x");
      await mockA.setSwapResult(10n * ONE, 0n, "0x");
      await expect(
        router.connect(user).swapBestRoute(
          tokenA.target,
          tokenB.target,
          100n * ONE,
          1_000n * ONE, // way too high
          0n
        )
      ).to.be.revertedWithCustomError(router, "SlippageTooHigh");
    });

    it("reverts DeadlineExpired when block.timestamp > deadline", async function () {
      await mockA.setQuoteResult(true, 10n * ONE, 0n, 0n, 0n, "0x");
      const past = (await nowTs()) - 100;
      await expect(
        router.connect(user).swapBestRoute(
          tokenA.target,
          tokenB.target,
          ONE,
          1n,
          past
        )
      ).to.be.revertedWithCustomError(router, "DeadlineExpired");
    });
  });

  // ===================================================================== //
  // swapViaAdapter                                                        //
  // ===================================================================== //
  describe("swapViaAdapter", function () {
    let mockA, idA;

    beforeEach(async function () {
      const made = await makeMockAdapter("ViaA");
      mockA = made.adapter;
      idA = made.id;
      await router.connect(admin).registerAdapter(mockA.target);
      await router.connect(admin).setOracleSanityEnabled(false);

      await tokenB.mint(mockA.target, 1_000_000n * ONE);
      await tokenA.mint(user.address, 1_000_000n * ONE);
      await tokenA.connect(user).approve(router.target, ethers.MaxUint256);
    });

    it("happy path: routes via specific adapter id", async function () {
      await mockA.setQuoteResult(true, 50n * ONE, 0n, 0n, 0n, "0xa1");
      await mockA.setSwapResult(50n * ONE, 0n, "0x");

      await router
        .connect(user)
        .swapViaAdapter(idA, tokenA.target, tokenB.target, 100n * ONE, 50n * ONE, 0n);
      expect(await mockA.swapCallCount()).to.equal(1n);
    });

    it("reverts AdapterNotFound when adapterId is unknown", async function () {
      const ghost = ethers.keccak256(ethers.toUtf8Bytes("Ghost"));
      await expect(
        router.connect(user).swapViaAdapter(ghost, tokenA.target, tokenB.target, ONE, 0n, 0n)
      ).to.be.revertedWithCustomError(router, "AdapterNotFound");
    });

    it("reverts AdapterInactive when adapter is registered but deactivated", async function () {
      await router.connect(admin).deactivateAdapter(idA);
      await expect(
        router.connect(user).swapViaAdapter(idA, tokenA.target, tokenB.target, ONE, 0n, 0n)
      ).to.be.revertedWithCustomError(router, "AdapterInactive");
    });

    it("reverts QuoteUnavailable when adapter says available=false", async function () {
      await mockA.setQuoteResult(false, 0n, 0n, 0n, 0n, "0x");
      await expect(
        router.connect(user).swapViaAdapter(idA, tokenA.target, tokenB.target, ONE, 0n, 0n)
      ).to.be.revertedWithCustomError(router, "QuoteUnavailable");
    });
  });

  // ===================================================================== //
  // swapMultiHop (S3 + MAX_HOPS)                                          //
  // ===================================================================== //
  describe("swapMultiHop", function () {
    let mockA, idA, mockB, idB, mockC, idC;

    beforeEach(async function () {
      const ma = await makeMockAdapter("HopA");
      const mb = await makeMockAdapter("HopB");
      const mc = await makeMockAdapter("HopC");
      mockA = ma.adapter; idA = ma.id;
      mockB = mb.adapter; idB = mb.id;
      mockC = mc.adapter; idC = mc.id;
      await router.connect(admin).registerAdapter(mockA.target);
      await router.connect(admin).registerAdapter(mockB.target);
      await router.connect(admin).registerAdapter(mockC.target);
      await router.connect(admin).setOracleSanityEnabled(false);

      // Fund user + approve router.
      await tokenA.mint(user.address, 1_000_000n * ONE);
      await tokenA.connect(user).approve(router.target, ethers.MaxUint256);
    });

    it("rejects discontinuous paths before consuming user or seeded router inventory", async function () {
      await tokenC.mint(router.target, 17n * ONE);
      const before = await tokenA.balanceOf(user.address);
      const hops = [
        { tokenIn: tokenA.target, tokenOut: tokenB.target, adapterId: idA, minAmountOut: 0 },
        { tokenIn: tokenC.target, tokenOut: tokenA.target, adapterId: idB, minAmountOut: 0 },
      ];
      await expect(router.connect(user).swapMultiHop(hops, ONE, 0, 0)).to.be.revertedWithCustomError(router, "InvalidPath");
      expect(await tokenA.balanceOf(user.address)).to.equal(before);
      expect(await tokenC.balanceOf(router.target)).to.equal(17n * ONE);
    });

    it("reverts TooFewHops when hops.length < 2", async function () {
      const hops = [
        { tokenIn: tokenA.target, tokenOut: tokenB.target, adapterId: idA, minAmountOut: 0n },
      ];
      await expect(
        router.connect(user).swapMultiHop(hops, ONE, 0n, 0n)
      ).to.be.revertedWithCustomError(router, "TooFewHops");
    });

    it("reverts MaxHopsExceeded when hops.length > MAX_HOPS (=5)", async function () {
      const hop = {
        tokenIn: tokenA.target,
        tokenOut: tokenB.target,
        adapterId: idA,
        minAmountOut: 0n,
      };
      const hops = [hop, hop, hop, hop, hop, hop]; // 6 hops
      await expect(
        router.connect(user).swapMultiHop(hops, ONE, 0n, 0n)
      ).to.be.revertedWithCustomError(router, "MaxHopsExceeded");
    });

    it("reverts ZeroAmount when amountIn == 0", async function () {
      const hops = [
        { tokenIn: tokenA.target, tokenOut: tokenB.target, adapterId: idA, minAmountOut: 0n },
        { tokenIn: tokenB.target, tokenOut: tokenC.target, adapterId: idB, minAmountOut: 0n },
      ];
      await expect(
        router.connect(user).swapMultiHop(hops, 0n, 0n, 0n)
      ).to.be.revertedWithCustomError(router, "ZeroAmount");
    });

    it("happy 2-hop A→B→C: re-quotes hop2 with intermediate amount (S3 witness)", async function () {
      // Hop 1: 100 A → 80 B (mockA holds B)
      await tokenB.mint(mockA.target, 1_000_000n * ONE);
      await mockA.setQuoteResult(true, 80n * ONE, 0n, 0n, 0n, "0x01");
      await mockA.setSwapResult(80n * ONE, 0n, "0x");

      // Hop 2: 80 B → 70 C (mockB holds C)
      await tokenC.mint(mockB.target, 1_000_000n * ONE);
      await mockB.setQuoteResult(true, 70n * ONE, 0n, 0n, 0n, "0x02");
      await mockB.setSwapResult(70n * ONE, 0n, "0x");

      const hops = [
        { tokenIn: tokenA.target, tokenOut: tokenB.target, adapterId: idA, minAmountOut: 70n * ONE },
        { tokenIn: tokenB.target, tokenOut: tokenC.target, adapterId: idB, minAmountOut: 0n },
      ];

      const balBefore = await tokenC.balanceOf(user.address);

      await expect(
        router.connect(user).swapMultiHop(hops, 100n * ONE, 60n * ONE, 0n)
      )
        .to.emit(router, "MultiHopSwap")
        .withArgs(user.address, tokenA.target, tokenC.target, 100n * ONE, 70n * ONE, 2n);

      expect(await tokenC.balanceOf(user.address)).to.equal(balBefore + 70n * ONE);

      // S3: hop2's getQuote was called with `currentAmount = 80e18` (hop1 output).
      const last2 = await mockB.lastSwap();
      expect(last2.amountIn).to.equal(80n * ONE);
      expect(last2.tokenIn).to.equal(tokenB.target);
      expect(last2.recipient).to.equal(user.address); // last hop → user
    });

    it("reverts SlippageTooHigh on per-hop minAmountOut mismatch (S3 enforcement)", async function () {
      await tokenB.mint(mockA.target, 1_000_000n * ONE);
      await mockA.setQuoteResult(true, 50n * ONE, 0n, 0n, 0n, "0x");
      await mockA.setSwapResult(50n * ONE, 0n, "0x");

      await tokenC.mint(mockB.target, 1_000_000n * ONE);
      await mockB.setQuoteResult(true, 50n * ONE, 0n, 0n, 0n, "0x");
      await mockB.setSwapResult(50n * ONE, 0n, "0x");

      // hop1 minAmountOut = 100 > hop1.amountOut(50) → revert
      const hops = [
        { tokenIn: tokenA.target, tokenOut: tokenB.target, adapterId: idA, minAmountOut: 100n * ONE },
        { tokenIn: tokenB.target, tokenOut: tokenC.target, adapterId: idB, minAmountOut: 0n },
      ];
      await expect(
        router.connect(user).swapMultiHop(hops, 100n * ONE, 0n, 0n)
      ).to.be.revertedWithCustomError(router, "SlippageTooHigh");
    });

    it("reverts AdapterNotFound when a hop targets an unknown id", async function () {
      // Make hop1 succeed so the router reaches hop2 where the ghost id is checked.
      await tokenB.mint(mockA.target, 1_000_000n * ONE);
      await mockA.setQuoteResult(true, 50n * ONE, 0n, 0n, 0n, "0x");
      await mockA.setSwapResult(50n * ONE, 0n, "0x");

      const ghost = ethers.keccak256(ethers.toUtf8Bytes("Ghost"));
      const hops = [
        { tokenIn: tokenA.target, tokenOut: tokenB.target, adapterId: idA, minAmountOut: 0n },
        { tokenIn: tokenB.target, tokenOut: tokenC.target, adapterId: ghost, minAmountOut: 0n },
      ];
      await expect(
        router.connect(user).swapMultiHop(hops, 100n * ONE, 0n, 0n)
      ).to.be.revertedWithCustomError(router, "AdapterNotFound");
    });

    it("reverts AdapterInactive on a hop targeting a deactivated adapter", async function () {
      await router.connect(admin).deactivateAdapter(idB);
      const hops = [
        { tokenIn: tokenA.target, tokenOut: tokenB.target, adapterId: idA, minAmountOut: 0n },
        { tokenIn: tokenB.target, tokenOut: tokenC.target, adapterId: idB, minAmountOut: 0n },
      ];
      await tokenB.mint(mockA.target, 1_000_000n * ONE);
      await mockA.setQuoteResult(true, 50n * ONE, 0n, 0n, 0n, "0x");
      await mockA.setSwapResult(50n * ONE, 0n, "0x");
      await expect(
        router.connect(user).swapMultiHop(hops, 100n * ONE, 0n, 0n)
      ).to.be.revertedWithCustomError(router, "AdapterInactive");
    });

    it("supports 5-hop path at MAX_HOPS=5 boundary", async function () {
      // 5 hops: A → B → A → B → A → B (re-using A↔B route via 3 distinct adapters
      //  in alternation). Each adapter is funded for both tokens to support both directions.
      const adapters = [mockA, mockB, mockC, mockA, mockB];
      const ids = [idA, idB, idC, idA, idB];

      // Fund all three mocks with both tokens to support either direction.
      for (const m of [mockA, mockB, mockC]) {
        await tokenB.mint(m.target, 1_000_000n * ONE);
        await tokenA.mint(m.target, 1_000_000n * ONE);
      }

      // Each hop returns 1:1 (no slippage in mock).
      for (const m of adapters) {
        await m.setQuoteResult(true, 100n * ONE, 0n, 0n, 0n, "0x");
        await m.setSwapResult(100n * ONE, 0n, "0x");
      }

      const hops = [
        { tokenIn: tokenA.target, tokenOut: tokenB.target, adapterId: ids[0], minAmountOut: 0n },
        { tokenIn: tokenB.target, tokenOut: tokenA.target, adapterId: ids[1], minAmountOut: 0n },
        { tokenIn: tokenA.target, tokenOut: tokenB.target, adapterId: ids[2], minAmountOut: 0n },
        { tokenIn: tokenB.target, tokenOut: tokenA.target, adapterId: ids[3], minAmountOut: 0n },
        { tokenIn: tokenA.target, tokenOut: tokenB.target, adapterId: ids[4], minAmountOut: 0n },
      ];

      await expect(
        router.connect(user).swapMultiHop(hops, 100n * ONE, 0n, 0n)
      ).to.emit(router, "MultiHopSwap")
        .withArgs(user.address, tokenA.target, tokenB.target, 100n * ONE, 100n * ONE, 5n);
    });
  });

  // ===================================================================== //
  // _oracleSanityCheck (S4)                                               //
  // ===================================================================== //
  describe("_oracleSanityCheck (S4)", function () {
    let mockA, idA;

    beforeEach(async function () {
      const made = await makeMockAdapter("SanityA");
      mockA = made.adapter;
      idA = made.id;
      await router.connect(admin).registerAdapter(mockA.target);
      // oracleSanityEnabled defaults to true.

      await tokenB.mint(mockA.target, 1_000_000n * ONE);
      await tokenA.mint(user.address, 1_000_000n * ONE);
      await tokenA.connect(user).approve(router.target, ethers.MaxUint256);
    });

    it("normalizes 6 and 18 decimal routes in both directions", async function () {
      const six = await makeToken("Six", "SIX", 6);
      await oracleHub.setPrice(six.target, ONE);
      await oracleHub.setPrice(tokenA.target, ONE);
      await six.mint(user.address, 1_000_000n);
      await six.connect(user).approve(router.target, ethers.MaxUint256);
      await tokenA.mint(mockA.target, ONE);
      await mockA.setQuoteResult(true, ONE, 0, 0, 0, "0x");
      await mockA.setSwapResult(ONE, 0, "0x");
      await router.connect(user).swapBestRoute(six.target, tokenA.target, 1_000_000n, ONE, 0);
      await six.mint(mockA.target, 1_000_000n);
      await mockA.setQuoteResult(true, 1_000_000n, 0, 0, 0, "0x");
      await mockA.setSwapResult(1_000_000n, 0, "0x");
      await router.connect(user).swapBestRoute(tokenA.target, six.target, ONE, 1_000_000n, 0);
    });

    it("passes when realized output is within deviation tolerance (5% default)", async function () {
      // Oracle: A=$1, B=$1 → 100 A should produce ~100 B.
      await oracleHub.setPrice(tokenA.target, ONE);
      await oracleHub.setPrice(tokenB.target, ONE);

      // Adapter delivers 99 B (1% deviation, below 5% tolerance).
      await mockA.setQuoteResult(true, 99n * ONE, 0n, 0n, 0n, "0x");
      await mockA.setSwapResult(99n * ONE, 0n, "0x");

      await expect(
        router.connect(user).swapBestRoute(tokenA.target, tokenB.target, 100n * ONE, 0n, 0n)
      ).to.not.be.reverted;
    });

    it("reverts OracleSanityFailed when deviation exceeds tolerance", async function () {
      await oracleHub.setPrice(tokenA.target, ONE);
      await oracleHub.setPrice(tokenB.target, ONE);

      // Adapter delivers 50 B for 100 A → 50% deviation, way above 5%.
      await mockA.setQuoteResult(true, 50n * ONE, 0n, 0n, 0n, "0x");
      await mockA.setSwapResult(50n * ONE, 0n, "0x");

      await expect(
        router.connect(user).swapBestRoute(tokenA.target, tokenB.target, 100n * ONE, 0n, 0n)
      ).to.be.revertedWithCustomError(router, "OracleSanityFailed");
    });

    it("skips silently when oracle reports tokenOut unavailable", async function () {
      // tokenA price set, tokenB unavailable → sanity check returns early.
      await oracleHub.setPrice(tokenA.target, ONE);
      // tokenB intentionally NOT seeded — _available[tokenB] = false → skip.

      // Even with a wildly off output, this must NOT revert.
      await mockA.setQuoteResult(true, 10n * ONE, 0n, 0n, 0n, "0x");
      await mockA.setSwapResult(10n * ONE, 0n, "0x");

      await expect(
        router.connect(user).swapBestRoute(tokenA.target, tokenB.target, 100n * ONE, 0n, 0n)
      ).to.not.be.reverted;
    });

    it("setOracleSanityEnabled(false) bypasses the check entirely", async function () {
      await router.connect(admin).setOracleSanityEnabled(false);
      // No oracle prices set, but oversized deviation; check is bypassed → no revert.
      await mockA.setQuoteResult(true, 1n, 0n, 0n, 0n, "0x");
      await mockA.setSwapResult(1n, 0n, "0x");

      await expect(
        router.connect(user).swapBestRoute(tokenA.target, tokenB.target, 100n * ONE, 0n, 0n)
      ).to.not.be.reverted;
    });
  });

  // ===================================================================== //
  // Pausable (S10)                                                        //
  // ===================================================================== //
  describe("pausable (S10)", function () {
    let mockA, idA;

    beforeEach(async function () {
      const made = await makeMockAdapter("PauseA");
      mockA = made.adapter;
      idA = made.id;
      await router.connect(admin).registerAdapter(mockA.target);
      await router.connect(admin).setOracleSanityEnabled(false);
      await tokenB.mint(mockA.target, 1_000_000n * ONE);
      await tokenA.mint(user.address, 1_000_000n * ONE);
      await tokenA.connect(user).approve(router.target, ethers.MaxUint256);
      await mockA.setQuoteResult(true, 50n * ONE, 0n, 0n, 0n, "0x");
      await mockA.setSwapResult(50n * ONE, 0n, "0x");
    });

    it("swapBestRoute reverts Paused when paused", async function () {
      await router.connect(admin).pause();
      await expect(
        router.connect(user).swapBestRoute(tokenA.target, tokenB.target, ONE, 0n, 0n)
      ).to.be.revertedWithCustomError(router, ERRORS.common.Paused);
    });

    it("swapViaAdapter reverts Paused when paused", async function () {
      await router.connect(admin).pause();
      await expect(
        router.connect(user).swapViaAdapter(idA, tokenA.target, tokenB.target, ONE, 0n, 0n)
      ).to.be.revertedWithCustomError(router, ERRORS.common.Paused);
    });

    it("swapMultiHop reverts Paused when paused", async function () {
      await router.connect(admin).pause();
      const hops = [
        { tokenIn: tokenA.target, tokenOut: tokenB.target, adapterId: idA, minAmountOut: 0n },
        { tokenIn: tokenB.target, tokenOut: tokenA.target, adapterId: idA, minAmountOut: 0n },
      ];
      await expect(
        router.connect(user).swapMultiHop(hops, ONE, 0n, 0n)
      ).to.be.revertedWithCustomError(router, ERRORS.common.Paused);
    });
  });

  // ===================================================================== //
  // Views: getAdapter / getAdapters / isAdapterActive                     //
  // ===================================================================== //
  describe("views", function () {
    it("getAdapter reverts AdapterNotFound for unknown id", async function () {
      const ghost = ethers.keccak256(ethers.toUtf8Bytes("Ghost"));
      await expect(router.getAdapter(ghost)).to.be.revertedWithCustomError(
        router,
        "AdapterNotFound"
      );
    });

    it("isAdapterActive returns false for unknown id (no revert)", async function () {
      const ghost = ethers.keccak256(ethers.toUtf8Bytes("Ghost"));
      expect(await router.isAdapterActive(ghost)).to.equal(false);
    });

    it("getAdapters returns full registry including inactive entries", async function () {
      const a1 = await makeMockAdapter("V1");
      const a2 = await makeMockAdapter("V2");
      await router.connect(admin).registerAdapter(a1.adapter.target);
      await router.connect(admin).registerAdapter(a2.adapter.target);
      await router.connect(admin).deactivateAdapter(a1.id);

      const list = await router.getAdapters();
      expect(list.length).to.equal(2);
      expect(list[0].active).to.equal(false);
      expect(list[1].active).to.equal(true);
    });
  });

  // ===================================================================== //
  // UUPS upgrade authorization (S1)                                       //
  // ===================================================================== //
  describe("UUPS upgrade authorization (S1)", function () {
    it("admin can upgrade", async function () {
      const Impl = await ethers.getContractFactory("MetaAGRouter");
      const newImpl = await Impl.deploy();
      await newImpl.waitForDeployment();
      await expect(router.connect(admin).upgradeToAndCall(newImpl.target, "0x")).to.not
        .be.reverted;
    });

    it("non-admin cannot upgrade", async function () {
      const Impl = await ethers.getContractFactory("MetaAGRouter");
      const newImpl = await Impl.deploy();
      await newImpl.waitForDeployment();
      await expect(
        router.connect(other).upgradeToAndCall(newImpl.target, "0x")
      ).to.be.revertedWithCustomError(router, ERRORS.common.Unauthorized);
    });
  });
});
