const { expect } = require('chai');
const { ethers } = require('hardhat');

// An approved extension is allowed generic, provenance-carrying telemetry;
// it is not a canonical oracle, treasury, governance source or another asset.
describe('EventEmitter authenticated domains regression', function () {
  let hub, admin, optical, pool, factory, token, unrelated, registry, opticalRegistry;
  beforeEach(async function () {
    [admin, optical, pool, factory, token, unrelated] = await ethers.getSigners();
    const Hub = await ethers.getContractFactory('EventEmitter');
    const impl = await Hub.deploy();
    const Proxy = await ethers.getContractFactory('UUPSProxy');
    const proxy = await Proxy.deploy(await impl.getAddress(), Hub.interface.encodeFunctionData('initialize', [admin.address]));
    hub = Hub.attach(await proxy.getAddress());
    registry = await (await ethers.getContractFactory('MockPoolRegistry')).deploy();
    opticalRegistry = await (await ethers.getContractFactory('MockOpticalRegistry')).deploy();
    await hub.setPoolRegistry(await registry.getAddress());
    await hub.setOpticalRegistry(await opticalRegistry.getAddress());
    await registry.setRegistered(pool.address, true);
    await opticalRegistry.setApproved(optical.address, true);
    await hub.setSidioraFactory(factory.address);
    await hub.connect(factory).registerToken(token.address, pool.address);
  });
  it('public probe includes each dynamic path and revocations fail closed', async function () {
    await hub.setAuthorizedEmitter(admin.address, true);
    for (const signer of [admin, pool, optical, factory, token]) {
      expect(await hub.isAuthorizedEmitter(signer.address)).to.equal(true);
    }
    expect(await hub.isAuthorizedEmitter(unrelated.address)).to.equal(false);
    await opticalRegistry.setApproved(optical.address, false);
    await registry.setRegistered(pool.address, false);
    await hub.deregisterToken(token.address);
    for (const signer of [pool, optical, token]) expect(await hub.isAuthorizedEmitter(signer.address)).to.equal(false);
  });
  it('approved optical cannot forge oracle, treasury or governance typed logs', async function () {
    await expect(hub.connect(optical).emitPriceUpdated(token.address, 1, optical.address, 100, 100, ethers.ZeroHash)).to.be.revertedWithCustomError(hub, 'Unauthorized');
    await expect(hub.connect(optical).emitTreasuryFlow(0, token.address, optical.address, 100)).to.be.revertedWithCustomError(hub, 'Unauthorized');
    await expect(hub.connect(optical).emitGovernance(0, ethers.ZeroHash, optical.address, '0x')).to.be.revertedWithCustomError(hub, 'Unauthorized');
    await expect(hub.connect(optical).emitTokenTransfer(token.address, token.address, optical.address, 100)).to.be.revertedWithCustomError(hub, 'Unauthorized');
  });
  it('selector grants authorize only the configured domain and can be revoked', async function () {
    await hub.setAuthorizedEmitter(unrelated.address, true);
    const selector = hub.interface.getFunction('emitPriceUpdated').selector;
    await expect(hub.connect(unrelated).setTypedEmitter(unrelated.address, selector, true)).to.be.reverted;
    await hub.setTypedEmitter(unrelated.address, selector, true);
    await expect(hub.connect(unrelated).emitPriceUpdated(token.address, 1, unrelated.address, 100, 100, ethers.ZeroHash)).to.emit(hub, 'PriceUpdated');
    await expect(hub.connect(unrelated).emitTreasuryFlow(0, token.address, unrelated.address, 100)).to.be.revertedWithCustomError(hub, 'Unauthorized');
    await hub.setTypedEmitter(unrelated.address, selector, false);
    await expect(hub.connect(unrelated).emitPriceUpdated(token.address, 2, unrelated.address, 100, 100, ethers.ZeroHash)).to.be.revertedWithCustomError(hub, 'Unauthorized');
  });
  it('token registration permits only that token to mirror its own actions', async function () {
    await expect(hub.connect(token).emitTokenTransfer(token.address, pool.address, unrelated.address, 2)).to.emit(hub, 'TokenTransfer');
    await expect(hub.connect(token).emitTokenTransfer(unrelated.address, pool.address, token.address, 2)).to.be.revertedWithCustomError(hub, 'Unauthorized');
    await expect(hub.connect(token).emitAssetApproval(unrelated.address, token.address, optical.address, 2, false)).to.be.revertedWithCustomError(hub, 'Unauthorized');
  });
  it('a registry entry without actual pool identity cannot forge swap subjects', async function () {
    await expect(hub.connect(pool).emitSwap(ethers.id('other-pool'), unrelated.address, true, 1, 2, 0, 2)).to.be.revertedWithCustomError(hub, 'Unauthorized');
  });
  it('registered contract pool can mirror only its canonical pool identifier', async function () {
    const source = await (await ethers.getContractFactory('MockEventSourcePool')).deploy();
    const address = await source.getAddress();
    await registry.setRegistered(address, true);
    await expect(source.mirrorSwap(await hub.getAddress(), ethers.zeroPadValue(address, 32))).to.emit(hub, 'Swap');
    await expect(source.mirrorSwap(await hub.getAddress(), ethers.id('other'))).to.be.revertedWithCustomError(hub, 'Unauthorized');
  });
  it('wired canonical registry is authenticated without a broad static grant', async function () {
    expect(await hub.isAuthorizedEmitter(await registry.getAddress())).to.equal(true);
    await hub.setPoolRegistry(ethers.ZeroAddress);
    expect(await hub.isAuthorizedEmitter(await registry.getAddress())).to.equal(false);
  });
  it('production optical registry authorizes a real optical, revokes it and validates code identity', async function () {
    const Registry = await ethers.getContractFactory('OpticalRegistry');
    const implementation = await Registry.deploy();
    const Proxy = await ethers.getContractFactory('UUPSProxy');
    const proxy = await Proxy.deploy(await implementation.getAddress(), Registry.interface.encodeFunctionData('initialize', [await hub.getAddress(), admin.address]));
    const productionRegistry = Registry.attach(await proxy.getAddress());
    const source = await (await ethers.getContractFactory('CooldownOptical')).deploy(await registry.getAddress(), admin.address, 30);
    const address = await source.getAddress();
    await hub.setOpticalRegistry(await productionRegistry.getAddress());
    expect(await hub.isAuthorizedEmitter(address)).to.equal(false);
    await productionRegistry.registerOptical(address, 'Cooldown', 'Production preset', 1, 'audit regression');
    expect(await productionRegistry.isRegistered(address)).to.equal(true);
    expect(await productionRegistry.isApproved(address)).to.equal(true);
    expect(await hub.isAuthorizedEmitter(address)).to.equal(true);
    // Impersonation isolates the hub authorization boundary; the preset itself
    // does not implement an arbitrary telemetry dispatcher.
    await ethers.provider.send('hardhat_impersonateAccount', [address]);
    await ethers.provider.send('hardhat_setBalance', [address, '0x1000000000000000000']);
    const caller = await ethers.getSigner(address);
    try {
      await expect(hub.connect(caller).emitOpticalLifecycle(5, address, ethers.ZeroAddress, ethers.id('Cooldown'), '0x')).to.emit(hub, 'OpticalLifecycle');
      await expect(hub.connect(caller).emitPriceUpdated(token.address, 1, address, 100, 100, ethers.ZeroHash)).to.be.revertedWithCustomError(hub, 'Unauthorized');
      await productionRegistry.deregisterOptical(address);
      expect(await hub.isAuthorizedEmitter(address)).to.equal(false);
      await expect(hub.connect(caller).emitOpticalLifecycle(5, address, ethers.ZeroAddress, ethers.id('Cooldown'), '0x')).to.be.revertedWithCustomError(hub, 'Unauthorized');
      await productionRegistry.registerOptical(address, 'Cooldown', 'Production preset', 1, 'audit regression');
      await ethers.provider.send('hardhat_setCode', [address, '0x60006000']);
      expect(await productionRegistry.isRegistered(address)).to.equal(false);
      expect(await productionRegistry.isApproved(address)).to.equal(false);
      expect(await hub.isAuthorizedEmitter(address)).to.equal(false);
    } finally {
      await ethers.provider.send('hardhat_stopImpersonatingAccount', [address]);
    }
  });
});
