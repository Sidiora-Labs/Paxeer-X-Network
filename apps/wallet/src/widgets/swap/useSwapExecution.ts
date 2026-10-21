'use client';

/**
 * Swap execution state machine.
 *
 * Owns `executing`, `txHash`, `execError`. The orchestrator passes in the
 * resolved best quote and active token pair; the hook calls `executeSwap`
 * via the wallet signer and tracks the result.
 */

import { useCallback, useState } from 'react';
import { useWalletActions } from '@/providers/WalletProvider';
import { executeSwap, type SwapToken, type SwapQuote, type SwapParams } from '@/lib/swap';
import {
  validateEvmAddress,
  validateSlippageBps,
  validateSpendableBalance,
  validateSwapQuote,
} from '@/lib/txValidation';

export interface UseSwapExecutionResult {
  executing: boolean;
  txHash: string;
  execError: string;
  execute: (
    bestQuote: SwapQuote,
    fromToken: SwapToken,
    toToken: SwapToken,
    slippageBps: number,
    recipient: string,
    balanceRaw?: bigint | null,
  ) => Promise<void>;
  reset: () => void;
  clearError: () => void;
}

export function useSwapExecution(): UseSwapExecutionResult {
  const { getSigner } = useWalletActions();

  const [executing, setExecuting] = useState(false);
  const [txHash, setTxHash] = useState('');
  const [execError, setExecError] = useState('');

  const execute = useCallback(
    async (
      bestQuote: SwapQuote,
      fromToken: SwapToken,
      toToken: SwapToken,
      slippageBps: number,
      recipient: string,
      balanceRaw?: bigint | null,
    ) => {
      setExecuting(true);
      setExecError('');
      try {
        validateEvmAddress(recipient);
        validateSlippageBps(slippageBps);
        validateSwapQuote(bestQuote);
        validateSpendableBalance(BigInt(bestQuote.amountIn), balanceRaw);
        const signer = await getSigner();
        const params: SwapParams = {
          tokenIn: fromToken,
          tokenOut: toToken,
          amountIn: bestQuote.amountIn,
          slippageBps,
          recipient,
        };
        const hash = await executeSwap(signer, bestQuote, params);
        setTxHash(hash);
      } catch (e) {
        setExecError(e instanceof Error ? e.message : 'Swap failed');
      } finally {
        setExecuting(false);
      }
    },
    [getSigner],
  );

  const reset = useCallback(() => {
    setTxHash('');
    setExecError('');
  }, []);

  const clearError = useCallback(() => setExecError(''), []);

  return { executing, txHash, execError, execute, reset, clearError };
}
