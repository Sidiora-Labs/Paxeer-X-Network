import { SIDIORA_DECIMALS, encodeAbiCall } from '@sidiora/layerx-sdk';

import {
  ModuleError,
  decodeAbiString,
  ethCall,
  moduleAddress,
  sendModuleTransaction,
  type ModuleProvider,
  type ModuleTransaction,
} from './index.js';

export const FEE_TOKEN_PRECOMPILE = '0x0000000000000000000000000000000000001018';
export const SIDIORA_FEE_DENOM = 'usid';
export const SIDIORA_FEE_DECIMALS = SIDIORA_DECIMALS;

const DENOM = /^[a-zA-Z][a-zA-Z0-9/:._-]{2,127}$/u;

export interface FeeTokenModule {
  readonly address: string;
  setFeeDenom(denom: string): ModuleTransaction;
  clearFeeDenom(): ModuleTransaction;
  getFeeDenomCallData(account: string): string;
  getFeeDenom(account: string): Promise<string>;
  send(from: string, tx: ModuleTransaction): Promise<string>;
}

export function feeToken(provider: ModuleProvider, address: string = FEE_TOKEN_PRECOMPILE): FeeTokenModule {
  const target = moduleAddress(address);
  const getFeeDenomCallData = (account: string): string =>
    encodeAbiCall('getFeeDenom', ['address'], [moduleAddress(account, 'account')]);
  return {
    address: target,
    setFeeDenom: (denom) => {
      if (typeof denom !== 'string' || !DENOM.test(denom)) {
        throw new ModuleError('invalid_value', 'denom');
      }
      return { to: target, data: encodeAbiCall('setFeeDenom', ['string'], [denom]), value: 0n };
    },
    clearFeeDenom: () => ({ to: target, data: encodeAbiCall('clearFeeDenom', [], []), value: 0n }),
    getFeeDenomCallData,
    getFeeDenom: async (account) => decodeAbiString(await ethCall(provider, target, getFeeDenomCallData(account))),
    send: (from, tx) => sendModuleTransaction(provider, from, tx),
  };
}
