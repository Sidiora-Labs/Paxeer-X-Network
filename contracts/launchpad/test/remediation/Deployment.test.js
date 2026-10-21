const { expect } = require('chai');
const { ethers, network, artifacts } = require('hardhat');
const fs = require('fs');
const path = require('path');
const core = require('../../scripts/deploy');
const meta = require('../../scripts/meta-ag/deploy-pecor-meta-ag');

describe('Remediation: actual ephemeral deployment scripts', function () {
  this.timeout(240000);
  const files = ['hardhat-addresses.json', 'hardhat-operations.json'].map(n => path.resolve(__dirname, '../../deployments', n));
  const backups = new Map();
  let savedEnv, deployer, outsider, coreOnlyFiles;
  before(async function () {
    expect(network.name).to.equal('hardhat');
    savedEnv = { ...process.env };
    for (const file of files) {
      backups.set(file, fs.existsSync(file) ? fs.readFileSync(file) : null);
      if (fs.existsSync(file)) fs.unlinkSync(file);
    }
    [deployer, outsider] = await ethers.getSigners();
    const usd = await (await ethers.getContractFactory('MockERC20')).deploy('USDL', 'USDL', 6);
    const sid = await (await ethers.getContractFactory('SidioraERC20')).deploy('SID', 'SID', 10n ** 15n, deployer.address);
    await Promise.all([usd.waitForDeployment(), sid.waitForDeployment()]);
    process.env.USDL_ADDRESS = await usd.getAddress();
    process.env.SID_ADDRESS = await sid.getAddress();
    process.env.GOVERNANCE_MODE = 'token';
    delete process.env.GOVERNANCE_ADMIN_ADDRESS;
    process.env.GUARDIAN_ADDRESS = outsider.address;
    process.env.FEE_COLLECTOR = outsider.address;
    process.env.RELAYER_ADDRESS = deployer.address;
    process.env.KEEPER_ADDRESS = deployer.address;
    process.env.VAULT_ADAPTER_FEE_BPS = '0';
    process.env.ORACLE_DEVIATION_BPS = '500';
    process.env.ENABLE_SIDIORA_SPOT_REFERENCE = 'false';
    process.env.TIMELOCK_DELAY = '3600';
    process.env.NETWORK_TYPE = 'hardhat';
    process.env.SEED_PRICES = 'true';
    delete process.env.ADMIN_ADDRESS;
    delete process.env.WPAX_ADDRESS;
    delete process.env.INCIDENT_GUARDIAN_ADDRESS;
    delete process.env.SIDIORA_MIN_LIQUIDITY_RAW;
    delete process.env.USDL_USD_ORACLE_ADDRESS;
    delete process.env.RESUME_DEPLOYMENT;
  });
  after(function () {
    for (const key of Object.keys(process.env)) if (!(key in savedEnv)) delete process.env[key];
    Object.assign(process.env, savedEnv);
    for (const file of files) {
      if (backups.get(file) === null) { if (fs.existsSync(file)) fs.unlinkSync(file); }
      else fs.writeFileSync(file, backups.get(file));
    }
  });
  it('keeps every production runtime within the EIP-170 limit', async function () {
    for (const fqName of await artifacts.getAllFullyQualifiedNames()) {
      if (fqName.includes('/test/')) continue;
      const artifact = await artifacts.readArtifact(fqName);
      expect((artifact.deployedBytecode.length - 2) / 2, fqName).to.be.at.most(24576);
    }
  });
  it('rejects external core admin before deploying or writing a checkpoint', async function () {
    process.env.ADMIN_ADDRESS = outsider.address;
    const before = await ethers.provider.getTransactionCount(deployer.address);
    let error;
    try { await core.main(); } catch (e) { error = e; }
    expect(error.message).to.include('ADMIN_ADDRESS override unsupported');
    expect(await ethers.provider.getTransactionCount(deployer.address)).to.equal(before);
    expect(fs.existsSync(files[0])).to.equal(false);
    delete process.env.ADMIN_ADDRESS;
  });
  it('rejects non-six-decimal USDL before deploying or writing a checkpoint', async function () {
    const wrongDecimals = await (await ethers.getContractFactory('MockERC20')).deploy('USDL', 'USDL', 18);
    await wrongDecimals.waitForDeployment();
    const originalUsdl = process.env.USDL_ADDRESS;
    process.env.USDL_ADDRESS = await wrongDecimals.getAddress();
    const before = await ethers.provider.getTransactionCount(deployer.address);
    try {
      let error;
      try { await core.main(); } catch (e) { error = e; }
      expect(error?.message).to.include('requires 6-decimal USDL');
      expect(await ethers.provider.getTransactionCount(deployer.address)).to.equal(before);
      for (const file of files) expect(fs.existsSync(file)).to.equal(false);
    } finally { process.env.USDL_ADDRESS = originalUsdl; }
  });
  it('deploys core with treasury producer roles, typed events and governed admin handoff', async function () {
    await core.main();
    const a = JSON.parse(fs.readFileSync(files[0]));
    const treasury = await ethers.getContractAt('Treasury', a.Treasury_proxy);
    const config = await ethers.getContractAt('ProtocolConfig', a.ProtocolConfig_proxy);
    expect(await config.creationFee()).to.equal(1_000_000n);
    for (const name of ['FeeAccumulator', 'SidioraFactory', 'Router']) expect(await treasury.hasRole(ethers.id('DEPOSITOR_ROLE'), a[`${name}_proxy`])).to.equal(true);
    for (const [key, address] of Object.entries(a).filter(([key]) => key.endsWith('_proxy'))) {
      const ct = await ethers.getContractAt(key.replace('_proxy', ''), address);
      expect(await ct.hasRole(ethers.ZeroHash, a.Timelock), key).to.equal(true);
      expect(await ct.hasRole(ethers.ZeroHash, deployer.address), key).to.equal(false);
    }
    const emitter = await ethers.getContractAt('EventEmitter', a.EventEmitter_proxy);
    expect(await emitter.sidioraFactory()).to.equal(a.SidioraFactory_proxy);
    for (const [name, methods] of Object.entries({ ProtocolConfig: ['emitConfigUpdated'], FeeAccumulator: ['emitFeeRecorded', 'emitFeeDistributed', 'emitFeeFlow'], SidioraNFT: ['emitFeeStrategyChanged', 'emitNftMint'], Router: ['emitRouterTrade'], FeesRouter: ['emitFeeDistributed'] })) {
      for (const method of methods) expect(await emitter.isTypedEmitter(a[`${name}_proxy`], emitter.interface.getFunction(method).selector), `${name}.${method}`).to.equal(true);
    }
    const timelock = await ethers.getContractAt('Timelock', a.Timelock);
    expect(await timelock.proposer()).to.equal(a.GovernanceModule_proxy);
    const beacon = await ethers.getContractAt('PoolBeacon', a.PoolBeacon);
    expect(await beacon.pendingOwner()).to.equal(a.Timelock);
    expect(a._meta.status).to.equal('pending-beacon-acceptance');
    await network.provider.send('evm_setNextBlockTimestamp', [a._governance.beaconAcceptance.eta]);
    await timelock.executeTransaction(a.PoolBeacon, 0, a._governance.beaconAcceptance.data, a._governance.beaconAcceptance.eta);
    expect(await beacon.owner()).to.equal(a.Timelock);
    // Reconcile persisted governance state after the delayed acceptance executed.
    // A read-only resume must finalize the manifest without another transaction.
    const nonceAfterAcceptance = await ethers.provider.getTransactionCount(deployer.address);
    process.env.RESUME_DEPLOYMENT = 'true';
    try { await core.main(); } finally { delete process.env.RESUME_DEPLOYMENT; }
    expect(await ethers.provider.getTransactionCount(deployer.address)).to.equal(nonceAfterAcceptance);
    expect(JSON.parse(fs.readFileSync(files[0]))._governance.status).to.equal('finalized');
    expect(await config.creationFee()).to.equal(1_000_000n);
    const journal = JSON.parse(fs.readFileSync(files[1]));
    expect(journal.events.length).to.be.greaterThan(25);
    for (const event of journal.events) expect(await ethers.provider.getCode(event.address)).to.not.equal('0x');
    coreOnlyFiles = files.map(file => fs.readFileSync(file));
  });
  it('fails closed on stale guardian and old plans, and leaves missing guardian authority with Timelock', async function () {
    const original = fs.readFileSync(files[0]);
    const before = await ethers.provider.getTransactionCount(deployer.address);
    try {
      const fixture = JSON.parse(original);
      fixture._meta.guardian = deployer.address;
      fs.writeFileSync(files[0], JSON.stringify(fixture));
      let stale;
      try { await meta.main(); } catch (error) { stale = error; }
      expect(stale?.message).to.include('guardian differs');
      delete fixture._meta.guardian;
      fs.writeFileSync(files[0], JSON.stringify(fixture));
      let safeDefault;
      try { await meta.main({ afterCheckpoint(stage, addresses) {
        if (stage === 'session') {
          expect(addresses._metaAgDeployment.config.incidentGuardian).to.equal(fixture.Timelock.toLowerCase());
          expect(addresses._metaAgDeployment.config.incidentGuardianSource).to.equal('timelock-only');
          throw new Error('Inspected safe incident default');
        }
      } }); } catch (error) { safeDefault = error; }
      expect(safeDefault?.message).to.equal('Inspected safe incident default');
      fixture._metaAgDeployment = { version: 1, config: {} };
      fs.writeFileSync(files[0], JSON.stringify(fixture));
      let legacy;
      try { await meta.main(); } catch (error) { legacy = error; }
      expect(legacy?.message).to.include('explicit operator migration required');
      expect(await ethers.provider.getTransactionCount(deployer.address)).to.equal(before);
    } finally { fs.writeFileSync(files[0], original); }
  });
  it('rejects external layered admin before the first transaction then deploys governed PECOR and MetaAG', async function () {
    process.env.ADMIN_ADDRESS = outsider.address;
    const before = await ethers.provider.getTransactionCount(deployer.address);
    let error;
    try { await meta.main(); } catch (e) { error = e; }
    expect(error.message).to.include('MetaAG final admin');
    expect(await ethers.provider.getTransactionCount(deployer.address)).to.equal(before);
    delete process.env.ADMIN_ADDRESS;
    await meta.main();
    const a = JSON.parse(fs.readFileSync(files[0]));
    for (const name of ['PriceOracle', 'OracleHub', 'TransactionTracker', 'PECORVault', 'PECOR', 'PECOROrders', 'MetaAGRouter', 'MetaAGQuoter', 'VaultAdapter', 'SidioraAdapter', 'SidioraFeedAdapter']) {
      const ct = await ethers.getContractAt(name, a[`${name}_proxy`] || a[name]);
      expect(await ct.hasRole(ethers.ZeroHash, a.Timelock), name).to.equal(true);
      expect(await ct.hasRole(ethers.ZeroHash, deployer.address), name).to.equal(false);
    }
    const router = await ethers.getContractAt('MetaAGRouter', a.MetaAGRouter_proxy);
    expect(await router.adapterCount()).to.equal(2n);
    const feed = await ethers.getContractAt('SidioraFeedAdapter', a.SidioraFeedAdapter);
    expect(await feed.spotReferenceEnabled()).to.equal(false);
    expect(await feed.minLiquidityThreshold()).to.be.greaterThan(0n);
    expect(a._meta_pecor.status).to.equal('finalized');
    for (const name of ['PECOR', 'PECOROrders', 'VaultAdapter']) {
      const contract = await ethers.getContractAt(name, a[`${name}_proxy`] || a[name]);
      expect(await contract.feeCollector(), name).to.equal(outsider.address);
    }
    const engine = await ethers.getContractAt('PECOR', a.PECOR_proxy);
    expect(await engine.hasRole(ethers.id('FEE_COLLECTOR_ROLE'), outsider.address)).to.equal(true);
    expect(await engine.hasRole(ethers.id('FEE_COLLECTOR_ROLE'), deployer.address)).to.equal(false);
    const controller = await ethers.getContractAt('EmergencyPauseController', a.EmergencyPauseController);
    expect(await controller.hasRole(ethers.ZeroHash, a.Timelock)).to.equal(true);
    expect(await controller.hasRole(ethers.ZeroHash, deployer.address)).to.equal(false);
    expect(await controller.hasRole(ethers.id('INCIDENT_GUARDIAN_ROLE'), outsider.address)).to.equal(true);
    expect(a._meta_pecor.incidentGuardianSource).to.equal('core-guardian');
    expect(a._meta_pecor.incidentController).to.equal(a.EmergencyPauseController);
    const incidentTargets = ['PECOR', 'PECOROrders', 'MetaAGRouter', 'OracleHub', 'PriceOracle'];
    for (const name of incidentTargets) {
      const target = await ethers.getContractAt(name, a[`${name}_proxy`]);
      expect(await controller.allowedTargets(a[`${name}_proxy`]), name).to.equal(true);
      expect(await target.hasRole(ethers.id('PAUSER_ROLE'), a.EmergencyPauseController), name).to.equal(true);
      expect(await target.hasRole(ethers.id('PAUSER_ROLE'), deployer.address), name).to.equal(false);
    }
    const beforeHealth = await ethers.provider.getTransactionCount(deployer.address);
    const health = await require('../../scripts/lib/production-health').inspectHealth({
      provider: ethers.provider, manifest: a,
      policy: {
        requiredContracts: [...incidentTargets.map(name => `${name}_proxy`), 'PECORVault_proxy', 'EmergencyPauseController'],
        requiredAssets: [a._meta.usdl, a.WPAX],
        expectedRoles: [
          ...incidentTargets.map(name => ({ contract: `${name}_proxy`, role: ethers.id('PAUSER_ROLE'), account: a.EmergencyPauseController, present: true })),
          { contract: 'EmergencyPauseController', role: ethers.ZeroHash, account: a.Timelock, present: true },
          { contract: 'EmergencyPauseController', role: ethers.ZeroHash, account: deployer.address, present: false },
          { contract: 'EmergencyPauseController', role: ethers.id('INCIDENT_GUARDIAN_ROLE'), account: outsider.address, present: true },
        ],
      },
    });
    expect(health.checks.filter(check => check.status === 'critical'), JSON.stringify(health.checks.filter(check => check.status === 'critical'))).to.deep.equal([]);
    expect(health.status).to.equal('ok');
    expect(health.blockHash).to.equal((await ethers.provider.getBlock(health.blockNumber)).hash);
    expect(await ethers.provider.getTransactionCount(deployer.address)).to.equal(beforeHealth);
    const incidentSnapshot = await network.provider.send('evm_snapshot');
    try {
      await expect(controller.pauseTargets([a.PECOR_proxy], ethers.id('unauthorized'))).to.be.reverted;
      await controller.connect(outsider).pauseTargets(incidentTargets.map(name => a[`${name}_proxy`]), ethers.id('deployment-incident'), { gasLimit: 1500000 });
      for (const name of incidentTargets) {
        const target = await ethers.getContractAt(name, a[`${name}_proxy`]);
        expect(await target.emergencyPaused(), name).to.equal(true);
        await expect(target.connect(outsider).unpause()).to.be.reverted;
      }
      await expect(controller.connect(outsider).setTarget(a.PECOR_proxy, false)).to.be.reverted;
    } finally { await network.provider.send('evm_revert', [incidentSnapshot]); }
    const beforeRepair = await ethers.provider.getTransactionCount(deployer.address);
    await require('../../scripts/meta-ag/resume-pecor-deploy').main();
    expect(await ethers.provider.getTransactionCount(deployer.address)).to.equal(beforeRepair);
  });
  for (const faultStage of ['implementation:EventEmitter', 'proxy:EventEmitter', 'wiring:registry-factory', 'handoff:EventEmitter']) {
    it(`resumes a durable ${faultStage} interruption without duplicate contract creations`, async function () {
      const snapshot = await network.provider.send('evm_snapshot');
      const previousFiles = files.map(file => fs.existsSync(file) ? fs.readFileSync(file) : null);
      const previousResume = process.env.RESUME_DEPLOYMENT;
      try {
        for (const file of files) if (fs.existsSync(file)) fs.unlinkSync(file);
        delete process.env.RESUME_DEPLOYMENT;
        const firstBlock = await ethers.provider.getBlockNumber();
        let observedFault;
        try {
          await core.main({ afterCheckpoint(stage) {
            if (stage === faultStage) throw new Error(`Injected durable interruption: ${stage}`);
          } });
        } catch (error) { observedFault = error; }
        expect(observedFault?.message).to.equal(`Injected durable interruption: ${faultStage}`);
        const partial = JSON.parse(fs.readFileSync(files[0]));
        const preserved = Object.entries(partial).filter(([key, value]) => !key.startsWith('_') && typeof value === 'string' && ethers.isAddress(value));
        expect(preserved.length).to.be.greaterThan(0);
        process.env.RESUME_DEPLOYMENT = 'true';
        if (faultStage === 'implementation:EventEmitter') {
          const originalSid = process.env.SID_ADDRESS;
          const beforeMismatch = await ethers.provider.getTransactionCount(deployer.address);
          let mismatch;
          try {
            process.env.SID_ADDRESS = process.env.USDL_ADDRESS;
            await core.main();
          } catch (error) { mismatch = error; }
          finally { process.env.SID_ADDRESS = originalSid; }
          expect(mismatch?.message).to.match(/identity|config|mismatch/i);
          expect(await ethers.provider.getTransactionCount(deployer.address)).to.equal(beforeMismatch);
        }
        if (faultStage === 'proxy:EventEmitter') {
          const implementationSlot = '0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc';
          const originalSlot = await ethers.provider.getStorage(partial.EventEmitter_proxy, implementationSlot);
          const beforeDrift = await ethers.provider.getTransactionCount(deployer.address);
          let drift;
          try {
            await network.provider.send('hardhat_setStorageAt', [partial.EventEmitter_proxy, implementationSlot, ethers.zeroPadValue(process.env.USDL_ADDRESS, 32)]);
            await core.main();
          } catch (error) { drift = error; }
          finally { await network.provider.send('hardhat_setStorageAt', [partial.EventEmitter_proxy, implementationSlot, originalSlot]); }
          expect(drift?.message).to.match(/implementation|identity|mismatch/i);
          expect(await ethers.provider.getTransactionCount(deployer.address)).to.equal(beforeDrift);
        }
        await core.main();
        const complete = JSON.parse(fs.readFileSync(files[0]));
        for (const [key, value] of preserved) expect(complete[key], `reused ${key}`).to.equal(value);
        const finalAddresses = Object.entries(complete).filter(([key, value]) => !key.startsWith('_') && typeof value === 'string' && ethers.isAddress(value));
        const creations = [];
        for (let number = firstBlock + 1; number <= await ethers.provider.getBlockNumber(); number++) {
          const block = await ethers.provider.getBlock(number);
          for (const hash of block.transactions) {
            const transaction = await ethers.provider.getTransaction(hash);
            if (transaction.to === null) creations.push((await ethers.provider.getTransactionReceipt(hash)).contractAddress);
          }
        }
        expect(creations.length).to.equal(finalAddresses.length);
        expect(new Set(creations.map(address => address.toLowerCase())).size).to.equal(creations.length);
        for (const [, address] of finalAddresses) expect(creations.map(value => value.toLowerCase())).to.include(address.toLowerCase());
        for (const [key, address] of finalAddresses.filter(([key]) => key.endsWith('_proxy'))) {
          const contract = await ethers.getContractAt(key.replace('_proxy', ''), address);
          expect(await contract.hasRole(ethers.ZeroHash, complete.Timelock), key).to.equal(true);
          expect(await contract.hasRole(ethers.ZeroHash, deployer.address), key).to.equal(false);
        }
        expect(complete._meta.status).to.equal('pending-beacon-acceptance');
        const nonce = await ethers.provider.getTransactionCount(deployer.address);
        await core.main();
        expect(await ethers.provider.getTransactionCount(deployer.address)).to.equal(nonce);
      } finally {
        if (previousResume === undefined) delete process.env.RESUME_DEPLOYMENT;
        else process.env.RESUME_DEPLOYMENT = previousResume;
        for (let index = 0; index < files.length; index++) {
          if (previousFiles[index] === null) { if (fs.existsSync(files[index])) fs.unlinkSync(files[index]); }
          else fs.writeFileSync(files[index], previousFiles[index]);
        }
        await network.provider.send('evm_revert', [snapshot]);
      }
    });
  }

  it('resumes after every individually confirmed deployment transaction without duplicating creations', async function () {
    const snapshot = await network.provider.send('evm_snapshot');
    const previousFiles = files.map(file => fs.existsSync(file) ? fs.readFileSync(file) : null);
    const previousResume = process.env.RESUME_DEPLOYMENT;
    const confirmed = new Set();
    const interrupted = new Set();
    try {
      for (const file of files) if (fs.existsSync(file)) fs.unlinkSync(file);
      delete process.env.RESUME_DEPLOYMENT;
      const firstBlock = await ethers.provider.getBlockNumber();
      let complete = false;
      for (let attempt = 0; attempt < 250 && !complete; attempt++) {
        try {
          await core.main({ afterCheckpoint(stage, addresses) {
            const plan = addresses._deployment;
            const records = [...Object.values(plan?.records || {}), ...Object.values(plan?.steps || {})];
            const unseen = records.filter(record => record.status === 'confirmed' && record.txHash && !confirmed.has(record.txHash));
            if (unseen.length) {
              // A single transaction must always be durable before the next one is sent.
              expect(unseen.length, `checkpoint ${stage}`).to.equal(1);
              confirmed.add(unseen[0].txHash);
              interrupted.add(unseen[0].txHash);
              throw new Error('Injected interruption after confirmed transaction');
            }
          } });
          complete = true;
        } catch (error) {
          if (error.message !== 'Injected interruption after confirmed transaction') throw error;
        }
        process.env.RESUME_DEPLOYMENT = 'true';
      }
      expect(complete, 'exhaustive restart loop completed').to.equal(true);
      const final = JSON.parse(fs.readFileSync(files[0]));
      const created = [];
      const allTransactions = [];
      for (let number = firstBlock + 1; number <= await ethers.provider.getBlockNumber(); number++) {
        const block = await ethers.provider.getBlock(number);
        for (const hash of block.transactions) {
          allTransactions.push(hash);
          const transaction = await ethers.provider.getTransaction(hash);
          if (transaction.to === null) created.push((await ethers.provider.getTransactionReceipt(hash)).contractAddress);
        }
      }
      expect(interrupted.size).to.be.greaterThan(50);
      expect(allTransactions.length).to.equal(interrupted.size);
      console.log(`Exhaustive resume confirmed: ${interrupted.size} transaction interruptions, ${created.length} contract creations`);
      for (const hash of allTransactions) expect(interrupted.has(hash), `interrupted transaction ${hash}`).to.equal(true);
      const finalAddresses = Object.entries(final).filter(([key, value]) => !key.startsWith('_') && typeof value === 'string' && ethers.isAddress(value));
      expect(created.length).to.equal(finalAddresses.length);
      for (const [key, address] of finalAddresses.filter(([key]) => key.endsWith('_proxy'))) {
        const contract = await ethers.getContractAt(key.replace('_proxy', ''), address);
        expect(await contract.hasRole(ethers.ZeroHash, final.Timelock), key).to.equal(true);
        expect(await contract.hasRole(ethers.ZeroHash, deployer.address), key).to.equal(false);
      }
      const timelock = await ethers.getContractAt('Timelock', final.Timelock);
      expect(await timelock.proposer()).to.equal(final.GovernanceModule_proxy);
      const before = await ethers.provider.getTransactionCount(deployer.address);
      await core.main();
      expect(await ethers.provider.getTransactionCount(deployer.address)).to.equal(before);
    } finally {
      if (previousResume === undefined) delete process.env.RESUME_DEPLOYMENT;
      else process.env.RESUME_DEPLOYMENT = previousResume;
      for (let index = 0; index < files.length; index++) {
        if (previousFiles[index] === null) { if (fs.existsSync(files[index])) fs.unlinkSync(files[index]); }
        else fs.writeFileSync(files[index], previousFiles[index]);
      }
      await network.provider.send('evm_revert', [snapshot]);
    }
  });

  for (const faultStage of ['implementation:PriceOracle', 'proxy:PriceOracle', 'immutable:EmergencyPauseController', 'wiring:incident-pauser:PECOR', 'wiring:relayer', 'handoff:PriceOracle']) {
    it(`resumes layered ${faultStage} interruption and preserves inherited core identity`, async function () {
      const snapshot = await network.provider.send('evm_snapshot');
      const previousFiles = files.map(file => fs.existsSync(file) ? fs.readFileSync(file) : null);
      const previousResume = process.env.RESUME_DEPLOYMENT;
      try {
        expect(coreOnlyFiles, 'core fixture completed').to.exist;
        for (let index = 0; index < files.length; index++) fs.writeFileSync(files[index], coreOnlyFiles[index]);
        delete process.env.RESUME_DEPLOYMENT;
        const inherited = JSON.parse(coreOnlyFiles[0]);
        const firstBlock = await ethers.provider.getBlockNumber();
        let observedFault;
        try {
          await meta.main({ afterCheckpoint(stage) {
            if (stage === faultStage) throw new Error(`Injected layered interruption: ${stage}`);
          } });
        } catch (error) { observedFault = error; }
        expect(observedFault?.message).to.equal(`Injected layered interruption: ${faultStage}`);
        const partial = JSON.parse(fs.readFileSync(files[0]));
        const preserved = Object.entries(partial).filter(([key, value]) => !key.startsWith('_') && typeof value === 'string' && ethers.isAddress(value));
        process.env.RESUME_DEPLOYMENT = 'true';
        const beforeMismatch = await ethers.provider.getTransactionCount(deployer.address);
        process.env.INCIDENT_GUARDIAN_ADDRESS = deployer.address;
        let identityError;
        try { await meta.main(); } catch (error) { identityError = error; }
        finally { delete process.env.INCIDENT_GUARDIAN_ADDRESS; }
        expect(identityError?.message).to.match(/identity|configuration/i);
        expect(await ethers.provider.getTransactionCount(deployer.address)).to.equal(beforeMismatch);
        await meta.main();
        const complete = JSON.parse(fs.readFileSync(files[0]));
        expect(complete._deployment).to.deep.equal(inherited._deployment);
        for (const [key, value] of preserved) expect(complete[key], `reused ${key}`).to.equal(value);
        const finalAddresses = Object.entries(complete).filter(([key, value]) => !key.startsWith('_') && typeof value === 'string' && ethers.isAddress(value) && !(key in inherited));
        const creations = [];
        for (let number = firstBlock + 1; number <= await ethers.provider.getBlockNumber(); number++) {
          const block = await ethers.provider.getBlock(number);
          for (const hash of block.transactions) {
            if ((await ethers.provider.getTransaction(hash)).to === null) creations.push((await ethers.provider.getTransactionReceipt(hash)).contractAddress);
          }
        }
        expect(creations.length).to.equal(finalAddresses.length);
        expect(new Set(creations.map(address => address.toLowerCase())).size).to.equal(creations.length);
        for (const [, address] of finalAddresses) expect(creations.map(value => value.toLowerCase())).to.include(address.toLowerCase());
        for (const name of ['PriceOracle', 'OracleHub', 'TransactionTracker', 'PECORVault', 'PECOR', 'PECOROrders', 'MetaAGRouter', 'MetaAGQuoter', 'VaultAdapter', 'SidioraAdapter', 'SidioraFeedAdapter']) {
          const contract = await ethers.getContractAt(name, complete[`${name}_proxy`] || complete[name]);
          expect(await contract.hasRole(ethers.ZeroHash, complete.Timelock), name).to.equal(true);
          expect(await contract.hasRole(ethers.ZeroHash, deployer.address), name).to.equal(false);
        }
        expect(complete._meta_pecor.status).to.equal('finalized');
        const nonce = await ethers.provider.getTransactionCount(deployer.address);
        await meta.main();
        expect(await ethers.provider.getTransactionCount(deployer.address)).to.equal(nonce);
      } finally {
        if (previousResume === undefined) delete process.env.RESUME_DEPLOYMENT;
        else process.env.RESUME_DEPLOYMENT = previousResume;
        for (let index = 0; index < files.length; index++) {
          if (previousFiles[index] === null) { if (fs.existsSync(files[index])) fs.unlinkSync(files[index]); }
          else fs.writeFileSync(files[index], previousFiles[index]);
        }
        await network.provider.send('evm_revert', [snapshot]);
      }
    });
  }

});
