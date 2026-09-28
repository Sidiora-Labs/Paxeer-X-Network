import {
  EXCHANGE_EVENTS,
  LAYERX_EXCHANGE_PRECOMPILE,
  decodeEventFrom,
  exchangeCancelOrderCall,
  exchangeDepositMarginCall,
  exchangeDepositMarginTokenCall,
  exchangePlaceOrderCall,
  exchangeRequestSettlementCall,
  exchangeWithdrawMarginCall,
  type PrecompileEventSpec,
} from '@sidiora/layerx-sdk';

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

export interface ExchangeOrder {
  readonly marketId: string;
  readonly side: number;
  readonly price: bigint;
  readonly quantity: bigint;
  readonly timeInForce: number;
}

export interface ExchangeModule {
  readonly address: string;
  readonly events: readonly PrecompileEventSpec[];
  placeOrder(order: ExchangeOrder): ModuleTransaction;
  cancelOrder(orderId: string): ModuleTransaction;
  requestSettlement(positionId: string): ModuleTransaction;
  depositMargin(account: string, amountWei: bigint): ModuleTransaction;
  depositMarginToken(pointer: string, amount: bigint, account: string): ModuleTransaction;
  withdrawMargin(account: string, assetId: string, amount: bigint): ModuleTransaction;
  send(from: string, tx: ModuleTransaction): Promise<string>;
  decodeEvent(log: ModuleLog): ModuleEvent;
}

export function exchange(provider: ModuleProvider, address: string = LAYERX_EXCHANGE_PRECOMPILE): ExchangeModule {
  const target = moduleAddress(address);
  const events = retargetEvents(EXCHANGE_EVENTS, target);
  return {
    address: target,
    events,
    placeOrder: (order) => retarget(exchangePlaceOrderCall(order), target),
    cancelOrder: (orderId) => retarget(exchangeCancelOrderCall(orderId), target),
    requestSettlement: (positionId) => retarget(exchangeRequestSettlementCall(positionId), target),
    depositMargin: (account, amountWei) => retarget(exchangeDepositMarginCall(account, amountWei), target),
    depositMarginToken: (pointer, amount, account) =>
      retarget(exchangeDepositMarginTokenCall(pointer, amount, account), target),
    withdrawMargin: (account, assetId, amount) => retarget(exchangeWithdrawMarginCall(account, assetId, amount), target),
    send: (from, tx) => sendModuleTransaction(provider, from, tx),
    decodeEvent: (log) => decodeEventFrom(events, log),
  };
}
