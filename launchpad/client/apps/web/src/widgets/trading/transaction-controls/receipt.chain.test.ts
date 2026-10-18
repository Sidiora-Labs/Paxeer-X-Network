import { it, expect } from 'vitest';
import { createServer } from 'node:net';
import { spawn } from 'node:child_process';
import { createPublicClient, createWalletClient, createTestClient, http, defineChain } from 'viem';
import { privateKeyToAccount } from 'viem/accounts';
import { attemptState, CompletionLedger, type TxAttempt, type PendingAttemptRecord } from './domain';
import { ReceiptRecoveryScheduler, type RecoveryUpdate } from './recovery';
it('actual private chain125 receipt settles exact hash and revert never completes', async () => {
  const port=Number(process.env.L26_RECEIPT_PORT??'22608');
  if(!Number.isInteger(port)||port<1024||port>65535)throw new Error('Invalid owned receipt fixture port');
  const guard = createServer();
  await new Promise<void>((resolve, reject) => { guard.once('error', reject); guard.listen(port, '127.0.0.1', resolve); });
  await new Promise<void>((resolve, reject) => guard.close(err => err ? reject(err) : resolve()));
  const proc = spawn('/root/.foundry/bin/anvil', ['--port', String(port), '--chain-id', '125', '--silent'], { stdio: 'ignore' });
  let spawnError: Error | undefined; proc.once('error', error => { spawnError = error; });
  const chain = defineChain({ id: 125, name: 'L26 private', nativeCurrency: { name: 'PAX', symbol: 'PAX', decimals: 18 }, rpcUrls: { default: { http: [`http://127.0.0.1:${port}`] } } });
  const client = createPublicClient({ chain, transport: http() });
  const account = privateKeyToAccount('0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80');
  const wallet = createWalletClient({ account, chain, transport: http() });
  try {
    let ready = false; for (let i = 0; i < 40; i++) { if (spawnError || proc.exitCode !== null) throw spawnError ?? new Error('Owned Anvil exited before readiness'); try { ready = await client.getChainId() === 125; if (ready) break; } catch { } await new Promise(r => setTimeout(r, 50)); } expect(ready).toBe(true); await new Promise(r => setTimeout(r, 100)); if (spawnError || proc.exitCode !== null) throw spawnError ?? new Error('Owned Anvil bind failure');
    // Private bytecode deployment returns runtime that always reverts. Not current core deployment evidence.
    const deploy = await wallet.sendTransaction({ type: 'legacy', data: '0x6005600c60003960056000f360006000fd' });
    const deployed = await client.waitForTransactionReceipt({ hash: deploy }); expect(deployed.status).toBe('success');
    const txHash = await wallet.sendTransaction({ type: 'legacy', to: deployed.contractAddress!, gas: 100000n });
    const a: TxAttempt = { intentId: 'private-revert', chainId: 125, from: account.address, txHash, submittedAt: Date.now(), action: 'fixture', actionFingerprint: 'fixture' };
    const receipt = await client.waitForTransactionReceipt({ hash: txHash });
    expect(receipt.status).toBe('reverted'); expect(attemptState(a, Date.now(), receipt)).toBe('confirmed_reverted'); expect(new CompletionLedger().claim(a, receipt)).toBe(false);
    const ok: TxAttempt = { ...a, txHash: deploy }; expect(attemptState(ok, Date.now(), deployed)).toBe('confirmed_success');
    let active = 0, maximum = 0;
    const readers = Array.from({ length: 3 }, () => new ReceiptRecoveryScheduler(async attempt => {
      active++; maximum = Math.max(maximum, active);
      try { return await client.getTransactionReceipt({ hash: attempt.txHash as `0x${string}` }); } finally { active--; }
    }));
    try {
      const updates = await Promise.all(readers.flatMap(reader => [a, ok].map(attempt => new Promise<RecoveryUpdate>(resolve => {
        reader.watch({ ...attempt, version: 1 }, resolve);
      }))));
      expect(maximum).toBeLessThanOrEqual(4); expect(active).toBe(0);
      expect(updates.filter(update => update.state === 'confirmed_success')).toHaveLength(3);
      expect(updates.filter(update => update.state === 'confirmed_reverted')).toHaveLength(3);
      expect(updates.every(update => update.receipt?.transactionHash === update.attempt.txHash)).toBe(true);
    } finally { for (const reader of readers) reader.close(); }
    const testClient = createTestClient({ mode: 'anvil', chain, transport: http() });
    await testClient.setAutomine(false);
    const pendingHash = await wallet.sendTransaction({ type: 'legacy', to: account.address, value: 1n, gas: 21000n });
    const pending: PendingAttemptRecord = { ...a, version: 1, txHash: pendingHash, submittedAt: Date.now() - 61000 };
    const recovery = new ReceiptRecoveryScheduler(attempt => client.getTransactionReceipt({ hash: attempt.txHash as `0x${string}` }));
    try {
      let terminalResolve!: (update: RecoveryUpdate) => void;
      const terminal = new Promise<RecoveryUpdate>(resolve => { terminalResolve = resolve; });
      const first = await new Promise<RecoveryUpdate>(resolve => {
        recovery.watch(pending, update => { resolve(update); if (update.receipt) terminalResolve(update); });
      });
      expect(first.state).toBe('delayed'); expect(first.receipt).toBeUndefined();
      await testClient.mine({ blocks: 1 }); recovery.wake(pendingHash);
      expect((await terminal).state).toBe('confirmed_success');
    } finally { recovery.close(); await testClient.setAutomine(true); }

  } finally { proc.kill('SIGTERM'); await new Promise<void>(resolve => { if (proc.exitCode !== null) resolve(); else proc.once('exit', () => resolve()); }); }
}, 20000);
