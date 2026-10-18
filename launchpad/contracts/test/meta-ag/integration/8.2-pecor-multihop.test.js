/**
 * Sidiora Meta-AG — Phase 8 Integration Test 8.2
 *
 * Multi-hop routing through MetaAGRouter.swapMultiHop. Exercises:
 *   - 3-hop chain: sidA → USDL → USDC → WPAX
 *     (SidioraAdapter SELL → VaultAdapter → VaultAdapter)
 *   - S3: per-hop re-quote uses the ACTUAL intermediate amount, not the
 *     first quote's projection. We deliberately quote stale (bigger) and
 *     verify the realized output reflects the smaller actual intermediate.
 *   - MAX_HOPS = 5 boundary (5 hops succeed, 6 hops revert MaxHopsExceeded).
 *   - TooFewHops < 2 revert.
 *
 * Spec: docs/architecture/pecor-sidiora-aggregator-spec.md §10.2, §12 S3.
 *
 * Invariants newly witnessed by 8.2:
 *   - S3   per-hop re-quote against the actual intermediate amount
 *   - S9   approval reset across every hop, all adapters end with zero allowance
 *   - VaultAdapter port-fix: works as hop 2 + hop 3 (different tokenIn each hop)
 *   - MAX_HOPS = 5 boundary
 */

const { expect } = require("chai");
const { ethers } = require("hardhat");

const { ADAPTER_IDS } = require("../helpers/constants");
const { ERRORS } = require("../helpers/errors");
const { deployPecorFixture } = require("../helpers/fixtures");

const ONE = 10n ** 18n;

async function stageSidioraSell(fx, sidToken, amountIn, amountOut) {
  await fx.mocks.sidioraQuoter.setSellQuote(amountOut, 0n, 30n);
  await fx.mocks.sidioraRouter.setSellTokenIn(sidToken.target);
  await fx.mocks.sidioraRouter.setSellReturn(amountOut);
}

