import {
  PERPS_EVENTS,
  PERPS_PRECOMPILE,
  decodeEventFrom,
  encodeAbiCall,
  perpsCancelOrderCall,
  perpsClosePositionCall,
  perpsDepositCall,
  perpsOpenPositionCall,
  perpsWithdrawCall,
  type PerpsOpenPosition,
  type PrecompileEventSpec,
} from '@sidiora/layerx-sdk/browser';

import {
  ModuleError,
  ethCall,
  moduleAddress,
  retarget,
  retargetEvents,
  sendModuleTransaction,
  type ModuleEvent,
  type ModuleLog,
  type ModuleProvider,
  type ModuleTransaction,
} from './index.js';

export type ExchangeOrder = PerpsOpenPosition;

/** The perps venue the 0x1015 precompile serves: USDL collateral, uint256 market, order and position ids. */
export interface ExchangeModule {
  readonly address: string;
  readonly events: readonly PrecompileEventSpec[];
  deposit(token: string, amount: bigint): ModuleTransaction;
  withdraw(token: string, amount: bigint): ModuleTransaction;
  openPosition(order: ExchangeOrder): ModuleTransaction;
  cancelOrder(orderId: bigint): ModuleTransaction;
  closePosition(positionId: bigint): ModuleTransaction;
  /** The exact approve of `amount` of `token` to the venue a deposit or openPosition pulls, or undefined when the allowance covers it. */
  approval(owner: string, token: string, amount: bigint): Promise<ModuleTransaction | undefined>;
  send(from: string, tx: ModuleTransaction): Promise<string>;
  decodeEvent(log: ModuleLog): ModuleEvent;
}

export function exchange(provider: ModuleProvider, address: string = PERPS_PRECOMPILE): ExchangeModule {
  const target = moduleAddress(address);
  const events = retargetEvents(PERPS_EVENTS, target);
  return {
    address: target,
    events,
    deposit: (token, amount) => retarget(perpsDepositCall(token, amount), target),
    withdraw: (token, amount) => retarget(perpsWithdrawCall(token, amount), target),
    openPosition: (order) => retarget(perpsOpenPositionCall(order), target),
    cancelOrder: (orderId) => retarget(perpsCancelOrderCall(orderId), target),
    closePosition: (positionId) => retarget(perpsClosePositionCall(positionId), target),
    approval: async (owner, token, amount) => {
      const asset = moduleAddress(token, 'token');
      const answer = await ethCall(
        provider,
        asset,
        encodeAbiCall('allowance', ['address', 'address'], [moduleAddress(owner, 'owner'), target]),
      );
      if (answer.length < 66) throw new ModuleError('invalid_answer', 'allowance');
      if (BigInt(answer.slice(0, 66)) >= amount) return undefined;
      return { to: asset, data: encodeAbiCall('approve', ['address', 'uint256'], [target, amount]), value: 0n };
    },
    send: (from, tx) => sendModuleTransaction(provider, from, tx),
    decodeEvent: (log) => decodeEventFrom(events, log),
  };
}
