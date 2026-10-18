const { expect } = require('chai');
const { ethers, network } = require('hardhat');
const fs = require('fs');
const path = require('path');
const core = require('../../scripts/deploy');

describe('Deployment: administrator governance through Timelock without SID', function () {
  this.timeout(240000);
  const files = ['hardhat-addresses.json', 'hardhat-operations.json']
    .map(name => path.resolve(__dirname, '../../deployments', name));
  const backups = new Map();
  let savedEnv, deployer, governanceAdmin, outsider, usd;

  before(async function () {
    expect(network.name).to.equal('hardhat');
    savedEnv = { ...process.env };
    for (const file of files) backups.set(file, fs.existsSync(file) ? fs.readFileSync(file) : null);
    [deployer, governanceAdmin, outsider] = await ethers.getSigners();
  });

  beforeEach(async function () {
    // Only ephemeral hardhat manifests are touched; external network files stay intact.
    for (const file of files) if (fs.existsSync(file)) fs.unlinkSync(file);
    usd = await (await ethers.getContractFactory('MockERC20')).deploy('USD Ledger', 'USDL', 6);
    await usd.waitForDeployment();
    process.env.USDL_ADDRESS = usd.target;
    process.env.GOVERNANCE_MODE = 'admin';
    process.env.GOVERNANCE_ADMIN_ADDRESS = governanceAdmin.address;
    process.env.GUARDIAN_ADDRESS = outsider.address;
    process.env.TIMELOCK_DELAY = '3600';
    process.env.NETWORK_TYPE = 'hardhat';
    delete process.env.SID_ADDRESS;
    delete process.env.ADMIN_ADDRESS;
    delete process.env.RESUME_DEPLOYMENT;
  });

  after(function () {
    if (savedEnv) {
      for (const key of Object.keys(process.env)) if (!(key in savedEnv)) delete process.env[key];
      Object.assign(process.env, savedEnv);
    }
    for (const file of files) {
      if (!backups.has(file)) continue;
      if (backups.get(file) === null) {
        if (fs.existsSync(file)) fs.unlinkSync(file);
      } else fs.writeFileSync(file, backups.get(file));
    }
  });

  async function failure(promise) {
    let error;
    try { await promise; } catch (caught) { error = caught; }
    expect(error, 'expected operation to reject').to.be.instanceOf(Error);
    return error;
  }

  it('deploys, hands off roles, charges one USDL and executes administrator decisions only after delay', async function () {
    const addresses = await core.main();
    const gov = await ethers.getContractAt('GovernanceModule', addresses.GovernanceModule_proxy);
    const config = await ethers.getContractAt('ProtocolConfig', addresses.ProtocolConfig_proxy);
    const treasury = await ethers.getContractAt('Treasury', addresses.Treasury_proxy);
    const router = await ethers.getContractAt('Router', addresses.Router_proxy);
    const timelock = await ethers.getContractAt('Timelock', addresses.Timelock);
    const beacon = await ethers.getContractAt('PoolBeacon', addresses.PoolBeacon);
    const role = await gov.ADMIN_ROLE();

    expect(await gov.votingToken()).to.equal(ethers.ZeroAddress);
    expect(await gov.adminOnlyMode()).to.equal(true);
    expect(await gov.adminModeActive()).to.equal(true);
    expect(await gov.hasRole(role, governanceAdmin.address)).to.equal(true);
    expect(await gov.hasRole(role, deployer.address)).to.equal(false);
    expect(await gov.hasRole(ethers.ZeroHash, addresses.Timelock)).to.equal(true);
    expect(await gov.hasRole(ethers.ZeroHash, deployer.address)).to.equal(false);
    expect(await timelock.proposer()).to.equal(gov.target);
    for (const [key, address] of Object.entries(addresses).filter(([key]) => key.endsWith('_proxy'))) {
      const contract = await ethers.getContractAt(key.replace('_proxy', ''), address);
      expect(await contract.hasRole(ethers.ZeroHash, addresses.Timelock), key).to.equal(true);
      expect(await contract.hasRole(ethers.ZeroHash, deployer.address), key).to.equal(false);
    }
    expect(await config.creationFee()).to.equal(1_000_000n);

    await usd.mint(deployer.address, 1_000_000n);
    await usd.approve(router.target, 1_000_000n);
    const payerBefore = await usd.balanceOf(deployer.address);
    const actualBefore = await usd.balanceOf(treasury.target);
    const ledgerBefore = await treasury.getBalance(usd.target);
    await router.createMarket('Admin Governed Market', 'AGM', 0, ethers.ZeroAddress);
    expect(payerBefore - await usd.balanceOf(deployer.address)).to.equal(1_000_000n);
    expect(await usd.balanceOf(treasury.target) - actualBefore).to.equal(1_000_000n);
    expect(await treasury.getBalance(usd.target) - ledgerBefore).to.equal(1_000_000n);

    await expect(gov.connect(governanceAdmin).propose([config.target], [0], ['0x'], 'No token voting'))
      .to.be.revertedWithCustomError(gov, 'AdminOnlyGovernance');
    await expect(gov.connect(governanceAdmin).deactivateAdminMode())
      .to.be.revertedWithCustomError(gov, 'AdminOnlyGovernance');

    const data = config.interface.encodeFunctionData('setCreationFee', [2_000_000n]);
    const eta = (await ethers.provider.getBlock('latest')).timestamp + Number(await timelock.getMinDelay()) + 60;
    await expect(config.connect(governanceAdmin).setCreationFee(2_000_000n)).to.be.reverted;
    await expect(gov.connect(outsider).adminExecute(config.target, 0, data, eta))
      .to.be.revertedWithCustomError(gov, 'MissingRole');
    await expect(gov.adminExecute(config.target, 0, data, eta)).to.be.revertedWithCustomError(gov, 'MissingRole');
    await gov.connect(governanceAdmin).adminExecute(config.target, 0, data, eta);
    expect(await config.creationFee()).to.equal(1_000_000n);
    await expect(timelock.connect(outsider).executeTransaction(config.target, 0, data, eta))
      .to.be.revertedWithCustomError(timelock, 'DelayNotMet');
    await network.provider.send('evm_setNextBlockTimestamp', [eta]);
    // Execution is permissionless after the administrator-approved operation matures.
    await timelock.connect(outsider).executeTransaction(config.target, 0, data, eta);
    expect(await config.creationFee()).to.equal(2_000_000n);

    const acceptance = addresses._governance.beaconAcceptance;
    const now = (await ethers.provider.getBlock('latest')).timestamp;
    if (now < acceptance.eta) await network.provider.send('evm_setNextBlockTimestamp', [acceptance.eta]);
    await timelock.executeTransaction(acceptance.target, 0, acceptance.data, acceptance.eta);
    expect(await beacon.owner()).to.equal(timelock.target);
    process.env.RESUME_DEPLOYMENT = 'true';
    const nonceBeforeResume = await ethers.provider.getTransactionCount(deployer.address);
    const resumed = await core.main();
    expect(await ethers.provider.getTransactionCount(deployer.address)).to.equal(nonceBeforeResume);
    expect(resumed._meta.status).to.equal('finalized');
    expect(resumed._governance.status).to.equal('finalized');
    expect(await gov.hasRole(role, governanceAdmin.address)).to.equal(true);
    expect(await gov.adminModeActive()).to.equal(true);
    expect(await config.creationFee()).to.equal(2_000_000n);
  });

  it('rejects a changed governance administrator on resume without transactions or checkpoint mutation', async function () {
    const sentinel = 'Intentional stop after confirmed governance proxy';
    const interrupted = await failure(core.main({ afterCheckpoint(stage) {
      if (stage === 'proxy:GovernanceModule') throw new Error(sentinel);
    } }));
    expect(interrupted.message).to.equal(sentinel);
    const beforeFiles = files.map(file => fs.readFileSync(file));
    const nonce = await ethers.provider.getTransactionCount(deployer.address);
    process.env.RESUME_DEPLOYMENT = 'true';
    process.env.GOVERNANCE_ADMIN_ADDRESS = outsider.address;
    const changed = await failure(core.main());
    expect(changed.message).to.include('configuration identity mismatch');
    expect(await ethers.provider.getTransactionCount(deployer.address)).to.equal(nonce);
    files.forEach((file, index) => expect(fs.readFileSync(file).equals(beforeFiles[index]), file).to.equal(true));
  });

  it('defaults to administrator governance with the deployer as administrator when no override or SID is supplied', async function () {
    delete process.env.GOVERNANCE_MODE;
    delete process.env.GOVERNANCE_ADMIN_ADDRESS;
    const sentinel = 'Inspected default admin initializer';
    const error = await failure(core.main({ afterCheckpoint(stage, addresses) {
      if (stage === 'proxy:GovernanceModule') {
        expect(addresses.GovernanceModule_proxy).to.be.a('string');
        throw new Error(sentinel);
      }
    } }));
    expect(error.message).to.equal(sentinel);
    const addresses = JSON.parse(fs.readFileSync(files[0]));
    const gov = await ethers.getContractAt('GovernanceModule', addresses.GovernanceModule_proxy);
    expect(await gov.adminOnlyMode()).to.equal(true);
    expect(await gov.adminModeActive()).to.equal(true);
    expect(await gov.votingToken()).to.equal(ethers.ZeroAddress);
    expect(await gov.hasRole(await gov.ADMIN_ROLE(), deployer.address)).to.equal(true);
  });
});
