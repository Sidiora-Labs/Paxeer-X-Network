const { expect } = require('chai');
const hre = require('hardhat');
const { ethers } = hre;
const { deployPECORVault } = require('../meta-ag/helpers/fixtures');
const { FeeStrategy } = require('../helpers/constants');

describe('Production primitive integration invariants', function () {
  it('uses production WPAX bytecode through proxied vault unwrap, recipient rejection and operator reentry', async function () {
    const [admin, operator] = await ethers.getSigners();
    const wpax = await (await ethers.getContractFactory('WPAX9')).deploy();
    const vault = await deployPECORVault({weth:wpax.target,admin:admin.address});
    await vault.registerToken(wpax.target,false);
    await vault.setOperator(operator.address,true);
    const receiver = await (await ethers.getContractFactory('PrimitiveVaultReceiver')).deploy(vault.target);
    // Receiver also holds operator authority: callback rejection must come
    // from the reentrancy guard rather than merely failing authorization.
    await vault.setOperator(receiver.target,true);
    await vault.depositNative({value:1000});
    await receiver.configure(true,false);
    await expect(vault.connect(operator).withdrawNative(100,receiver.target)).to.be.revertedWithCustomError(vault,'NativeTransferFailed');
    expect(await vault.getReserves(wpax.target)).to.equal(1000);
    expect(await wpax.balanceOf(vault.target)).to.equal(1000);
    expect(await ethers.provider.getBalance(wpax.target)).to.equal(1000);
    expect(await ethers.provider.getBalance(vault.target)).to.equal(0);
    await receiver.configure(false,true);
    await vault.connect(operator).withdrawNative(400,receiver.target);
    expect(await receiver.received()).to.equal(400);
    expect(await receiver.reentrySucceeded()).to.equal(false);
    expect(await vault.getReserves(wpax.target)).to.equal(600);
    expect(await wpax.balanceOf(vault.target)).to.equal(600);
    expect(await ethers.provider.getBalance(wpax.target)).to.equal(600);
    expect(await ethers.provider.getBalance(vault.target)).to.equal(0);
    await vault.connect(operator).withdrawNative(600,operator.address);
    expect(await vault.getReserves(wpax.target)).to.equal(0);
    expect(await wpax.balanceOf(vault.target)).to.equal(0);
    expect(await ethers.provider.getBalance(wpax.target)).to.equal(0);
  });

  it('preserves production SidioraNFT proxy enumeration and pool identity through a seeded campaign and callbacks', async function () {
    const owners = (await ethers.getSigners()).slice(0,3);
    const NFT = await ethers.getContractFactory('SidioraNFT');
    const impl = await NFT.deploy();
    // No external event registry required for ERC721 ownership semantics.
    const proxy = await (await ethers.getContractFactory('UUPSProxy')).deploy(impl.target,NFT.interface.encodeFunctionData('initialize',['Audit','AUD',ethers.ZeroAddress,owners[0].address]));
    const nft = NFT.attach(proxy.target);
    await nft.grantRole(await nft.MINTER_ROLE(),owners[0].address);
    const model = new Map();
    const pools = new Map();
    let seed = 0x165721;
    const random = n => { seed = (Math.imul(seed,1664525)+1013904223) >>> 0; return seed % n; };
    async function assertModel() {
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
      for (const [id,owner] of model) {
        expect(await nft.ownerOf(id)).to.equal(owners[owner].address);
        expect(await nft.getPoolAddress(id)).to.equal(pools.get(id));
        expect(await nft.poolTokenId(pools.get(id))).to.equal(id);
      }
    }
    for (let step=0;step<40;step++) {
      if (step<8 || random(4)===0) {
        const id = Number(await nft.nextTokenId());
        const pool = ethers.getAddress(ethers.zeroPadValue(ethers.toBeHex(10000+id),20));
        const owner = random(3);
        // Synthetic pools lack a snapshot-capable accumulator; AIRDROP
        // rejection and supported AIRDROP paths belong to core integration.
        const strategies = [FeeStrategy.CLAIM,FeeStrategy.BURN,FeeStrategy.LP_REWARDS];
        await nft.mint(owners[owner].address,pool,strategies[random(strategies.length)]);
        model.set(id,owner); pools.set(id,pool);
      } else {
        const ids = [...model.keys()]; const id = ids[random(ids.length)];
        const oldOwner = model.get(id); const newOwner = random(3);
        await nft.connect(owners[oldOwner]).transferFrom(owners[oldOwner].address,owners[newOwner].address,id);
        model.set(id,newOwner);
      }
      await assertModel();
    }
    const id = [...model.keys()][0]; const owner = model.get(id);
    const forward = await (await ethers.getContractFactory('PrimitiveNFTReceiver')).deploy(owners[1].address,false);
    await nft.connect(owners[owner])['safeTransferFrom(address,address,uint256)'](owners[owner].address,forward.target,id);
    model.set(id,1); await assertModel();
    const refusing = await (await ethers.getContractFactory('PrimitiveNFTReceiver')).deploy(ethers.ZeroAddress,true);
    await expect(nft.connect(owners[1])['safeTransferFrom(address,address,uint256)'](owners[1].address,refusing.target,id)).to.be.revertedWithCustomError(refusing,'ReceiverRefused').withArgs(id);
    await assertModel();
    // Production NFT intentionally exposes no public burn/remint operation.
    // Those transitions remain covered by the base enumeration harness.
  });

  it('recomputes the domain and rejects old-chain permits on an EVM with changed chain ID', async function () {
    const [owner,spender] = await ethers.getSigners();
    const token = await (await ethers.getContractFactory('MockERC20Base')).deploy('Fork Test','FORK',18);
    const originalChain = (await ethers.provider.getNetwork()).chainId;
    const changedChain = originalChain + 1n;
    const { createProvider } = require('hardhat/internal/core/providers/construction');
    const raw = await createProvider({...hre.config,networks:{...hre.config.networks,hardhat:{...hre.config.networks.hardhat,chainId:Number(changedChain)}}},'hardhat',hre.artifacts);
    const branch = new ethers.BrowserProvider(raw);
    // Hardhat cannot mutate CHAINID on an existing provider. Copy the
    // deployed runtime (including cached original-chain immutables) and token
    // storage into a distinct EVM with a different actual CHAINID instead.
    await raw.request({method:'hardhat_setCode',params:[token.target,await ethers.provider.getCode(token.target)]});
    for (let slot=0;slot<3;slot++) {
      await raw.request({method:'hardhat_setStorageAt',params:[token.target,ethers.toQuantity(slot),await ethers.provider.getStorage(token.target,slot)]});
    }
    const signer = await branch.getSigner(owner.address);
    const forkToken = new ethers.Contract(token.target,token.interface,signer);
    const domain = {name:'Fork Test',version:'1',chainId:originalChain,verifyingContract:token.target};
    const changedDomain = {...domain,chainId:changedChain};
    expect(await token.DOMAIN_SEPARATOR()).to.equal(ethers.TypedDataEncoder.hashDomain(domain));
    expect(await forkToken.DOMAIN_SEPARATOR()).to.equal(ethers.TypedDataEncoder.hashDomain(changedDomain));
    const deadline = BigInt(Math.max((await ethers.provider.getBlock('latest')).timestamp,(await branch.getBlock('latest')).timestamp)+1000);
    const types = {Permit:[{name:'owner',type:'address'},{name:'spender',type:'address'},{name:'value',type:'uint256'},{name:'nonce',type:'uint256'},{name:'deadline',type:'uint256'}]};
    const value = {owner:owner.address,spender:spender.address,value:77,nonce:0,deadline};
    const oldSig = ethers.Signature.from(await owner.signTypedData(domain,types,value));
    await expect(forkToken.permit(owner.address,spender.address,77,deadline,oldSig.v,oldSig.r,oldSig.s)).to.be.reverted;
    expect(await forkToken.nonces(owner.address)).to.equal(0);
    const currentSig = ethers.Signature.from(await owner.signTypedData(changedDomain,types,value));
    await (await forkToken.permit(owner.address,spender.address,77,deadline,currentSig.v,currentSig.r,currentSig.s)).wait();
    expect(await forkToken.nonces(owner.address)).to.equal(1);
    expect(await forkToken.allowance(owner.address,spender.address)).to.equal(77);
    await expect(forkToken.permit(owner.address,spender.address,77,deadline,currentSig.v,currentSig.r,currentSig.s)).to.be.reverted;
    expect(await forkToken.nonces(owner.address)).to.equal(1);
  });
});
