const { expect } = require('chai');
const { ethers, network } = require('hardhat');

describe('Primitive audit remediation', function () {
  it('records historical balances and supply across mint, transfer, self-transfer and burn', async function () {
    const [a, b] = await ethers.getSigners();
    const t = await (await ethers.getContractFactory('MockERC20Base')).deploy('Audit', 'AUD', 18);
    const mint = await (await t.mint(a.address, 100)).wait();
    const transfer = await (await t.transfer(b.address, 30)).wait();
    await t.connect(b).transfer(b.address, 1);
    const burn = await (await t.burn(a.address, 20)).wait();
    await network.provider.send('evm_mine');
    expect(await t.getPastBalance(a.address, mint.blockNumber)).to.equal(100);
    expect(await t.getPastVotes(a.address, transfer.blockNumber)).to.equal(70);
    expect(await t.getPastBalance(b.address, burn.blockNumber)).to.equal(30);
    expect(await t.getPastTotalSupply(mint.blockNumber)).to.equal(100);
    expect(await t.getPastTotalSupply(burn.blockNumber)).to.equal(80);
    expect(await t.getPastBalance(a.address, mint.blockNumber - 1)).to.equal(0);
    await expect(t.getPastTotalSupply(await ethers.provider.getBlockNumber())).to.be.revertedWithCustomError(t, 'FutureLookup');
  });

  it('rejects replayed and invalid permits without consuming extra nonces', async function () {
    const [a, b] = await ethers.getSigners();
    const t = await (await ethers.getContractFactory('MockERC20Base')).deploy('Audit', 'AUD', 18);
    const chainId = (await ethers.provider.getNetwork()).chainId;
    const domain = { name: 'Audit', version: '1', chainId, verifyingContract: await t.getAddress() };
    expect(await t.DOMAIN_SEPARATOR()).to.equal(ethers.TypedDataEncoder.hashDomain(domain));
    const types = { Permit: [ {name:'owner',type:'address'}, {name:'spender',type:'address'}, {name:'value',type:'uint256'}, {name:'nonce',type:'uint256'}, {name:'deadline',type:'uint256'} ] };
    const deadline = BigInt((await ethers.provider.getBlock('latest')).timestamp + 1000);
    const s = ethers.Signature.from(await a.signTypedData(domain, types, {owner:a.address,spender:b.address,value:12,nonce:0,deadline}));
    await t.permit(a.address,b.address,12,deadline,s.v,s.r,s.s);
    await expect(t.permit(a.address,b.address,12,deadline,s.v,s.r,s.s)).to.be.revertedWithCustomError(t,'InvalidPermit');
    expect(await t.nonces(a.address)).to.equal(1);
    await expect(t.permit(a.address,b.address,13,deadline,s.v,s.r,s.s)).to.be.revertedWithCustomError(t,'InvalidPermit');
    expect(await t.nonces(a.address)).to.equal(1);
  });

  it('supports storage-writing WPAX wallets and preserves backing on rejection and reentry', async function () {
    const w = await (await ethers.getContractFactory('WPAX9')).deploy();
    const receiver = await (await ethers.getContractFactory('PrimitiveWallet')).deploy(await w.getAddress());
    await receiver.deposit({value:100});
    await expect(receiver.withdraw(40,true,false)).to.be.revertedWith('WPAX9: native transfer failed');
    expect(await w.balanceOf(await receiver.getAddress())).to.equal(100);
    expect(await ethers.provider.getBalance(await w.getAddress())).to.equal(100);
    await receiver.withdraw(40,false,false);
    expect(await receiver.received()).to.equal(40);
    await receiver.withdraw(20,false,true);
    expect(await receiver.received()).to.equal(100);
    expect(await w.balanceOf(await receiver.getAddress())).to.equal(0);
    expect(await ethers.provider.getBalance(await w.getAddress())).to.equal(0);
  });

  it('rejects no-code token targets for each safe transfer operation', async function () {
    const [a,b] = await ethers.getSigners();
    const t = await (await ethers.getContractFactory('TransferHelperWrapper')).deploy();
    await expect(t.safeTransfer(a.address,b.address,1)).to.be.revertedWithCustomError(t,'TransferFailed');
    await expect(t.safeTransferFrom(a.address,a.address,b.address,1)).to.be.revertedWithCustomError(t,'TransferFromFailed');
    await expect(t.safeApprove(a.address,b.address,1)).to.be.revertedWithCustomError(t,'ApproveFailed');
  });

  it('rejects malformed ERC20 success words without a decoding panic', async function () {
    const [a] = await ethers.getSigners();
    const t = await (await ethers.getContractFactory('TransferHelperWrapper')).deploy();
    const malformed = await (await ethers.getContractFactory('PrimitiveMalformedToken')).deploy();
    await expect(t.safeTransfer(await malformed.getAddress(),a.address,1)).to.be.revertedWithCustomError(t,'TransferFailed');
    await expect(t.safeTransferFrom(await malformed.getAddress(),a.address,a.address,1)).to.be.revertedWithCustomError(t,'TransferFromFailed');
    await expect(t.safeApprove(await malformed.getAddress(),a.address,1)).to.be.revertedWithCustomError(t,'ApproveFailed');
  });

  it('rejects zero role grants and preserves a surviving default admin', async function () {
    const [a,b] = await ethers.getSigners();
    const ac = await (await ethers.getContractFactory('MockAccessControl')).deploy();
    const role = ethers.ZeroHash;
    await expect(ac.grantRole(role,ethers.ZeroAddress)).to.be.revertedWithCustomError(ac,'InvalidRoleAccount');
    await expect(ac.renounceRole(role,a.address)).to.be.revertedWithCustomError(ac,'LastDefaultAdmin');
    await ac.grantRole(role,b.address);
    expect(await ac.registeredDefaultAdminCount()).to.equal(2);
    await ac.renounceRole(role,a.address);
    expect(await ac.hasRole(role,b.address)).to.equal(true);
    await expect(ac.connect(b).revokeRole(role,b.address)).to.be.revertedWithCustomError(ac,'LastDefaultAdmin');
  });

  it('requires beacon ownership acceptance from the nominated recipient', async function () {
    const [a,b,c] = await ethers.getSigners();
    const impl = await (await ethers.getContractFactory('MockImplementation')).deploy();
    const beacon = await (await ethers.getContractFactory('UpgradeableBeacon')).deploy(await impl.getAddress(),a.address);
    await beacon.transferOwnership(b.address);
    expect(await beacon.owner()).to.equal(a.address);
    await expect(beacon.connect(c).acceptOwnership()).to.be.revertedWithCustomError(beacon,'Unauthorized');
    await beacon.connect(b).acceptOwnership();
    expect(await beacon.owner()).to.equal(b.address);
    expect(await beacon.pendingOwner()).to.equal(ethers.ZeroAddress);
  });

  it('rejects beacon implementations without code', async function () {
    const [a] = await ethers.getSigners();
    const Proxy = await ethers.getContractFactory('BeaconProxy');
    for (const target of [ethers.ZeroAddress, a.address]) {
      const beacon = await (await ethers.getContractFactory('PrimitiveBadBeacon')).deploy(target);
      await expect(Proxy.deploy(await beacon.getAddress(),'0x')).to.be.revertedWithCustomError(Proxy,'BeaconCallFailed');
    }
  });

  it('rejects unintended delegate context and rolls back failed upgrade migrations and native value', async function () {
    const [a] = await ethers.getSigners();
    const V1 = await ethers.getContractFactory('MockUUPS');
    const v1 = await V1.deploy();
    const v2 = await (await ethers.getContractFactory('PrimitiveMigration')).deploy();
    const proxy = await (await ethers.getContractFactory('UUPSProxy')).deploy(await v1.getAddress(),v1.interface.encodeFunctionData('initialize',[a.address]));
    const p = V1.attach(await proxy.getAddress());
    await p.setValue(77);
    await expect(p.upgradeToAndCall(await v2.getAddress(),'0x',{value:1})).to.be.revertedWithCustomError(p,'NonPayableUpgrade');
    await expect(p.upgradeToAndCall(await v2.getAddress(),v2.interface.encodeFunctionData('fail'))).to.be.revertedWithCustomError(v2,'MigrationRefused').withArgs(42);
    expect(await p.value()).to.equal(77);
    expect(await p.version()).to.equal(1);
    const wrapper = await (await ethers.getContractFactory('PrimitiveDelegateWrapper')).deploy(await v1.getAddress());
    await expect(V1.attach(await wrapper.getAddress()).upgradeToAndCall(await v2.getAddress(),'0x')).to.be.revertedWithCustomError(v1,'UUPSNotThroughActiveProxy');
  });

  it('keeps enumeration valid across self-transfer, callback forwarding, burn and remint', async function () {
    const [a,b] = await ethers.getSigners();
    const nft = await (await ethers.getContractFactory('MockERC721Enumerable')).deploy();
    for (let i=0;i<8;i++) await nft.mint(a.address,i);
    await nft.approve(b.address,3);
    await nft.connect(b).transferFrom(a.address,a.address,3);
    expect(await nft.getApproved(3)).to.equal(ethers.ZeroAddress);
    for (let i=0;i<8;i++) expect(await nft.tokenOfOwnerByIndex(a.address,i)).to.equal(i);
    const receiver = await (await ethers.getContractFactory('PrimitiveNFTReceiver')).deploy(b.address,false);
    await nft['safeTransferFrom(address,address,uint256)'](a.address,await receiver.getAddress(),3);
    await nft.burn(4); await nft.mint(b.address,4);
    const global = new Set();
    for (let i=0;i<8;i++) global.add((await nft.tokenByIndex(i)).toString());
    expect(global.size).to.equal(8);
    for (const owner of [a,b]) {
      const n = Number(await nft.balanceOf(owner.address)); const ids = new Set();
      for (let i=0;i<n;i++) { const id = await nft.tokenOfOwnerByIndex(owner.address,i); ids.add(id.toString()); expect(await nft.ownerOf(id)).to.equal(owner.address); }
      expect(ids.size).to.equal(n);
    }
    const refusing = await (await ethers.getContractFactory('PrimitiveNFTReceiver')).deploy(ethers.ZeroAddress,true);
    await expect(nft['safeTransferFrom(address,address,uint256)'](a.address,await refusing.getAddress(),0)).to.be.revertedWithCustomError(refusing,'ReceiverRefused').withArgs(0);
    expect(await nft.ownerOf(0)).to.equal(a.address);
  });

  it('maintains unique global and owner membership through a seeded transition campaign', async function () {
    const owners = (await ethers.getSigners()).slice(0,3);
    const nft = await (await ethers.getContractFactory('MockERC721Enumerable')).deploy();
    const model = new Map();
    let seed = 0x51d10;
    const random = (n) => { seed = (Math.imul(seed,1664525) + 1013904223) >>> 0; return seed % n; };
    for (let step=0;step<48;step++) {
      const id = random(12);
      if (!model.has(id)) {
        const next = random(3); await nft.mint(owners[next].address,id); model.set(id,next);
      } else {
        const current = model.get(id);
        if (random(4) === 0) { await nft.burn(id); model.delete(id); }
        else { const next = random(3); await nft.connect(owners[current]).transferFrom(owners[current].address,owners[next].address,id); model.set(id,next); }
      }
      expect(await nft.totalSupply()).to.equal(model.size);
      const global = [];
      for (let i=0;i<model.size;i++) global.push(Number(await nft.tokenByIndex(i)));
      expect(new Set(global).size).to.equal(model.size);
      expect(global.sort((a,b)=>a-b)).to.deep.equal([...model.keys()].sort((a,b)=>a-b));
      for (let owner=0;owner<3;owner++) {
        const expected = [...model].filter(([,o])=>o===owner).map(([id])=>id).sort((a,b)=>a-b);
        expect(await nft.balanceOf(owners[owner].address)).to.equal(expected.length);
        const actual = [];
        for (let i=0;i<expected.length;i++) actual.push(Number(await nft.tokenOfOwnerByIndex(owners[owner].address,i)));
        expect(actual.sort((a,b)=>a-b)).to.deep.equal(expected);
      }
    }
  });

  it('represents the absolute magnitude of int256.min', async function () {
    const m = await (await ethers.getContractFactory('PrimitiveAbs')).deploy();
    expect(await m.abs(-(1n << 255n))).to.equal(1n << 255n);
  });
});
