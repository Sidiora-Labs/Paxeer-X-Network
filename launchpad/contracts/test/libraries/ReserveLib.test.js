const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("ReserveLib", function () {
  let lib;

  // Protocol defaults from architecture
  const VIRTUAL_USDL = ethers.parseUnits("10000", 6); // 10,000 USDL
  const VIRTUAL_TOKEN = ethers.parseUnits("1000000000", 6); // 1B tokens
  const TOTAL_SUPPLY = ethers.parseUnits("1000000000", 6); // 1B tokens

  before(async function () {
    const Wrapper = await ethers.getContractFactory("ReserveLibWrapper");
    lib = await Wrapper.deploy();
    await lib.waitForDeployment();
  });

  describe("getEffectiveReserves", function () {
    it("should sum virtual and real USDL", async function () {
      expect(await lib.getEffectiveReserves(VIRTUAL_USDL, 0)).to.equal(VIRTUAL_USDL);
      expect(await lib.getEffectiveReserves(VIRTUAL_USDL, ethers.parseUnits("500", 6))).to.equal(
        ethers.parseUnits("10500", 6)
      );
    });

    it("should handle zero virtual (edge case)", async function () {
      expect(await lib.getEffectiveReserves(0, ethers.parseUnits("1000", 6))).to.equal(
        ethers.parseUnits("1000", 6)
      );
    });
  });

  describe("getAmountOut", function () {
    it("should calculate correct buy output at initial reserves", async function () {
      // BUY: 100 USDL → tokens at initial reserves (10k virtual + 0 real, 1B tokens)
      const effectiveUsdl = VIRTUAL_USDL; // 10,000 (no real yet)
      const amountIn = ethers.parseUnits("100", 6);
      const amountOut = await lib.getAmountOut(effectiveUsdl, VIRTUAL_TOKEN, amountIn);

      // Expected: (1B * 100) / (10000 + 100) = 100B / 10100 ≈ 9,900,990.099...e18
      // Floor: 9900990099009900990099009n
      expect(amountOut).to.be.gt(ethers.parseUnits("9900000", 6));
      expect(amountOut).to.be.lt(ethers.parseUnits("10000000", 6));
    });

    it("should calculate correct sell output", async function () {
      // SELL: 10M tokens → USDL. Assume pool has 10k effective USDL, 1B tokens
      const tokenAmount = ethers.parseUnits("10000000", 6); // 10M
      const amountOut = await lib.getAmountOut(VIRTUAL_TOKEN, VIRTUAL_USDL, tokenAmount);

      // (10000 * 10M) / (1B + 10M) = 100B / 1.01B ≈ 99.0099 USDL
      expect(amountOut).to.be.gt(ethers.parseUnits("99", 6));
      expect(amountOut).to.be.lt(ethers.parseUnits("100", 6));
    });

    it("should revert on zero input", async function () {
      await expect(
        lib.getAmountOut(VIRTUAL_USDL, VIRTUAL_TOKEN, 0)
      ).to.be.revertedWithCustomError(lib, "InsufficientInput");
    });

    it("should revert on zero reserves", async function () {
      await expect(
        lib.getAmountOut(0, VIRTUAL_TOKEN, ethers.parseUnits("100", 6))
      ).to.be.revertedWithCustomError(lib, "InsufficientLiquidity");
      await expect(
        lib.getAmountOut(VIRTUAL_USDL, 0, ethers.parseUnits("100", 6))
      ).to.be.revertedWithCustomError(lib, "InsufficientLiquidity");
    });

    it("should never output more than reserveOut", async function () {
      // Even with enormous input, output is bounded by reserveOut
      const hugeInput = ethers.parseUnits("999999999999", 6);
      const amountOut = await lib.getAmountOut(VIRTUAL_USDL, VIRTUAL_TOKEN, hugeInput);
      expect(amountOut).to.be.lt(VIRTUAL_TOKEN);
    });

    it("should be monotonically increasing with input", async function () {
      const out1 = await lib.getAmountOut(VIRTUAL_USDL, VIRTUAL_TOKEN, ethers.parseUnits("10", 6));
      const out2 = await lib.getAmountOut(VIRTUAL_USDL, VIRTUAL_TOKEN, ethers.parseUnits("100", 6));
      const out3 = await lib.getAmountOut(VIRTUAL_USDL, VIRTUAL_TOKEN, ethers.parseUnits("1000", 6));
      expect(out2).to.be.gt(out1);
      expect(out3).to.be.gt(out2);
    });

    it("should show diminishing returns (price impact)", async function () {
      // 2x input should yield less than 2x output
      const out1 = await lib.getAmountOut(VIRTUAL_USDL, VIRTUAL_TOKEN, ethers.parseUnits("100", 6));
      const out2 = await lib.getAmountOut(VIRTUAL_USDL, VIRTUAL_TOKEN, ethers.parseUnits("200", 6));
      expect(out2).to.be.lt(out1 * 2n);
    });

    it("should preserve k approximately (buy scenario)", async function () {
      // After a buy: new effectiveUsdl = old + amountIn, new tokenReserve = old - amountOut
      const amountIn = ethers.parseUnits("500", 6);
      const amountOut = await lib.getAmountOut(VIRTUAL_USDL, VIRTUAL_TOKEN, amountIn);

      const kBefore = VIRTUAL_USDL * VIRTUAL_TOKEN;
      const newUsdl = VIRTUAL_USDL + amountIn;
      const newToken = VIRTUAL_TOKEN - amountOut;
      const kAfter = newUsdl * newToken;

      // k should increase slightly due to integer rounding (floor division)
      expect(kAfter).to.be.gte(kBefore);
    });
  });

  describe("getAmountIn", function () {
    it("should calculate required input for desired output (buy)", async function () {
      // Want 10M tokens, how much USDL needed?
      const desiredOut = ethers.parseUnits("10000000", 6); // 10M
      const amountIn = await lib.getAmountIn(VIRTUAL_USDL, VIRTUAL_TOKEN, desiredOut);

      // Verify: getAmountOut with this input gives >= desiredOut
      const actualOut = await lib.getAmountOut(VIRTUAL_USDL, VIRTUAL_TOKEN, amountIn);
      expect(actualOut).to.be.gte(desiredOut);
    });

    it("should revert on zero output", async function () {
      await expect(
        lib.getAmountIn(VIRTUAL_USDL, VIRTUAL_TOKEN, 0)
      ).to.be.revertedWithCustomError(lib, "InsufficientInput");
    });

    it("should revert when desired output >= reserveOut", async function () {
      await expect(
        lib.getAmountIn(VIRTUAL_USDL, VIRTUAL_TOKEN, VIRTUAL_TOKEN)
      ).to.be.revertedWithCustomError(lib, "InsufficientLiquidity");
    });

    it("should revert on zero reserves", async function () {
      await expect(
        lib.getAmountIn(0, VIRTUAL_TOKEN, ethers.parseUnits("100", 6))
      ).to.be.revertedWithCustomError(lib, "InsufficientLiquidity");
    });
  });

  describe("round-trip buy-then-sell", function () {
    it("should return less USDL than spent (due to price impact)", async function () {
      // Buy tokens with 100 USDL
      const buyIn = ethers.parseUnits("100", 6);
      const tokensReceived = await lib.getAmountOut(VIRTUAL_USDL, VIRTUAL_TOKEN, buyIn);

      // New reserves after buy
      const newUsdl = VIRTUAL_USDL + buyIn;
      const newToken = VIRTUAL_TOKEN - tokensReceived;

      // Sell all tokens back
      const usdlReturned = await lib.getAmountOut(newToken, newUsdl, tokensReceived);

      // Should get less back than we put in (price impact)
      expect(usdlReturned).to.be.lt(buyIn);
    });
  });

  describe("getPrice", function () {
    it("should return correct initial price", async function () {
      // price = 10000e18 * 1e18 / 1e27 = 10000e36 / 1e27 = 1e13 (= 0.00001 USDL per token)
      const price = await lib.getPrice(VIRTUAL_USDL, VIRTUAL_TOKEN);
      expect(price).to.equal(10000000000000n); // 1e13 = 0.00001e18
    });

    it("should increase after buying", async function () {
      const priceBefore = await lib.getPrice(VIRTUAL_USDL, VIRTUAL_TOKEN);

      // Simulate buy: 1000 USDL in
      const buyIn = ethers.parseUnits("1000", 6);
      const tokensOut = await lib.getAmountOut(VIRTUAL_USDL, VIRTUAL_TOKEN, buyIn);
      const newUsdl = VIRTUAL_USDL + buyIn;
      const newToken = VIRTUAL_TOKEN - tokensOut;

      const priceAfter = await lib.getPrice(newUsdl, newToken);
      expect(priceAfter).to.be.gt(priceBefore);
    });

    it("should decrease after selling", async function () {
      // First buy to get tokens, then sell
      const buyIn = ethers.parseUnits("1000", 6);
      const tokensOut = await lib.getAmountOut(VIRTUAL_USDL, VIRTUAL_TOKEN, buyIn);
      const afterBuyUsdl = VIRTUAL_USDL + buyIn;
      const afterBuyToken = VIRTUAL_TOKEN - tokensOut;

      const priceBefore = await lib.getPrice(afterBuyUsdl, afterBuyToken);

      // Sell half the tokens back
      const sellAmount = tokensOut / 2n;
      const usdlOut = await lib.getAmountOut(afterBuyToken, afterBuyUsdl, sellAmount);
      const afterSellUsdl = afterBuyUsdl - usdlOut;
      const afterSellToken = afterBuyToken + sellAmount;

      const priceAfter = await lib.getPrice(afterSellUsdl, afterSellToken);
      expect(priceAfter).to.be.lt(priceBefore);
    });

    it("should revert on zero token reserve", async function () {
      await expect(
        lib.getPrice(VIRTUAL_USDL, 0)
      ).to.be.revertedWithCustomError(lib, "InsufficientLiquidity");
    });
  });

  describe("getMarketCap", function () {
    it("should return correct initial market cap", async function () {
      // price = 0.00001 USDL per token, supply = 1B
      // marketCap = 0.00001 * 1B = 10,000 USDL
      const mc = await lib.getMarketCap(VIRTUAL_USDL, VIRTUAL_TOKEN, TOTAL_SUPPLY);
      expect(mc).to.equal(ethers.parseUnits("10000", 6));
    });

    it("should increase market cap after buy", async function () {
      const mcBefore = await lib.getMarketCap(VIRTUAL_USDL, VIRTUAL_TOKEN, TOTAL_SUPPLY);

      const buyIn = ethers.parseUnits("5000", 6);
      const tokensOut = await lib.getAmountOut(VIRTUAL_USDL, VIRTUAL_TOKEN, buyIn);
      const newUsdl = VIRTUAL_USDL + buyIn;
      const newToken = VIRTUAL_TOKEN - tokensOut;

      const mcAfter = await lib.getMarketCap(newUsdl, newToken, TOTAL_SUPPLY);
      expect(mcAfter).to.be.gt(mcBefore);
    });
  });

  describe("large number handling", function () {
    it("should handle very small buys (1 wei)", async function () {
      // 1 wei USDL buy — should not revert
      const amountOut = await lib.getAmountOut(VIRTUAL_USDL, VIRTUAL_TOKEN, 1n);
      // At initial ratio, 1 wei USDL → ~100,000 wei tokens (price is 0.00001)
      expect(amountOut).to.be.gte(0);
    });

    it("should handle reserves after many buys", async function () {
      // Simulate accumulated real USDL from many buys
      const realUsdl = ethers.parseUnits("50000", 6); // 50k real
      const effectiveUsdl = VIRTUAL_USDL + realUsdl; // 60k effective
      const reducedTokens = ethers.parseUnits("200000000", 6); // 200M remaining

      const amountOut = await lib.getAmountOut(
        effectiveUsdl,
        reducedTokens,
        ethers.parseUnits("100", 6)
      );
      expect(amountOut).to.be.gt(0);
      expect(amountOut).to.be.lt(reducedTokens);
    });
  });
  describe("audit remediation: full precision rational math", function () {
    it("quotes a representable output when the numerator exceeds uint256", async function () {
      const reserveIn = 1n << 180n;
      const reserveOut = 1n << 200n;
      const amountIn = 1n << 160n;
      expect(await lib.getAmountOut(reserveIn, reserveOut, amountIn)).to.equal(
        reserveOut * amountIn / (reserveIn + amountIn));
    });
    it("quotes inverse input with conservative rounding and an overflowing numerator", async function () {
      const reserveIn = 1n << 180n;
      const reserveOut = 1n << 200n;
      const output = 1n << 160n;
      const input = await lib.getAmountIn(reserveIn, reserveOut, output);
      expect(input).to.equal(reserveIn * output / (reserveOut - output) + 1n);
      expect(await lib.getAmountOut(reserveIn, reserveOut, input)).to.be.gte(output);
    });
  });

});
