import { FC, memo, useMemo } from "react";
import { TrailingCallbackType } from "@paxeer-network/types";
import { Select, cn } from "@paxeer-network/ui";

type TrailingCallbackSelectProps = {
  quote: string;
  value: string;
  onValueChange: (value: TrailingCallbackType) => void;
};

export const TrailingCallbackSelect: FC<TrailingCallbackSelectProps> = memo(
  (props) => {
    const { quote } = props;

    const options = useMemo(() => {
      return [
        { label: quote, value: TrailingCallbackType.VALUE },
        { label: "%", value: TrailingCallbackType.RATE },
      ];
    }, [quote]);

    return (
      <Select.options
        variant="text"
        size="xs"
        options={options}
        classNames={{
          trigger: cn(
            "oui-orderEntry-trailingCallbackSelect-btn",
            "oui-absolute oui-end-0 oui-top-1",
            "oui-w-[124px] oui-justify-end",
            "oui-text-base-contrast-36",
          ),
        }}
        value={props.value}
        onValueChange={props.onValueChange}
        contentProps={{
          align: "end",
          alignOffset: -1,
          sideOffset: -4,
          className:
            "oui-orderEntry-trailingCallbackSelect-content oui-border oui-border-line-6",
        }}
      />
    );
  },
);

TrailingCallbackSelect.displayName = "trailingCallbackSelect";
