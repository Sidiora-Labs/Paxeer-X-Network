import React from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { cn, Flex, Switch } from "@paxeer-network/ui";

export interface ReduceOnlySwitchProps {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  className?: string;
}

export const ReduceOnlySwitch: React.FC<ReduceOnlySwitchProps> = ({
  checked,
  onCheckedChange,
  className,
}) => {
  const { t } = useTranslation();

  return (
    <Flex
      itemAlign={"center"}
      gapX={1}
      className={cn("oui-orderEntry-reduceOnly", className)}
    >
      <Switch
        data-testid="oui-testid-orderEntry-reduceOnly-switch"
        className={cn("oui-reduceOnly-switch", "oui-h-[14px]")}
        id={"reduceOnly"}
        checked={checked}
        onCheckedChange={onCheckedChange}
      />
      <label
        htmlFor={"reduceOnly"}
        className={"oui-reduceOnly-label oui-text-xs"}
      >
        {t("orderEntry.reduceOnly")}
      </label>
    </Flex>
  );
};
