import {
  LAUNCHPAD_EVENTS,
  LAUNCHPAD_PRECOMPILE,
  decodeEventFrom,
  launchpadBuyCall,
  launchpadClaimFeesCall,
  launchpadCreateMarketCall,
  launchpadSellCall,
  launchpadSetFeeStrategyCall,
  launchpadTokenCall,
  type LaunchpadSwapOrder,
  type LaunchpadTokenWrite,
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

export interface LaunchpadModule {
  readonly address: string;
  readonly events: readonly PrecompileEventSpec[];
  buy(order: LaunchpadSwapOrder): ModuleTransaction;
  sell(order: LaunchpadSwapOrder): ModuleTransaction;
  createMarket(name: string, symbol: string, feeStrategy: number): ModuleTransaction;
  setFeeStrategy(token: string, feeStrategy: number): ModuleTransaction;
  claimFees(token: string, recipient: string): ModuleTransaction;
  tokenWrite(write: LaunchpadTokenWrite, token: string): ModuleTransaction;
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
    createMarket: (name, symbol, feeStrategy) => retarget(launchpadCreateMarketCall(name, symbol, feeStrategy), target),
    setFeeStrategy: (token, feeStrategy) => retarget(launchpadSetFeeStrategyCall(token, feeStrategy), target),
    claimFees: (token, recipient) => retarget(launchpadClaimFeesCall(token, recipient), target),
    tokenWrite: (write, token) => retarget(launchpadTokenCall(write, token), target),
    send: (from, tx) => sendModuleTransaction(provider, from, tx),
    decodeEvent: (log) => decodeEventFrom(events, log),
  };
}
