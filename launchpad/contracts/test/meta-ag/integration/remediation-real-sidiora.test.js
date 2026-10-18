const { expect } = require("chai");
const { ethers } = require("hardhat");
const { deployRealSidioraFixture } = require("../../helpers/realSidioraFixture");
const { deployPriceOracle, deployOracleHub, deployPECORVault, deployPECOR,
  deployPECOROrders, deployVaultAdapter, deploySidioraAdapter, deployMetaAGRouter } = require("../helpers/fixtures");

// These tests execute real launch tokens, Factory, Pool, Router, Quoter and
// EventEmitter. The wrapped native fixture is the only protocol rail substitute.
describe("Audit remediation: real six-decimal Sidiora and PECOR integration", function () {
  let fx, oracle, vault, engine, orders, meta, sidiora, inventory, a, b;
  const ONE = 10n ** 18n;
  const unit = n => ethers.parseUnits(String(n), 6);
  async function deadline() { return (await ethers.provider.getBlock("latest")).timestamp + 3600; }
  async function create(name, optical = ethers.ZeroAddress) {
    const receipt = await (await fx.router.connect(fx.alice).createMarket(name, name, 0, optical)).wait();
    const event = receipt.logs.map(l => { try { return fx.router.interface.parseLog(l); } catch { return null; } })
      .find(e => e && e.name === "MarketCreated");
    return { receipt, nftId: event.args[3], token: await ethers.getContractAt("SidioraERC20", event.args[0]),
      pool: await ethers.getContractAt("SidioraPool", event.args[1]) };
  }
  beforeEach(async function () {
    fx = await deployRealSidioraFixture();
    a = await create("REAL_A"); b = await create("REAL_B");
    const WETH = await ethers.getContractFactory("MockWETH9");
    const weth = await WETH.deploy();
    oracle = await deployPriceOracle();
    const hub = await deployOracleHub({ primaryOracle: oracle.target, deviationBps: 10000 });
    vault = await deployPECORVault({ weth: weth.target });
    engine = await deployPECOR({ priceOracle: oracle.target, vault: vault.target, weth: weth.target });
    sidiora = await deploySidioraAdapter({ poolRegistry: fx.registry.target, quoter: fx.quoter.target,
      sidioraRouter: fx.router.target, usdl: fx.usdl.target });
    inventory = await deployVaultAdapter({ vault: vault.target, priceOracle: oracle.target, feeBps: 0 });
    meta = await deployMetaAGRouter({ oracleHub: hub.target, maxOracleSanityDeviation: 10000 });
    await meta.registerAdapter(sidiora.target); await meta.registerAdapter(inventory.target);
    await oracle.setRelayer(fx.deployer.address, true);
    for (const token of [fx.usdl, a.token, b.token]) {
      await oracle.registerToken(token.target, 60, 100, 1, ONE * 1000000n, 3600);
      await oracle.updatePrice(token.target, token === fx.usdl ? ONE : await (token === a.token ? a.pool : b.pool).getPrice());
      await vault.registerToken(token.target, token === fx.usdl);
    }
    for (const operator of [engine, inventory]) await vault.setOperator(operator.target, true);
    await fx.usdl.mint(fx.deployer.address, unit(1000000));
    await fx.usdl.approve(vault.target, ethers.MaxUint256);
    await vault.deposit(fx.usdl.target, unit(1000000));
    await fx.usdl.connect(fx.bob).approve(meta.target, ethers.MaxUint256);
  });
  it("public market creation supplies authorization and creation-fee accounting without fixture repair", async function () {
    const role = ethers.id("POOL_ROLE");
    expect(await fx.accumulator.hasRole(role, a.pool.target)).to.equal(true);
    expect(await fx.eventEmitter.isAuthorizedEmitter(a.pool.target)).to.equal(true);
    expect(await a.token.decimals()).to.equal(6);
    expect(await fx.usdl.decimals()).to.equal(6);
    expect(await fx.treasury.getBalance(fx.usdl.target)).to.equal(unit(200));
    await expect(fx.eventEmitter.connect(fx.bob).emitSwap(ethers.zeroPadValue("0x01", 32), fx.bob.address, true, 1, 1, 0, 1)).to.be.reverted;
  });
  it("central hub payloads match actual Factory, registry, NFT, pool and fee producer events", async function () {
    function logs(receipt, contract, name) {
      return receipt.logs.filter(l => l.address.toLowerCase() === contract.target.toLowerCase())
        .map(l => { try { return contract.interface.parseLog(l); } catch { return null; } })
        .filter(l => l && l.name === name);
    }
    const market = logs(a.receipt, fx.eventEmitter, "MarketCreated")[0].args;
    const registration = logs(a.receipt, fx.eventEmitter, "PoolRegistered")[0].args;
    const minted = logs(a.receipt, fx.eventEmitter, "NftMint")[0].args;
    expect(market.pool).to.equal(a.pool.target);
    expect(market.token).to.equal(a.token.target);
    expect(market.creator).to.equal(fx.alice.address);
    expect(registration.pool).to.equal(a.pool.target);
    expect(registration.nftId).to.equal(a.nftId);
    expect(minted.tokenId).to.equal(a.nftId);
    expect(minted.creator).to.equal(fx.alice.address);
    expect(minted.pool).to.equal(a.pool.target);
    const amount = unit(1000);
    const receipt = await (await fx.router.connect(fx.bob).buy(a.pool.target, amount, 0, await deadline())).wait();
    const local = logs(receipt, fx.accumulator, "FeeRecorded")[0].args;
    const mirror = logs(receipt, fx.eventEmitter, "FeeFlow").find(l => l.args.kind === 0n).args;
    expect(mirror.pool).to.equal(a.pool.target);
    expect(mirror.amount).to.equal(local.feeAmount);
    expect(mirror.protocolCut).to.equal(local.protocolCut);
    expect(mirror.poolCut).to.equal(local.poolCut);
    const swap = logs(receipt, fx.eventEmitter, "Swap")[0].args;
    const routed = logs(receipt, fx.eventEmitter, "RouterTrade")[0].args;
    expect(swap.sender).to.equal(fx.bob.address);
    expect(swap.amountIn).to.equal(amount);
    expect(routed.sender).to.equal(fx.bob.address);
    expect(routed.amountOut).to.equal(swap.amountOut);
    expect(logs(receipt, fx.accumulator, "MirrorFailed")).to.have.length(0);
  });
  it("missing Factory authorization fails public creation atomically", async function () {
    const unwired = await deployRealSidioraFixture({ authorizeFactory: false });
    const balance = await unwired.usdl.balanceOf(unwired.alice.address);
    await expect(unwired.router.connect(unwired.alice).createMarket("UNWIRED", "FAIL", 0, ethers.ZeroAddress)).to.be.reverted;
    expect(await unwired.usdl.balanceOf(unwired.alice.address)).to.equal(balance);
    expect(await unwired.registry.getPoolCount()).to.equal(0);
  });
  it("real buy and sell realize independent before-state quotes and preserve reserve backing", async function () {
    const amount = unit(1000);
    const [virtual, real, tokens] = await a.pool.getReserves();
    const fee = amount * await a.pool.currentFeeBps() / 10000n;
    const net = amount - fee;
    const independentOut = tokens * net / (virtual + real + net);
    const quote = await fx.quoter.quoteExactInput(a.pool.target, amount, true);
    expect(quote.feeAmount).to.equal(fee);
    expect(quote.amountOut).to.equal(independentOut);
    const before = await a.token.balanceOf(fx.bob.address);
    await meta.connect(fx.bob).swapViaAdapter(await sidiora.adapterId(), fx.usdl.target, a.token.target, amount, quote.amountOut, await deadline());
    expect(await a.token.balanceOf(fx.bob.address)).to.equal(before + quote.amountOut);
    expect(await fx.usdl.balanceOf(a.pool.target)).to.equal(await a.pool.realUsdlBalance());
    expect(await a.token.balanceOf(a.pool.target)).to.equal(await a.pool.tokenReserve());
    const sell = quote.amountOut / 2n;
    await a.token.connect(fx.bob).approve(meta.target, sell);
    const [sellVirtual, sellReal, sellTokens] = await a.pool.getReserves();
    const sellFee = sell * await a.pool.currentFeeBps() / 10000n;
    const sellNet = sell - sellFee;
    const independentSell = (sellVirtual + sellReal) * sellNet / (sellTokens + sellNet);
    const sellQuote = await fx.quoter.quoteExactInput(a.pool.target, sell, false);
    expect(sellQuote.amountOut).to.equal(independentSell);
    const usdlBefore = await fx.usdl.balanceOf(fx.bob.address);
    await meta.connect(fx.bob).swapViaAdapter(await sidiora.adapterId(), a.token.target, fx.usdl.target, sell, sellQuote.amountOut, await deadline());
    expect(await fx.usdl.balanceOf(fx.bob.address)).to.equal(usdlBefore + sellQuote.amountOut);
    expect(await fx.usdl.balanceOf(a.pool.target)).to.equal(await a.pool.realUsdlBalance());
    expect(await a.token.allowance(meta.target, sidiora.target)).to.equal(0);
    expect(await a.token.balanceOf(meta.target)).to.equal(0);
  });
  it("stateful real-pool buy/sell sequence conserves backing and the constant-product floor", async function () {
    await a.token.connect(fx.bob).approve(fx.router.target, ethers.MaxUint256);
    for (let i = 0; i < 16; i++) {
      const [virtualBefore, realBefore, tokensBefore] = await a.pool.getReserves();
      const invariantBefore = (virtualBefore + realBefore) * tokensBefore;
      if (i % 3 !== 2) {
        await fx.router.connect(fx.bob).buy(a.pool.target, unit(10 + i * 7), 0, await deadline());
      } else {
        const input = (await a.token.balanceOf(fx.bob.address)) / 9n;
        await fx.router.connect(fx.bob).sell(a.pool.target, input, 0, await deadline());
      }
      const [virtualAfter, realAfter, tokensAfter] = await a.pool.getReserves();
      expect(virtualAfter).to.equal(virtualBefore);
      expect((virtualAfter + realAfter) * tokensAfter).to.be.gte(invariantBefore);
      expect(await fx.usdl.balanceOf(a.pool.target)).to.equal(realAfter);
      expect(await a.token.balanceOf(a.pool.target)).to.equal(tokensAfter);
    }
  });
  it("real quote/execution parity survives pool age decay with fresh independent oracle observations", async function () {
    const feeBefore = await a.pool.currentFeeBps();
    await ethers.provider.send("evm_increaseTime", [3600]);
    await ethers.provider.send("evm_mine", []);
    for (const [token, price] of [[fx.usdl, ONE], [a.token, await a.pool.getPrice()], [b.token, await b.pool.getPrice()]]) {
      await oracle.updatePrice(token.target, price);
    }
    const feeAfter = await a.pool.currentFeeBps();
    expect(feeAfter).to.be.lte(feeBefore);
    const input = unit(100);
    const [virtual, real, tokens] = await a.pool.getReserves();
    const net = input - input * feeAfter / 10000n;
    const expected = tokens * net / (virtual + real + net);
    const quote = await fx.quoter.quoteExactInput(a.pool.target, input, true);
    expect(quote.amountOut).to.equal(expected);
    await meta.connect(fx.bob).swapViaAdapter(await sidiora.adapterId(), fx.usdl.target, a.token.target, input, expected, await deadline());
    expect(await a.token.balanceOf(fx.bob.address)).to.equal(expected);
  });
  it("real token-to-token route settles both pools and clears transient approvals", async function () {
    await fx.router.connect(fx.bob).buy(a.pool.target, unit(5000), 0, await deadline());
    const input = (await a.token.balanceOf(fx.bob.address)) / 4n;
    await a.token.connect(fx.bob).approve(meta.target, input);
    const quote = await fx.quoter.quoteMultihop(a.token.target, b.token.target, input);
    await meta.connect(fx.bob).swapViaAdapter(await sidiora.adapterId(), a.token.target, b.token.target, input, quote.amountOut, await deadline());
    expect(await b.token.balanceOf(fx.bob.address)).to.equal(quote.amountOut);
    for (const market of [a, b]) {
      expect(await fx.usdl.balanceOf(market.pool.target)).to.equal(await market.pool.realUsdlBalance());
      expect(await market.token.balanceOf(market.pool.target)).to.equal(await market.pool.tokenReserve());
    }
    expect(await a.token.allowance(meta.target, sidiora.target)).to.equal(0);
    expect(await fx.usdl.balanceOf(meta.target)).to.equal(0);
  });
  it("vault-to-Sidiora multi-hop settles actual six-decimal intermediate custody", async function () {
    const Token = await ethers.getContractFactory("MockStandardERC20");
    const usdc = await Token.deploy("USD Coin", "USDC", 6);
    await vault.registerToken(usdc.target, true);
    await oracle.registerToken(usdc.target, 60, 100, 1, ONE * 1000000n, 3600);
    await oracle.updatePrice(usdc.target, ONE);
    const input = unit(100);
    await usdc.mint(fx.bob.address, input);
    await usdc.connect(fx.bob).approve(meta.target, input);
    const quote = await fx.quoter.quoteExactInput(a.pool.target, input, true);
    const vaultUsdlBefore = await vault.getAvailableReserves(fx.usdl.target);
    const hops = [
      { adapterId: await inventory.adapterId(), tokenIn: usdc.target, tokenOut: fx.usdl.target, minAmountOut: input },
      { adapterId: await sidiora.adapterId(), tokenIn: fx.usdl.target, tokenOut: a.token.target, minAmountOut: quote.amountOut },
    ];
    await meta.connect(fx.bob).swapMultiHop(hops, input, quote.amountOut, await deadline());
    expect(await a.token.balanceOf(fx.bob.address)).to.equal(quote.amountOut);
    expect(await vault.getAvailableReserves(fx.usdl.target)).to.equal(vaultUsdlBefore - input);
    expect(await vault.getAvailableReserves(usdc.target)).to.equal(input);
    expect(await usdc.balanceOf(meta.target)).to.equal(0);
    expect(await fx.usdl.balanceOf(meta.target)).to.equal(0);
    expect(await usdc.allowance(meta.target, inventory.target)).to.equal(0);
    expect(await fx.usdl.allowance(meta.target, sidiora.target)).to.equal(0);
  });
  it("real launch-token inventory executes PECOR swaps and preserves collectible output fees", async function () {
    await fx.router.connect(fx.alice).buy(a.pool.target, unit(1000), 0, await deadline());
    const inventoryTokens = await a.token.balanceOf(fx.alice.address);
    await a.token.connect(fx.alice).approve(vault.target, inventoryTokens);
    await vault.connect(fx.alice).deposit(a.token.target, inventoryTokens);
    const price = await a.pool.getPrice();
    await oracle.updatePrice(a.token.target, price);
    await engine.setTieredFees(0, 0);
    await engine.setSwapFee(30);
    await engine.setPriceImpact(false, 0);
    await engine.setFeeCollector(fx.deployer.address);
    const input = unit(10);
    const gross = input * ONE / price;
    const fee = gross * 30n / 10000n;
    const expected = gross - fee;
    expect(await engine.getQuoteExactIn(fx.usdl.target, a.token.target, input)).to.equal(expected);
    await fx.usdl.connect(fx.bob).approve(vault.target, input);
    const availableBefore = await vault.getAvailableReserves(a.token.target);
    await engine.connect(fx.bob).swapExactIn(fx.usdl.target, a.token.target, input, expected, await deadline());
    expect(await a.token.balanceOf(fx.bob.address)).to.equal(expected);
    expect(await engine.accruedFees(a.token.target)).to.equal(fee);
    expect(await vault.operatorReservations(engine.target, a.token.target)).to.equal(fee);
    expect(await vault.getAvailableReserves(a.token.target)).to.equal(availableBefore - gross);
    const beforeCollector = await a.token.balanceOf(fx.deployer.address);
    await engine.collectFees(a.token.target);
    expect(await a.token.balanceOf(fx.deployer.address)).to.equal(beforeCollector + fee);
    expect(await vault.getReservedBalance(a.token.target)).to.equal(0);
    expect(await vault.getAvailableReserves(a.token.target)).to.equal(availableBefore - gross);
  });
  it("pending order escrow remains unavailable to engine and cancellation conserves custody", async function () {
    orders = await deployPECOROrders({ priceOracle: oracle.target, vault: vault.target });
    await vault.setOperator(orders.target, true);
    await orders.setKeeper(fx.deployer.address, true);
    await fx.usdl.connect(fx.bob).approve(vault.target, ethers.MaxUint256);
    const before = await fx.usdl.balanceOf(fx.bob.address);
    const available = await vault.getAvailableReserves(fx.usdl.target);
    await orders.connect(fx.bob).placeLimitBuy(fx.usdl.target, a.token.target, unit(500), ONE, await deadline());
    expect(await vault.getAvailableReserves(fx.usdl.target)).to.equal(available);
    expect(await vault.getReservedBalance(fx.usdl.target)).to.equal(unit(500));
    await orders.connect(fx.bob).cancelLimitOrder(1);
    expect(await fx.usdl.balanceOf(fx.bob.address)).to.equal(before);
    expect(await vault.getReservedBalance(fx.usdl.target)).to.equal(0);
    expect(await vault.getAvailableReserves(fx.usdl.target)).to.equal(available);
  });
  it("real Launchpad creation anchors vesting and blocks transfer laundering before the cliff", async function () {
    const Launchpad = await ethers.getContractFactory("LaunchpadOptical");
    const optical = await Launchpad.deploy(fx.registry.target, fx.alice.address, fx.alice.address,
      [], 100, 1000, 0, 0, fx.alice.address, fx.accumulator.target);
    const market = await create("VESTED", optical.target);
    await fx.router.connect(fx.alice).buy(market.pool.target, unit(1000), 0, await deadline());
    const allocation = await market.token.balanceOf(fx.alice.address);
    const metadata = await fx.registry.getPoolMetadata(market.pool.target);
    expect(await optical.poolStartTime(market.pool.target)).to.equal(metadata.createdAt);
    await expect(market.token.connect(fx.alice).transfer(fx.bob.address, 1)).to.be.reverted;
    await market.token.connect(fx.alice).approve(fx.charlie.address, allocation);
    await expect(market.token.connect(fx.charlie).transferFrom(fx.alice.address, fx.bob.address, 1)).to.be.reverted;
    await ethers.provider.send("evm_increaseTime", [600]);
    await ethers.provider.send("evm_mine", []);
    const release = allocation * 4n / 10n;
    await market.token.connect(fx.alice).transfer(fx.bob.address, release);
    expect(await optical.tokensSold(market.pool.target, fx.alice.address)).to.equal(release);
    await expect(market.token.connect(fx.alice).transfer(fx.bob.address, allocation - release)).to.be.reverted;
    await market.token.connect(fx.alice).approve(fx.router.target, allocation / 20n);
    await fx.router.connect(fx.alice).sell(market.pool.target, allocation / 20n, 0, await deadline());
    expect(await optical.tokensSold(market.pool.target, fx.alice.address)).to.equal(release + allocation / 20n);
  });

});
