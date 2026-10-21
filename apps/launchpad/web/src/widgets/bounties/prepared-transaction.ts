import { getAddress, type Address, type Hex } from 'viem';
import type { V2ContractTypes } from '@kindle/sdk';

export type TransactionRequest = V2ContractTypes['TransactionRequest'];

/** Deployment address map published by the runtime network authority (role name to address). */
export type DeploymentAddresses = Readonly<Record<string, string>>;

export interface PreparedTransactionContext {
  /** Connected wallet that will sign; the prepared `from` must equal it. */
  wallet: string;
  /** Runtime authority deployment map; the prepared `to` must be one of these addresses. */
  deployment: DeploymentAddresses;
}

export interface ValidatedTransaction {
  chainId: 125;
  from: Address;
  to: Address;
  /** Authority role name of `to` (for the review step). */
  contractRole: string;
  data: Hex;
  value: bigint;
  gas: bigint | null;
  maxFeePerGas: bigint | null;
  maxPriorityFeePerGas: bigint | null;
}

export class PreparedTransactionError extends Error {
  constructor(readonly reason: 'chain' | 'from' | 'to' | 'calldata' | 'amount' | 'value', message: string) { super(message); }
}

const ADDRESS = /^0x[0-9a-fA-F]{40}$/;
const ZERO = /^0x0{40}$/;
const UINT = /^(0|[1-9][0-9]{0,77})$/;
/** Selector (4 bytes) plus whole bytes only. */
const CALLDATA = /^0x(?:[0-9a-fA-F]{2}){4,}$/;
const UINT256_LIMIT = 1n << 256n;

function uint(value: string, field: string): bigint {
  if (!UINT.test(value)) throw new PreparedTransactionError('amount', `Prepared ${field} is not an unsigned integer`);
  const parsed = BigInt(value);
  if (parsed >= UINT256_LIMIT) throw new PreparedTransactionError('amount', `Prepared ${field} exceeds uint256`);
  return parsed;
}

function optionalUint(value: string | null, field: string): bigint | null {
  return value === null ? null : uint(value, field);
}

/** Role name of `address` in the deployment map, or null when the authority does not publish it. */
export function deploymentRole(address: string, deployment: DeploymentAddresses): string | null {
  if (!ADDRESS.test(address)) return null;
  const target = address.toLowerCase();
  for (const [role, candidate] of Object.entries(deployment)) {
    if (typeof candidate === 'string' && ADDRESS.test(candidate) && !ZERO.test(candidate) && candidate.toLowerCase() === target) return role;
  }
  return null;
}

/**
 * Validates a server-prepared transaction (bounty funding or settlement)
 * before it reaches the wallet: chain 125, `from` is the connected wallet,
 * `to` is a contract the runtime deployment authority publishes, calldata is
 * whole-byte hex with a selector, every amount is a uint256, and no native
 * PAX principal moves (escrow holds USDL or launched tokens only).
 */
export function validatePreparedTransaction(request: TransactionRequest, context: PreparedTransactionContext): ValidatedTransaction {
  if (request.chainId !== '125') throw new PreparedTransactionError('chain', 'Prepared transaction targets another chain');
  if (!ADDRESS.test(context.wallet) || !ADDRESS.test(request.from) || request.from.toLowerCase() !== context.wallet.toLowerCase()) {
    throw new PreparedTransactionError('from', 'Prepared transaction signer is not the connected wallet');
  }
  if (!ADDRESS.test(request.to) || ZERO.test(request.to)) throw new PreparedTransactionError('to', 'Prepared transaction contract address is invalid');
  const contractRole = deploymentRole(request.to, context.deployment);
  if (contractRole === null) throw new PreparedTransactionError('to', 'Prepared transaction contract is not part of the attested deployment');
  if (typeof request.data !== 'string' || !CALLDATA.test(request.data)) throw new PreparedTransactionError('calldata', 'Prepared calldata is not valid hex');
  const value = uint(request.valueRaw, 'value');
  if (value !== 0n) throw new PreparedTransactionError('value', 'Prepared transaction would transfer native PAX principal');
  return {
    chainId: 125,
    from: getAddress(request.from),
    to: getAddress(request.to),
    contractRole,
    data: request.data as Hex,
    value,
    gas: optionalUint(request.gasLimitRaw, 'gas limit'),
    maxFeePerGas: optionalUint(request.maxFeePerGasRaw, 'max fee per gas'),
    maxPriorityFeePerGas: optionalUint(request.maxPriorityFeePerGasRaw, 'priority fee per gas'),
  };
}

/** Upper bound of the PAX gas cost: gas limit times the fee per gas the wallet will be asked to pay. */
export function gasCostUpperBound(gas: bigint, feePerGas: bigint): bigint {
  if (gas <= 0n || feePerGas < 0n) throw new PreparedTransactionError('amount', 'Gas estimate is not positive');
  return gas * feePerGas;
}
