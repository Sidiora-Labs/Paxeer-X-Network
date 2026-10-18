import { useTranslation } from "@paxeer-network/i18n";
import { OrderType } from "@paxeer-network/types";
import { Select, Text } from "@paxeer-network/ui";

type Props = {
  type: OrderType;
  onChange: (type: OrderType) => void;
  disabled?: boolean;
};

export const OrderPriceType = (props: Props) => {
  const { t } = useTranslation();
  const options = [
    { label: t("orderEntry.orderType.limitOrder"), value: OrderType.LIMIT },
    { label: t("orderEntry.orderType.marketOrder"), value: OrderType.MARKET },
  ];
  return (
    <Select.options
      value={props.type}
      options={options}
      disabled={props.disabled}
      onValueChange={props.onChange}
      size={"xs"}
      classNames={{
        trigger:
          "oui-bg-transparent  oui-w-auto oui-outline-line-1 oui-input-root oui-bg-base-6 oui-h-10 lg:oui-h-8 oui-outline-line-12 ",
      }}
      valueFormatter={(value, option) => {
        const displayLabel = {
          [OrderType.LIMIT]: t("orderEntry.orderType.limit"),
          [OrderType.MARKET]: t("common.marketPrice"),
        }[value];
        return <Text size={"2xs"}>{displayLabel}</Text>;
      }}
    />
  );
};
