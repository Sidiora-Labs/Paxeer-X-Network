import { FC } from "react";
import { isYieldBearingAsset } from "../../constants/yieldBearingAssets";
import { useYieldAPY } from "../depositForm/hooks/useYieldAPY";
import { DeUsdReminder } from "./deUsdReminder";
import { YusdReminder } from "./yusdReminder";

export interface YieldBearingReminderProps {
  /** Token symbol to check and display APY for */
  symbol?: string;
  /** Additional CSS class name */
  className?: string;
}

/**
 * YieldBearingReminder Component
 * Router component that renders the appropriate reminder based on token type
 * - YUSD: Dark blue theme with white text and coin icons
 * - deUSD: Light pink/purple theme with dark text and deUSD logo icons
 */
export const YieldBearingReminder: FC<YieldBearingReminderProps> = ({
  symbol,
  className,
}) => {
  const { apy, loading, externalUrl } = useYieldAPY(symbol);

  // Don't render if not a yield-bearing asset
  if (!isYieldBearingAsset(symbol)) {
    return null;
  }

  // Determine which reminder to show based on symbol
  const isDeUsd = symbol?.toLowerCase() === "deusd";

  if (isDeUsd) {
    return (
      <DeUsdReminder
        apy={apy}
        loading={loading}
        externalUrl={externalUrl}
        className={className}
      />
    );
  }

  return (
    <YusdReminder
      apy={apy}
      loading={loading}
      externalUrl={externalUrl}
      className={className}
    />
  );
};
