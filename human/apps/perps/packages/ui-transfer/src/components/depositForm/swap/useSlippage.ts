import { useLocalStorage } from "@paxeer-network/hooks";

const PAXEER_NETWORK_SWAP_DEPOSIT_SLIPPAGE_KEY = "paxeerNetwork_swap_deposit_slippage";

export const useSlippage = () => {
  const [slippage, setSlippage] = useLocalStorage(
    PAXEER_NETWORK_SWAP_DEPOSIT_SLIPPAGE_KEY,
    1,
  );

  return { slippage, onSlippageChange: setSlippage };
};
