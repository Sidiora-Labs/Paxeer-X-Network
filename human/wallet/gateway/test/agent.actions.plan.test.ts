import { describe, it, expect, beforeAll } from 'vitest';
import { decodeFunctionData, keccak256, toBytes } from 'viem';
import {
  buildAllowanceAndCallPlan,
  buildLayerxDepositPlan,
  didClaimFor,
  approvalCalldata,
  PlanError,
} from '../src/agent/actions/plan.js';

/**
 * Plan builders are the wallet-owns-encoding boundary: the agent supplies
 * intent, the wallet produces exact calldata. These lock the encoding + the
 * fail-closed validation (no network needed for the paths under test).
 */

const VAULT = '0xf756895fD414f7D20413B61c9291ABe98fcED1CE';

beforeAll(() => {
  process.env.LAYERX_VAULT_ADDRESS = VAULT;
  process.env.LAYERX_USDL_ADDRESS = '0x85FcD13735F4309833A503EE804ea32395851479';
});

describe('didClaimFor', () => {
  it('is the lowercase 0x keccak256 of the DID string', () => {
    const did = 'did:matrix:alice:00112233aabbccdd';
    expect(didClaimFor(did)).toBe(keccak256(toBytes(did)));
    expect(didClaimFor(did)).toMatch(/^0x[0-9a-f]{64}$/);
  });
});

describe('buildAllowanceAndCallPlan', () => {
  it('encodes the method + raw args and derives the ERC-20 approval leg', () => {
    const plan = buildAllowanceAndCallPlan({
      token: '0x85FcD13735F4309833A503EE804ea32395851479',
      amount: '250000000',
      spender: VAULT,
      contract: VAULT,
      method: 'depositUSDL(uint256,bytes32)',
      args: ['250000000', '0x' + '11'.repeat(32)],
    });

    expect(plan.amountWei).toBe('250000000');
    expect(plan.callValueWei).toBe('0');
    expect(plan.selector).toBe(plan.callData.slice(0, 10));

    // The primary call decodes back to depositUSDL with the exact args.
    const decoded = decodeFunctionData({
      abi: [
        {
          type: 'function',
          name: 'depositUSDL',
          stateMutability: 'nonpayable',
          inputs: [
            { name: 'amount', type: 'uint256' },
            { name: 'did', type: 'bytes32' },
          ],
          outputs: [{ name: '', type: 'uint256' }],
        },
      ],
      data: plan.callData,
    });
    expect(decoded.functionName).toBe('depositUSDL');
    expect(decoded.args?.[0]).toBe(250000000n);

    // The approval leg is approve(spender, amount) on the token.
    const approve = decodeFunctionData({
      abi: [
        {
          type: 'function',
          name: 'approve',
          stateMutability: 'nonpayable',
          inputs: [
            { name: 'spender', type: 'address' },
            { name: 'amount', type: 'uint256' },
          ],
          outputs: [{ name: '', type: 'bool' }],
        },
      ],
      data: approvalCalldata(plan),
    });
    expect(approve.functionName).toBe('approve');
    expect((approve.args?.[0] as string).toLowerCase()).toBe(VAULT.toLowerCase());
    expect(approve.args?.[1]).toBe(250000000n);
  });

  it('rejects a non-positive amount', () => {
    expect(() =>
      buildAllowanceAndCallPlan({
        token: VAULT,
        amount: '0',
        spender: VAULT,
        contract: VAULT,
        method: 'depositUSDL(uint256,bytes32)',
        args: ['0', '0x' + '00'.repeat(32)],
      }),
    ).toThrowError(PlanError);
  });

  it('rejects a malformed method signature', () => {
    try {
      buildAllowanceAndCallPlan({
        token: VAULT,
        amount: '1',
        spender: VAULT,
        contract: VAULT,
        method: 'not a signature',
        args: [],
      });
      expect.unreachable('should have thrown');
    } catch (err) {
      expect(err).toBeInstanceOf(PlanError);
      expect((err as PlanError).code).toBe('INVALID_REQUEST');
    }
  });
});

describe('buildLayerxDepositPlan', () => {
  it('rejects a malformed did_claim before any network read', async () => {
    await expect(
      buildLayerxDepositPlan({ amount: '250', didClaim: '0xnothex' }),
    ).rejects.toBeInstanceOf(PlanError);
  });

  it('rejects a did_claim that is not exactly 32 bytes', async () => {
    await expect(
      buildLayerxDepositPlan({ amount: '250', didClaim: '0x' + 'ab'.repeat(20) }),
    ).rejects.toBeInstanceOf(PlanError);
  });
});
