/**
 * Sidiora Meta-AG — Phase 8 Integration Test 8.5
 *
 * Governance + access-control invariants across the entire wired stack.
 * In production the `admin` signer is replaced by the Sidiora Launchpad
 * Timelock — these tests use the fixture's admin signer as a stand-in for
 * Timelock and assert that EVERY admin op:
 *   - Reverts when called by an arbitrary EOA (`other`).
 *   - Succeeds when called by the configured admin.
 *
 * Plus: invariant S8 — no EOA other than the configured admin holds
 *   DEFAULT_ADMIN_ROLE on any of the 8 UUPS proxies.
 *
 * Spec: docs/architecture/pecor-sidiora-aggregator-spec.md §12 (S1, S8).
 */

const { expect } = require("chai");
const { ethers } = require("hardhat");

const { PECOR_ROLES } = require("../helpers/constants");
const { ERRORS } = require("../helpers/errors");
const { deployPecorFixture } = require("../helpers/fixtures");

describe("meta-ag/integration/8.5 — pecor-governance-timelock", function () {
  let fx;
  let admin, other;

  beforeEach(async function () {
    fx = await deployPecorFixture();
    ({ admin, other } = fx.signers);
  });

  // =========================================================================
  // S8 — DEFAULT_ADMIN_ROLE topology across 8 UUPS proxies
  // =========================================================================
  it("S8 — DEFAULT_ADMIN_ROLE held only by `admin` (Timelock proxy) on every UUPS contract", async function () {
    const proxies = [
      { name: "PriceOracle", c: fx.oracle },
      { name: "OracleHub", c: fx.hub },
      { name: "TransactionTracker", c: fx.tracker },
      { name: "PECORVault", c: fx.vault },
      { name: "PECOR", c: fx.pecor },
      { name: "PECOROrders", c: fx.pecorOrders },
      { name: "MetaAGRouter", c: fx.router },
      { name: "MetaAGQuoter", c: fx.quoter },
    ];
    expect(proxies.length).to.equal(8); // §12 S1 mandate

    for (const { name, c } of proxies) {
      expect(
        await c.hasRole(PECOR_ROLES.DEFAULT_ADMIN_ROLE, admin.address),
        `${name}: admin should hold DEFAULT_ADMIN_ROLE`
      ).to.equal(true);
      expect(
        await c.hasRole(PECOR_ROLES.DEFAULT_ADMIN_ROLE, other.address),
        `${name}: random EOA must NOT hold DEFAULT_ADMIN_ROLE`
      ).to.equal(false);
    }
  });

  it("S8 — DEFAULT_ADMIN_ROLE on immutable adapters held only by `admin`", async function () {
    expect(
      await fx.vaultAdapter.hasRole(PECOR_ROLES.DEFAULT_ADMIN_ROLE, admin.address)
    ).to.equal(true);
    expect(
      await fx.vaultAdapter.hasRole(PECOR_ROLES.DEFAULT_ADMIN_ROLE, other.address)
    ).to.equal(false);
    expect(
      await fx.sidioraAdapter.hasRole(PECOR_ROLES.DEFAULT_ADMIN_ROLE, admin.address)
    ).to.equal(true);
    expect(
      await fx.sidioraAdapter.hasRole(PECOR_ROLES.DEFAULT_ADMIN_ROLE, other.address)
    ).to.equal(false);
    expect(
      await fx.priceOracleAdapter.adapterName()
    ).to.equal("PaxeerPriceOracle.v1"); // sanity — adapter is wired
  });

  // =========================================================================
  // Per-proxy admin op gating: EOA reverts, Timelock succeeds.
  // =========================================================================

  it("MetaAGRouter — registerAdapter / setOracleHub / pause: EOA reverts, admin succeeds", async function () {
    // EOA reverts on any admin op
    await expect(
      fx.router.connect(other).registerAdapter(fx.priceOracleAdapter.target)
    ).to.be.revertedWithCustomError(fx.router, ERRORS.common.Unauthorized);

    await expect(
      fx.router.connect(other).setOracleHub(fx.hub.target)
    ).to.be.revertedWithCustomError(fx.router, ERRORS.common.Unauthorized);

    await expect(fx.router.connect(other).pause()).to.be.revertedWithCustomError(
      fx.router,
      ERRORS.common.Unauthorized
    );

    // Admin succeeds (idempotent setOracleHub + pause/unpause cycle)
    await fx.router.connect(admin).setOracleHub(fx.hub.target);
    await fx.router.connect(admin).pause();
    await fx.router.connect(admin).unpause();
  });

  it("PECORVault — setOperator / registerToken / emergencyWithdraw: EOA reverts, admin succeeds", async function () {
    await expect(
      fx.vault.connect(other).setOperator(other.address, true)
    ).to.be.revertedWithCustomError(fx.vault, ERRORS.common.Unauthorized);

    await expect(
      fx.vault.connect(other).registerToken(fx.tokens.sidA.target, false)
    ).to.be.revertedWithCustomError(fx.vault, ERRORS.common.Unauthorized);

    await expect(
      fx.vault
        .connect(other)
        .emergencyWithdraw(fx.tokens.usdl.target, 1n, other.address)
    ).to.be.revertedWithCustomError(fx.vault, ERRORS.common.Unauthorized);

    // Admin succeeds — flip operator on for `other`, register a fresh token
    await fx.vault.connect(admin).setOperator(other.address, true);
    expect(await fx.vault.authorizedOperators(other.address)).to.equal(true);

    const ERC20 = await ethers.getContractFactory("MockStandardERC20");
    const fresh = await ERC20.deploy("Fresh", "FRSH", 18);
    await fresh.waitForDeployment();
    await fx.vault.connect(admin).registerToken(fresh.target, false);
  });

  it("PriceOracle — setRelayer / registerToken / pause: EOA reverts, admin succeeds", async function () {
    await expect(
      fx.oracle.connect(other).setRelayer(other.address, true)
    ).to.be.revertedWithCustomError(fx.oracle, ERRORS.common.Unauthorized);

    const ONE = 10n ** 18n;
    await expect(
      fx.oracle
        .connect(other)
        .registerToken(other.address, 60, 100n, ONE / 100n, ONE * 1_000n, 3600)
    ).to.be.revertedWithCustomError(fx.oracle, ERRORS.common.Unauthorized);

    await expect(fx.oracle.connect(other).pause()).to.be.revertedWithCustomError(
      fx.oracle,
      ERRORS.common.Unauthorized
    );

    // Admin succeeds
    await fx.oracle.connect(admin).pause();
    await fx.oracle.connect(admin).unpause();
  });

  it("OracleHub — registerAdapter / setDeviationThreshold / pause: EOA reverts, admin succeeds", async function () {
    const ERC20 = await ethers.getContractFactory("MockStandardERC20");
    const ghost = await ERC20.deploy("Ghost", "GHST", 18);
    await ghost.waitForDeployment();

    await expect(
      fx.hub.connect(other).registerAdapter(ghost.target, 99)
    ).to.be.revertedWithCustomError(fx.hub, ERRORS.common.Unauthorized);

    await expect(
      fx.hub.connect(other).setDeviationThreshold(750n)
    ).to.be.revertedWithCustomError(fx.hub, ERRORS.common.Unauthorized);

    await expect(fx.hub.connect(other).pause()).to.be.revertedWithCustomError(
      fx.hub,
      ERRORS.common.Unauthorized
    );

    await fx.hub.connect(admin).setDeviationThreshold(750n);
    await fx.hub.connect(admin).pause();
    await fx.hub.connect(admin).unpause();
  });

  it("PECOROrders — setKeeper / pause: EOA reverts; admin succeeds & flips KEEPER_ROLE atomically", async function () {
    await expect(
      fx.pecorOrders.connect(other).setKeeper(other.address, true)
    ).to.be.revertedWithCustomError(fx.pecorOrders, ERRORS.common.Unauthorized);

    await expect(
      fx.pecorOrders.connect(other).pause()
    ).to.be.revertedWithCustomError(fx.pecorOrders, ERRORS.common.Unauthorized);

    await fx.pecorOrders.connect(admin).setKeeper(other.address, true);
    expect(
      await fx.pecorOrders.hasRole(PECOR_ROLES.KEEPER_ROLE, other.address)
    ).to.equal(true);

    await fx.pecorOrders.connect(admin).setKeeper(other.address, false);
    expect(
      await fx.pecorOrders.hasRole(PECOR_ROLES.KEEPER_ROLE, other.address)
    ).to.equal(false);
  });

  it("TransactionTracker — setAuthorizedEmitter: EOA reverts, admin succeeds & flips EMITTER_ROLE", async function () {
    await expect(
      fx.tracker.connect(other).setAuthorizedEmitter(other.address, true)
    ).to.be.revertedWithCustomError(fx.tracker, ERRORS.common.Unauthorized);

    await fx.tracker.connect(admin).setAuthorizedEmitter(other.address, true);
    expect(
      await fx.tracker.hasRole(PECOR_ROLES.EMITTER_ROLE, other.address)
    ).to.equal(true);
    expect(await fx.tracker.authorizedEmitters(other.address)).to.equal(true);
  });

  it("VaultAdapter / SidioraAdapter — setFee / setQuoter etc: EOA reverts, admin succeeds", async function () {
    await expect(
      fx.vaultAdapter.connect(other).setFee(50n)
    ).to.be.revertedWithCustomError(fx.vaultAdapter, ERRORS.common.Unauthorized);
    await fx.vaultAdapter.connect(admin).setFee(50n);
    expect(await fx.vaultAdapter.feeBps()).to.equal(50n);

    await expect(
      fx.sidioraAdapter.connect(other).setQuoter(fx.mocks.sidioraQuoter.target)
    ).to.be.revertedWithCustomError(
      fx.sidioraAdapter,
      ERRORS.common.Unauthorized
    );
    await fx.sidioraAdapter
      .connect(admin)
      .setQuoter(fx.mocks.sidioraQuoter.target);
  });

  // =========================================================================
  // S1 — UUPS upgrade authorization, witnessed via reverted bogus upgrade attempt
  // =========================================================================
  it("S1 — non-admin upgradeToAndCall reverts on every UUPS proxy", async function () {
    const proxies = [fx.oracle, fx.hub, fx.tracker, fx.vault, fx.pecor, fx.pecorOrders, fx.router, fx.quoter];
    const bogusImpl = ethers.getAddress("0x000000000000000000000000000000000000bEEF");
    for (const proxy of proxies) {
      await expect(
        proxy.connect(other).upgradeToAndCall(bogusImpl, "0x")
      ).to.be.revertedWithCustomError(proxy, ERRORS.common.Unauthorized);
    }
  });
});
