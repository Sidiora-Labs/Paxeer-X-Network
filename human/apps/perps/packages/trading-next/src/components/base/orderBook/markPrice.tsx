import { FC, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  Tooltip,
  cn,
  Flex,
  Text,
  SimpleDialog,
  useScreen,
} from "@paxeer-network/ui";

/**
 * default style is desktop effect
 */
export const MarkPriceView: FC<{
  markPrice: number;
  quote_dp: number;
  className?: string;
  iconSize?: number;
}> = (props) => {
  const { isMobile } = useScreen();

  return isMobile ? <MobileLayout {...props} /> : <DesktopLayout {...props} />;
};

const DesktopLayout: FC<{
  markPrice: number;
  quote_dp: number;
  className?: string;
  iconSize?: number;
}> = (props) => {
  const { quote_dp, className, iconSize = 18 } = props;
  const { t } = useTranslation();
  return (
    <Tooltip
      content={t("trading.orderBook.markPrice.tooltip")}
      className="oui-max-w-[270px]"
    >
      <Flex
        gap={1}
        className={cn("oui-cursor-pointer oui-text-2xs", className)}
      >
        <FlagIcon size={iconSize} />
        <Text.numeral
          dp={quote_dp}
          color="warning"
          className="oui-underline oui-text-base oui-decoration-dashed oui-decoration-1 oui-underline-offset-4 oui-decoration-warning-darken"
        >
          {props.markPrice}
        </Text.numeral>
      </Flex>
    </Tooltip>
  );
};

const MobileLayout: FC<{
  markPrice: number;
  quote_dp: number;
  className?: string;
  iconSize?: number;
}> = (props) => {
  const { quote_dp, className, iconSize = 18 } = props;
  const [open, setOpen] = useState(false);
  const { t } = useTranslation();
  return (
    <>
      <Flex
        gap={1}
        className={cn("oui-cursor-pointer oui-text-2xs", className)}
        onClick={() => {
          setOpen(true);
        }}
      >
        <FlagIcon size={iconSize} />
        <Text.numeral
          dp={quote_dp}
          color="warning"
          className="oui-underline oui-decoration-dashed oui-decoration-1 oui-underline-offset-4 oui-decoration-warning-darken"
        >
          {props.markPrice}
        </Text.numeral>
      </Flex>
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
        <Text>{t("trading.orderBook.markPrice.tooltip")}</Text>
      </SimpleDialog>
    </>
  );
};

const FlagIcon = (props: { size: number }) => {
  return (
    <svg
      width={props.size}
      height={props.size}
      viewBox="0 0 18 18"
      fill="currenColor"
      xmlns="http://www.w3.org/2000/svg"
      className="oui-fill-warning-darken"
    >
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        d="M3.75 1.5a.75.75 0 0 1 .75.75h6a.75.75 0 0 1 .75.75v1.5h3a.75.75 0 0 1 .75.75V12a.75.75 0 0 1-.75.75H7.5a.75.75 0 0 1-.75-.75v-1.5H4.5v5.25a.75.75 0 0 1-1.5 0V2.25a.75.75 0 0 1 .75-.75M4.5 9h5.25V3.75H4.5zm6.75-3v3.75a.75.75 0 0 1-.75.75H8.25v.75h5.25V6z"
        // fill="#FF7D00"
      />
    </svg>
  );
};
