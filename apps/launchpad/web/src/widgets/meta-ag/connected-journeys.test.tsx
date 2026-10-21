import { test, expect } from 'vitest';
import { readFileSync, writeFileSync, mkdirSync, appendFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { createHash } from 'node:crypto';
import { createPublicClient, createWalletClient, defineChain, parseAbi, erc20Abi, keccak256, stringToHex, type Hex } from 'viem';
import { privateKeyToAccount } from 'viem/accounts';
import { createAdvancedProvider, type AdvancedManifest } from './advanced-provider';
import { createCapturedRpcTransport, runActualWorkload, assertActualPage, type MeasurementIdentity, type Observation, type OperationObservation, type WorkloadParameters, type MeasurementKind, type MeasuredOutcome } from './measurement-harness';
import { exactUnits, reconcileOrder, validateCustomRoute, type Hop, type Address } from './domain';

test('advanced deployment and funded lifecycle use real chain125 contracts', async () => {
  const fixturePath = process.env.L28_CHAIN_FIXTURE_JSON;
  if (!fixturePath) throw new Error('L28_CHAIN_FIXTURE_JSON must select a funded, serial-admitted private advanced deployment; missing contract authority is a qualification blocker');
  const fixture = JSON.parse(readFileSync(fixturePath, 'utf8')) as {
    realm: 'private_fixture'; rpcUrl: string; manifestFile: string; manifestSha256: string;
    userPrivateKeyFile: string; keeperPrivateKeyFile: string; storageFile: string;
    token: Address; buyAmount: string; limitPrice: string; minimumOutput: string;
    measurement?: WorkloadParameters & { accountPrivateKeyFiles: string[] };
    adminPrivateKeyFile: string;
    core: { router: Address; pool: Address; routerCodeHash: Hex; poolCodeHash: Hex; amountInRaw: string; minimumOutputRaw: string };
    multihop: { hops: Array<Omit<Hop,'minAmountOut'> & { minAmountOut: string }>; inputRaw: string; finalMinimumRaw: string; gasLimit: string };
  };
  const rpc = new URL(fixture.rpcUrl);
  if (fixture.realm !== 'private_fixture' || !['127.0.0.1','localhost','[::1]'].includes(rpc.hostname)) throw new Error('Advanced lifecycle writes require an isolated loopback fixture');
  const manifestJson = readFileSync(fixture.manifestFile, 'utf8');
  expect(createHash('sha256').update(manifestJson).digest('hex')).toBe(fixture.manifestSha256);
  const manifest = JSON.parse(manifestJson) as AdvancedManifest;
  const chain = defineChain({ id: 125, name: 'Admitted private advanced fixture', nativeCurrency: { name: 'PAX', symbol: 'PAX', decimals: 18 }, rpcUrls: { default: { http: [fixture.rpcUrl] } } });
  const outputDirectory = process.env.L28_OBSERVATION_DIRECTORY, sourceManifestPath = process.env.L28_SOURCE_MANIFEST_JSON;
  if (!outputDirectory || !sourceManifestPath) throw new Error('Actual raw capture/source binding is mandatory');
  const sourceManifestRaw = readFileSync(sourceManifestPath,'utf8'), sourceManifest = JSON.parse(sourceManifestRaw);
  const identity: MeasurementIdentity = { runId: sourceManifest.runId, manifestSha256: fixture.manifestSha256, sourceManifestSha256: createHash('sha256').update(sourceManifestRaw).digest('hex'), deploymentRevision: manifest.revision, chainId: 125 };
  const stringify = (value: unknown) => JSON.stringify(value, (_,item) => typeof item === 'bigint' ? item.toString() : item);
  const sink = (event: Observation | OperationObservation) => appendFileSync(resolve(outputDirectory,event.type === 'rpc' ? 'rpc.jsonl' : event.type === 'operation' || event.type === 'operation_deadline' ? 'operations.jsonl' : 'scenarios.jsonl'), stringify(event)+'\n', { mode: 0o600 });
  const record = (scenario: string, raw: unknown) => sink({ ...identity, type: 'scenario', scenario, observedAtUnixMs: Date.now(), raw });
  const captured = createCapturedRpcTransport(fixture.rpcUrl,identity,sink);
  const client = createPublicClient({ chain, transport: captured.transport });
  const account = privateKeyToAccount(readFileSync(fixture.userPrivateKeyFile, 'utf8').trim() as Hex);
  const keeperAccount = privateKeyToAccount(readFileSync(fixture.keeperPrivateKeyFile, 'utf8').trim() as Hex);
  const wallet = createWalletClient({ account, chain, transport: captured.transport });
  const keeper = createWalletClient({ account: keeperAccount, chain, transport: captured.transport });
  const storagePath = resolve(fixture.storageFile);
  mkdirSync(dirname(storagePath), { recursive: true, mode: 0o700 });
  const read = (): Record<string,string> => { try { return JSON.parse(readFileSync(storagePath, 'utf8')); } catch (error) { if ((error as NodeJS.ErrnoException).code === 'ENOENT') return {}; throw error; } };
  const persist = (value: Record<string,string>) => writeFileSync(storagePath, JSON.stringify(value), { mode: 0o600 });
  const storage = { getItem: (key: string) => read()[key] ?? null, setItem: (key: string, value: string) => persist({ ...read(), [key]: value }), removeItem: (key: string) => { const value = read(); delete value[key]; persist(value); } };
  expect(manifest.confirmations).toBe(1);
  const keeperRole = await client.readContract({ address: manifest.orders!.address, abi: parseAbi(['function hasRole(bytes32 role,address account) view returns (bool)']), functionName: 'hasRole', args: [keccak256(stringToHex('KEEPER_ROLE')), keeperAccount.address] });
  expect(keeperRole).toBe(true);
  const provider = await createAdvancedProvider({ publicClient: client, walletClient: wallet, manifestJson, manifestSha256: fixture.manifestSha256, attempts: storage });
  expect([...provider.blockers]).toEqual([]);
  record('deployment_actual',{ chainId: await client.getChainId(), manifest, block: await client.getBlock(), blockers: [...provider.blockers] });
  expect(await provider.decimals(manifest.usdl)).toBe(6);
  expect(exactUnits('1.000001', await provider.decimals(manifest.usdl))).toBe(1000001n);
  expect(exactUnits('0.25', manifest.priceScale)).toBe(250000000000000000n);
  record('scenario_2',{ decimals: await provider.decimals(manifest.usdl), amountRaw: exactUnits('1.000001',await provider.decimals(manifest.usdl)), priceRaw: exactUnits('0.25',manifest.priceScale) });
  expect(() => exactUnits('1', undefined)).toThrow();
  const owner = await provider.owner();
  const outputDecimals = await provider.decimals(fixture.token);
  const prepared = await provider.prepareOrder({ kind: 'limit-buy', owner, token: fixture.token, settlementAsset: manifest.usdl, amount: fixture.buyAmount, limitPrice: fixture.limitPrice, expiresAt: BigInt(Math.floor(Date.now() / 1000)) + 1800n }, exactUnits(fixture.minimumOutput, outputDecimals));
  record('order_review_before_signature',prepared);
  const created = await provider.placeOrder(prepared);
  record('actual_order_placement',{ created, receipt: await client.getTransactionReceipt({ hash: created.hash }) });
  const active = await provider.order(created.orderId, created.family, created.creationBlock);
  expect(active.state).toBe('active'); expect(active.filledRaw).toBe(0n);
  const snapshot = async () => ({ balance: await client.readContract({ address: manifest.usdl, abi: erc20Abi, functionName: 'balanceOf', args: [owner] }), nonce: await client.getTransactionCount({ address: owner }) });
  const beforeRejected = await snapshot();
  const variant = async (changes: Partial<AdvancedManifest>) => {
    const json = JSON.stringify({ ...manifest, ...changes });
    return createAdvancedProvider({ publicClient: client, walletClient: wallet, manifestJson: json, manifestSha256: createHash('sha256').update(json).digest('hex'), attempts: storage });
  };
  const unavailablePrice = await variant({ quoter: null, keeperHealthUrl: null });
  const retained = await unavailablePrice.order(created.orderId, created.family, created.creationBlock);
  expect(retained).toMatchObject({ state: 'active', filledRaw: 0n, refundRaw: 0n, eligible: null, keeperHealthy: false });
  await expect(unavailablePrice.prepareOrder({ kind: 'limit-buy', owner, token: fixture.token, settlementAsset: manifest.usdl, amount: fixture.buyAmount, limitPrice: fixture.limitPrice, expiresAt: prepared.intent.expiresAt }, prepared.minimumOutput)).rejects.toThrow('oracle');
  const keeperDown = await variant({ keeperHealthUrl: null });
  await expect(keeperDown.prepareOrder({ kind: 'limit-buy', owner, token: fixture.token, settlementAsset: manifest.usdl, amount: fixture.buyAmount, limitPrice: fixture.limitPrice, expiresAt: prepared.intent.expiresAt }, prepared.minimumOutput)).rejects.toThrow('Execution service');
  const degradedEligible = await keeperDown.order(created.orderId,created.family,created.creationBlock);
  expect(degradedEligible.eligible).toBe(true); expect(degradedEligible.keeperHealthy).toBe(false); expect(degradedEligible.filledRaw).toBe(0n);
  record('scenario_3_eligible_degraded',degradedEligible);
  const changedAbi = await variant({ orders: { ...manifest.orders!, runtimeCodeHash: `0x${'00'.repeat(32)}` } });
  expect(changedAbi.blockers.get('orders')).toContain('ABI/code identity changed');
  await expect(changedAbi.cancel(active)).rejects.toThrow('ABI/code identity changed');
  await expect(provider.cancel({ ...active, owner: keeperAccount.address.toLowerCase() as Address })).rejects.toThrow('account mismatch');
  expect(() => reconcileOrder(active, { ...active, state: 'filled', filledRaw: 1n, eventRevision: active.eventRevision + 1n, executionTxHash: `0x${'ab'.repeat(32)}`, emitter: keeperAccount.address, committed: true, transferVerified: true }, manifest.orders!.address)).toThrow('Untrusted');
  expect(await snapshot()).toEqual(beforeRejected);
  expect(await provider.order(created.orderId, created.family, created.creationBlock)).toMatchObject({ state: 'active', filledRaw: 0n, refundRaw: 0n });
  const cancelResults = await Promise.allSettled([provider.cancel(active), provider.cancel(active)]);
  expect(cancelResults.filter(result => result.status === 'fulfilled')).toHaveLength(1);
  expect(cancelResults.filter(result => result.status === 'rejected')).toHaveLength(1);
  const completedCancel = cancelResults.find(result => result.status === 'fulfilled');
  if (!completedCancel || completedCancel.status !== 'fulfilled') throw new Error('One real cancellation must be submitted');
  const cancellation = completedCancel.value;
  record('scenario_8_cancel_concurrency',{ fulfilled: cancelResults.filter(result => result.status === 'fulfilled').length, rejected: cancelResults.filter(result => result.status === 'rejected').length, cancellation });
  const restoredProvider = await createAdvancedProvider({ publicClient: client, walletClient: wallet, manifestJson, manifestSha256: fixture.manifestSha256, attempts: storage });
  expect((await restoredProvider.pendingCancel(active))?.txHash).toBe(cancellation.txHash);
  await expect(restoredProvider.cancel(active)).rejects.toThrow('resume existing hash');
  const recovered = await restoredProvider.recoverCancel(active);
  expect(recovered.outcome.state).toBe('cancelled'); expect(recovered.outcome.refundRaw).toBe(prepared.intent.amountRaw);
  expect(recovered.order.filledRaw).toBe(0n);
  record('scenario_4_cancel_wins',{ recovered, receipt: await client.getTransactionReceipt({ hash: cancellation.txHash as Hex }), balance: await snapshot() });
  const deterministicFill = await provider.placeOrder(await provider.prepareOrder({ kind: 'limit-buy',owner,token: fixture.token,settlementAsset: manifest.usdl,amount: fixture.buyAmount,limitPrice: fixture.limitPrice,expiresAt: (await client.getBlock()).timestamp + 1800n },exactUnits(fixture.minimumOutput,outputDecimals)));
  const filledBeforeCancelHash = await keeper.writeContract({ address: manifest.orders!.address,abi: parseAbi(['function executeLimitOrder(uint256 id)']),functionName: 'executeLimitOrder',args: [deterministicFill.orderId],gas: 2000000n });
  expect((await provider.receipt(filledBeforeCancelHash)).status).toBe('success');
  const losingCancelHash = await wallet.writeContract({ address: manifest.orders!.address,abi: parseAbi(['function cancelLimitOrder(uint256 id)']),functionName: 'cancelLimitOrder',args: [deterministicFill.orderId],gas: 2000000n });
  const losingReceipt = await provider.receipt(losingCancelHash); expect(losingReceipt.status).toBe('reverted');
  const verifiedFilled = await provider.order(deterministicFill.orderId,deterministicFill.family,deterministicFill.creationBlock);
  expect(verifiedFilled.state).toBe('filled'); expect(verifiedFilled.filledRaw).toBeGreaterThan(0n); expect(verifiedFilled.refundRaw).toBe(0n);
  record('scenario_3_later_canonical_fill',verifiedFilled);
  record('scenario_4_fill_wins',{ filled: verifiedFilled,fillReceipt: await client.getTransactionReceipt({ hash: filledBeforeCancelHash }),losingReceipt });
  const fillPrepared = await provider.prepareOrder({ kind: 'limit-buy', owner, token: fixture.token, settlementAsset: manifest.usdl, amount: fixture.buyAmount, limitPrice: fixture.limitPrice, expiresAt: BigInt(Math.floor(Date.now() / 1000)) + 1800n }, exactUnits(fixture.minimumOutput, outputDecimals));
  const fillCreated = await provider.placeOrder(fillPrepared);
  const fillActive = await provider.order(fillCreated.orderId, fillCreated.family, fillCreated.creationBlock);
  const keeperAbi = parseAbi(['function executeLimitOrder(uint256 id)']);
  const results = await Promise.allSettled([
    keeper.writeContract({ address: manifest.orders!.address, abi: keeperAbi, functionName: 'executeLimitOrder', args: [fillCreated.orderId], gas: 2000000n }).then(hash => provider.receipt(hash)),
    provider.cancel(fillActive),
  ]);
  const final = await provider.order(fillCreated.orderId, fillCreated.family, fillCreated.creationBlock);
  expect(['filled','cancelled']).toContain(final.state);
  if (final.state === 'filled') { expect(final.filledRaw).toBeGreaterThan(0n); expect(final.refundRaw).toBe(0n); }
  else { expect(final.filledRaw).toBe(0n); expect(final.refundRaw).toBe(fillActive.amountRaw); }
  if (results[1].status === 'fulfilled') {
    const settled = await provider.recoverCancel(fillActive);
    expect(settled.outcome.state).toBe(final.state === 'filled' ? 'filled_race' : 'cancelled');
    expect(settled.outcome.refundRaw).toBe(final.state === 'filled' ? 0n : fillActive.amountRaw);
  }
  expect(await provider.order(fillCreated.orderId, fillCreated.family, fillCreated.creationBlock)).toMatchObject({ state: final.state, eventRevision: final.eventRevision, filledRaw: final.filledRaw, refundRaw: final.refundRaw, executionTxHash: final.executionTxHash });
  record('scenario_8_actual_race',{ final, results: results.map(result => result.status === 'fulfilled' ? { status: result.status, value: result.value } : { status: result.status, reason: String(result.reason) }) });
  const page = await provider.orderPage(null, 25);
  expect(page.items.every(value => value.owner === owner)).toBe(true);
  const quote = await provider.quote(manifest.usdl, fixture.token, fixture.buyAmount);
  expect(quote).not.toBeNull();
  if (!quote) throw new Error('Funded advanced fixture requires a real executable adapter quote');
  await expect(provider.executeRoute({ ...quote }, quote.outputRaw, BigInt(Math.floor(Date.now() / 1000)) + 300n)).rejects.toThrow('provider-issued');
  const executedRoute = await provider.executeRoute(quote, exactUnits(fixture.minimumOutput, outputDecimals), BigInt(Math.floor(Date.now() / 1000)) + 300n);
  expect(executedRoute.status).toBe('success');
  record('actual_best_route',{ quote, receipt: executedRoute, diagnostics: [...provider.adapterDiagnostics] });
  const activeAdapters = await provider.adapters();
  expect(activeAdapters.length).toBeGreaterThan(0);
  const first = activeAdapters[0];
  const singleHop = [{ adapterId: quote.adapterId, adapterAddress: quote.adapterAddress, tokenIn: manifest.usdl, tokenOut: fixture.token, minAmountOut: exactUnits(fixture.minimumOutput, outputDecimals) }];
  const custom = await provider.prepareCustomRoute(singleHop, quote.amountInRaw, singleHop[0].minAmountOut, BigInt(Math.floor(Date.now() / 1000)) + 300n);
  const simulated = custom.simulation ? custom : await provider.approveCustomRoute(custom);
  expect(simulated.simulation).not.toBeNull();
  const revokeHash = await wallet.writeContract({ address: manifest.usdl, abi: erc20Abi, functionName: 'approve', args: [manifest.router!.address,0n] });
  expect((await provider.receipt(revokeHash)).status).toBe('success');
  const beforeRevoked = await snapshot();
  await expect(provider.executeCustomRoute(simulated)).rejects.toThrow('Allowance changed');
  expect(await snapshot()).toEqual(beforeRevoked);
  await expect(provider.prepareCustomRoute([{ ...singleHop[0], tokenOut: manifest.usdl }], quote.amountInRaw,1n,BigInt(Math.floor(Date.now() / 1000)) + 300n)).rejects.toThrow('Cyclic');
  const allFailed = await variant({ adapters: manifest.adapters.map(binding => ({ ...binding, runtimeCodeHash: `0x${'00'.repeat(32)}` })) });
  expect(await allFailed.quote(manifest.usdl,fixture.token,fixture.buyAmount)).toBeNull();
  expect(allFailed.adapterDiagnostics.size).toBe(manifest.adapters.length);
  if (manifest.adapters.length < 2) throw new Error('Adversarial fixture requires two real supported adapters for independent dependency failure');
  const oneFailed = await variant({ adapters: manifest.adapters.map((binding,index) => index === 0 ? { ...binding, runtimeCodeHash: `0x${'00'.repeat(32)}` } : binding) });
  const alternate = await oneFailed.quote(manifest.usdl,fixture.token,fixture.buyAmount);
  expect(alternate).not.toBeNull();
  expect(alternate?.adapterId.toLowerCase()).not.toBe(manifest.adapters[0].id.toLowerCase());
  expect(oneFailed.adapterDiagnostics.get(manifest.adapters[0].id)).toContain('ABI/code identity changed');
  expect(() => validateCustomRoute([{ adapterId: first.id, adapterAddress: first.address, tokenIn: manifest.usdl, tokenOut: fixture.token, minAmountOut: 1n, inputDecimals: 6, outputDecimals }, { adapterId: first.id, adapterAddress: first.address, tokenIn: manifest.usdl, tokenOut: fixture.token, minAmountOut: 1n, inputDecimals: 6, outputDecimals }], activeAdapters, 1n, 1n, 100n, 1n)).toThrow('Disconnected');
  const eoaManifest = JSON.stringify({ ...manifest, orders: { ...manifest.orders, address: owner } });
  const eoa = await createAdvancedProvider({ publicClient: client, walletClient: wallet, manifestJson: eoaManifest, manifestSha256: createHash('sha256').update(eoaManifest).digest('hex'), attempts: storage });
  expect(eoa.blockers.get('orders')).toContain('EOA');
  expect(eoa.blockers.has('routes')).toBe(false);
  if (!fixture.core || !fixture.adminPrivateKeyFile || !fixture.multihop) throw new Error('Actual core swap, privileged adapter and supported multihop fixture authority required');
  const coreRouterCode = await client.getCode({ address: fixture.core.router }), corePoolCode = await client.getCode({ address: fixture.core.pool });
  if (!coreRouterCode || coreRouterCode === '0x' || !corePoolCode || corePoolCode === '0x') throw new Error('Actual core contract bytecode absent');
  expect(keccak256(coreRouterCode)).toBe(fixture.core.routerCodeHash); expect(keccak256(corePoolCode)).toBe(fixture.core.poolCodeHash);
  const coreApprove = await wallet.writeContract({ address: manifest.usdl, abi: erc20Abi, functionName: 'approve', args: [fixture.core.router,BigInt(fixture.core.amountInRaw)] });
  expect((await provider.receipt(coreApprove)).status).toBe('success');
  const coreSwap = await wallet.writeContract({ address: fixture.core.router, abi: parseAbi(['function buy(address pool,uint256 usdlAmount,uint256 minTokenOut,uint256 deadline) returns (uint256)']), functionName: 'buy', args: [fixture.core.pool,BigInt(fixture.core.amountInRaw),BigInt(fixture.core.minimumOutputRaw),(await client.getBlock()).timestamp + 300n] });
  const coreReceipt = await provider.receipt(coreSwap); expect(coreReceipt.status).toBe('success');
  record('scenario_1',{ ordersAddress: owner, blockers: [...eoa.blockers], coreReceipt });
  const adminAccount = privateKeyToAccount(readFileSync(fixture.adminPrivateKeyFile,'utf8').trim() as Hex), admin = createWalletClient({ account: adminAccount,chain,transport: captured.transport });
  expect(await client.readContract({ address: manifest.router!.address,abi: parseAbi(['function hasRole(bytes32 role,address account) view returns (bool)']), functionName: 'hasRole', args: [`0x${'00'.repeat(32)}`,adminAccount.address] })).toBe(true);
  const quotedBeforeDeactivation = await provider.quote(manifest.usdl,fixture.token,fixture.buyAmount);
  if (!quotedBeforeDeactivation) throw new Error('Actual adapter deactivation requires executable quote');
  const adapterAdminAbi = parseAbi(['function deactivateAdapter(bytes32 id)','function activateAdapter(bytes32 id)']);
  try {
    const hash = await admin.writeContract({ address: manifest.router!.address,abi: adapterAdminAbi,functionName: 'deactivateAdapter',args: [quotedBeforeDeactivation.adapterId as Hex] });
    expect((await provider.receipt(hash)).status).toBe('success');
    const beforeBlockedRoute = await snapshot();
    await expect(provider.executeRoute(quotedBeforeDeactivation,1n,(await client.getBlock()).timestamp + 300n)).rejects.toThrow('deactivated');
    expect(await snapshot()).toEqual(beforeBlockedRoute);
    expect((await provider.adapters()).some(item => item.id.toLowerCase() === quotedBeforeDeactivation.adapterId.toLowerCase())).toBe(false);
    record('scenario_5',{ quote: quotedBeforeDeactivation, deactivationReceipt: await client.getTransactionReceipt({ hash }), before: beforeBlockedRoute, after: await snapshot() });
  } finally {
    const hash = await admin.writeContract({ address: manifest.router!.address,abi: adapterAdminAbi,functionName: 'activateAdapter',args: [quotedBeforeDeactivation.adapterId as Hex] });
    expect((await provider.receipt(hash)).status).toBe('success');
  }
  const hops = fixture.multihop.hops.map(hop => ({ ...hop,minAmountOut: BigInt(hop.minAmountOut) }));
  if (hops.length < 2) throw new Error('Actual multihop fixture must exercise two continuous supported hops');
  const multihop = await provider.prepareCustomRoute(hops,BigInt(fixture.multihop.inputRaw),BigInt(fixture.multihop.finalMinimumRaw),(await client.getBlock()).timestamp + 300n);
  const fundedMultihop = multihop.simulation ? multihop : await provider.approveCustomRoute(multihop);
  expect(fundedMultihop.simulation).not.toBeNull();
  const beforeMultihop = await snapshot();
  const failedHop = fundedMultihop.hops.map((hop,index) => ({ tokenIn: hop.tokenIn,tokenOut: hop.tokenOut,adapterId: hop.adapterId as Hex,minAmountOut: index === 0 ? (1n << 255n) : hop.minAmountOut }));
  const failedHopHash = await wallet.writeContract({ address: manifest.router!.address,abi: parseAbi(['function swapMultiHop((address tokenIn,address tokenOut,bytes32 adapterId,uint256 minAmountOut)[] hops,uint256 amountIn,uint256 amountOutMin,uint256 deadline) returns (uint256)']),functionName: 'swapMultiHop',args: [failedHop, fundedMultihop.inputRaw,fundedMultihop.finalMinimumRaw,fundedMultihop.deadline],gas: BigInt(fixture.multihop.gasLimit) });
  const failedHopReceipt = await provider.receipt(failedHopHash); expect(failedHopReceipt.status).toBe('reverted');
  expect((await snapshot()).balance).toBe(beforeMultihop.balance);
  record('scenario_6',{ hops: fundedMultihop.hops, failedHopReceipt, before: beforeMultihop, after: await snapshot() });
  record('actual_revoked_allowance',{ revokeHash, beforeRevoked, after: await snapshot() });

  if (process.env.L28_MEASURE === '1') {
    const parameters = fixture.measurement;
    if (!parameters || parameters.durationMs > 120000 || !Array.isArray(parameters.accountPrivateKeyFiles) || parameters.accountPrivateKeyFiles.length < 2) throw new Error('Explicit actual mixed workload and funded account cohort required');
    const states = await Promise.all(parameters.accountPrivateKeyFiles.map(async file => {
      const measuredAccount = privateKeyToAccount(readFileSync(file,'utf8').trim() as Hex), measuredWallet = createWalletClient({ account: measuredAccount,chain,transport: captured.transport });
      const measuredProvider = await createAdvancedProvider({ publicClient: client,walletClient: measuredWallet,manifestJson,manifestSha256: fixture.manifestSha256,attempts: storage });
      const account = await measuredProvider.owner();
      expect(await client.readContract({ address: manifest.usdl,abi: erc20Abi,functionName: 'balanceOf',args: [account] })).toBeGreaterThan(exactUnits(fixture.buyAmount,6));
      const prepare = () => measuredProvider.prepareOrder({ kind: 'limit-buy',owner: account,token: fixture.token,settlementAsset: manifest.usdl,amount: fixture.buyAmount,limitPrice: fixture.limitPrice,expiresAt: BigInt(Math.floor(Date.now()/1000)) + 1800n },exactUnits(fixture.minimumOutput,outputDecimals));
      const activeCreated = await measuredProvider.placeOrder(await prepare());
      return { account, measuredWallet, measuredProvider, prepare, activeCreated };
    }));
    const workers = states.map((state,index) => ({ account: state.account, run: async (kind: MeasurementKind): Promise<{ outcome: MeasuredOutcome; raw: unknown }> => {
      const actual = state.measuredProvider;
      if (kind === 'capabilities') { const before = captured.snapshot(); const result = { health: await actual.health(),adapters: await actual.adapters() }; return { outcome: result.health.healthy ? 'verified' : 'unknown',raw: { result,before,after: captured.snapshot() } }; }
      if (kind === 'pagination') { const result = await actual.orderPage(null,25); assertActualPage(result,state.account,25); return { outcome: 'verified',raw: result }; }
      if (kind === 'eligibility') { const result = await actual.order(state.activeCreated.orderId,state.activeCreated.family,state.activeCreated.creationBlock); return { outcome: result.eligible === null || !result.keeperHealthy ? 'unknown' : 'verified',raw: result }; }
      if (kind === 'quotes') { const result = await actual.quote(manifest.usdl,fixture.token,fixture.buyAmount); return { outcome: result ? 'verified' : 'no-route',raw: { quote: result,diagnostics: [...actual.adapterDiagnostics] } }; }
      if (kind === 'cancels') { const created = await actual.placeOrder(await state.prepare()),order = await actual.order(created.orderId,created.family,created.creationBlock); const attempt = await actual.cancel(order),result = await actual.recoverCancel(order); return { outcome: result.outcome.state === 'cancelled' ? 'verified' : 'unknown',raw: { created,attempt,result } }; }
      if (kind === 'approvals') { const hash = await state.measuredWallet.writeContract({ address: manifest.usdl,abi: erc20Abi,functionName: 'approve',args: [manifest.orders!.address,exactUnits(fixture.buyAmount,6)] }); const receipt = await actual.receipt(hash),allowance = await client.readContract({ address: manifest.usdl,abi: erc20Abi,functionName: 'allowance',args: [state.account,manifest.orders!.address] }); return { outcome: receipt.status === 'reverted' ? 'reverted' : allowance >= exactUnits(fixture.buyAmount,6) ? 'verified' : 'unknown',raw: { receipt,allowance } }; }
      if (kind === 'accountSwitches') { const other = states[(index+1)%states.length],page = await other.measuredProvider.orderPage(null,25); assertActualPage(page,other.account,25); await expect(actual.order(other.activeCreated.orderId,other.activeCreated.family,other.activeCreated.creationBlock)).rejects.toThrow('ownership'); return { outcome: 'verified',raw: { from: state.account,to: await other.measuredProvider.owner(),page } }; }
      if (kind === 'customRoutes') { const result = await actual.prepareCustomRoute(hops,BigInt(fixture.multihop.inputRaw),BigInt(fixture.multihop.finalMinimumRaw),(await client.getBlock()).timestamp + 300n); return { outcome: result.simulation ? 'verified' : 'unsupported',raw: result }; }
      if (kind === 'positions') return { outcome: 'unsupported',raw: { vault: manifest.vault,userRedemptionInterface: null,reason: 'Actual accepted contract interface has no user-owned shares/redemption primitive' } };
      throw new Error('Unsupported actual workload operation');
    }}));
    const measurement = await runActualWorkload(identity,parameters,workers,sink);
    record('workload_transport_totals',captured.snapshot());
    writeFileSync(resolve(outputDirectory,'measurement.json'),stringify({ ...identity,...measurement,profile: parameters.profile,observedSurface: 'real_client_rpc_provider',quoteApiCapacityAccepted: false,observedRpcBudget: captured.snapshot(),integrationGaps: ['Consolidated capability30second cache authority absent','Separate quote/eligibility cache TTL authority absent','L28 receipt path still bypasses L26 bounded recovery adapter','Position valuation/pagination requires admitted real ownership/redemption interface','Actual quote service API/global shared load acceptance deferred to serial9.1'] })+'\n',{ mode: 0o600 });
    for (const state of states) { const order = await state.measuredProvider.order(state.activeCreated.orderId,state.activeCreated.family,state.activeCreated.creationBlock); if (['active','triggered'].includes(order.state)) { await state.measuredProvider.cancel(order); await state.measuredProvider.recoverCancel(order); } }
  }
}, 240_000);
