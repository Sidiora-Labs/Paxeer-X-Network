import {
  BRIDGE_EVENTS,
  LAYERX_BRIDGE_PRECOMPILE,
  bridgeInCall,
  bridgeOutCall,
  decodeEventFrom,
  type BridgeInAttestation,
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

export interface BridgeModule {
  readonly address: string;
  readonly events: readonly PrecompileEventSpec[];
  bridgeIn(attestation: BridgeInAttestation): ModuleTransaction;
  bridgeOut(chain: bigint, asset: string, amount: bigint, recipient: string): ModuleTransaction;
  send(from: string, tx: ModuleTransaction): Promise<string>;
  decodeEvent(log: ModuleLog): ModuleEvent;
}

export function bridge(provider: ModuleProvider, address: string = LAYERX_BRIDGE_PRECOMPILE): BridgeModule {
  const target = moduleAddress(address);
  const events = retargetEvents(BRIDGE_EVENTS, target);
  return {
    address: target,
    events,
    bridgeIn: (attestation) => retarget(bridgeInCall(attestation), target),
    bridgeOut: (chain, asset, amount, recipient) => retarget(bridgeOutCall(chain, asset, amount, recipient), target),
    send: (from, tx) => sendModuleTransaction(provider, from, tx),
    decodeEvent: (log) => decodeEventFrom(events, log),
  };
}
