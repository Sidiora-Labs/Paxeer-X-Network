import { useConvertFormScript } from "../convertForm/convertForm.script";

/**
 * Reverse convert reuses the forward convert session and pending recovery.
 * An account can have only one pending convert at a time, whether it is
 * collateral → USDC or USDC → USDG.
 */
export const useReverseConvertScript = () =>
  useConvertFormScript({ reverse: true });
