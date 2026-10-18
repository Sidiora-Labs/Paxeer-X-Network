import React, { useState } from "react";
import {
  Flex,
  PopoverRoot,
  PopoverTrigger,
  PopoverContent,
  Text,
  SpotIcon,
  PerpsIcon,
  SelectedChoicesFillIcon,
  PopupUnionIcon,
  CloseRoundFillIcon,
} from "@paxeer-network/ui";
import { cn } from "@paxeer-network/ui";

type TradingMenuChoiceProps = {
  icon: React.ReactNode;
  label: string;
  selected?: boolean;
  onClick?: () => void;
  className?: string;
};

const TradingMenuChoice: React.FC<TradingMenuChoiceProps> = (props) => {
  const { icon, label, selected, onClick, className } = props;
  return (
    <Flex
      itemAlign="center"
      className={cn(
        "oui-w-full oui-px-2 oui-py-3",
        "oui-rounded-md",
        "oui-cursor-pointer",
        className,
      )}
      onClick={onClick}
    >
      <Flex gap={2} itemAlign="center">
        {icon}
        <Text size="sm" intensity={80} weight="semibold">
          {label}
        </Text>
      </Flex>
      <Flex className="oui-ms-auto">
        {selected ? <SelectedChoicesFillIcon size={14} /> : null}
      </Flex>
    </Flex>
  );
};

export type BottomNavTradingMenuProps = {
  active: boolean;
  activeIcon: React.ReactNode;
  inactiveIcon: React.ReactNode;
  label: string;
  onNavigate: () => void;
};

export const BottomNavTradingMenu: React.FC<BottomNavTradingMenuProps> = (
  props,
) => {
  const { active, activeIcon, inactiveIcon, onNavigate, label } = props;
  const [open, setOpen] = useState(false);

  const onClickTrigger = () => {
    if (!active) {
      onNavigate();
      return;
    }
    setOpen((v) => !v);
  };

  return (
    <PopoverRoot open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        {!open ? (
          <>
            <Flex
              width={48}
              height={48}
              r="full"
              itemAlign={"center"}
              justify={"center"}
              className="oui-bg-base-7 oui--mt-6"
              onClick={onClickTrigger}
            >
              {active ? activeIcon : inactiveIcon}
            </Flex>
            <Text
              size="2xs"
              intensity={active ? 98 : 36}
              className="oui-text-center"
            >
              {label}
            </Text>
          </>
        ) : (
          <Flex
            width={48}
            height={48}
            r="full"
            itemAlign={"center"}
            justify={"center"}
            className="oui-relative oui--mt-12"
            onClick={(e) => {
              e.stopPropagation?.();
              setOpen(false);
            }}
          >
            <PopupUnionIcon
              size={78}
              className="oui-text-base-8 oui-absolute oui-start-1/2 oui-top-1/2 -oui-translate-x-1/2 -oui-translate-y-1/2"
            />
            <Flex
              className="oui-absolute oui-start-1/2 oui-top-1/2 -oui-translate-x-1/2 -oui-translate-y-1/2"
              itemAlign="center"
              justify="center"
            >
              <CloseRoundFillIcon size={28} />
            </Flex>
          </Flex>
        )}
      </PopoverTrigger>
      <PopoverContent
        sideOffset={12}
        align="center"
        side="top"
        className="!oui-w-[180px] !oui-bg-base-8 !oui-p-1 oui-border-none oui--mb-[22px]"
      >
        <Flex direction="column" className="oui-gap-1 oui-w-full">
          <TradingMenuChoice
            icon={<SpotIcon size={20} />}
            label="Spot"
            onClick={() => {
              window.location.href = "https://woofi.com/swap";
              setOpen(false);
            }}
            className=""
          />
          <TradingMenuChoice
            icon={<PerpsIcon size={20} />}
            label="Perps"
            selected={true}
            onClick={() => setOpen(false)}
            className="oui-bg-base-6"
          />
        </Flex>
      </PopoverContent>
    </PopoverRoot>
  );
};
