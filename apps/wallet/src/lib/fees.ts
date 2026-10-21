import { ethers } from 'ethers';
import { preferencesRepository } from '../platform/storage/repositories';

export interface ResolvedFees {
  mode: 'auto' | 'economy' | 'priority' | 'custom';
  maxFeePerGas: bigint;
  maxPriorityFeePerGas: bigint;
  source: 'rpc' | 'custom' | 'fallback';
}

const MIN_GAS_PRICE = 100_000_000_000n;

function atLeastFloor(value: bigint | null | undefined): bigint {
  return value && value > MIN_GAS_PRICE ? value : MIN_GAS_PRICE;
}

export async function resolveFeeOverrides(
  provider: ethers.Provider,
): Promise<ResolvedFees> {
  const preferences = preferencesRepository.read();
  if (
    preferences.feeMode === 'custom' &&
    preferences.customMaxFeeGwei &&
    preferences.customPriorityFeeGwei
  ) {
    const maxFeePerGas = ethers.parseUnits(preferences.customMaxFeeGwei, 'gwei');
    const maxPriorityFeePerGas = ethers.parseUnits(
      preferences.customPriorityFeeGwei,
      'gwei',
    );
    if (maxPriorityFeePerGas > maxFeePerGas) {
      throw new Error('Priority fee cannot exceed the maximum fee.');
    }
    return {
      mode: 'custom',
      maxFeePerGas: atLeastFloor(maxFeePerGas),
      maxPriorityFeePerGas: atLeastFloor(maxPriorityFeePerGas),
      source: 'custom',
    };
  }

  try {
    const fees = await provider.getFeeData();
    let maxFeePerGas = atLeastFloor(fees.maxFeePerGas ?? fees.gasPrice);
    let maxPriorityFeePerGas = atLeastFloor(
      fees.maxPriorityFeePerGas ?? maxFeePerGas,
    );
    if (preferences.feeMode === 'economy') {
      maxFeePerGas = atLeastFloor((maxFeePerGas * 90n) / 100n);
      maxPriorityFeePerGas = atLeastFloor((maxPriorityFeePerGas * 90n) / 100n);
    } else if (preferences.feeMode === 'priority') {
      maxFeePerGas = (maxFeePerGas * 125n) / 100n;
      maxPriorityFeePerGas = (maxPriorityFeePerGas * 125n) / 100n;
    }
    if (maxPriorityFeePerGas > maxFeePerGas) {
      maxPriorityFeePerGas = maxFeePerGas;
    }
    return {
      mode: preferences.feeMode === 'custom' ? 'auto' : preferences.feeMode,
      maxFeePerGas,
      maxPriorityFeePerGas,
      source: 'rpc',
    };
  } catch {
    return {
      mode: preferences.feeMode === 'custom' ? 'auto' : preferences.feeMode,
      maxFeePerGas: MIN_GAS_PRICE,
      maxPriorityFeePerGas: MIN_GAS_PRICE,
      source: 'fallback',
    };
  }
}

export function estimateFeeWei(gasLimit: bigint, fees: ResolvedFees): bigint {
  return gasLimit * fees.maxFeePerGas;
}
