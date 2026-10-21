import { SIDIORA_TOKEN } from '@sidiora/layerx-sdk/browser';

import { FEE_TOKEN_PRECOMPILE, SIDIORA_FEE_DECIMALS, SIDIORA_FEE_DENOM } from './fee-token.js';
import { ModuleError, moduleAddress } from './index.js';

export type FeeChoiceId = 'pax_gas' | 'sid_sponsored' | 'sid_native';
export type FeeChoiceLabel = 'PAX gas' | 'SID sponsored' | 'SID native';

export interface FeeDenomination {
  readonly symbol: 'PAX' | 'SID';
  readonly name: 'Paxeer' | 'Sidiora';
  readonly decimals: number;
  readonly native: boolean;
  readonly token: string | null;
  readonly denom: string | null;
}

export type FeePath =
  | { readonly kind: 'native_gas'; readonly method: 'eth_sendTransaction' }
  | { readonly kind: 'sponsored_batch'; readonly method: 'eth_sign'; readonly construction: 'sponsored_batch'; readonly submit: 'gateway' }
  | {
      readonly kind: 'fee_token_preference';
      readonly method: 'eth_sendTransaction';
      readonly precompile: string;
      readonly setFeeDenom: string;
    };

export interface FeeChoice {
  readonly id: FeeChoiceId;
  readonly label: FeeChoiceLabel;
  readonly denomination: FeeDenomination;
  readonly path: FeePath;
}

export interface FeeAmount {
  readonly choice: FeeChoiceId;
  readonly amount: bigint;
  readonly symbol: 'PAX' | 'SID';
  readonly decimals: number;
  readonly display: string;
}

export interface FeeChoiceHelper {
  readonly choices: readonly FeeChoice[];
  choice(id: FeeChoiceId): FeeChoice;
  denominate(id: FeeChoiceId, amount: bigint): FeeAmount;
}

const PAX: FeeDenomination = { symbol: 'PAX', name: 'Paxeer', decimals: 18, native: true, token: null, denom: null };

function formatUnits(amount: bigint, decimals: number): string {
  const scale = 10n ** BigInt(decimals);
  const whole = amount / scale;
  const fraction = (amount % scale).toString().padStart(decimals, '0').replace(/0+$/u, '');
  return fraction === '' ? whole.toString() : `${whole.toString()}.${fraction}`;
}

export function feeChoice(feeTokenAddress: string = FEE_TOKEN_PRECOMPILE): FeeChoiceHelper {
  const precompile = moduleAddress(feeTokenAddress, 'feeTokenAddress');
  const sid: FeeDenomination = {
    symbol: 'SID',
    name: 'Sidiora',
    decimals: SIDIORA_FEE_DECIMALS,
    native: false,
    token: SIDIORA_TOKEN.toLowerCase(),
    denom: SIDIORA_FEE_DENOM,
  };
  const choices: readonly FeeChoice[] = [
    { id: 'pax_gas', label: 'PAX gas', denomination: PAX, path: { kind: 'native_gas', method: 'eth_sendTransaction' } },
    {
      id: 'sid_sponsored',
      label: 'SID sponsored',
      denomination: sid,
      path: { kind: 'sponsored_batch', method: 'eth_sign', construction: 'sponsored_batch', submit: 'gateway' },
    },
    {
      id: 'sid_native',
      label: 'SID native',
      denomination: sid,
      path: {
        kind: 'fee_token_preference',
        method: 'eth_sendTransaction',
        precompile,
        setFeeDenom: SIDIORA_FEE_DENOM,
      },
    },
  ];
  const choice = (id: FeeChoiceId): FeeChoice => {
    const found = choices.find((candidate) => candidate.id === id);
    if (found === undefined) {
      throw new ModuleError('invalid_value', 'fee choice');
    }
    return found;
  };
  return {
    choices,
    choice,
    denominate: (id, amount) => {
      if (typeof amount !== 'bigint' || amount < 0n) {
        throw new ModuleError('invalid_value', 'amount');
      }
      const { denomination } = choice(id);
      return {
        choice: id,
        amount,
        symbol: denomination.symbol,
        decimals: denomination.decimals,
        display: `${formatUnits(amount, denomination.decimals)} ${denomination.symbol}`,
      };
    },
  };
}
