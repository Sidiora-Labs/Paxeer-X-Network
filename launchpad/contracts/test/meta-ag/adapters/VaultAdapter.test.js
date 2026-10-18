/**
 * Sidiora Meta-AG — VaultAdapter unit tests (Phase 5 / Task 5.1 — deferred to Session 8)
 *
 * Spec: docs/architecture/pecor-sidiora-aggregator-spec.md §7.9 (FROZEN 2026-04-24)
 * Interface: contracts/meta-ag/interfaces/IProtocolAdapter.sol
 * Contract: contracts/meta-ag/adapters/VaultAdapter.sol
 *
 * Regressions exercised:
 *   - I1  getQuote never reverts (returns available=false)
 *   - I2  executeSwap enforces min-out (SlippageExceeded revert)
 *   - I3  executeSwap honors deadline (Expired revert) / 0 = no-deadline
 *   - I4  Adapter pulls tokenIn from `from`, sends tokenOut to recipient
 *   - I5  adapterId = keccak256("PECORVault.v1")
 *   - I6  adapterData round-trips (vault adapter ignores it — pair implicit)
 *   - S9  Vault exposure is via OPERATOR_ROLE, not raw approvals — no dust
 *         remains on the adapter (pullTokens/pushTokens move funds atomically)
 */

const { expect } = require("chai");
const { ethers } = require("hardhat");

const {
  PECOR_ROLES,
  BPS,
  ADAPTER_IDS,
} = require("../helpers/constants");
const {
  deployPriceOracle,
  deployPECORVault,
  deployVaultAdapter,
} = require("../helpers/fixtures");
const { pushPrice } = require("../helpers/oracle");

const ONE = 10n ** 18n;
const ONE_6 = 10n ** 6n;
const ONE_HOUR = 3600;

async function makeToken(name, symbol, decimals = 18) {
  const ERC20 = await ethers.getContractFactory("MockERC20");
  const t = await ERC20.deploy(name, symbol, decimals);
  await t.waitForDeployment();
  return t;
}

async function makeWETH() {
  const W = await ethers.getContractFactory("MockWETH9");
  const w = await W.deploy();
  await w.waitForDeployment();
  return w;
}

async function makeTxTracker() {
  const T = await ethers.getContractFactory("MockTxTracker");
  const t = await T.deploy();
  await t.waitForDeployment();
  return t;
}

/**
 * Price-bound helper — wide enough to fit $0.01–$100k with hourly staleness.
 */
function priceBound() {
  return {
    heartbeat: 60,
    deviationBps: 100n, // 1%
    minPrice: ONE / 100n,
    maxPrice: ONE * 100_000n,
    maxStaleness: ONE_HOUR,
  };
}

