import { test } from 'node:test';
import assert from 'node:assert/strict';
import { decodeFunctionData, encodeFunctionData, zeroAddress } from 'viem';
import { factoryAbi, routerAbi, quoterAbi, protocolConfigAbi, validateDeployment } from '../dist/index.js';

test('current factory creation ABI preserves parameters exactly', () => {
  const args = ['Market', 'MKT', 3, zeroAddress];
  const encoded = encodeFunctionData({ abi: factoryAbi, functionName: 'createMarket', args });
  const decoded = decodeFunctionData({ abi: factoryAbi, data: encoded });
  assert.equal(decoded.functionName, 'createMarket');
  assert.deepEqual(decoded.args, args);
});

test('current contracts expose AMM and creation fee capabilities', () => {
  assert.ok(routerAbi.some(entry => entry.type === 'function' && entry.name === 'buy'));
  assert.ok(quoterAbi.some(entry => entry.type === 'function' && entry.name === 'quoteExactInput'));
  assert.ok(protocolConfigAbi.some(entry => entry.type === 'function' && entry.name === 'creationFee'));
});

test('deployment validation rejects absent, zero, malformed addresses and wrong chain', () => {
  assert.throws(() => validateDeployment({ chainId: 1 }), /chain 125/);
  assert.throws(() => validateDeployment({ chainId: 125, factory: zeroAddress }), /factory/);
  assert.throws(() => validateDeployment({ chainId: 125, factory: 'bad' }), /factory/);
});
