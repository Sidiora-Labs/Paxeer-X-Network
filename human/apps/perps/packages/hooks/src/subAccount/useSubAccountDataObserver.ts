import { useEffect, useState } from "react";
import { mutate } from "swr";
import { API, WSMessage } from "@paxeer-network/types";
import { useAppStore } from "../paxeer-network/appStore";
import { useFundingRatesStore } from "../paxeer-network/useFundingRates";
import { useIndexPricesStream } from "../paxeer-network/useIndexPricesStream";
import { useMarkPricesStream } from "../paxeer-network/useMarkPricesStream";
import { POSITION_EMPTY } from "../paxeer-network/usePositionStream/usePosition.store";
import { useSymbolsInfo } from "../paxeer-network/useSymbolsInfo";
import { mergeHoldingBalance } from "../utils/mergeHoldingBalance";
import { object2underscore } from "../utils/ws";
import { formatPortfolio, Portfolio } from "./calculator/portfolio";
import { calcByPrice, formatPositions } from "./calculator/positions";
import { useSubAccountQuery } from "./useSubAccountQuery";
import { useSubAccountWS } from "./useSubAccountWS";

export const useSubAccountDataObserver = (accountId?: string) => {
  const ws = useSubAccountWS({ accountId });

  const { data: markPrices } = useMarkPricesStream();
  const { data: indexPrices } = useIndexPricesStream();
  const symbolsInfo = useSymbolsInfo();
  const fundingRates = useFundingRatesStore();
  const tokensInfo = useAppStore((state) => state.tokensInfo);

  const [holding, setHolding] = useState<API.Holding[]>([]);
  const [positions, setPositions] = useState(
    POSITION_EMPTY as API.PositionsTPSLExt,
  );
  const [portfolio, setPortfolio] = useState<Portfolio>();

  // need to get sub account info to calculate portfolio and positions
  const { data: accountInfo } = useSubAccountQuery<API.AccountInfo>(
    "/v1/client/info",
    { accountId, revalidateOnFocus: false },
  );

  const { data: positionsInfo } = useSubAccountQuery<API.PositionInfo>(
    "/v1/positions",
    {
      accountId,
      formatter: (data) => data,
    },
  );

  const { data: holdingRes } = useSubAccountQuery<API.Holding[]>(
    "/v1/client/holding",
    {
      accountId,
      formatter: (data) => data.holding,
    },
  );

  useEffect(() => {
    const portfolio = formatPortfolio({
      holding,
      positions,
      markPrices,
      indexPrices,
      accountInfo,
      symbolsInfo,
      tokensInfo,
    });
    setPortfolio(portfolio!);
  }, [
    holding,
    positions,
    markPrices,
    indexPrices,
    accountInfo,
    symbolsInfo,
    tokensInfo,
  ]);

  useEffect(() => {
    if (!positionsInfo) return;

    if (positionsInfo.rows?.length === 0) {
      setPositions(positionsInfo as API.PositionsTPSLExt);
      return;
    }
    const _positionsInfo = calcByPrice(positionsInfo!, markPrices, indexPrices);

    const _positions = formatPositions(
      _positionsInfo!,
      accountInfo,
      symbolsInfo,
      fundingRates,
    );

    setPositions({
      ..._positions,
      rows: _positions.rows.filter(
        (item) =>
          item.position_qty !== 0 ||
          item.pending_long_qty !== 0 ||
          item.pending_short_qty !== 0,
      ),
    });
  }, [
    positionsInfo,
    accountInfo,
    symbolsInfo,
    fundingRates,
    markPrices,
    indexPrices,
  ]);

  useEffect(() => {
    if (holdingRes) {
      setHolding(holdingRes);
    }
  }, [holdingRes]);

  useEffect(() => {
    if (!accountId) return;

    const unsubscribe = ws.privateSubscribe(
      {
        id: "balance",
        event: "subscribe",
        topic: "balance",
        ts: Date.now(),
      },
      {
        onMessage: (data: any) => {
          const holding = data?.balances ?? ({} as Record<string, any>);

          setHolding((prev) => {
            return prev.map((item) => {
              if (holding[item.token]) {
                return mergeHoldingBalance(item, holding[item.token]);
              }
              return item;
            });
          });
        },
      },
    );

    return () => unsubscribe?.();
  }, [accountId]);

  useEffect(() => {
    if (!accountId) return;
    const key = ["/v1/positions", accountId];
    const unsubscribe = ws.privateSubscribe("position", {
      onMessage: (data: { positions: WSMessage.Position[] }) => {
        const { positions: nextPositions } = data;

        mutate(
          key,
          (prevPositions: any) => {
            if (!!prevPositions) {
              const newPositions = {
                ...prevPositions,
                rows: prevPositions.rows.map((row: any) => {
                  const itemIndex = nextPositions.findIndex(
                    (item) => item.symbol === row.symbol,
                  );

                  if (itemIndex >= 0) {
                    const itemArr = nextPositions.splice(itemIndex, 1);

                    const item = itemArr[0];

                    // ignore the ws update with averageOpenPrice === 0
                    if (item.averageOpenPrice === 0 && item.positionQty !== 0) {
                      return row;
                    }
                    // console.log("---->>>>>>!!!! item", item);

                    return object2underscore(item);
                  }
                  return row;
                }),
              };

              if (nextPositions.length > 0) {
                newPositions.rows = [
                  ...newPositions.rows,
                  ...nextPositions.map((item) => {
                    return object2underscore(item);
                  }),
                ];
              }

              return newPositions;
            }
          },
          {
            revalidate: false,
          },
        );
      },
    });
    return () => unsubscribe?.();
  }, [accountId]);

  return { portfolio, positions };
};
