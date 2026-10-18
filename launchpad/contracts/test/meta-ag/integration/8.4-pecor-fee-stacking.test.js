/**
 * Sidiora Meta-AG — Phase 8 Integration Test 8.4
 *
 * Numerical regression for spec §8.3 (Fee Stacking Policy, INVARIANT S11):
 *
 *   - Sidiora pool fee  = 30 bps
 *   - VaultAdapter fee  = 20 bps
 *   - MetaAGRouter fee  = 0 bps
 *
 * Three scenarios encoded by the spec table:
 *   (a) 1,000 USDL → Sidiora Token X            → 30 bps total
 *   (b) 1,000 USDC → WPAX                       → 20 bps total
 *   (c) 1,000 USDC → USDL → Sidiora Token X     → 50 bps aggregate (20 + 30)
 *
 * Spec: docs/architecture/pecor-sidiora-aggregator-spec.md §8.3.
 *
 * Invariant witness: MetaAGRouter NEVER inflates the per-hop adapter fees.
 * Aggregate fee == sum of per-hop adapter fees.
 */

const { expect } = require("chai");
const { ethers } = require("hardhat");

const { ADAPTER_IDS } = require("../helpers/constants");
const { deployPecorFixture } = require("../helpers/fixtures");

const ONE = 10n ** 18n;
const BPS = 10_000n;

/** Stage Sidiora BUY (USDL → tokenOut) with a 30-bps pool fee. */
async function stageSidioraBuyWithPoolFee(
  fx,
  tokenOut,
  amountInUsdl,
  oraclePriceTokenOut
) {
  // Gross output = amountIn × $1 / priceOut. Fee = 30 bps. Net = gross × (1 - 0.003).
  const gross = (amountInUsdl * ONE) / oraclePriceTokenOut;
  const fee = (gross * 30n) / BPS;
  const net = gross - fee;
  await fx.mocks.sidioraQuoter.setBuyQuote(net, fee, 50n);
  await fx.mocks.sidioraRouter.setBuyTokenOut(tokenOut.target);
  await fx.mocks.sidioraRouter.setBuyReturn(net);
  await tokenOut.mint(fx.mocks.sidioraRouter.target, net);
  return { gross, fee, net };
}

