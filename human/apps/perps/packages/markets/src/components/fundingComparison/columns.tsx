import { useCallback, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { cn, Flex, TokenIcon, useScreen, Badge } from "@paxeer-network/ui";
import type { Column } from "@paxeer-network/ui";
import { Text } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { PaxeerNetworkIcon } from "../../icons";
import { SymbolDisplay } from "../symbolDisplay";
import { useEXchanges } from "./useEXchanges";

const CDN_PREFIX = "/assets/static/exchange_logo";

export const useFundingColumns = () => {
  const { t } = useTranslation();
  const { isMobile } = useScreen();
  const { exchanges, brokerName, brokerIconSrc } = useEXchanges();
  const getImgSrc = useCallback(
    (val: string) => {
      if (val === brokerName) {
        return brokerIconSrc;
      }
      return `${CDN_PREFIX}/${val.toLowerCase().replace(" ", "_")}.png`;
    },
    [brokerName, brokerIconSrc],
  );
  return useMemo<Column[]>(() => {
    return [
      {
        title: t("markets.column.market"),
        dataIndex: "symbol",
        width: 220,
        onSort: true,
        className: isMobile ? "oui-ps-0" : undefined,
        render: (value, record) => {
          if (isMobile) {
            return (
              <Flex direction="column" itemAlign="start" gapY={1}>
                <Flex gapX={1} itemAlign="center">
                  <TokenIcon symbol={value} className="oui-size-[18px]" />
                  <SymbolDisplay formatString="base" size="2xs">
                    {value}
                  </SymbolDisplay>
                </Flex>
                {typeof record.leverage === "number" && (
                  <Badge size="xs" color="primary">
                    {record.leverage}x
                  </Badge>
                )}
              </Flex>
            );
          }

          return (
            <Flex gapX={1} itemAlign="center">
              <TokenIcon symbol={value} className="oui-size-5" />
              <SymbolDisplay formatString="base" size="xs">
                {value}
              </SymbolDisplay>
              {typeof record.leverage === "number" && (
                <Badge size="xs" color="primary">
                  {record.leverage}x
                </Badge>
              )}
            </Flex>
          );
        },
      },
      {
        title: (
          <Flex gapX={1}>
            <PaxeerNetworkIcon /> {t("markets.openInterest")}
          </Flex>
        ),
        dataIndex: "openInterest",
        width: 120,
        // align: "right",
        onSort: true,
        render: (value) => {
          if (value === "-") {
            return "-";
          }
          return (
            <Text.numeral currency="$" dp={0} rm={Decimal.ROUND_DOWN}>
              {value}
            </Text.numeral>
          );
        },
      },
      ...exchanges.map<Column>((item) => {
        const imgSrc = getImgSrc(item);
        return {
          title: (
            <Flex justify="start" itemAlign="center" gap={1}>
              {!item.includes(" - ") && imgSrc && (
                <img
                  src={imgSrc}
                  className={cn(
                    "oui-size-6 oui-rounded-full oui-object-cover",
                    isMobile ? "oui-size-[18px]" : "oui-size-6",
                  )}
                />
              )}
              {item}
            </Flex>
          ),
          dataIndex: item,
          onSort: true,
          width: item.includes(" - ") ? 160 : 130,
          render(value: number | null) {
            if (value === null) {
              return "-";
            }
            return (
              <Text.numeral
                rule="percentages"
                dp={5}
                coloring={item.includes(" - ") || item === brokerName}
                rm={Decimal.ROUND_DOWN}
                showIdentifier
              >
                {value}
              </Text.numeral>
            );
          },
        };
      }),
    ];
  }, [t, isMobile, exchanges, brokerName, getImgSrc]);
};
