import { FC } from "react";
import { Trans, useTranslation } from "@paxeer-network/i18n";
import { Box, cn, Flex, Text } from "@paxeer-network/ui";
import { TooltipIcon } from "../icons/tooltipIcon";

export interface BaseReminderProps {
  /** APY value in percentage (e.g., 8.5 for 8.5%) */
  apy: number | null;
  /** Whether the APY data is currently loading */
  loading: boolean;
  /** External URL to the asset issuer's website */
  externalUrl: string | null;
  /** Additional CSS class name */
  className?: string;
  /** Theme configuration */
  // theme: ReminderTheme;
  /** Test ID for the component */
  testId?: string;
}

/**
 * Base Yield Reminder Component
 * Generic reminder component that renders based on provided theme configuration
 * Used by YUSD and deUSD specific components
 */
export const BaseReminder: FC<BaseReminderProps> = ({
  apy,
  loading,
  externalUrl,
  className,
  testId,
}) => {
  const { t } = useTranslation();
  const formattedAPY = apy !== null ? apy.toFixed(1) : null;

  return (
    <Box
      className={cn(
        "oui-border-[0.5px] oui-border-primary oui-bg-primary/[0.12]",
        className,
      )}
      px={3}
      py={2}
      r="lg"
      width="100%"
    >
      <Flex direction="column" itemAlign="start">
        {formattedAPY !== null ? (
          <Text size="sm" intensity={98}>
            <Trans
              i18nKey="transfer.deposit.yieldReminder.earnAPY"
              values={{ apy: formattedAPY }}
              components={[<span className="oui-text-success" key="0" />]}
            />
          </Text>
        ) : (
          <Text size="sm" intensity={98}>
            {t("transfer.deposit.yieldReminder.earnRewards")}
          </Text>
        )}
        <Text size="2xs" intensity={54} className="oui-font-normal">
          {t("transfer.deposit.yieldReminder.distribution")}
        </Text>
      </Flex>

      <Flex justify="between" itemAlign="center" mt={3}>
        {/* Disclaimer - only show when APY is available */}
        {formattedAPY !== null && (
          <Flex gap={1}>
            <TooltipIcon />
            <Text size="2xs" intensity={36} className="oui-font-normal">
              {t("transfer.deposit.yieldReminder.disclaimer")}
            </Text>
          </Flex>
        )}

        {externalUrl && (
          <a
            href={externalUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="oui-inline-flex oui-items-center oui-gap-[2px]  oui-font-normal oui-no-underline oui-transition-opacity hover:oui-opacity-80"
          >
            {t("common.details")}
            <svg
              width="14"
              height="14"
              viewBox="0 0 14 14"
              fill="none"
              className="oui-text-base-contrast-54"
              xmlns="http://www.w3.org/2000/svg"
            >
              <path
                d="M3.507 6.996c0-.322.26-.583.583-.583h4.407l-1.73-1.748.82-.82 2.749 2.732a.6.6 0 0 1 .17.42.6.6 0 0 1-.17.418l-2.75 2.731-.82-.82 1.73-1.747H4.09a.583.583 0 0 1-.583-.583"
                fill="currentColor"
              />
            </svg>
          </a>
        )}
      </Flex>
    </Box>
  );
};
