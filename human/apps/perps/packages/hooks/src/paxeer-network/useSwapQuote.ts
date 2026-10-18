import { useRef, useState } from "react";
import { useMemoizedFn } from "../shared/useMemoizedFn";
import { useMutation } from "../useMutation";
import {
  normalizeSwapQuoteResponse,
  isSwapQuoteData,
  SWAP_QUOTE_URL,
  unwrapSwapQuoteResponse,
  type SwapQuoteError,
  type SwapQuoteData,
  type SwapQuoteRequest,
  type SwapQuoteResponse,
} from "./swapQuote";

export {
  normalizeSwapQuoteResponse,
  isSwapQuoteData,
  SWAP_QUOTE_URL,
  unwrapSwapQuoteResponse,
} from "./swapQuote";
export type {
  GasEstimate,
  SwapQuoteData,
  SwapQuoteError,
  SwapQuoteRequest,
  SwapQuoteResponse,
  SwapQuoteToken,
} from "./swapQuote";

export const useSwapQuote = () => {
  const [trigger, { reset, isMutating }] = useMutation<
    SwapQuoteResponse,
    SwapQuoteError
  >(SWAP_QUOTE_URL);
  const generation = useRef(0);
  const [accepted, setAccepted] = useState<{
    data: SwapQuoteData;
    request: SwapQuoteRequest;
  }>();
  const [quoteError, setQuoteError] = useState<SwapQuoteError>();

  const postQuote = useMemoizedFn(async (request: SwapQuoteRequest | null) => {
    const id = ++generation.current;
    setQuoteError(undefined);
    setAccepted(undefined);

    try {
      const response = await trigger(request).then(normalizeSwapQuoteResponse);
      const quote = unwrapSwapQuoteResponse(response);
      if (
        id === generation.current &&
        request &&
        quote &&
        isSwapQuoteData(quote)
      ) {
        setAccepted({ data: quote, request });
      }
      return response;
    } catch (requestError) {
      if (id === generation.current)
        setQuoteError(requestError as SwapQuoteError);
      throw requestError;
    }
  });

  const resetQuote = useMemoizedFn(() => {
    generation.current++;
    setAccepted(undefined);
    reset();
    setQuoteError(undefined);
  });

  return [
    postQuote,
    {
      data: accepted?.data,
      request: accepted?.request,
      error: quoteError,
      reset: resetQuote,
      isMutating,
    },
  ] as const;
};