describe("meta-ag/integration/8.4 — pecor-fee-stacking (S11)", function () {
  let fx;
  let user;
  let usdl, usdc, wpax, sidA;
  let router, vaultAdapter;

  beforeEach(async function () {
    fx = await deployPecorFixture();
    ({ user } = fx.signers);
    ({ usdl, usdc, wpax, sidA } = fx.tokens);
    ({ router, vaultAdapter } = fx);

    // Spec §8.3 fee config.
    await vaultAdapter.connect(fx.signers.admin).setFee(20n); // VaultAdapter = 20 bps
    // MetaAGRouter has NO levy — there's no `setFee` on MetaAGRouter (S11
    // structurally guaranteed by the contract surface).
  });

  // =========================================================================
  // Case (a) — Sidiora-only path, 30 bps total
  // =========================================================================
  it("(a) 1,000 USDL → sidA via Sidiora pool: aggregate fee == 30 bps (Sidiora pool only)", async function () {
    const amountIn = 1_000n * ONE;
    const sidAPrice = ONE / 2n; // $0.50
    const { net } = await stageSidioraBuyWithPoolFee(fx, sidA, amountIn, sidAPrice);

    // Expected gross at oracle prices: 1000 USDL × $1 / $0.50 = 2000 sidA.
    // After 30 bps Sidiora fee: 2000 × 9970 / 10000 = 1994 sidA.
    const oracleGross = 2_000n * ONE;
    const expectedNet = (oracleGross * 9970n) / BPS;
    expect(net).to.equal(expectedNet);
    expect(net).to.equal(1994n * ONE);

    await usdl.mint(user.address, amountIn);
    await usdl.connect(user).approve(router.target, amountIn);
    const before = await sidA.balanceOf(user.address);

    await router
      .connect(user)
      .swapBestRoute(usdl.target, sidA.target, amountIn, expectedNet, 0);

    const after = await sidA.balanceOf(user.address);
    const realized = after - before;
    // Aggregate fee in bps:
    const aggFeeBps = ((oracleGross - realized) * BPS) / oracleGross;
    expect(aggFeeBps).to.equal(30n);
    expect(realized).to.equal(expectedNet);
  });

  // =========================================================================
  // Case (b) — Vault-only path, 20 bps total
  // =========================================================================
  it("(b) 1,000 USDC → WPAX via VaultAdapter: aggregate fee == 20 bps (VaultAdapter only)", async function () {
    const amountIn = 1_000n * ONE;
    // Gross at oracle prices: 1000 USDC × $1 / $2 = 500 WPAX.
    // VaultAdapter 20 bps fee: 500 × 9980 / 10000 = 499 WPAX.
    const oracleGross = 500n * ONE;
    const expectedNet = (oracleGross * 9980n) / BPS;
    expect(expectedNet).to.equal(499n * ONE);

    await usdc.mint(user.address, amountIn);
    await usdc.connect(user).approve(router.target, amountIn);
    const before = await wpax.balanceOf(user.address);

    await router
      .connect(user)
      .swapBestRoute(usdc.target, wpax.target, amountIn, expectedNet, 0);

    const realized = (await wpax.balanceOf(user.address)) - before;
    const aggFeeBps = ((oracleGross - realized) * BPS) / oracleGross;
    expect(aggFeeBps).to.equal(20n);
    expect(realized).to.equal(expectedNet);
  });

  // =========================================================================
  // Case (c) — Multi-hop USDC → USDL → sidA, 50 bps aggregate
  // =========================================================================
  it("(c) 1,000 USDC → USDL → sidA via [Vault, Sidiora]: aggregate fee == 50 bps (20 + 30)", async function () {
    const amountIn = 1_000n * ONE;

    // Hop 1: VaultAdapter USDC → USDL @ 20 bps
    //   Gross: 1000 × $1 / $1 = 1000 USDL. Net: 998 USDL.
    const hop1Net = (1_000n * ONE * 9980n) / BPS;
    expect(hop1Net).to.equal(998n * ONE);

    // Hop 2: SidioraAdapter USDL → sidA @ 30 bps
    //   Gross: 998 × $1 / $0.50 = 1996 sidA. Net: 1996 × 9970 / 10000 = 1990.012 sidA.
    const hop2Gross = (hop1Net * ONE) / (ONE / 2n); // == hop1Net * 2
    const hop2Net = (hop2Gross * 9970n) / BPS;
    // Stage Sidiora mock to return exactly hop2Net.
    await fx.mocks.sidioraQuoter.setBuyQuote(hop2Net, hop2Gross - hop2Net, 50n);
    await fx.mocks.sidioraRouter.setBuyTokenOut(sidA.target);
    await fx.mocks.sidioraRouter.setBuyReturn(hop2Net);
    await sidA.mint(fx.mocks.sidioraRouter.target, hop2Net);

    await usdc.mint(user.address, amountIn);
    await usdc.connect(user).approve(router.target, amountIn);
    const before = await sidA.balanceOf(user.address);

    const hops = [
      {
        adapterId: ADAPTER_IDS.VAULT,
        tokenIn: usdc.target,
        tokenOut: usdl.target,
        minAmountOut: hop1Net,
      },
      {
        adapterId: ADAPTER_IDS.SIDIORA,
        tokenIn: usdl.target,
        tokenOut: sidA.target,
        minAmountOut: hop2Net,
      },
    ];

    await router.connect(user).swapMultiHop(hops, amountIn, hop2Net, 0);

    const realized = (await sidA.balanceOf(user.address)) - before;
    expect(realized).to.equal(hop2Net);

    // Aggregate fee: oracle-implied gross for the full chain is
    //   1000 USDC × $1 / $0.50 = 2000 sidA.
    // Aggregate fee bps = (2000 - 1990.012) / 2000 = 4.994 bps × 10 = 49.94 bps,
    // which rounds to 50 bps when expressed cumulatively (20 + 30, additive).
    const oracleGross = 2_000n * ONE;
    const aggFeeBps = ((oracleGross - realized) * BPS) / oracleGross;
    // 49 (4.994e-3 * 1e4 floored) is the correct truncated bps.
    // Spec §8.3 invariant: aggregate ≈ sum of per-hop fees, within ±1 bps
    // of rounding. We assert the bound.
    expect(aggFeeBps).to.be.gte(49n);
    expect(aggFeeBps).to.be.lte(50n);
  });
});
