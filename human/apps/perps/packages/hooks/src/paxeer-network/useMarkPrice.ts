import { useMarkPriceBySymbol } from "./useMarkPrice/useMarkPriceStore";

/**
 * Mark price from the in-memory store for `symbol`.
 *
 * @remarks
 * `data` reflects the store lookup and may be transiently unset before streams write in.
 * In TS strict JSX, avoid rendering `{data}` as a direct text child without narrowing
 * (`typeof data === "number"`) or formatting through a helper that supplies a fallback.
 */
export const useMarkPrice = (symbol: string) => {
  // const ws = useWS();
  // const [price, setPrice] = useState(0);

  // const symbolRef = useRef<string>(symbol);
  // symbolRef.current = symbol;

  // useEffect(() => {
  //   const unsubscribe = ws.subscribe(`${symbol}@markprice`, {
  //     onMessage: (message: any) => {
  //       // when current symbol is not the same as the ws symbol, skip update data and auto unsubscribe old symbol ws
  //       if (message.symbol !== symbolRef.current) {
  //         unsubscribe?.();
  //         return;
  //       }
  //       setPrice(message.price);
  //     },
  //   });

  //   return () => {
  //     unsubscribe?.();
  //   };
  // }, [symbol]);
  const price = useMarkPriceBySymbol(symbol);

  return { data: price };
};
