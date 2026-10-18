import { useLocalStorage } from "@paxeer-network/hooks";

export const useTradingLocalStorage = (props?: {
  pnlNotionalDecimalPrecision?: number;
}) => {
  const [unPnlPriceBasis, setUnPnlPriceBasic] = useLocalStorage(
    "unPnlPriceBasis",
    "markPrice",
  );
  const [pnlNotionalDecimalPrecision, setPnlNotionalDecimalPrecision] =
    useLocalStorage(
      "pnlNotionalDecimalPrecision",
      props?.pnlNotionalDecimalPrecision ?? 2,
    );
  const [showAllSymbol, setShowAllSymbol] = useLocalStorage(
    "showAllSymbol",
    true,
  );

  const [hideAssets, setHideAssets] = useLocalStorage("hideAssets", false);

  return {
    unPnlPriceBasis,
    setUnPnlPriceBasic,
    pnlNotionalDecimalPrecision,
    setPnlNotionalDecimalPrecision,
    showAllSymbol,
    setShowAllSymbol,
    hideAssets,
    setHideAssets,
  };
};
