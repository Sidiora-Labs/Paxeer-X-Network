import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { formatTransaction, recoverTransactionAddress, serializeTransaction, type Hex, type RpcTransaction } from 'viem';
import { transactionHash } from '../src/provision/bind.js';

/**
 * Mined chain 125 transactions exactly as a public node answers eth_getTransactionByHash: one legacy
 * EIP-155 (type 0x0) and one EIP-1559 (type 0x2). The node serves no eth_getRawTransactionByHash, so the
 * signed bytes are rebuilt from the node's fields and signature.
 */
const mined = JSON.parse(readFileSync(new URL('./fixtures/chain125-transactions.json', import.meta.url), 'utf8')) as RpcTransaction[];

describe('local transaction hash', () => {
  it('reproduces the node hash of mined chain 125 legacy and EIP-1559 transactions', async () => {
    expect(mined.map((tx) => tx.type)).toEqual(['0x0', '0x2']);
    for (const rpcTx of mined) {
      const tx = formatTransaction(rpcTx);
      expect(tx.chainId).toBe(125);
      const raw = serializeTransaction({ ...tx, data: tx.input } as Parameters<typeof serializeTransaction>[0], { r: tx.r, s: tx.s, v: tx.v, yParity: tx.yParity });
      expect(transactionHash(raw)).toBe(rpcTx.hash);
      expect((await recoverTransactionAddress({ serializedTransaction: raw as Parameters<typeof recoverTransactionAddress>[0]['serializedTransaction'] })).toLowerCase())
        .toBe(rpcTx.from);
    }
  });
});
