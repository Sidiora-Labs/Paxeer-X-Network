import { FC } from "react";
import { Trans, useTranslation } from "@paxeer-network/i18n";
import { MarginMode } from "@paxeer-network/types";
import {
  ChevronRightIcon,
  CloseIcon,
  Divider,
  Flex,
  IconButton,
  Text,
  TokenIcon,
  cn,
} from "@paxeer-network/ui";
import { SymbolBadge } from "../symbolBadge";
import type { MarginModeSwitchState } from "./marginModeSwitch.script";

export type MarginModeSwitchProps = Pick<
  MarginModeSwitchState,
  | "symbol"
  | "isMobile"
  | "currentMarginMode"
  | "selectedMarginMode"
  | "isPermissionlessListing"
  | "onSelect"
> & {
  close?: () => void;
  onOpenSettings?: () => void;
};

export const MarginModeSwitch: FC<MarginModeSwitchProps> = (props) => {
  const { t } = useTranslation();

  const handleSelect = (mode: MarginMode) => {
    props.onSelect(mode);
  };

  const titleClassName = props.isMobile
    ? "oui-text-lg oui-leading-[26px]"
    : "oui-text-base oui-leading-6";

  const headerPadding = props.isMobile ? "oui-pt-3" : "oui-px-5 oui-pt-3";
  const contentPadding = props.isMobile ? "oui-py-4" : "oui-p-5";

  return (
    <Flex
      direction="column"
      className={cn(
        "oui-w-full",
        props.isMobile
          ? "oui-rounded-t-xl oui-bg-base-8"
          : "oui-rounded-xl oui-bg-base-8",
      )}
      data-testid="oui-testid-marginModeSwitch"
    >
      <div className={cn("oui-w-full", headerPadding)}>
        <Flex
          itemAlign="center"
          justify="between"
          className={cn(props.isMobile && "oui-px-4")}
        >
          {props.isMobile ? (
            <button
              type="button"
              className="oui-size-[18px] oui-opacity-0"
              aria-hidden="true"
              tabIndex={-1}
            />
          ) : null}

          <Text
            className={cn(
              "oui-font-semibold oui-tracking-[0.03em]",
              titleClassName,
            )}
            intensity={98}
          >
            {t("marginMode.switchMarginMode")}
          </Text>

          <IconButton
            color="light"
            className="oui-size-[18px]"
            onClick={props.close}
            aria-label="Close"
            data-testid="oui-testid-marginModeSwitch-close"
          >
            <CloseIcon size={18} color="white" opacity={0.98} />
          </IconButton>
        </Flex>
        <Divider className="oui-mt-[9px] oui-w-full" />
      </div>

      <div
        className={cn(
          "oui-w-full",
          contentPadding,
          props.isMobile && "oui-px-4",
        )}
      >
        <Flex itemAlign="center" gap={2}>
          <TokenIcon symbol={props.symbol} className="oui-size-5" />
          <Text.formatted
            className="oui-tracking-[0.03em]"
            rule="symbol"
            formatString="base"
            size="base"
            weight="semibold"
            intensity={98}
            suffix={<SymbolBadge symbol={props.symbol} />}
          >
            {props.symbol}
          </Text.formatted>
        </Flex>

        <Flex direction="column" gap={3} className="oui-mt-3 oui-w-full">
          <OptionCard
            mode={MarginMode.CROSS}
            selected={props.selectedMarginMode === MarginMode.CROSS}
            isCurrent={props.currentMarginMode === MarginMode.CROSS}
            onClick={() => handleSelect(MarginMode.CROSS)}
            disabled={props.isPermissionlessListing}
          />
          <OptionCard
            mode={MarginMode.ISOLATED}
            selected={props.selectedMarginMode === MarginMode.ISOLATED}
            isCurrent={props.currentMarginMode === MarginMode.ISOLATED}
            onClick={() => handleSelect(MarginMode.ISOLATED)}
          />
        </Flex>

        <Flex justify="center" className="oui-mt-3 oui-w-full">
          <button
            type="button"
            className={cn(
              "oui-flex oui-items-center oui-gap-1",
              "oui-group",
              "oui-text-xs oui-leading-[15px] oui-font-semibold oui-text-base-contrast-54 oui-tracking-[0.03em]",
              props.onOpenSettings
                ? "oui-cursor-pointer hover:oui-text-base-contrast-80 oui-transition-colors"
                : "oui-cursor-default",
            )}
            onClick={props.onOpenSettings}
            disabled={!props.onOpenSettings}
            data-testid="oui-testid-marginModeSwitch-settings"
          >
            <span>{t("marginMode.marginModeSettings")}</span>
            <ChevronRightIcon
              size={18}
              color="white"
              opacity={1}
              className={cn(
                "oui-text-base-contrast-54 oui-transition-colors",
                props.onOpenSettings && "group-hover:oui-text-base-contrast-80",
              )}
            />
          </button>
        </Flex>

        {props.isMobile ? (
          <div className="oui-mt-4 oui-h-[34px] oui-w-full" />
        ) : null}
      </div>
    </Flex>
  );
};

