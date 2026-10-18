// HyperPaxeer — © Paxlabs Inc 2026
const { expect } = require('chai');
const { ethers, network } = require('hardhat');
const fs = require('fs');
const path = require('path');
const { deployPECORVault, deployPECOROrders } = require('../helpers/fixtures');
const ONE = 10n ** 18n;
const evidence = {};
async function setup() {
  const [admin, user, keeper, other] = await ethers.getSigners();
  const weth = await (await ethers.getContractFactory('MockWETH9')).deploy();
  const oracle = await (await ethers.getContractFactory('PECORAuditOracle')).deploy();
  const stable = await (await ethers.getContractFactory('MockStandardERC20')).deploy('Stable','USD',18);
  const token = await (await ethers.getContractFactory('MockStandardERC20')).deploy('Asset','A',18);
  const vault = await deployPECORVault({weth:weth.target,admin:admin.address});
  for (const [asset, flag] of [[stable,true],[token,false]]) {
    await vault.registerToken(asset.target,flag); await oracle.setPrice(asset.target,ONE);
  }
  const orders = await deployPECOROrders({priceOracle:oracle.target,vault:vault.target,admin:admin.address});
  await vault.setOperator(orders.target,true); await orders.setKeeper(keeper.address,true);
  await orders.setPricingConfig(0,0,0,false,0);
  await stable.mint(user.address,1000000n*ONE); await stable.connect(user).approve(vault.target,ethers.MaxUint256);
  await token.mint(admin.address,1000000n*ONE); await token.approve(vault.target,ethers.MaxUint256);
  await vault.deposit(token.target,1000000n*ONE);
  return {admin,user,keeper,other,oracle,stable,token,vault,orders};
}
describe('PECOR audit acceptance coverage',function(){
  this.timeout(180000);
  after(function(){
    const file=process.env.PECOR_COVERAGE_OUTPUT
      ? path.resolve(process.env.PECOR_COVERAGE_OUTPUT)
      : path.join(require('os').tmpdir(), `pecor-coverage.${process.pid}.json`);
    fs.mkdirSync(path.dirname(file),{recursive:true}); fs.writeFileSync(file,JSON.stringify(evidence,null,2)+'\n');
  });
  it('scans 1000 real expired/nonmatching orders with bounded gas and complete cursor coverage',async function(){
    const e=await setup(); const amount=1000n;
    const expiry=(await ethers.provider.getBlock('latest')).timestamp+5000;
    // All entries use real placement and escrow; no direct storage mutation.
    for(let i=0;i<1000;i++) await e.orders.connect(e.user).placeLimitBuy(e.stable.target,e.token.target,amount,i%2===0?ONE:ONE/2n,i%2===0?expiry:0);
    await e.orders.connect(e.user).placeLimitBuy(e.stable.target,e.token.target,amount,ONE,0);
    await network.provider.send('evm_increaseTime',[6000]); await network.provider.send('evm_mine');
    expect(await e.orders.getActiveLimitOrderCount()).to.equal(1001n);
    expect(await e.vault.getReservedBalance(e.stable.target)).to.equal(1001n*amount);
    const gas100=await e.orders.getOrdersPage.estimateGas(0,100,100,0);
    const oversized=await e.orders.getOrdersPage.estimateGas(0,1000000,1000000,0);
    expect(gas100).to.be.lt(6000000n);
    expect(oversized>gas100?oversized-gas100:gas100-oversized).to.be.lt(1000n);
    let cursor=0n,pages=0; const found=[];
    while(cursor<1001n){
      const [ids,next]=await e.orders.getOrdersPage(cursor,100,100,0);
      expect(next-cursor).to.be.lte(100n); expect(next).to.be.gt(cursor);
      found.push(...ids); cursor=next; pages++;
    }
    expect(found).to.deep.equal([1001n]); expect(pages).to.equal(11);
    const before=await e.stable.balanceOf(e.user.address);
    await e.orders.connect(e.other).expireLimitOrder(1); await e.orders.connect(e.user).cancelLimitOrder(2);
    expect(await e.stable.balanceOf(e.user.address)).to.equal(before+2n*amount);
    expect(await e.vault.getReservedBalance(e.stable.target)).to.equal(999n*amount);
    expect(await e.orders.getActiveLimitOrderCount()).to.equal(999n);
    evidence.PECOR_07={placed_orders:1001,expired_candidates:500,nonmatching_candidates:500,cursor_pages:pages,max_scan:100,gas_100:gas100.toString(),gas_oversized:oversized.toString(),healthy_id:1001,refunds:'expiry and cancellation returned actual escrow'};
  });
  it('isolates reverting output-token fills in first middle and last batch positions',async function(){
    const e=await setup();
    const bad=await (await ethers.getContractFactory('MockRevertingERC20')).deploy('Broken','BAD',18);
    await bad.setShouldRevert(false); await e.vault.registerToken(bad.target,false); await e.oracle.setPrice(bad.target,ONE);
    await bad.mint(e.admin.address,10n*ONE); await bad.approve(e.vault.target,ethers.MaxUint256);
    await e.vault.deposit(bad.target,10n*ONE); await bad.setShouldRevert(true);
    await e.orders.connect(e.user).placeLimitBuy(e.stable.target,bad.target,ONE,ONE,0);
    for(let i=0;i<6;i++) await e.orders.connect(e.user).placeLimitBuy(e.stable.target,e.token.target,ONE,ONE,0);
    const before=await e.token.balanceOf(e.user.address);
    for(const ids of [[1,2,3],[4,1,5],[6,7,1]]){
      expect(await e.orders.connect(e.keeper).batchExecuteLimitOrders.staticCall(ids)).to.equal(2n);
      await expect(e.orders.connect(e.keeper).batchExecuteLimitOrders(ids)).to.emit(e.orders,'OrderExecutionFailed').withArgs(1,false);
      expect((await e.orders.getLimitOrder(1)).status).to.equal(0n);
    }
    expect(await e.token.balanceOf(e.user.address)).to.equal(before+6n*ONE);
    expect(await e.vault.getReservedBalance(e.stable.target)).to.equal(ONE);
    expect(await e.vault.getReserves(bad.target)).to.equal(10n*ONE);
    await e.orders.connect(e.user).cancelLimitOrder(1);
    expect(await e.vault.getReservedBalance(e.stable.target)).to.equal(0n);
    evidence.PECOR_06={reverting_token:'MockRevertingERC20',positions:['first','middle','last'],healthy_fills:6,bad_order:'pending and refundable after all failures'};
  });
  it('enforces atomic output floors after a stablecoin depeg while retaining USD trigger semantics',async function(){
    const e=await setup();
    await e.orders.connect(e.user).placeOrder([0,e.stable.target,e.token.target,100n*ONE,ONE,0,0,90n*ONE]);
    await e.orders.connect(e.user).placeLimitBuy(e.stable.target,e.token.target,100n*ONE,ONE,0);
    await e.oracle.setPrice(e.stable.target,ONE/2n);
    const [ok,reason]=await e.orders.canExecuteLimitOrder(1);
    expect(ok).to.equal(false); expect(reason).to.equal('Insufficient output');
    await expect(e.orders.connect(e.keeper).executeLimitOrder(1)).to.be.revertedWithCustomError(e.orders,'InsufficientOutput');
    expect((await e.orders.getLimitOrder(1)).status).to.equal(0n);
    expect(await e.vault.getReservedBalance(e.stable.target)).to.equal(200n*ONE);
    const before=await e.token.balanceOf(e.user.address);
    await e.orders.connect(e.keeper).executeLimitOrder(2);
    expect(await e.token.balanceOf(e.user.address)).to.equal(before+50n*ONE);
    await e.orders.connect(e.user).cancelLimitOrder(1);
    expect(await e.vault.getReservedBalance(e.stable.target)).to.equal(0n);
    evidence.PECOR_13={stable_USD_price_before:ONE.toString(),stable_USD_price_after:(ONE/2n).toString(),token_USD_trigger:ONE.toString(),unprotected_fill:(50n*ONE).toString(),atomic_floor:(90n*ONE).toString(),protected_order:'reverted with escrow preserved then cancelled'};
  });
  it('reconciles a healthy bounded subset when another registered asset reverts balanceOf',async function(){
    const e=await setup();
    // Actual deployed EVM fixture returns decimals18 and reverts other selectors;
    // no hardhat_setCode, fabricated balances or Solidity artifact is involved.
    const runtime='60003560e01c63313ce5671460145760006000fd5b601260005260206000f3';
    const length=(runtime.length/2).toString(16).padStart(2,'0');
    const creation=`0x60${length}600c60003960${length}6000f3${runtime}`;
    const receipt=await (await e.admin.sendTransaction({data:creation})).wait(); const broken=receipt.contractAddress;
    expect(await ethers.provider.getCode(broken)).to.equal(`0x${runtime}`);
    await e.vault.registerToken(broken,false);
    const asset=new ethers.Contract(broken,['function decimals() view returns(uint256)','function balanceOf(address) view returns(uint256)'],e.admin);
    expect(await asset.decimals()).to.equal(18n); await expect(asset.balanceOf(e.vault.target)).to.be.reverted;
    await e.token.mint(e.vault.target,7n*ONE); const before=await e.vault.getReserves(e.token.target);
    await expect(e.vault.syncAllReserves()).to.be.reverted;
    expect(await e.vault.getReserves(e.token.target)).to.equal(before);
    await expect(e.vault.syncReservesSubset([e.token.target])).to.emit(e.vault,'ReservesSync');
    expect(await e.vault.getReserves(e.token.target)).to.equal(before+7n*ONE);
    await e.vault.setTokenQuarantine(broken,true); expect(await e.vault.getAvailableReserves(broken)).to.equal(0n);
    const [page,next]=await e.vault.getRegisteredTokensPage(0,100);
    expect(page).to.include(broken); expect(next).to.equal(3n);
    evidence.VAULT_10={fixture_runtime:`0x${runtime}`,failing_selector:'balanceOf(address)',all_assets_sync:'atomic revert',healthy_subset_reconciled:(7n*ONE).toString(),quarantine_and_registry_page:'verified'};
  });
});
