import { FC } from "react";
import { BaseReminder } from "./baseReminder";

export interface DeUsdReminderProps {
  /** APY value in percentage (e.g., 8.5 for 8.5%) */
  apy: number | null;
  /** Whether the APY data is currently loading */
  loading: boolean;
  /** External URL to the asset issuer's website */
  externalUrl: string | null;
  /** Additional CSS class name */
  className?: string;
}

/**
 * DeUSD Yield Reminder Component
 * Light theme with dark text and button style
 * Strictly follows Figma design: https://www.figma.com/design/CdydOeLyOKPJUNw96Wnwzd/WOOFi---Portfolio?node-id=8417-14708&m=dev
 */
export const DeUsdReminder: FC<DeUsdReminderProps> = (props) => {
  return <BaseReminder {...props} testId="deusd-yield-reminder" />;
};
