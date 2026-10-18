import { type API } from "@paxeer-network/types";
import { useAppStore } from "./appStore";

/**
 * The return type of useCollateral hook, containing account collateral information
 */
export type CollateralOutputs = {
  /**
   * Total collateral value in the account
   *
   * This includes all assets that can be used as margin
   */
  totalCollateral: number;
  /**
   * Available collateral that can be used for new positions
   *
   * Calculated as: totalCollateral - margin requirements
   */
  freeCollateral: number;
  /**
   * Legacy free collateral metric backed only by USDC
   *
   * Isolated-margin trading limits use `freeCollateral` instead. This field is
   * retained for backward compatibility.
   */
  freeCollateralUSDCOnly: number;
  /**
   * Total portfolio value including all positions and collateral
   *
   * Can be null if data is not available
   */
  totalValue: number | null;
  /**
   * Current available balance that can be withdrawn
   *
   * Excludes locked collateral and pending settlements
   */
  availableBalance: number;
  /**
   * Unrealized profit and loss across all open positions
   *
   * Positive value indicates profit, negative indicates loss
   */
  unsettledPnL: number;
  /**
   * List of holdings in the account
   *
   * Each holding represents a specific token and its quantity
   */
  holding?: API.Holding[];
  /**
   * Detailed account information and settings
   *
   * Contains account configuration, limits and risk parameters
   */
  accountInfo?: API.AccountInfo;
  /**
   * USDC holding in the account
   */
  usdcHolding: number;
  // positions: API.Position[];
};

// const positionsPath = pathOr([], [0, "rows"]);
// const totalCollateralPath = pathOr(0, [0, "totalCollateral"]);

/**
 *  Hook to get and calculate collateral-related data for an account
 * @example
 * ```typescript
 * const {
 *  totalCollateral,
 *  freeCollateral,
 *  totalValue,
 *  availableBalance,
 *  unsettledPnL,
 *  accountInfo,
 * } = useCollateral({ dp: 4 });
 * ```
 */
export const useCollateral = (
  options: {
    /**
     * Decimal precision for numerical values (default: 6)
     * */
    dp: number;
  } = { dp: 6 },
): CollateralOutputs => {
  const { dp } = options;
  const {
    totalCollateral,
    totalValue,
    freeCollateral,
    freeCollateralUSDCOnly,
    availableBalance,
    unsettledPnL,
    holding,
    usdcHolding,
  } = useAppStore((state) => state.portfolio);
  const accountInfo = useAppStore((state) => state.accountInfo);
  return {
    totalCollateral: totalCollateral.toDecimalPlaces(dp).toNumber(),
    freeCollateral: freeCollateral.toDecimalPlaces(dp).toNumber(),
    freeCollateralUSDCOnly: freeCollateralUSDCOnly
      .toDecimalPlaces(dp)
      .toNumber(),
    totalValue: totalValue?.toDecimalPlaces(dp).toNumber() ?? null,
    availableBalance,
    unsettledPnL,
    accountInfo,
    holding,
    usdcHolding,
    // @hidden
    // positions: positionsPath(positions),
  };
};