const OptionCard: FC<{
  mode: MarginMode;
  selected: boolean;
  isCurrent: boolean;
  onClick: () => void;
  disabled?: boolean;
}> = (props) => {
  const { t } = useTranslation();

  const title =
    props.mode === MarginMode.CROSS
      ? t("marginMode.crossMargin")
      : t("marginMode.isolatedMargin");
  return (
    <div
      role="button"
      tabIndex={props.disabled ? -1 : 0}
      className={cn(
        "oui-relative oui-w-full oui-rounded-md oui-p-2",
        "oui-bg-base-6",
        "oui-text-start",
        props.disabled
          ? "oui-cursor-not-allowed oui-opacity-50 oui-border oui-border-transparent"
          : props.selected
            ? "oui-border oui-border-primary"
            : "oui-border oui-border-transparent hover:oui-border-line-12",
        !props.disabled && "oui-cursor-pointer",
      )}
      onClick={props.disabled ? undefined : props.onClick}
      onKeyDown={(e) => {
        if (props.disabled) return;
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          props.onClick();
        }
      }}
      aria-disabled={props.disabled}
      data-testid={`oui-testid-marginModeSwitch-option-${props.mode}`}
    >
      <Flex direction="column" gap={2} itemAlign="start" className="oui-w-full">
        <Text
          className="oui-text-sm oui-font-semibold oui-leading-5 oui-tracking-[0.03em]"
          intensity={98}
        >
          {title}
        </Text>
        {props.mode === MarginMode.ISOLATED ? (
          <Text className="oui-text-2xs oui-leading-[15px] oui-text-base-contrast-36 oui-font-semibold oui-tracking-[0.03em]">
            <Trans
              i18nKey="marginMode.isolatedMarginDescription"
              components={[
                <a
                  key="isolated-margin-doc"
                  href="https://paxeer-network.network/docs/introduction/trade-on-paxeer-network/perpetual-futures/isolated-margin"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="whitespace-nowrap oui-font-semibold oui-text-warning oui-underline oui-underline-offset-2 hover:oui-text-warning/90"
                  onClick={(e) => e.stopPropagation()}
                />,
              ]}
            />
          </Text>
        ) : (
          <Text className="oui-text-2xs oui-leading-[15px] oui-text-base-contrast-36 oui-font-semibold oui-tracking-[0.03em]">
            {t("marginMode.crossMarginDescription")}
          </Text>
        )}
      </Flex>

      {props.isCurrent ? (
        <div
          className={cn(
            "oui-absolute -oui-end-px -oui-top-px",
            "oui-rounded-bl-md oui-rounded-tr-md",
            "oui-bg-primary oui-px-1 oui-py-0.5",
          )}
        >
          <Text className="oui-text-2xs oui-leading-none oui-font-semibold oui-text-primary-contrast">
            {t("marginMode.current")}
          </Text>
        </div>
      ) : null}
    </div>
  );
};
