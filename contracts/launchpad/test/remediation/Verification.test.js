const { expect } = require('chai');
const { artifacts, ethers } = require('hardhat');
const { CONTRACT_SOURCES, buildQueue, prepareVerification, verifyContract, verificationExitCode } = require('../../scripts/verify');

describe('Verification: recorded deployment identity and confirmed explorer status', function () {
  const address = '0x0000000000000000000000000000000000000125';
  const secondAddress = '0x0000000000000000000000000000000000000126';

  async function fixture(name = 'ProtocolConfig', args = []) {
    const artifact = await artifacts.readArtifact(`${CONTRACT_SOURCES[name]}:${name}`);
    const encoded = new ethers.Interface(artifact.abi).encodeDeploy(args);
    const record = { name, args, address, status: 'confirmed', creationHash: ethers.keccak256(artifact.bytecode + encoded.slice(2)), runtimeHash: ethers.keccak256(artifact.deployedBytecode) };
    const item = { label: name, address, contractName: name, record };
    const provider = { getCode: async () => artifact.deployedBytecode };
    return { artifact, item, provider };
  }

  async function rejection(promise, message) {
    let caught;
    try { await promise; } catch (error) { caught = error; }
    expect(caught, 'expected preflight rejection').to.be.instanceOf(Error);
    expect(caught.message).to.include(message);
  }

  function responses(entries, inspect) {
    let calls = 0;
    const fetchImpl = async (url, options) => {
      const entry = entries[calls++];
      if (!entry) throw new Error('Unexpected explorer request');
      if (inspect) await inspect(url, options, calls);
      if (entry instanceof Error) throw entry;
      return { ok: entry.status >= 200 && entry.status < 300, status: entry.status, text: async () => entry.raw ?? JSON.stringify(entry.body) };
    };
    return { fetchImpl, calls: () => calls };
  }
  const unverified = { status: 200, body: { is_verified: false } };
  const verified = { status: 200, body: { is_verified: true } };
  const accepted = { status: 200, body: { message: 'Verification started' } };
  const prepared = { address, contractPath: 'contracts/test/MockUUPS.sol:UUPSProxy', compilerVersion: 'v0.8.27+commit.40a35a09', constructorArgs: '0x1234', input: { language: 'Solidity', sources: { 'sample.sol': { content: 'contract Sample {}' } } } };

  it('uses artifact-linked compiler input for the updated governance and protocol artifacts', async function () {
    for (const name of ['GovernanceModule', 'ProtocolConfig']) {
      const { item, provider } = await fixture(name);
      const result = await prepareVerification(item, { artifacts, provider });
      const info = await artifacts.getBuildInfo(`${CONTRACT_SOURCES[name]}:${name}`);
      expect(result.input).to.deep.equal(info.input);
      expect(result.compilerVersion).to.equal(`v${info.solcLongVersion}`);
      expect(result.input.sources[CONTRACT_SOURCES[name]].content).to.include(name === 'GovernanceModule' ? 'initializeAdmin' : '1e6');
    }
  });

  it('encodes and matches proxy constructor arguments against the recorded creation hash', async function () {
    const { item, provider, artifact } = await fixture('UUPSProxy', [secondAddress, '0x1234']);
    const result = await prepareVerification(item, { artifacts, provider });
    expect(result.constructorArgs).to.equal(new ethers.Interface(artifact.abi).encodeDeploy(item.record.args));
    item.record.args = [secondAddress, '0x5678'];
    await rejection(prepareVerification(item, { artifacts, provider }), 'constructor arguments differ');
  });

  it('rejects a changed creation hash before consulting the network', async function () {
    const { item } = await fixture();
    item.record.creationHash = ethers.ZeroHash;
    await rejection(prepareVerification(item, { artifacts, provider: { getCode: async () => { throw new Error('must not read code'); } } }), 'Creation artifact');
  });

  it('rejects changed and missing onchain code', async function () {
    const { item } = await fixture();
    for (const code of ['0x', '0x6000']) await rejection(prepareVerification(item, { artifacts, provider: { getCode: async () => code } }), 'Onchain runtime differs');
  });

  it('rejects mismatched compiler output and missing artifact-linked builds', async function () {
    const { item, provider, artifact } = await fixture();
    const info = await artifacts.getBuildInfo(`${CONTRACT_SOURCES.ProtocolConfig}:ProtocolConfig`);
    const changed = JSON.parse(JSON.stringify(info));
    changed.output.contracts[CONTRACT_SOURCES.ProtocolConfig].ProtocolConfig.evm.bytecode.object = '6000';
    const fakeArtifacts = { readArtifact: async () => artifact, getBuildInfo: async () => changed };
    await rejection(prepareVerification(item, { artifacts: fakeArtifacts, provider }), 'Compiler output differs');
    fakeArtifacts.getBuildInfo = async () => undefined;
    await rejection(prepareVerification(item, { artifacts: fakeArtifacts, provider }), 'build info not found');
  });

  it('maps confirmed implementations and proxies from deployment records', async function () {
    const first = (await fixture()).item.record;
    const second = { ...(await fixture('UUPSProxy', [secondAddress, '0x'])).item.record, address: secondAddress };
    const manifest = { ProtocolConfigImpl: address, ProtocolConfig: secondAddress, _deployment: { records: { ProtocolConfigImpl: first, ProtocolConfig: second } } };
    expect(buildQueue(manifest).map(item => [item.label, item.contractName])).to.deep.equal([['ProtocolConfigImpl', 'ProtocolConfig'], ['ProtocolConfig', 'UUPSProxy']]);
    second.status = 'prepared';
    expect(() => buildQueue(manifest)).to.throw('Unconfirmed');
    second.status = 'confirmed';
    second.address = address;
    expect(() => buildQueue(manifest)).to.throw('inconsistent');
    expect(() => buildQueue({})).to.throw('records not found');
    first.name = 'UnknownContract';
    expect(() => buildQueue(manifest)).to.throw('No source mapped');
  });

  it('submits the exact compiler, constructor arguments and standard input then confirms verification', async function () {
    const mock = responses([unverified, accepted, verified], async (url, options, call) => {
      if (call !== 2) return;
      expect(url).to.include('/verification/via/standard-input');
      expect(options.method).to.equal('POST');
      expect(options.body.get('compiler_version')).to.equal(prepared.compilerVersion);
      expect(options.body.get('contract_name')).to.equal(prepared.contractPath);
      expect(options.body.get('autodetect_constructor_args')).to.equal('false');
      expect(options.body.get('constructor_args')).to.equal('1234');
      expect(JSON.parse(await options.body.get('files[0]').text())).to.deep.equal(prepared.input);
    });
    expect(await verifyContract(prepared, { fetchImpl: mock.fetchImpl, pollAttempts: 1 })).to.deep.equal({ status: 'verified' });
    expect(mock.calls()).to.equal(3);
  });

  it('keeps accepted jobs pending until an explicit verified flag appears', async function () {
    const mock = responses([unverified, accepted, unverified, unverified]);
    const result = await verifyContract(prepared, { fetchImpl: mock.fetchImpl, pollAttempts: 2, sleepImpl: async () => {} });
    expect(result.status).to.equal('pending');
    expect(mock.calls()).to.equal(4);
    expect(verificationExitCode([result])).to.equal(1);
  });

  it('skips publication for an already verified address', async function () {
    const mock = responses([verified]);
    expect(await verifyContract(prepared, { fetchImpl: mock.fetchImpl })).to.deep.equal({ status: 'verified', alreadyVerified: true });
    expect(mock.calls()).to.equal(1);
  });

  it('does not trust an already-verified submission message without checking the address flag', async function () {
    const mock = responses([unverified, { status: 400, body: { message: 'Already verified' } }, verified]);
    expect((await verifyContract(prepared, { fetchImpl: mock.fetchImpl, pollAttempts: 1 })).status).to.equal('verified');
    const pending = responses([unverified, { status: 400, body: { message: 'Already verified' } }, unverified]);
    expect((await verifyContract(prepared, { fetchImpl: pending.fetchImpl, pollAttempts: 1 })).status).to.equal('pending');
  });

  it('reports HTTP errors, JSON errors, non-JSON replies and transport failures as failed', async function () {
    for (const entry of [
      { status: 500, body: { message: 'Unavailable' } },
      { status: 200, body: { error: 'Compiler error' } },
      { status: 200, body: { status: '0', message: 'Rejected' } },
      { status: 200, raw: '<html>gateway</html>' },
      new Error('Connection refused'),
    ]) {
      const mock = responses([unverified, entry]);
      const result = await verifyContract(prepared, { fetchImpl: mock.fetchImpl, pollAttempts: 1 });
      expect(result.status).to.equal('failed');
      expect(result.error).to.be.a('string').and.not.equal('');
      expect(verificationExitCode([result])).to.equal(1);
    }
  });

  it('requires every nonempty result to be verified for a successful exit', function () {
    expect(verificationExitCode([])).to.equal(1);
    expect(verificationExitCode([{ status: 'verified' }])).to.equal(0);
    expect(verificationExitCode([{ status: 'verified' }, { status: 'pending' }])).to.equal(1);
    expect(verificationExitCode([{ status: 'verified' }, { status: 'failed' }])).to.equal(1);
  });
});
