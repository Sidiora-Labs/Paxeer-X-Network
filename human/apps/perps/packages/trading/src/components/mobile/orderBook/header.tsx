import React, { FC, useMemo } from "react";
import { useLocalStorage } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import {
  CaretDownIcon,
  CaretUpIcon,
  Flex,
  Picker,
  Text,
} from "@paxeer-network/ui";
import { SelectOption } from "@paxeer-network/ui/src/select/withOptions";
import { ORDERBOOK_MOBILE_COIN_TYPE_KEY } from "../../base/orderBook/orderContext";

interface Props {
  quote: string;
  base: string;
}

export const Header: FC<Props> = (props) => {
  const { t } = useTranslation();

  const { base, quote } = props;

  const [coinUnit, setCoinUnit] = useLocalStorage<"qty" | "base" | "quote">(
    ORDERBOOK_MOBILE_COIN_TYPE_KEY,
    "qty",
  );

  const options = useMemo<SelectOption[]>(() => {
    return [
      {
        value: "qty",
        label: `${t("common.quantity")}(${base})`,
        data: [t("common.quantity"), base],
      },
      {
        value: "base",
        label: `${t("common.total")}(${base})`,
        data: [t("common.total"), base],
      },
      {
        value: "quote",
        label: `${t("common.total")}(${quote})`,
        data: [t("common.total"), quote],
      },
    ];
  }, [t, base, quote]);

  return (
    <Flex
      justify={"between"}
      width={"100%"}
      className="oui-py-[5px] oui-text-2xs oui-text-base-contrast-36"
    >
      <Flex
        direction={"column"}
        itemAlign={"start"}
        id="oui-order-book-header-price"
        className="oui-orderBook-header-price"
      >
        <Text>{t("common.price")}</Text>
        <Text>{`(${quote})`}</Text>
      </Flex>
      <Picker
        size="sm"
        value={coinUnit}
        onValueChange={setCoinUnit}
        options={options}
        valueRenderer={(_, { open, data }) => {
          return (
            <Flex justify="between" itemAlign="center" gap={1}>
              {Array.isArray(data) && (
                <Flex direction={"column"} itemAlign={"end"}>
                  {data[0] && <Text>{data[0]}</Text>}
                  {data[1] && <Text>({data[1]})</Text>}
                </Flex>
              )}
              {open ? (
                <CaretUpIcon size={14} color="inherit" />
              ) : (
                <CaretDownIcon size={14} color="inherit" />
              )}
            </Flex>
          );
        }}
      />
    </Flex>
  );
};
