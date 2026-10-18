import { FC } from "react";
import { BaseReminder } from "./baseReminder";

export interface YusdReminderProps {
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
 * YUSD Yield Reminder Component
 * Dark theme with white text and coin icons
 * Strictly follows Figma design
 */
export const YusdReminder: FC<YusdReminderProps> = (props) => {
  return <BaseReminder {...props} testId="yusd-yield-reminder" />;
};
