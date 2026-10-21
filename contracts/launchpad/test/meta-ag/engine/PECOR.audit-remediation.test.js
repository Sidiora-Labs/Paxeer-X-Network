// HyperPaxeer — © Paxlabs Inc 2026
const { expect } = require("chai");
const { ethers } = require("hardhat");
const { deployPECORVault, deployPECOR, deployPECOROrders, deployTransactionTracker } = require("../helpers/fixtures");
const ONE = 10n ** 18n;

async function fixture({ inputTax = 0, reserves = 1000n * ONE } = {}) {
  const [admin, user, trader, keeper] = await ethers.getSigners();
  const weth = await (await ethers.getContractFactory("MockWETH9")).deploy();
  const oracle = await (await ethers.getContractFactory("PECORAuditOracle")).deploy();
  const vault = await deployPECORVault({ weth: weth.target, admin: admin.address });
  const input = inputTax
    ? await (await ethers.getContractFactory("MockFeeOnTransferERC20")).deploy("Input", "IN", 18, inputTax)
    : await (await ethers.getContractFactory("MockStandardERC20")).deploy("Input", "IN", 18);
  const output = await (await ethers.getContractFactory("MockStandardERC20")).deploy("Output", "OUT", 18);
  for (const [token, stable] of [[input, true], [output, false], [weth, false]]) {
    await vault.registerToken(token.target, stable);
    await oracle.setPrice(token.target, ONE);
  }
  const pecor = await deployPECOR({ priceOracle: oracle.target, vault: vault.target, weth: weth.target, admin: admin.address });
  const orders = await deployPECOROrders({ priceOracle: oracle.target, vault: vault.target, admin: admin.address });
  await vault.setOperator(pecor.target, true);
  await vault.setOperator(orders.target, true);
  await orders.setKeeper(keeper.address, true);
  await pecor.setPriceImpact(false, 0);
  await pecor.setTieredFees(0, 0);
  await orders.setPricingConfig(0, 0, 0, false, 0);
  if (reserves) {
    await output.mint(admin.address, reserves);
    await output.approve(vault.target, reserves);
    await vault.deposit(output.target, reserves);
  }
  for (const account of [user, trader]) {
    await input.mint(account.address, 1000n * ONE);
    await output.mint(account.address, 1000n * ONE);
    await input.connect(account).approve(vault.target, ethers.MaxUint256);
    await output.connect(account).approve(vault.target, ethers.MaxUint256);
  }
  return { admin, user, trader, keeper, weth, oracle, vault, input, output, pecor, orders };
}

async function assertBacked(vault, token, held) {
  const reserves = await vault.getReserves(token.target);
  const reserved = await vault.getReservedBalance(token.target);
  expect(await token.balanceOf(vault.target)).to.equal(reserves);
  expect(reserved).to.equal(held);
  expect(await vault.getAvailableReserves(token.target)).to.equal(reserves - reserved);
}

