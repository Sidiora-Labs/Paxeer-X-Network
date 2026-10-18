import { FC, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  ArrowDownShortIcon,
  ArrowUpShortIcon,
  Box,
  cn,
  Flex,
  Text,
  Tooltip,
  SimpleDialog,
  useScreen,
} from "@paxeer-network/ui";

const UNDERLINE_CLASS =
  "oui-cursor-pointer oui-underline oui-decoration-dashed oui-decoration-1 oui-underline-offset-4 oui-decoration-base-contrast oui-inline-flex";

/**
 * default style is desktop effect
 */
export const MiddlePriceView: FC<{
  markPrice: number;
  lastPrice: number[];
  quote_dp: number;
  className?: string;
  iconSize?: number;
}> = (props) => {
  const { isMobile } = useScreen();

  return isMobile ? <MobileLayout {...props} /> : <DesktopLayout {...props} />;
};

const DesktopLayout: FC<{
  markPrice: number;
  lastPrice: number[];
  quote_dp: number;
  className?: string;
  iconSize?: number;
}> = (props) => {
  const { lastPrice, quote_dp, className, iconSize = 18 } = props;
  const { t } = useTranslation();

  const [prevLastPrice, middlePrice] = lastPrice;
  const down = middlePrice < prevLastPrice;
  const up = middlePrice > prevLastPrice;

  return (
    <Tooltip
      content={t("trading.orderBook.middlePrice.tooltip")}
      className="oui-max-w-[240px]"
    >
      <span className={UNDERLINE_CLASS}>
        <Flex
          gap={1}
          className={cn(
            up ? "oui-text-trade-profit" : down ? "oui-text-trade-loss" : "",
            className,
          )}
        >
          <Text.numeral dp={quote_dp} intensity={98}>
            {middlePrice}
          </Text.numeral>
          <Box width={19}>
            {down && (
              <ArrowDownShortIcon size={iconSize} color="danger" opacity={1} />
            )}
            {up && (
              <ArrowUpShortIcon size={iconSize} color="success" opacity={1} />
            )}
          </Box>
        </Flex>
      </span>
    </Tooltip>
  );
};

const MobileLayout: FC<{
  markPrice: number;
  lastPrice: number[];
  quote_dp: number;
  className?: string;
  iconSize?: number;
}> = (props) => {
  const { lastPrice, quote_dp, className, iconSize = 18 } = props;
  const [open, setOpen] = useState(false);
  const { t } = useTranslation();

  const [prevLastPrice, middlePrice] = lastPrice;
  const down = middlePrice < prevLastPrice;
  const up = middlePrice > prevLastPrice;

  const handleTriggerClick = () => setOpen(true);
  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      setOpen(true);
    }
  };

  return (
    <>
      <span
        role="button"
        tabIndex={0}
        className={UNDERLINE_CLASS}
        onClick={handleTriggerClick}
        onKeyDown={handleKeyDown}
        aria-label={t("trading.orderBook.middlePrice.tooltip")}
      >
        <Flex
          gap={1}
          className={cn(
            up ? "oui-text-trade-profit" : down ? "oui-text-trade-loss" : "",
            className,
          )}
        >
          <Text.numeral dp={quote_dp} intensity={98}>
            {middlePrice}
          </Text.numeral>
          <Box width={19}>
            {down && (
              <ArrowDownShortIcon size={iconSize} color="danger" opacity={1} />
            )}
            {up && (
              <ArrowUpShortIcon size={iconSize} color="success" opacity={1} />
            )}
          </Box>
        </Flex>
      </span>
      <SimpleDialog
        size="xs"
        open={open}
        onOpenChange={setOpen}
        title={t("common.tips")}
        actions={{
          primary: {
            label: t("common.ok"),
            onClick: () => setOpen(false),
          },
        }}
      >
        <Text>{t("trading.orderBook.middlePrice.tooltip")}</Text>
      </SimpleDialog>
    </>
  );
};
