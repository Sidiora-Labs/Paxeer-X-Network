import { CSSProperties, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { BBOOrderType } from "@paxeer-network/types";
import { Box, cn, Select, Text } from "@paxeer-network/ui";

export const BBOOrderTypeSelect = (props: {
  value?: BBOOrderType;
  onChange: (value: BBOOrderType) => void;
  contentStyle?: CSSProperties;
}) => {
  const { t } = useTranslation();

  const options = useMemo(
    () => [
      {
        label: t("orderEntry.bbo.counterparty1"),
        value: BBOOrderType.COUNTERPARTY1,
      },
      {
        label: t("orderEntry.bbo.counterparty5"),
        value: BBOOrderType.COUNTERPARTY5,
      },
      {
        label: t("orderEntry.bbo.queue1"),
        value: BBOOrderType.QUEUE1,
      },
      {
        label: t("orderEntry.bbo.queue5"),
        value: BBOOrderType.QUEUE5,
      },
    ],
    [t],
  );

  return (
    <Select.options
      testid="oui-testid-orderEntry-bbo-orderType-button"
      currentValue={props.value}
      value={props.value}
      options={options}
      onValueChange={props.onChange}
      contentProps={{
        className: cn(
          "oui-orderEntry-bboOrderTypeSelect-content",
          "oui-bg-base-8 oui-w-full",
        ),
        style: props.contentStyle,
      }}
      size={"sm"}
      classNames={{
        trigger: cn(
          "oui-orderEntry-bboOrderTypeSelect-btn",
          "oui-border-none oui-bg-transparent",
        ),
      }}
      valueFormatter={(value, option) => {
        const item = options.find((item) => item.value === value);

        return (
          <Box>
            <Text size="sm">{item?.label}</Text>
          </Box>
        );
      }}
    />
  );
};
