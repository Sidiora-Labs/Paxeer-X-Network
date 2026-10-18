import { describe, expect, it } from 'vitest';
import { CORE_DEPLOYMENT } from '@/core/network/deployment';
import { PreparedTransactionError, deploymentRole, gasCostUpperBound, validatePreparedTransaction, type TransactionRequest } from './prepared-transaction';

const wallet = '0x1111111111111111111111111111111111111111';
const deployment = { Router: CORE_DEPLOYMENT.router, USDL: CORE_DEPLOYMENT.usdl, FeesRouter: CORE_DEPLOYMENT.feesRouter };

function request(overrides: Partial<TransactionRequest> = {}): TransactionRequest {
  return { chainId: '125', from: wallet, to: CORE_DEPLOYMENT.usdl, data: '0x095ea7b3' + '0'.repeat(128), valueRaw: '0', gasLimitRaw: '65000', maxFeePerGasRaw: '1000000000', maxPriorityFeePerGasRaw: null, ...overrides };
}

function reason(run: () => unknown): string | null {
  try { run(); return null; } catch (error) { return error instanceof PreparedTransactionError ? error.reason : 'other'; }
}

describe('validatePreparedTransaction', () => {
  it('accepts a chain 125 request from the wallet to an attested deployment address', () => {
    const tx = validatePreparedTransaction(request({ from: wallet.toUpperCase().replace('0X', '0x') }), { wallet, deployment });
    expect(tx.contractRole).toBe('USDL');
    expect(tx.to.toLowerCase()).toBe(CORE_DEPLOYMENT.usdl.toLowerCase());
    expect(tx.gas).toBe(65000n);
    expect(tx.maxFeePerGas).toBe(1_000_000_000n);
    expect(tx.maxPriorityFeePerGas).toBeNull();
    expect(tx.value).toBe(0n);
  });
  it('rejects another chain', () => {
    expect(reason(() => validatePreparedTransaction({ ...request(), chainId: '1' as '125' }, { wallet, deployment }))).toBe('chain');
  });
  it('rejects a signer other than the connected wallet', () => {
    expect(reason(() => validatePreparedTransaction(request({ from: '0x2222222222222222222222222222222222222222' }), { wallet, deployment }))).toBe('from');
    expect(reason(() => validatePreparedTransaction(request(), { wallet: 'not-a-wallet', deployment }))).toBe('from');
  });
  it('rejects contracts outside the runtime deployment authority', () => {
    expect(reason(() => validatePreparedTransaction(request({ to: '0x3333333333333333333333333333333333333333' }), { wallet, deployment }))).toBe('to');
    expect(reason(() => validatePreparedTransaction(request({ to: '0x0000000000000000000000000000000000000000' }), { wallet, deployment: { Zero: '0x0000000000000000000000000000000000000000' } }))).toBe('to');
  });
  it('rejects calldata that is not whole-byte hex with a selector', () => {
    for (const data of ['0x', '0x1234', '0x095ea7b', '095ea7b3', '0x095ea7bz']) {
      expect(reason(() => validatePreparedTransaction(request({ data }), { wallet, deployment }))).toBe('calldata');
    }
  });
  it('rejects malformed amounts and any native principal', () => {
    expect(reason(() => validatePreparedTransaction(request({ gasLimitRaw: '-1' }), { wallet, deployment }))).toBe('amount');
    expect(reason(() => validatePreparedTransaction(request({ maxFeePerGasRaw: (1n << 256n).toString() }), { wallet, deployment }))).toBe('amount');
    expect(reason(() => validatePreparedTransaction(request({ valueRaw: '1' }), { wallet, deployment }))).toBe('value');
  });
});

describe('deploymentRole and gas bound', () => {
  it('maps addresses to role names case-insensitively', () => {
    expect(deploymentRole(CORE_DEPLOYMENT.router.toLowerCase(), deployment)).toBe('Router');
    expect(deploymentRole('0x4444444444444444444444444444444444444444', deployment)).toBeNull();
    expect(deploymentRole('nope', deployment)).toBeNull();
  });
  it('multiplies gas by fee and refuses a non-positive gas', () => {
    expect(gasCostUpperBound(21000n, 2n)).toBe(42000n);
    expect(() => gasCostUpperBound(0n, 2n)).toThrow(PreparedTransactionError);
  });
});
