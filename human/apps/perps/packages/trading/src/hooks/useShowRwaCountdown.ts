import { useEffect, useMemo, useState } from "react";
import { useGetRwaSymbolCloseTimeInterval } from "@paxeer-network/hooks";

export const useShowRwaCountdown = (symbol: string) => {
  const [showCountdown, setShowCountdown] = useState<boolean | undefined>(
    undefined,
  );

  const [manulClose, setManulClose] = useState<boolean>(false);

  const { isRwa, open, closeTimeInterval } =
    useGetRwaSymbolCloseTimeInterval(symbol);
  useEffect(() => {
    if (manulClose) {
      return;
    }
    setShowCountdown(isRwa && open && !!closeTimeInterval);
  }, [isRwa, open, closeTimeInterval, manulClose]);

  useEffect(() => {
    return () => {
      setShowCountdown(undefined);
      setManulClose(false);
    };
  }, [symbol]);

  const closeCountdown = () => {
    setShowCountdown(false);
    setManulClose(true);
  };

  return useMemo(() => {
    return {
      showCountdown,
      closeCountdown,
    };
  }, [showCountdown, closeCountdown]);
};
