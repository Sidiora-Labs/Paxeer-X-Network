import { FC, memo } from "react";
import { cn, Flex, Text } from "@paxeer-network/ui";
import { useOrderEntryContext } from "../../orderEntryContext";

const percentages = [1, 2, 3, 5];

type CallbackRatePercentagesProps = {
  className?: string;
  callback_rate?: string;
  onClick?: (value: string) => void;
};

export const CallbackRatePercentages: FC<CallbackRatePercentagesProps> = memo(
  (props) => {
    const { setOrderValue } = useOrderEntryContext();

    return (
      <Flex gapX={2} className={props.className}>
        {percentages.map((item) => {
          const value = item.toString();
          const isActive = props.callback_rate === value;

          return (
            <Flex
              key={item}
              justify="center"
              itemAlign="center"
              width={46}
              height={18}
              className={cn(
                "oui-cursor-pointer oui-select-none",
                "oui-rounded oui-border",
                isActive ? "oui-border-base-contrast-36" : "oui-border-line-12",
              )}
              onClick={() => {
                setOrderValue("callback_rate", value);
                props.onClick?.(value);
              }}
            >
              <Text size="2xs" intensity={isActive ? 80 : 36} weight="semibold">
                {item}%
              </Text>
            </Flex>
          );
        })}
      </Flex>
    );
  },
);

CallbackRatePercentages.displayName = "CallbackRatePercentages";
