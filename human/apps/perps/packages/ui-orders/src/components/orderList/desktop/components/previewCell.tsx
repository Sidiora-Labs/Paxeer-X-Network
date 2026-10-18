import { memo } from "react";
import { cn, Flex, Text } from "@paxeer-network/ui";
import { commifyOptional } from "@paxeer-network/utils";
import { isGrayCell } from "../../../../utils/util";

type PreviewCellProps = {
  status?: string;
  value: string;
  setEditing: (isEditing: boolean) => void;
  disabled?: boolean;
  suffix?: string;
  className?: string;
};

export const PreviewCell = memo((props: PreviewCellProps) => {
  const { status, value, disabled } = props;

  return (
    <div
      className={cn(
        "oui-flex oui-items-center oui-justify-start",
        "oui-relative oui-max-w-[110px] oui-gap-1 oui-font-semibold",
        isGrayCell(status) && "oui-text-base-contrast-20",
        props.className,
      )}
      onClick={(e) => {
        if (!disabled) {
          e.stopPropagation();
          e.preventDefault();
          props.setEditing(true);
        }
      }}
    >
      <Flex
        r="base"
        className={cn(
          "oui-h-[28px] oui-min-w-[70px]",
          !disabled && "oui-border oui-border-line-12 oui-bg-base-7 oui-px-2",
        )}
      >
        <Text size="2xs">
          {commifyOptional(value)}
          {props.suffix}
        </Text>
      </Flex>
    </div>
  );
});
