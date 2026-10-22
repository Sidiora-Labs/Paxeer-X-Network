import {
  LAUNCHPAD_EVENTS,
  LAUNCHPAD_PRECOMPILE,
  decodeEventFrom,
  launchpadApproval,
  launchpadBuyCall,
  launchpadClaimFeesCall,
  launchpadCreateMarketCall,
  launchpadSellCall,
  launchpadSetFeeStrategyCall,
  launchpadNftCall,
  type LaunchpadSpend,
  type LaunchpadSwapOrder,
  type LaunchpadNftWrite,
  type PrecompileEventSpec,
} from '@sidiora/layerx-sdk/browser';

import {
  moduleAddress,
  retarget,
  retargetEvents,
  sendModuleTransaction,
  type ModuleEvent,
  type ModuleLog,
  type ModuleProvider,
  type ModuleTransaction,
} from './index.js';

export type { LaunchpadSpend } from '@sidiora/layerx-sdk/browser';

export interface LaunchpadModule {
  readonly address: string;
  readonly events: readonly PrecompileEventSpec[];
  buy(order: LaunchpadSwapOrder): ModuleTransaction;
  sell(order: LaunchpadSwapOrder): ModuleTransaction;
  createMarket(name: string, symbol: string, feeStrategy: number, optical?: string): ModuleTransaction;
  setFeeStrategy(nftId: bigint, feeStrategy: number): ModuleTransaction;
  claimFees(nftId: bigint): ModuleTransaction;
  nftWrite(write: LaunchpadNftWrite, nftId: bigint): ModuleTransaction;
  /** The exact approve to 0x1017 `spend` needs before its write, or undefined when the allowance covers it. */
  approval(owner: string, spend: LaunchpadSpend): Promise<ModuleTransaction | undefined>;
  send(from: string, tx: ModuleTransaction): Promise<string>;
  decodeEvent(log: ModuleLog): ModuleEvent;
}

export function launchpad(provider: ModuleProvider, address: string = LAUNCHPAD_PRECOMPILE): LaunchpadModule {
  const target = moduleAddress(address);
  const events = retargetEvents(LAUNCHPAD_EVENTS, target);
  return {
    address: target,
    events,
    buy: (order) => retarget(launchpadBuyCall(order), target),
    sell: (order) => retarget(launchpadSellCall(order), target),
    createMarket: (name, symbol, feeStrategy, optical) =>
      retarget(optical === undefined ? launchpadCreateMarketCall(name, symbol, feeStrategy) : launchpadCreateMarketCall(name, symbol, feeStrategy, optical), target),
    setFeeStrategy: (nftId, feeStrategy) => retarget(launchpadSetFeeStrategyCall(nftId, feeStrategy), target),
    claimFees: (nftId) => retarget(launchpadClaimFeesCall(nftId), target),
    nftWrite: (write, nftId) => retarget(launchpadNftCall(write, nftId), target),
    approval: (owner, spend) => launchpadApproval(provider, moduleAddress(owner, 'owner'), spend),
    send: (from, tx) => sendModuleTransaction(provider, from, tx),
    decodeEvent: (log) => decodeEventFrom(events, log),
  };
}
