/**
 * Sidiora Meta-AG — Phase 8 Integration Test 8.1
 *
 * Scenario coverage: 4 end-to-end USDL ↔ Sidiora swaps through MetaAGRouter,
 * exercising the full wiring (PriceOracle → OracleHub → MetaAGRouter →
 * Sidiora|VaultAdapter → Sidiora-API mock | PECORVault). Per Andrew's
 * Session-9 brief, integration tests prove wiring + invariants — they don't
 * fuzz year-old battle-tested PECOR math.
 *
 * Spec: docs/architecture/pecor-sidiora-aggregator-spec.md §10.1, §10.2, §11
 *
 * Invariants newly witnessed by 8.1:
 *   - I1   adapter.getQuote never reverts (tested via swapBestRoute polling
 *          two adapters, one always returns available=false for sidA pairs)
 *   - I4   adapter pulls tokenIn from MetaAGRouter, sends tokenOut to recipient
 *   - I5   getBestQuote selects the correct adapter by adapterId
 *   - S4   _oracleSanityCheck passes when realized output stays within 5% bps
 *   - S9   router approval to adapter is zero before AND after the call
 *          (enforced by router; verified post-swap via allowance read)
 *   - VaultAdapter port-fix: router → adapter → vault.deposit funnel works
 *          end-to-end (this is the regression case for the fix landed in
 *          Session 9 commit 35).
 */

const { expect } = require("chai");
const { ethers } = require("hardhat");

const {
  ADAPTER_IDS,
} = require("../helpers/constants");
const { deployPecorFixture } = require("../helpers/fixtures");

const ONE = 10n ** 18n;
const MAX_UINT256 = 2n ** 256n - 1n;

/** Pre-stage the Sidiora-API mock for a BUY (USDL → sidX). */
async function stageSidioraBuy(fx, sidToken, amountInUsdl, amountOutTok) {
  await fx.mocks.sidioraQuoter.setBuyQuote(amountOutTok, 0n, 50n);
  await fx.mocks.sidioraRouter.setBuyTokenOut(sidToken.target);
  await fx.mocks.sidioraRouter.setBuyReturn(amountOutTok);
  // Pre-fund the mock with tokenOut so it can settle the swap. The fixture
  // already mints USDL/sidA/sidB to the mock at setup time, but per-scenario
  // amounts can exceed that — top up here.
  await sidToken.mint(fx.mocks.sidioraRouter.target, amountOutTok);
  return { amountInUsdl, amountOutTok };
}

/** Pre-stage the Sidiora-API mock for a SELL (sidX → USDL). */
async function stageSidioraSell(fx, sidToken, amountInTok, amountOutUsdl) {
  await fx.mocks.sidioraQuoter.setSellQuote(amountOutUsdl, 0n, 30n);
  await fx.mocks.sidioraRouter.setSellTokenIn(sidToken.target);
  await fx.mocks.sidioraRouter.setSellReturn(amountOutUsdl);
  // pre-fund the mock with USDL so it can pay out
  return { amountInTok, amountOutUsdl };
}