describe("meta-ag/integration/8.2 — pecor-multihop", function () {
  let fx;
  let user;
  let usdl, usdc, usdt, wpax, sidA;
  let router, vaultAdapter, sidioraAdapter;

  beforeEach(async function () {
    fx = await deployPecorFixture();
    ({ user } = fx.signers);
    ({ usdl, usdc, usdt, wpax, sidA } = fx.tokens);
    ({ router, vaultAdapter, sidioraAdapter } = fx);
  });

  // =========================================================================
  // 3-HOP — sidA → USDL → USDC → WPAX
  // =========================================================================
  it("3-hop chain (Sidiora SELL → Vault → Vault) re-quotes between hops and clears oracle sanity end-to-end", async function () {
    const amountIn = 200n * ONE; // 200 sidA
    const intermediateUsdl = 100n * ONE; // 200 sidA × $0.50 = $100
    const intermediateUsdc = 100n * ONE; // $100 → $100 USDC at 1:1, 0 bps fee
    const finalWpax = 50n * ONE; // $100 → 50 WPAX (WPAX = $2)
    await stageSidioraSell(fx, sidA, amountIn, intermediateUsdl);

    await sidA.mint(user.address, amountIn);
    await sidA.connect(user).approve(router.target, amountIn);

    const sidABefore = await sidA.balanceOf(user.address);
    const wpaxBefore = await wpax.balanceOf(user.address);

    const hops = [
      {
        adapterId: ADAPTER_IDS.SIDIORA,
        tokenIn: sidA.target,
        tokenOut: usdl.target,
        minAmountOut: intermediateUsdl,
      },
      {
        adapterId: ADAPTER_IDS.VAULT,
        tokenIn: usdl.target,
        tokenOut: usdc.target,
        minAmountOut: intermediateUsdc,
      },
      {
        adapterId: ADAPTER_IDS.VAULT,
        tokenIn: usdc.target,
        tokenOut: wpax.target,
        minAmountOut: finalWpax,
      },
    ];

    await expect(
      router.connect(user).swapMultiHop(hops, amountIn, finalWpax, 0)
    )
      .to.emit(router, "MultiHopSwap")
      .withArgs(user.address, sidA.target, wpax.target, amountIn, finalWpax, 3n);

    expect(await sidA.balanceOf(user.address)).to.equal(sidABefore - amountIn);
    expect(await wpax.balanceOf(user.address)).to.equal(wpaxBefore + finalWpax);
    // Router clean
    expect(await sidA.balanceOf(router.target)).to.equal(0n);
    expect(await usdl.balanceOf(router.target)).to.equal(0n);
    expect(await usdc.balanceOf(router.target)).to.equal(0n);
    expect(await wpax.balanceOf(router.target)).to.equal(0n);
    // S9 — every per-hop allowance ends at zero
    expect(await sidA.allowance(router.target, sidioraAdapter.target)).to.equal(0n);
    expect(await usdl.allowance(router.target, vaultAdapter.target)).to.equal(0n);
    expect(await usdc.allowance(router.target, vaultAdapter.target)).to.equal(0n);
    // Vault adapter holds zero dust on every leg
    expect(await usdl.allowance(vaultAdapter.target, fx.vault.target)).to.equal(0n);
    expect(await usdc.allowance(vaultAdapter.target, fx.vault.target)).to.equal(0n);
  });

  // =========================================================================
  // S3 — per-hop re-quote uses actual intermediate, not first quote
  // =========================================================================
  it("S3 — hop 2 re-quotes against the actual hop 1 output (stale projection ignored)", async function () {
    // Configure the Sidiora mock to return SLIGHTLY LESS than the quoter
    // advertises (within the 5% oracle sanity band): quoter says 100 USDL,
    // router pays 98 USDL. Hop 2 must re-quote on 98 — NOT trust the 100
    // projection — and route 98 USDC through the vault.
    const amountIn = 200n * ONE;
    const advertised = 100n * ONE;
    const actual = 98n * ONE;
    await fx.mocks.sidioraQuoter.setSellQuote(advertised, 0n, 30n);
    await fx.mocks.sidioraRouter.setSellTokenIn(sidA.target);
    await fx.mocks.sidioraRouter.setSellReturn(actual);

    await sidA.mint(user.address, amountIn);
    await sidA.connect(user).approve(router.target, amountIn);

    const expectedHop2Out = 98n * ONE; // 98 USDL → 98 USDC (1:1, 0 bps)
    const usdcBefore = await usdc.balanceOf(user.address);

    const hops = [
      {
        adapterId: ADAPTER_IDS.SIDIORA,
        tokenIn: sidA.target,
        tokenOut: usdl.target,
        minAmountOut: actual, // honest min for hop 1 reflects actual mock return
      },
      {
        adapterId: ADAPTER_IDS.VAULT,
        tokenIn: usdl.target,
        tokenOut: usdc.target,
        minAmountOut: expectedHop2Out,
      },
    ];

    await router.connect(user).swapMultiHop(hops, amountIn, expectedHop2Out, 0);
    expect(await usdc.balanceOf(user.address)).to.equal(usdcBefore + expectedHop2Out);
  });

  // =========================================================================
  // MAX_HOPS = 5 — boundary
  // =========================================================================
  it("MAX_HOPS = 5 — exactly 5 hops succeed (USDL → USDC → USDT → USDC → USDT → USDL)", async function () {
    const amountIn = 100n * ONE;
    await usdl.mint(user.address, amountIn);
    await usdl.connect(user).approve(router.target, amountIn);

    // 5 vault hops, each token at $1 with 0 bps fee → straight 100 USDL through.
    const hops = [
      {
        adapterId: ADAPTER_IDS.VAULT,
        tokenIn: usdl.target,
        tokenOut: usdc.target,
        minAmountOut: amountIn,
      },
      {
        adapterId: ADAPTER_IDS.VAULT,
        tokenIn: usdc.target,
        tokenOut: usdt.target,
        minAmountOut: amountIn,
      },
      {
        adapterId: ADAPTER_IDS.VAULT,
        tokenIn: usdt.target,
        tokenOut: usdc.target,
        minAmountOut: amountIn,
      },
      {
        adapterId: ADAPTER_IDS.VAULT,
        tokenIn: usdc.target,
        tokenOut: usdt.target,
        minAmountOut: amountIn,
      },
      {
        adapterId: ADAPTER_IDS.VAULT,
        tokenIn: usdt.target,
        tokenOut: usdl.target,
        minAmountOut: amountIn,
      },
    ];

    const usdlBefore = await usdl.balanceOf(user.address);
    await router.connect(user).swapMultiHop(hops, amountIn, amountIn, 0);
    // Net effect: amountIn USDL out, amountIn USDL back = unchanged
    expect(await usdl.balanceOf(user.address)).to.equal(usdlBefore);
  });

  it("MAX_HOPS = 5 — 6 hops revert MaxHopsExceeded", async function () {
    const amountIn = 100n * ONE;
    await usdl.mint(user.address, amountIn);
    await usdl.connect(user).approve(router.target, amountIn);

    const sixHops = Array.from({ length: 6 }, () => ({
      adapterId: ADAPTER_IDS.VAULT,
      tokenIn: usdl.target,
      tokenOut: usdc.target,
      minAmountOut: 0n,
    }));

    await expect(
      router.connect(user).swapMultiHop(sixHops, amountIn, 0n, 0)
    ).to.be.revertedWithCustomError(router, ERRORS.router.MaxHopsExceeded);
  });

  it("TooFewHops — single-hop array reverts", async function () {
    const amountIn = 100n * ONE;
    await usdl.mint(user.address, amountIn);
    await usdl.connect(user).approve(router.target, amountIn);

    await expect(
      router.connect(user).swapMultiHop(
        [
          {
            adapterId: ADAPTER_IDS.VAULT,
            tokenIn: usdl.target,
            tokenOut: usdc.target,
            minAmountOut: amountIn,
          },
        ],
        amountIn,
        amountIn,
        0
      )
    ).to.be.revertedWithCustomError(router, "TooFewHops");
  });
});