describe("PECOR shared-vault audit remediation", function () {
  it("keeps escrow unavailable to a competing swap and cancellation available while paused", async function () {
    const e = await fixture();
    await e.orders.connect(e.user).placeLimitBuy(e.input.target, e.output.target, 100n * ONE, ONE, 0);
    await assertBacked(e.vault, e.input, 100n * ONE);
    expect(await e.vault.getAvailableReserves(e.input.target)).to.equal(0n);
    await expect(e.pecor.connect(e.trader).swapExactIn(e.output.target, e.input.target, ONE, 0, 0)).to.be.revertedWithCustomError(e.pecor, "InsufficientLiquidity");
    await e.orders.pause();
    await expect(e.orders.connect(e.keeper).executeLimitOrder(1)).to.be.revertedWithCustomError(e.orders, "Paused");
    const before = await e.input.balanceOf(e.user.address);
    await e.orders.connect(e.user).cancelLimitOrder(1);
    expect(await e.input.balanceOf(e.user.address)).to.equal(before + 100n * ONE);
    await assertBacked(e.vault, e.input, 0n);
  });

  for (const tax of [0, 1000, 2500]) {
    it(`prices exact input using measured custody at ${tax} bps tax`, async function () {
      const e = await fixture({ inputTax: tax });
      const nominal = 100n * ONE;
      const received = nominal * BigInt(10000 - tax) / 10000n;
      const before = await e.output.balanceOf(e.user.address);
      await e.pecor.connect(e.user).swapExactIn(e.input.target, e.output.target, nominal, received, 0);
      expect(await e.output.balanceOf(e.user.address)).to.equal(before + received);
      expect(await e.vault.getReserves(e.input.target)).to.equal(received);
    });
  }

  for (const route of ["marketBuy", "marketSell", "nativeOutput"]) {
    it(`uses measured tax input for ${route}`, async function () {
      const e = await fixture({ inputTax: 1000 });
      if (route === "marketBuy") {
        const before = await e.output.balanceOf(e.user.address);
        await e.pecor.connect(e.user).marketBuy(e.input.target, e.output.target, 100n * ONE, 90n * ONE, 0);
        expect(await e.output.balanceOf(e.user.address)).to.equal(before + 90n * ONE);
      } else if (route === "marketSell") {
        await e.vault.setStablecoinStatus(e.input.target, false);
        await e.vault.setStablecoinStatus(e.output.target, true);
        const before = await e.output.balanceOf(e.user.address);
        await e.pecor.connect(e.user).marketSell(e.input.target, e.output.target, 100n * ONE, 90n * ONE, 0);
        expect(await e.output.balanceOf(e.user.address)).to.equal(before + 90n * ONE);
      } else {
        await e.vault.depositNative({ value: 100n * ONE });
        const before = await e.vault.getReserves(e.weth.target);
        await e.pecor.connect(e.user).swapExactInToNative(e.input.target, 100n * ONE, 90n * ONE, 0);
        expect(await e.vault.getReserves(e.weth.target)).to.equal(before - 90n * ONE);
      }
      expect(await e.vault.getReserves(e.input.target)).to.equal(90n * ONE);
    });
  }

  it("rejects taxed output delivery and rolls back received input", async function () {
    const e = await fixture();
    const taxed = await (await ethers.getContractFactory("MockFeeOnTransferERC20")).deploy("Taxed", "TAX", 18, 0);
    await e.vault.registerToken(taxed.target, false);
    await e.oracle.setPrice(taxed.target, ONE);
    await taxed.mint(e.admin.address, 100n * ONE);
    await taxed.approve(e.vault.target, 100n * ONE);
    await e.vault.deposit(taxed.target, 100n * ONE);
    await taxed.setFeeBps(1000);
    const inputBefore = await e.input.balanceOf(e.user.address);
    await expect(e.pecor.connect(e.user).swapExactIn(e.input.target, taxed.target, 10n * ONE, 9n * ONE, 0)).to.be.revertedWithCustomError(e.vault, "UnsupportedTransfer");
    expect(await e.input.balanceOf(e.user.address)).to.equal(inputBefore);
    await assertBacked(e.vault, taxed, 0n);
  });

  it("stores taxed order escrow at actual received amount and rejects taxed refund delivery atomically", async function () {
    const e = await fixture({ inputTax: 1000 });
    await e.orders.connect(e.user).placeLimitBuy(e.input.target, e.output.target, 100n * ONE, ONE, 0);
    expect((await e.orders.getLimitOrder(1)).amount).to.equal(90n * ONE);
    await assertBacked(e.vault, e.input, 90n * ONE);
    await expect(e.orders.connect(e.user).cancelLimitOrder(1)).to.be.revertedWithCustomError(e.vault, "UnsupportedTransfer");
    expect((await e.orders.getLimitOrder(1)).status).to.equal(0n);
    await e.input.setFeeBps(0);
    await e.orders.connect(e.user).cancelLimitOrder(1);
    await assertBacked(e.vault, e.input, 0n);
  });

  it("rejects exact-output taxed input without paying output or changing book/escrow", async function () {
    const e = await fixture({ inputTax: 1000 });
    const before = await e.output.balanceOf(e.user.address);
    await expect(e.pecor.connect(e.user).swapExactOut(e.input.target, e.output.target, 90n * ONE, 100n * ONE, 0)).to.be.revertedWithCustomError(e.pecor, "UnsupportedInputTransfer");
    expect(await e.output.balanceOf(e.user.address)).to.equal(before);
    await assertBacked(e.vault, e.input, 0n);
  });

  it("reserves exact-in fees, blocks competing consumption, and collects backed fees", async function () {
    const e = await fixture({ reserves: 100n * ONE });
    await e.pecor.setSwapFee(200);
    await e.pecor.setFeeCollector(e.admin.address);
    await e.pecor.connect(e.user).swapExactIn(e.input.target, e.output.target, 100n * ONE, 98n * ONE, 0);
    await assertBacked(e.vault, e.output, 2n * ONE);
    expect(await e.vault.getAvailableReserves(e.output.target)).to.equal(0n);
    await expect(e.pecor.connect(e.trader).swapExactIn(e.input.target, e.output.target, ONE, 0, 0)).to.be.reverted;
    const before = await e.output.balanceOf(e.admin.address);
    await e.pecor.collectFees(e.output.target);
    expect(await e.output.balanceOf(e.admin.address)).to.equal(before + 2n * ONE);
    await assertBacked(e.vault, e.output, 0n);
  });

  it("exact-output inversion preserves the smallest valid input before a discrete impact step", async function () {
    const e = await fixture({ reserves: 10_000_100n });
    await e.pecor.setPriceImpact(true, 100);
    const target = 99_996n;
    expect(await e.pecor.getQuoteExactIn(e.input.target, e.output.target, target)).to.equal(target);
    expect(await e.pecor.getQuoteExactOut(e.input.target, e.output.target, target)).to.equal(target);
    const before = await e.input.balanceOf(e.user.address);
    await e.pecor.connect(e.user).swapExactOut(e.input.target, e.output.target, target, target, 0);
    expect(await e.input.balanceOf(e.user.address)).to.equal(before - target);
  });

  it("matches exact-output inverse against brute force around impact and fee discontinuities", async function () {
    const e = await fixture({ reserves: 2000n });
    // High USD unit price moves tiny integer inputs through both fee thresholds.
    await e.oracle.setPrice(e.input.target, 1000n * ONE * ONE);
    await e.oracle.setPrice(e.output.target, 1000n * ONE * ONE);
    await e.pecor.setTieredFees(20, 50);
    await e.pecor.setSwapFee(30);
    await e.pecor.setPriceImpact(true, 500);
    const outputs = [];
    for (let input = 0n; input <= 220n; input++) outputs.push(await e.pecor.getQuoteExactIn(e.input.target, e.output.target, input));
    for (const target of [1n, 9n, 10n, 11n, 39n, 40n, 41n, 98n, 99n, 100n, 101n, 150n, 200n]) {
      const expected = outputs.findIndex(x => x >= target);
      const quoted = await e.pecor.getQuoteExactOut(e.input.target, e.output.target, target);
      expect(quoted, `target ${target}`).to.equal(BigInt(expected));
    }
  });

  it("backs exact-output retained fees in the paid output asset", async function () {
    const e = await fixture({ reserves: 1000n * ONE });
    await e.pecor.setSwapFee(100);
    await e.pecor.setPriceImpact(true, 500);
    await e.pecor.setFeeCollector(e.admin.address);
    const amountOut = 100n * ONE;
    const quote = await e.pecor.getQuoteExactOut(e.input.target, e.output.target, amountOut);
    expect(await e.pecor.getQuoteExactIn(e.input.target, e.output.target, quote)).to.be.gte(amountOut);
    await e.pecor.connect(e.user).swapExactOut(e.input.target, e.output.target, amountOut, quote, 0);
    const fees = await e.pecor.accruedFees(e.output.target);
    expect(fees).to.equal(quote - amountOut);
    await assertBacked(e.vault, e.output, fees);
    await e.pecor.collectFees(e.output.target);
    await assertBacked(e.vault, e.output, 0n);
  });

  it("defines tier thresholds per trade, so smaller split trades are charged their own tier", async function () {
    const e = await fixture({ reserves: 100000n * ONE });
    await e.pecor.setTieredFees(20, 50);
    const whole = await e.pecor.getQuoteExactIn(e.input.target, e.output.target, 10000n * ONE);
    const half = await e.pecor.getQuoteExactIn(e.input.target, e.output.target, 5000n * ONE);
    expect(whole).to.equal(9980n * ONE);
    expect(half * 2n).to.equal(10000n * ONE);
  });

  it("uses equivalent configured fee policies for immediate and conditional settlement", async function () {
    const e = await fixture();
    await e.pecor.setSwapFee(100);
    await e.orders.setPricingConfig(100, 0, 0, false, 0);
    const quote = await e.pecor.getQuoteExactIn(e.input.target, e.output.target, 100n * ONE);
    await e.orders.connect(e.user).placeOrder([0, e.input.target, e.output.target, 100n * ONE, ONE, 0, 0, quote]);
    const before = await e.output.balanceOf(e.user.address);
    await e.orders.connect(e.keeper).executeLimitOrder(1);
    expect(await e.output.balanceOf(e.user.address)).to.equal(before + quote);
    await assertBacked(e.vault, e.output, ONE);
    await e.orders.collectFees(e.output.target);
    await assertBacked(e.vault, e.output, 0n);
  });

  it("makes tracker failures observable without blocking placement, cancellation or swaps", async function () {
    const e = await fixture();
    const tracker = await (await ethers.getContractFactory("PECORAuditTracker")).deploy();
    await e.orders.setTransactionTracker(tracker.target);
    await expect(e.orders.connect(e.user).placeLimitBuy(e.input.target, e.output.target, ONE, ONE, 0)).to.emit(e.orders, "TrackerCallFailed");
    await expect(e.orders.connect(e.user).cancelLimitOrder(1)).to.emit(e.orders, "TrackerCallFailed");
    await e.pecor.setTransactionTracker(tracker.target);
    await expect(e.pecor.connect(e.user).swapExactIn(e.input.target, e.output.target, ONE, ONE, 0)).to.emit(e.pecor, "TrackerCallFailed");
  });

  it("records normal real analytics and tolerates later emitter revocation", async function () {
    const e = await fixture();
    const tracker = await deployTransactionTracker({ admin: e.admin.address });
    await tracker.setAuthorizedEmitter(e.pecor.target, true);
    await e.pecor.setTransactionTracker(tracker.target);
    await e.pecor.connect(e.user).swapExactIn(e.input.target, e.output.target, ONE, ONE, 0);
    expect((await tracker.userStats(e.user.address)).tradeCount).to.equal(1n);
    await tracker.setAuthorizedEmitter(e.pecor.target, false);
    await expect(e.pecor.connect(e.user).swapExactIn(e.input.target, e.output.target, ONE, ONE, 0)).to.emit(e.pecor, "TrackerCallFailed");
    expect((await tracker.userStats(e.user.address)).tradeCount).to.equal(1n);
  });

  it("isolates stale orders in batch settlement and discovery", async function () {
    const e = await fixture();
    const stale = await (await ethers.getContractFactory("MockStandardERC20")).deploy("Stale", "BAD", 18);
    await e.vault.registerToken(stale.target, false);
    await e.oracle.setPrice(stale.target, ONE);
    await e.orders.connect(e.user).placeLimitBuy(e.input.target, stale.target, ONE, ONE, 0);
    await e.orders.connect(e.user).placeLimitBuy(e.input.target, e.output.target, ONE, ONE, 0);
    await e.oracle.setUnavailable(stale.target, true);
    expect(await e.orders.getExecutableLimitOrders(10)).to.deep.equal([2n]);
    await expect(e.orders.connect(e.keeper).batchExecuteLimitOrders([1, 2])).to.emit(e.orders, "OrderExecutionFailed").withArgs(1, false);
    expect((await e.orders.getLimitOrder(1)).status).to.equal(0n);
    expect((await e.orders.getLimitOrder(2)).status).to.equal(2n);
    await assertBacked(e.vault, e.input, ONE);
  });

  it("rejects sender surcharge egress and preserves all custody/book/escrow state", async function () {
    const e = await fixture();
    const token = await (await ethers.getContractFactory("PECORAuditMetadataToken")).deploy(18);
    await e.vault.registerToken(token.target, false);
    await token.mint(e.admin.address, 100n * ONE);
    await token.approve(e.vault.target, 100n * ONE);
    await e.vault.deposit(token.target, 100n * ONE);
    await e.vault.setOperator(e.admin.address, true);
    await e.vault.reserveTokens(token.target, 50n * ONE);
    await token.setSenderSurcharge(1000);
    await expect(e.vault.pushTokens(token.target, e.user.address, 10n * ONE)).to.be.revertedWithCustomError(e.vault, "UnsupportedTransfer");
    await assertBacked(e.vault, token, 50n * ONE);
  });

  it("reconciles unreserved negative rebases and refuses to write off reserved liabilities", async function () {
    const e = await fixture();
    await e.vault.setOperator(e.admin.address, true);
    await e.vault.reserveTokens(e.output.target, 500n * ONE);
    await e.output.burn(e.vault.target, 100n * ONE);
    expect(await e.vault.getAvailableReserves(e.output.target)).to.equal(400n * ONE);
    await expect(e.vault.syncReserves(e.output.target)).to.emit(e.vault, "ReserveDeficitReconciled");
    await assertBacked(e.vault, e.output, 500n * ONE);
    await e.output.burn(e.vault.target, 401n * ONE);
    await expect(e.vault.syncReserves(e.output.target)).to.be.revertedWithCustomError(e.vault, "ReservedLiabilities");
    await expect(e.vault.releaseTokens(e.output.target, ONE)).to.be.revertedWithCustomError(e.vault, "ReservedLiabilities");
    expect(await e.vault.getReservedBalance(e.output.target)).to.equal(500n * ONE);
  });

  it("validates metadata rather than truncating unsupported exponents", async function () {
    const e = await fixture();
    for (const decimals of [0, 6, 18, 36, 37, 78, 255, 256, 262]) {
      const token = await (await ethers.getContractFactory("PECORAuditMetadataToken")).deploy(decimals);
      if (decimals <= 36) {
        await e.vault.registerToken(token.target, false);
        expect(await e.vault.getTokenDecimals(token.target)).to.equal(BigInt(decimals));
      } else await expect(e.vault.registerToken(token.target, false)).to.be.revertedWithCustomError(e.vault, "UnsupportedToken");
    }
    await expect(e.vault.registerToken(e.user.address, false)).to.be.revertedWithCustomError(e.vault, "UnsupportedToken");
    const noMetadata = await (await ethers.getContractFactory("PECORAuditTracker")).deploy();
    await expect(e.vault.registerToken(noMetadata.target, false)).to.be.revertedWithCustomError(e.vault, "UnsupportedToken");
  });

  it("recovers forced native value without spending WETH custody or reservations", async function () {
    const e = await fixture();
    await e.vault.depositNative({ value: ONE });
    await e.vault.setOperator(e.admin.address, true);
    await e.vault.reserveTokens(e.weth.target, ONE);
    // Creation bytecode selfdestructs in the same transaction, forcing native
    // value without executing the vault's receive() sender guard.
    await e.admin.sendTransaction({ data: `0x73${e.vault.target.slice(2)}ff`, value: ONE });
    expect(await ethers.provider.getBalance(e.vault.target)).to.equal(ONE);
    await expect(e.vault.connect(e.user).recoverNativeSurplus(e.trader.address)).to.be.revertedWithCustomError(e.vault, "MissingRole");
    const before = await ethers.provider.getBalance(e.trader.address);
    await expect(e.vault.recoverNativeSurplus(e.trader.address)).to.emit(e.vault, "NativeSurplusRecovered").withArgs(e.trader.address, ONE);
    expect(await ethers.provider.getBalance(e.trader.address)).to.equal(before + ONE);
    expect(await ethers.provider.getBalance(e.vault.target)).to.equal(0n);
    await assertBacked(e.vault, e.weth, ONE);
  });

  it("synchronizes keeper mirrors for generic grants, revocations and renunciation", async function () {
    const e = await fixture();
    const role = await e.orders.KEEPER_ROLE();
    await e.orders.grantRole(role, e.trader.address);
    expect(await e.orders.keepers(e.trader.address)).to.equal(true);
    await e.orders.revokeRole(role, e.trader.address);
    expect(await e.orders.keepers(e.trader.address)).to.equal(false);
    await e.orders.setKeeper(e.trader.address, true);
    await e.orders.connect(e.trader).renounceRole(role, e.trader.address);
    expect(await e.orders.keepers(e.trader.address)).to.equal(false);
    expect(await e.orders.hasRole(role, e.trader.address)).to.equal(false);
  });

  it("covers active order pages with a bounded scan cursor", async function () {
    const e = await fixture();
    for (let i = 0; i < 5; i++) await e.orders.connect(e.user).placeLimitBuy(e.input.target, e.output.target, ONE, ONE, 0);
    const [first, next] = await e.orders.getOrdersPage(0, 2, 1000000, 0);
    const [second, last] = await e.orders.getOrdersPage(next, 2, 1000000, 0);
    const [third, done] = await e.orders.getOrdersPage(last, 2, 1000000, 0);
    expect([...first, ...second, ...third]).to.deep.equal([1n, 2n, 3n, 4n, 5n]);
    expect(next).to.equal(2n); expect(last).to.equal(4n); expect(done).to.equal(5n);
    const [tokens, cursor] = await e.vault.getRegisteredTokensPage(0, ethers.MaxUint256);
    expect(tokens).to.have.length(3); expect(cursor).to.equal(3n);
  });

  it("compares full-precision decimal conversion with a big-integer rational reference", async function () {
    const math = await (await ethers.getContractFactory("PECORMathHarness")).deploy();
    const max = ethers.MaxUint256;
    const cases = [
      [max, max, max, 18, 18],
      [max, max, max, 36, 0],
      [max / 10n ** 36n, max, max, 0, 36],
      [max, 10n ** 36n, max, 36, 0],
      [10n ** 60n, 10n ** 60n, 10n ** 50n, 18, 6],
      [123456789n, 987654321n, 111111111n, 6, 18],
    ];
    // Fixed-seed properties include extreme-price scaled denominators that
    // previously overflowed before the final representable quotient.
    let seed = 17n;
    for (let i = 0; i < 30; i++) {
      seed = (seed * 1103515245n + 12345n) % (1n << 96n);
      cases.push([seed + 1n, (seed << 100n) + 1n, (seed << 110n) + 3n, i % 37, (i * 7) % 37]);
    }
    for (const [amount, priceIn, priceOut, dIn, dOut] of cases) {
      const numerator = amount * priceIn * 10n ** BigInt(dOut);
      const denominator = priceOut * 10n ** BigInt(dIn);
      const down = numerator / denominator;
      const up = (numerator + denominator - 1n) / denominator;
      if (up > max) continue;
      expect(await math.convert(amount, priceIn, priceOut, dIn, dOut, false)).to.equal(down);
      expect(await math.convert(amount, priceIn, priceOut, dIn, dOut, true)).to.equal(up);
    }
  });
});