describe("meta-ag/adapters/VaultAdapter", function () {
  let admin, user, recipient, feeCollector, other;
  let priceOracle, vault, adapter, weth, tracker;
  let tokenA, tokenB;

  beforeEach(async function () {
    [admin, user, recipient, feeCollector, other] = await ethers.getSigners();

    // Oracle + reserves plumbing
    priceOracle = await deployPriceOracle({ admin: admin.address });
    await priceOracle.connect(admin).setRelayer(admin.address, true);

    weth = await makeWETH();
    tracker = await makeTxTracker();
    vault = await deployPECORVault({
      weth: weth.target,
      tracker: tracker.target,
      admin: admin.address,
    });

    tokenA = await makeToken("Alpha", "A", 18);
    tokenB = await makeToken("Beta", "B", 18);

    // Register on vault
    await vault.connect(admin).registerToken(tokenA.target, false);
    await vault.connect(admin).registerToken(tokenB.target, false);

    // Register on oracle + seed prices
    const b = priceBound();
    await priceOracle
      .connect(admin)
      .registerToken(tokenA.target, b.heartbeat, b.deviationBps, b.minPrice, b.maxPrice, b.maxStaleness);
    await priceOracle
      .connect(admin)
      .registerToken(tokenB.target, b.heartbeat, b.deviationBps, b.minPrice, b.maxPrice, b.maxStaleness);
    // tokenA = $1, tokenB = $2
    await pushPrice({ priceOracle, relayer: admin, token: tokenA.target, price: ONE });
    await pushPrice({ priceOracle, relayer: admin, token: tokenB.target, price: 2n * ONE });

    // Deploy adapter + grant OPERATOR_ROLE so pullTokens/pushTokens work
    adapter = await deployVaultAdapter({
      vault: vault.target,
      priceOracle: priceOracle.target,
      feeBps: 20n, // 20 bps
      feeCollector: feeCollector.address,
      admin: admin.address,
    });
    await vault.connect(admin).setOperator(adapter.target, true);
  });

  // ------------------------------------------------------------------- //
  // constructor                                                          //
  // ------------------------------------------------------------------- //
  describe("constructor", function () {
    it("stores vault / priceOracle / feeBps / feeCollector and grants admin DEFAULT_ADMIN_ROLE", async function () {
      expect(await adapter.vault()).to.equal(vault.target);
      expect(await adapter.priceOracle()).to.equal(priceOracle.target);
      expect(await adapter.feeBps()).to.equal(20n);
      expect(await adapter.feeCollector()).to.equal(feeCollector.address);
      expect(await adapter.hasRole(PECOR_ROLES.DEFAULT_ADMIN_ROLE, admin.address)).to.equal(true);
    });

    it("rejects zero vault / oracle / feeCollector / admin", async function () {
      const Factory = await ethers.getContractFactory("VaultAdapter");
      await expect(
        Factory.deploy(ethers.ZeroAddress, priceOracle.target, 20n, feeCollector.address, admin.address)
      ).to.be.revertedWithCustomError(Factory, "ZeroAddress");
      await expect(
        Factory.deploy(vault.target, ethers.ZeroAddress, 20n, feeCollector.address, admin.address)
      ).to.be.revertedWithCustomError(Factory, "ZeroAddress");
      await expect(
        Factory.deploy(vault.target, priceOracle.target, 20n, ethers.ZeroAddress, admin.address)
      ).to.be.revertedWithCustomError(Factory, "ZeroAddress");
      await expect(
        Factory.deploy(vault.target, priceOracle.target, 20n, feeCollector.address, ethers.ZeroAddress)
      ).to.be.revertedWithCustomError(Factory, "ZeroAddress");
    });

    it("rejects feeBps > MAX_FEE_BPS (200)", async function () {
      const Factory = await ethers.getContractFactory("VaultAdapter");
      await expect(
        Factory.deploy(vault.target, priceOracle.target, 201n, feeCollector.address, admin.address)
      ).to.be.revertedWithCustomError(Factory, "FeeTooHigh");
    });

    it("exposes MAX_FEE_BPS = 200 and BPS_DENOMINATOR = 10000", async function () {
      expect(await adapter.MAX_FEE_BPS()).to.equal(BPS.VAULT_ADAPTER_MAX_FEE);
      expect(await adapter.BPS_DENOMINATOR()).to.equal(BPS.DENOMINATOR);
    });
  });

  // ------------------------------------------------------------------- //
  // metadata (I5)                                                        //
  // ------------------------------------------------------------------- //
  describe("metadata", function () {
    it("adapterId == keccak256('PECORVault.v1')", async function () {
      expect(await adapter.adapterId()).to.equal(ADAPTER_IDS.VAULT);
    });

    it("adapterName == 'PECORVault.v1'", async function () {
      expect(await adapter.adapterName()).to.equal("PECORVault.v1");
    });

    it("adapterVersion == '1.0.0'", async function () {
      expect(await adapter.adapterVersion()).to.equal("1.0.0");
    });
  });

  // ------------------------------------------------------------------- //
  // supportsSwap / getSupportedPairs                                     //
  // ------------------------------------------------------------------- //
  describe("capability queries", function () {
    it("supportsSwap true for registered pair; false for same-token; false for unregistered", async function () {
      expect(await adapter.supportsSwap(tokenA.target, tokenB.target)).to.equal(true);
      expect(await adapter.supportsSwap(tokenA.target, tokenA.target)).to.equal(false);
      const tokenX = await makeToken("Xeno", "X");
      expect(await adapter.supportsSwap(tokenA.target, tokenX.target)).to.equal(false);
      expect(await adapter.supportsSwap(tokenX.target, tokenA.target)).to.equal(false);
    });

    it("getSupportedPairs returns n*(n-1) permutations", async function () {
      const [ins, outs] = await adapter.getSupportedPairs();
      expect(ins.length).to.equal(2); // 2 * (2 - 1)
      expect(outs.length).to.equal(2);
      // permutations should cover both directions
      const pairs = ins.map((a, i) => `${a}->${outs[i]}`).sort();
      const expected = [
        `${tokenA.target}->${tokenB.target}`,
        `${tokenB.target}->${tokenA.target}`,
      ].sort();
      expect(pairs).to.deep.equal(expected);
    });

    it("getSupportedPairs returns empty arrays when fewer than 2 tokens registered", async function () {
      // Fresh vault with a single token
      const v2 = await deployPECORVault({
        weth: weth.target,
        tracker: tracker.target,
        admin: admin.address,
      });
      await v2.connect(admin).registerToken(tokenA.target, false);
      const adapter2 = await deployVaultAdapter({
        vault: v2.target,
        priceOracle: priceOracle.target,
        feeBps: 20n,
        feeCollector: feeCollector.address,
        admin: admin.address,
      });
      const [ins, outs] = await adapter2.getSupportedPairs();
      expect(ins.length).to.equal(0);
      expect(outs.length).to.equal(0);
    });

    it("getMaxInput returns zero on same-token / zero reserves / zero price", async function () {
      expect(await adapter.getMaxInput(tokenA.target, tokenA.target)).to.equal(0n);
      // No reserves yet on tokenB
      expect(await adapter.getMaxInput(tokenA.target, tokenB.target)).to.equal(0n);
    });

    it("getMaxInput = reserves_tokenOut * priceOut / priceIn when both prices and reserves present", async function () {
      // Seed tokenB reserves: deposit 100 tokenB into vault
      await tokenB.mint(admin.address, 100n * ONE);
      await tokenB.connect(admin).approve(vault.target, 100n * ONE);
      await vault.connect(admin).deposit(tokenB.target, 100n * ONE);

      // tokenA=$1 tokenB=$2 → 100 * 2 / 1 = 200
      const maxIn = await adapter.getMaxInput(tokenA.target, tokenB.target);
      expect(maxIn).to.equal(200n * ONE);
    });
  });

  // ------------------------------------------------------------------- //
  // getQuote (I1 — never reverts)                                        //
  // ------------------------------------------------------------------- //
  describe("getQuote (I1 — never reverts)", function () {
    it("returns available=false on same-token / zero amountIn", async function () {
      const r1 = await adapter.getQuote(tokenA.target, tokenA.target, ONE);
      expect(r1.available).to.equal(false);
      expect(r1.amountOut).to.equal(0n);

      const r2 = await adapter.getQuote(tokenA.target, tokenB.target, 0n);
      expect(r2.available).to.equal(false);
    });

    it("returns available=false when tokenIn or tokenOut is not registered on vault", async function () {
      const tokenX = await makeToken("Xeno", "X");
      const r1 = await adapter.getQuote(tokenX.target, tokenB.target, ONE);
      expect(r1.available).to.equal(false);
      const r2 = await adapter.getQuote(tokenA.target, tokenX.target, ONE);
      expect(r2.available).to.equal(false);
    });

    it("returns available=false when output reserves are insufficient for the quote", async function () {
      // No tokenB reserves yet → grossOut > reservesOut (reserves = 0) → available false
      const r = await adapter.getQuote(tokenA.target, tokenB.target, ONE);
      expect(r.available).to.equal(false);
    });

    it("returns available=true with populated net amountOut, fee, impact, and adapterData on the happy path", async function () {
      // Seed tokenB reserves
      await tokenB.mint(admin.address, 100n * ONE);
      await tokenB.connect(admin).approve(vault.target, 100n * ONE);
      await vault.connect(admin).deposit(tokenB.target, 100n * ONE);

      const amountIn = 2n * ONE; // $2 worth of tokenA
      const r = await adapter.getQuote(tokenA.target, tokenB.target, amountIn);
      expect(r.available).to.equal(true);
      // gross = 2 * 1 / 2 = 1 tokenB = 1e18; fee = 20 bps = 1e18 * 20/10000 = 2e15
      // net = 1e18 - 2e15 = 998e15
      const gross = ONE;
      const fee = (gross * 20n) / 10_000n;
      expect(r.amountOut).to.equal(gross - fee);
      expect(r.feeBps).to.equal(20n);
      expect(r.feeAmount).to.equal(fee);
      // priceImpactBps should be >0 but <10_000 (2 USD swap against 200 USD reserves = 100 bps)
      expect(r.priceImpactBps).to.be.gte(100n);
      expect(r.priceImpactBps).to.be.lte(101n);
      // adapterData is abi.encode(tokenIn, tokenOut)
      const decoded = ethers.AbiCoder.defaultAbiCoder().decode(
        ["address", "address"],
        r.adapterData
      );
      expect(decoded[0]).to.equal(tokenA.target);
      expect(decoded[1]).to.equal(tokenB.target);
    });

    it("returns available=false when a price is stale (oracle reverts; try/catch → price=0)", async function () {
      // Seed tokenB reserves
      await tokenB.mint(admin.address, 100n * ONE);
      await tokenB.connect(admin).approve(vault.target, 100n * ONE);
      await vault.connect(admin).deposit(tokenB.target, 100n * ONE);
      // Warp past staleness for tokenA (1h+)
      await ethers.provider.send("evm_increaseTime", [ONE_HOUR + 60]);
      await ethers.provider.send("evm_mine", []);
      const r = await adapter.getQuote(tokenA.target, tokenB.target, ONE);
      expect(r.available).to.equal(false);
    });
  });

  // ------------------------------------------------------------------- //
  // admin setters                                                        //
  // ------------------------------------------------------------------- //
  describe("admin setters", function () {
    it("setFee: admin-only, rejects > MAX_FEE_BPS", async function () {
      await expect(adapter.connect(admin).setFee(50n)).to.emit(adapter, "FeeUpdated").withArgs(20n, 50n);
      expect(await adapter.feeBps()).to.equal(50n);

      await expect(adapter.connect(other).setFee(30n)).to.be.revertedWithCustomError(
        adapter,
        "MissingRole"
      );
      await expect(adapter.connect(admin).setFee(300n)).to.be.revertedWithCustomError(
        adapter,
        "FeeTooHigh"
      );
    });

    it("setFeeCollector: admin-only, rejects zero address", async function () {
      await expect(adapter.connect(admin).setFeeCollector(other.address)).to.emit(adapter, "ConfigurationUpdated").withArgs(ethers.encodeBytes32String("feeCollector"), feeCollector.address, other.address);
      expect(await adapter.feeCollector()).to.equal(other.address);
      await expect(
        adapter.connect(other).setFeeCollector(feeCollector.address)
      ).to.be.revertedWithCustomError(adapter, "MissingRole");
      await expect(
        adapter.connect(admin).setFeeCollector(ethers.ZeroAddress)
      ).to.be.revertedWithCustomError(adapter, "ZeroAddress");
    });

    it("setPriceOracle: admin-only, rejects zero address", async function () {
      const o2 = await deployPriceOracle({ admin: admin.address });
      await expect(adapter.connect(admin).setPriceOracle(o2.target)).to.emit(adapter, "ConfigurationUpdated").withArgs(ethers.encodeBytes32String("priceOracle"), priceOracle.target, o2.target);
      expect(await adapter.priceOracle()).to.equal(o2.target);
      await expect(
        adapter.connect(other).setPriceOracle(o2.target)
      ).to.be.revertedWithCustomError(adapter, "MissingRole");
      await expect(
        adapter.connect(admin).setPriceOracle(ethers.ZeroAddress)
      ).to.be.revertedWithCustomError(adapter, "ZeroAddress");
    });
  });

  // ------------------------------------------------------------------- //
  // executeSwap (I2, I3, I4)                                             //
  // ------------------------------------------------------------------- //
  describe("executeSwap", function () {
    beforeEach(async function () {
      // Seed tokenB into vault reserves
      await tokenB.mint(admin.address, 200n * ONE);
      await tokenB.connect(admin).approve(vault.target, 200n * ONE);
      await vault.connect(admin).deposit(tokenB.target, 200n * ONE);
      // User has tokenA + approves the adapter; the adapter pulls into itself
      // and then funnels through vault.deposit. Mirrors MetaAGRouter's S9 flow.
      await tokenA.mint(user.address, 10n * ONE);
      await tokenA.connect(user).approve(adapter.target, 10n * ONE);
    });

    async function taxedToken() {
      const F = await ethers.getContractFactory("MockFeeOnTransferERC20");
      const t = await F.deploy("Taxed", "TAX", 18, 0);
      await vault.registerToken(t.target, false);
      const b = priceBound();
      await priceOracle.registerToken(t.target, b.heartbeat, b.deviationBps, b.minPrice, b.maxPrice, b.maxStaleness);
      await pushPrice({ priceOracle, relayer: admin, token: t.target, price: ONE });
      return t;
    }

    it("rejects taxed input without consuming seeded adapter dust", async function () {
      const taxed = await taxedToken();
      await taxed.mint(user.address, 10n * ONE);
      await taxed.mint(adapter.target, ONE);
      await taxed.connect(user).approve(adapter.target, 10n * ONE);
      await taxed.setFeeBps(1000);
      await expect(adapter.connect(user).executeSwap(taxed.target, tokenB.target, 10n * ONE, 0, user.address, recipient.address, 0, "0x"))
        .to.be.revertedWithCustomError(adapter, "TransferAmountMismatch");
      expect(await taxed.balanceOf(adapter.target)).to.equal(ONE);
      expect(await taxed.balanceOf(user.address)).to.equal(10n * ONE);
    });

    it("rejects a selective tax only at the adapter-to-vault boundary", async function () {
      const taxed = await taxedToken();
      await taxed.mint(user.address, 10n * ONE);
      await taxed.mint(adapter.target, ONE);
      await taxed.connect(user).approve(adapter.target, 10n * ONE);
      await taxed.setTaxedRecipient(vault.target);
      await taxed.setFeeBps(1000);
      const outputBefore = await tokenB.balanceOf(vault.target);
      await expect(adapter.connect(user).executeSwap(taxed.target, tokenB.target, 10n * ONE, 0, user.address, recipient.address, 0, "0x"))
        .to.be.revertedWithCustomError(adapter, "TransferAmountMismatch");
      expect(await taxed.balanceOf(user.address)).to.equal(10n * ONE);
      expect(await taxed.balanceOf(adapter.target)).to.equal(ONE);
      expect(await tokenB.balanceOf(vault.target)).to.equal(outputBefore);
    });

    it("rejects taxed output rather than violating recipient slippage", async function () {
      const taxed = await taxedToken();
      await taxed.mint(admin.address, 100n * ONE);
      await taxed.approve(vault.target, 100n * ONE);
      await vault.deposit(taxed.target, 100n * ONE);
      await taxed.setFeeBps(1000);
      const quoted = (await adapter.getQuote(tokenA.target, taxed.target, ONE)).amountOut;
      await expect(adapter.connect(user).executeSwap(tokenA.target, taxed.target, ONE, quoted, user.address, recipient.address, 0, "0x"))
        .to.be.revertedWithCustomError(vault, "UnsupportedTransfer");
      expect(await taxed.balanceOf(recipient.address)).to.equal(0n);
    });

    it("cannot spend another user's standing allowance", async function () {
      const before = await tokenA.balanceOf(user.address);
      await expect(adapter.connect(other).executeSwap(tokenA.target, tokenB.target, ONE, 0n, user.address, other.address, 0, "0x"))
        .to.be.revertedWithCustomError(adapter, "UnauthorizedPayer");
      expect(await tokenA.balanceOf(user.address)).to.equal(before);
      expect(await tokenA.allowance(user.address, adapter.target)).to.equal(10n * ONE);
    });

    it("reverts with Expired when deadline is in the past (I3)", async function () {
      const past = (await ethers.provider.getBlock("latest")).timestamp - 100;
      await expect(
        adapter.connect(user).executeSwap(
          tokenA.target,
          tokenB.target,
          ONE,
          0n,
          user.address,
          recipient.address,
          past,
          "0x"
        )
      ).to.be.revertedWithCustomError(adapter, "Expired");
    });

    it("deadline=0 is treated as no-deadline", async function () {
      const amountIn = 2n * ONE;
      await expect(
        adapter.connect(user).executeSwap(
          tokenA.target,
          tokenB.target,
          amountIn,
          0n,
          user.address,
          recipient.address,
          0,
          "0x"
        )
      ).not.to.be.reverted;
    });

    it("reverts with SameToken when tokenIn == tokenOut", async function () {
      await expect(
        adapter.connect(user).executeSwap(
          tokenA.target,
          tokenA.target,
          ONE,
          0n,
          user.address,
          recipient.address,
          0,
          "0x"
        )
      ).to.be.revertedWithCustomError(adapter, "SameToken");
    });

    it("reverts with ZeroAmount when amountIn == 0", async function () {
      await expect(
        adapter.connect(user).executeSwap(
          tokenA.target,
          tokenB.target,
          0n,
          0n,
          user.address,
          recipient.address,
          0,
          "0x"
        )
      ).to.be.revertedWithCustomError(adapter, "ZeroAmount");
    });

    it("reverts with SlippageExceeded when minAmountOut > actual net out (I2)", async function () {
      const amountIn = 2n * ONE;
      const gross = ONE; // 2 * 1 / 2
      const fee = (gross * 20n) / 10_000n;
      const net = gross - fee;
      await expect(
        adapter.connect(user).executeSwap(
          tokenA.target,
          tokenB.target,
          amountIn,
          net + 1n, // ask for one wei more than actual
          user.address,
          recipient.address,
          0,
          "0x"
        )
      ).to.be.revertedWithCustomError(adapter, "SlippageExceeded");
    });

    it("reverts with InsufficientLiquidity when gross output exceeds vault reserves", async function () {
      // 500 tokenA worth = $500 → 250 tokenB needed, only 200 reserves
      await tokenA.mint(user.address, 500n * ONE);
      await tokenA.connect(user).approve(adapter.target, 500n * ONE);
      await expect(
        adapter.connect(user).executeSwap(
          tokenA.target,
          tokenB.target,
          500n * ONE,
          0n,
          user.address,
          recipient.address,
          0,
          "0x"
        )
      ).to.be.revertedWithCustomError(adapter, "InsufficientLiquidity");
    });

    it("happy path: pulls tokenIn from `from`, pushes net to `recipient`, pushes fee to feeCollector, emits SwapExecuted (I4, I5)", async function () {
      const amountIn = 2n * ONE;
      const gross = ONE;
      const fee = (gross * 20n) / 10_000n;
      const net = gross - fee;

      const userInBefore = await tokenA.balanceOf(user.address);
      const recipientOutBefore = await tokenB.balanceOf(recipient.address);
      const collectorBefore = await tokenB.balanceOf(feeCollector.address);

      await expect(
        adapter.connect(user).executeSwap(
          tokenA.target,
          tokenB.target,
          amountIn,
          net,
          user.address,
          recipient.address,
          0,
          "0x"
        )
      )
        .to.emit(adapter, "SwapExecuted")
        .withArgs(
          ADAPTER_IDS.VAULT,
          tokenA.target,
          tokenB.target,
          amountIn,
          net,
          recipient.address
        );

      expect(await tokenA.balanceOf(user.address)).to.equal(userInBefore - amountIn);
      expect(await tokenB.balanceOf(recipient.address)).to.equal(recipientOutBefore + net);
      expect(await tokenB.balanceOf(feeCollector.address)).to.equal(collectorBefore + fee);

      // Adapter itself holds no dust
      expect(await tokenA.balanceOf(adapter.target)).to.equal(0n);
      expect(await tokenB.balanceOf(adapter.target)).to.equal(0n);
    });

    it("fee=0 path: no fee transfer to collector", async function () {
      await adapter.connect(admin).setFee(0n);

      const collectorBefore = await tokenB.balanceOf(feeCollector.address);
      await adapter.connect(user).executeSwap(
        tokenA.target,
        tokenB.target,
        2n * ONE,
        0n,
        user.address,
        recipient.address,
        0,
        "0x"
      );
      expect(await tokenB.balanceOf(feeCollector.address)).to.equal(collectorBefore);
    });

    it("adapterData is ignored (I6 — vault adapter: pair implicit in tokenIn/tokenOut)", async function () {
      // Pass malformed adapterData; swap should still succeed because the adapter
      // resolves the pair from tokenIn/tokenOut rather than trusting the blob.
      const bogus = ethers.AbiCoder.defaultAbiCoder().encode(
        ["address", "address"],
        [other.address, ethers.ZeroAddress]
      );
      await expect(
        adapter.connect(user).executeSwap(
          tokenA.target,
          tokenB.target,
          2n * ONE,
          0n,
          user.address,
          recipient.address,
          0,
          bogus
        )
      ).not.to.be.reverted;
    });
  });
});