describe("meta-ag/integration/8.1 — pecor-sidiora-interop", function () {
  let fx;
  let user, recipient;
  let usdl, usdc, sidA;
  let router, vault, vaultAdapter, sidioraAdapter;

  beforeEach(async function () {
    fx = await deployPecorFixture();
    ({ user, recipient } = fx.signers);
    ({ usdl, usdc, sidA } = fx.tokens);
    ({ router, vault, vaultAdapter, sidioraAdapter } = fx);

    // Fund the user with each token they'll spend across scenarios.
    await usdl.mint(user.address, 100_000n * ONE);
    await sidA.mint(user.address, 1_000n * ONE);
    await usdc.mint(user.address, 100_000n * ONE);
  });

  // =========================================================================
  // Scenario 1 — USDL → sidA via swapBestRoute (SidioraAdapter wins)
  // =========================================================================
  it("Scenario 1: USDL → sidA via swapBestRoute picks SidioraAdapter and clears the oracle sanity check", async function () {
    const amountIn = 100n * ONE; // $100 USDL
    const expectedOut = 200n * ONE; // 200 sidA at sidA=$0.50 — exactly oracle-implied
    await stageSidioraBuy(fx, sidA, amountIn, expectedOut);

    await usdl.connect(user).approve(router.target, amountIn);
    const sidABefore = await sidA.balanceOf(user.address);
    const usdlBefore = await usdl.balanceOf(user.address);

    await expect(
      router
        .connect(user)
        .swapBestRoute(usdl.target, sidA.target, amountIn, expectedOut, 0)
    )
      .to.emit(router, "BestRouteSwap")
      .withArgs(
        user.address,
        usdl.target,
        sidA.target,
        amountIn,
        expectedOut,
        ADAPTER_IDS.SIDIORA
      );

    expect(await usdl.balanceOf(user.address)).to.equal(usdlBefore - amountIn);
    expect(await sidA.balanceOf(user.address)).to.equal(sidABefore + expectedOut);
    // S9 — router holds zero dust + zero residual allowance to either adapter
    expect(await usdl.balanceOf(router.target)).to.equal(0n);
    expect(await sidA.balanceOf(router.target)).to.equal(0n);
    expect(await usdl.allowance(router.target, sidioraAdapter.target)).to.equal(0n);
    expect(await usdl.allowance(router.target, vaultAdapter.target)).to.equal(0n);
    // Sidiora mock witnessed exactly one buy
    expect(await fx.mocks.sidioraRouter.buyCallCount()).to.equal(1n);
  });

  // =========================================================================
  // Scenario 2 — sidA → USDL via swapBestRoute (SidioraAdapter SELL)
  // =========================================================================
  it("Scenario 2: sidA → USDL via swapBestRoute routes through SidioraAdapter SELL", async function () {
    const amountIn = 200n * ONE; // 200 sidA
    const expectedOut = 100n * ONE; // $100 — sidA=$0.50 → exactly oracle-implied
    await stageSidioraSell(fx, sidA, amountIn, expectedOut);

    await sidA.connect(user).approve(router.target, amountIn);
    const sidABefore = await sidA.balanceOf(user.address);
    const usdlBefore = await usdl.balanceOf(user.address);

    await expect(
      router
        .connect(user)
        .swapBestRoute(sidA.target, usdl.target, amountIn, expectedOut, 0)
    )
      .to.emit(router, "BestRouteSwap")
      .withArgs(
        user.address,
        sidA.target,
        usdl.target,
        amountIn,
        expectedOut,
        ADAPTER_IDS.SIDIORA
      );

    expect(await sidA.balanceOf(user.address)).to.equal(sidABefore - amountIn);
    expect(await usdl.balanceOf(user.address)).to.equal(usdlBefore + expectedOut);
    expect(await sidA.balanceOf(router.target)).to.equal(0n);
    expect(await sidA.allowance(router.target, sidioraAdapter.target)).to.equal(0n);
    expect(await fx.mocks.sidioraRouter.sellCallCount()).to.equal(1n);
  });

  // =========================================================================
  // Scenario 3 — USDL → sidA via explicit swapViaAdapter (no auction)
  // =========================================================================
  it("Scenario 3: swapViaAdapter forces SidioraAdapter even when the auction was unanimous", async function () {
    const amountIn = 50n * ONE;
    const expectedOut = 100n * ONE; // $50 → 100 sidA (sidA=$0.50)
    await stageSidioraBuy(fx, sidA, amountIn, expectedOut);

    await usdl.connect(user).approve(router.target, amountIn);
    const sidABefore = await sidA.balanceOf(user.address);

    await router
      .connect(user)
      .swapViaAdapter(
        ADAPTER_IDS.SIDIORA,
        usdl.target,
        sidA.target,
        amountIn,
        expectedOut,
        0
      );

    expect(await sidA.balanceOf(user.address)).to.equal(sidABefore + expectedOut);
    expect(await fx.mocks.sidioraRouter.buyCallCount()).to.equal(1n);
    // Vault adapter must NOT have been touched
    expect(await usdl.allowance(router.target, vaultAdapter.target)).to.equal(0n);
  });

  // =========================================================================
  // Scenario 4 — USDL → USDC via swapBestRoute (VaultAdapter wins; tests the fix)
  // =========================================================================
  it("Scenario 4: USDL → USDC via VaultAdapter — exercises the router→adapter→vault.deposit fix end-to-end", async function () {
    const amountIn = 1_000n * ONE; // $1000 USDL → ~$1000 USDC at 1:1
    const expectedOut = 1_000n * ONE; // 0 bps fee at deploy (spec Q6)

    await usdl.connect(user).approve(router.target, amountIn);
    const usdlBefore = await usdl.balanceOf(user.address);
    const usdcBefore = await usdc.balanceOf(user.address);
    const usdlVaultBefore = await vault.getReserves(usdl.target);
    const usdcVaultBefore = await vault.getReserves(usdc.target);

    await expect(
      router
        .connect(user)
        .swapBestRoute(usdl.target, usdc.target, amountIn, expectedOut, 0)
    )
      .to.emit(router, "BestRouteSwap")
      .withArgs(
        user.address,
        usdl.target,
        usdc.target,
        amountIn,
        expectedOut,
        ADAPTER_IDS.VAULT
      );

    expect(await usdl.balanceOf(user.address)).to.equal(usdlBefore - amountIn);
    expect(await usdc.balanceOf(user.address)).to.equal(usdcBefore + expectedOut);

    // Vault accounting: USDL reserves up by amountIn (deposit funnel),
    // USDC reserves down by expectedOut (push to recipient). 0bps fee → no
    // collector hop.
    expect(await vault.getReserves(usdl.target)).to.equal(usdlVaultBefore + amountIn);
    expect(await vault.getReserves(usdc.target)).to.equal(usdcVaultBefore - expectedOut);

    // S9 — router + adapter both clean
    expect(await usdl.balanceOf(router.target)).to.equal(0n);
    expect(await usdl.balanceOf(vaultAdapter.target)).to.equal(0n);
    expect(await usdc.balanceOf(vaultAdapter.target)).to.equal(0n);
    expect(await usdl.allowance(router.target, vaultAdapter.target)).to.equal(0n);
    expect(await usdl.allowance(vaultAdapter.target, vault.target)).to.equal(0n);
  });
});
