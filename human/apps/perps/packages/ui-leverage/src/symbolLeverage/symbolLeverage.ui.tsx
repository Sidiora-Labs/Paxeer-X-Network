import { useTranslation, Trans } from "@paxeer-network/i18n";
import { MarginMode } from "@paxeer-network/types";
import { TokenIcon, Text, Badge, Divider, Flex, cn } from "@paxeer-network/ui";
import { LeverageHeader } from "../index";
import {
  LeverageInput,
  LeverageSelector,
  LeverageSlider,
  LeverageFooter,
} from "../leverage.ui";
import { SymbolBadge } from "../symbolBadge";
import { SymbolLeverageScriptReturns } from "./symbolLeverage.script";

export const SymbolLeverage = (props: SymbolLeverageScriptReturns) => {
  const { t } = useTranslation();

  return (
    <div className="oui-flex oui-flex-col oui-gap-3 lg:oui-gap-4">
      <div className="oui-flex oui-items-center oui-gap-2">
        <TokenIcon symbol={props.symbol} className="oui-size-5" />
        <Text.formatted
          rule="symbol"
          formatString="base"
          size={props.isMobile ? "xs" : "base"}
          weight="semibold"
          intensity={98}
          suffix={<SymbolBadge symbol={props.symbol} />}
        >
          {props.symbol}
        </Text.formatted>
        <div
          className={cn(["oui-ms-auto oui-flex oui-items-center oui-gap-1"])}
        >
          <Badge color={props.isBuy ? "success" : "danger"} size="xs">
            {props.isBuy ? t("common.long") : t("common.short")}
          </Badge>
          <LeverageBadge
            leverage={props.currentLeverage}
            marginMode={props.marginMode}
          />
        </div>
      </div>
      <Divider />
      <Flex itemAlign={"start"} direction={"column"} mb={0}>
        <LeverageHeader currentLeverage={props.currentLeverage} />
        <LeverageInput {...props} />
        <LeverageSelector {...props} />
        <LeverageSlider {...props} />
        <Divider className="oui-mb-3 oui-w-full" />
        <div className="oui-flex oui-flex-col oui-gap-1 oui-pb-4 oui-text-xs oui-font-normal oui-text-base-contrast-54">
          {/* <div>
            <Trans
              i18nKey="leverage.maxPositionLeverage.tips"
              values={{ amount: props.maxPositionNotional }}
              components={[
                // @ts-ignore
                <Text.numeral
                  as="span"
                  key="0"
                  className="oui-text-base-contrast"
                  dp={0}
                />,
              ]}
            />
          </div> */}
          <div>
            <Trans
              i18nKey="leverage.maxAvailableLeverage.tips"
              values={{ leverage: props.maxPositionLeverage }}
              components={[
                // @ts-ignore
                <Text.numeral
                  dp={0}
                  suffix="x"
                  as="span"
                  key="0"
                  className="oui-text-base-contrast"
                />,
              ]}
            />
          </div>
          <div>{t("leverage.actualPositionLeverage.tips")}</div>
        </div>
        <div
          className={cn([
            "-oui-mb-2",
            props.overRequiredMargin || props.overMaxPositionLeverage
              ? "oui-block oui-text-xs oui-font-normal"
              : "oui-hidden",
          ])}
        >
          {props.overRequiredMargin && (
            <div>
              <Text color="warning">
                {t("leverage.overRequiredMargin.tips")}
              </Text>
            </div>
          )}
          {props.overMaxPositionLeverage && (
            <div>
              <Text color="warning">
                <Trans
                  i18nKey="leverage.overMaxPositionLeverage.tips"
                  values={{ leverage: props.maxPositionLeverage }}
                  components={[
                    // @ts-ignore
                    <Text.numeral dp={0} suffix="X" as="span" key="0" />,
                  ]}
                />
              </Text>
            </div>
          )}
        </div>
        <LeverageFooter {...props} />
      </Flex>
    </div>
  );
};

const LeverageBadge = ({
  leverage,
  marginMode,
}: {
  leverage: number;
  marginMode: MarginMode;
}) => {
  const { t } = useTranslation();

  return (
    <div
      className={cn(
        "oui-flex oui-h-[18px] oui-items-center oui-gap-1",
        "oui-cursor-pointer oui-rounded oui-bg-line-6 oui-px-2",
        "oui-text-2xs oui-font-semibold oui-text-base-contrast-36",
      )}
    >
      <Text>
        {marginMode === undefined
          ? "--"
          : marginMode === MarginMode.ISOLATED
            ? t("marginMode.isolated")
            : t("marginMode.cross")}
      </Text>
      {leverage === undefined ? (
        <Text size="2xs">--</Text>
      ) : (
        <Text.numeral dp={0} size="2xs" unit="X">
          {leverage}
        </Text.numeral>
      )}
    </div>
  );
};
