import { useEffect, useMemo, useState } from "react";
import React from "react";
import { Trans, useTranslation } from "@paxeer-network/i18n";
import { Flex, Tooltip, Text, cn, modal } from "@paxeer-network/ui";
import { useScreen } from "@paxeer-network/ui";

export type RwaTooltipProps = {
  isRwa: boolean;
  open?: boolean;
  closeTimeInterval?: number;
  openTimeInterval?: number;
};

export const RwaTooltip = (props: RwaTooltipProps) => {
  const { isRwa, open, closeTimeInterval, openTimeInterval } = props;
  const { isMobile } = useScreen();
  const { t } = useTranslation();
  const [isOpen, setIsOpen] = useState(false);

  const timeInterval = open ? closeTimeInterval : openTimeInterval;
  const tooltipContent = useMemo(() => {
    return <Content open={open} timeInterval={timeInterval} />;
  }, [open, t, timeInterval]);

  const triggerView = (
    <Flex
      r="base"
      px={2}
      className={cn(
        open ? "oui-bg-success/15" : "oui-bg-danger/15",
        "oui-shrink-0",
      )}
    >
      <Text size="2xs" color={open ? "success" : "danger"}>
        {open
          ? t("trading.rwa.marketHours")
          : t("trading.rwa.outsideMarketHours")}
      </Text>
    </Flex>
  );

  const handleClick = (e: React.MouseEvent<HTMLButtonElement>) => {
    e.stopPropagation();
    e.preventDefault();

    modal.alert({
      title: open
        ? t("trading.rwa.marketHours")
        : t("trading.rwa.outsideMarketHours"),
      message: <AlertContent open={open} timeInterval={timeInterval} />,
    });
  };

  if (!isRwa) {
    return null;
  }

  if (isMobile) {
    return <button onClick={handleClick}>{triggerView}</button>;
  }

  return (
    <Tooltip
      content={tooltipContent}
      open={isOpen}
      onOpenChange={setIsOpen}
      disableHoverableContent={false}
    >
      <button
        onMouseEnter={() => {
          setIsOpen(true);
        }}
        onClick={(e) => {
          e.stopPropagation();
          e.preventDefault();
        }}
      >
        {triggerView}
      </button>
    </Tooltip>
  );
};

const AlertContent = ({
  open,
  timeInterval,
}: {
  open?: boolean;
  timeInterval?: number;
}) => {
  const [innerTimeInterval, setInnerTimeInterval] = useState(timeInterval);

  useEffect(() => {
    setInnerTimeInterval(timeInterval);
  }, [timeInterval]);

  useEffect(() => {
    if (!innerTimeInterval || innerTimeInterval <= 0) {
      return;
    }

    const id = setInterval(() => {
      setInnerTimeInterval((prev) => {
        if (!prev || prev <= 1) {
          return 0;
        }
        return prev - 1;
      });
    }, 1000);
    return () => {
      clearInterval(id);
    };
  }, [innerTimeInterval]);

  return <Content open={open} timeInterval={innerTimeInterval} />;
};

const Content = ({
  open,
  timeInterval,
}: {
  open?: boolean;
  timeInterval?: number;
}) => {
  const { t } = useTranslation();
  return (
    <Flex
      direction="column"
      gapY={1}
      className="oui-text-2xs oui-max-w-[275px]"
      itemAlign="start"
      py={1}
    >
      {open
        ? t("trading.rwa.tooltip.description.open")
        : t("trading.rwa.tooltip.description.close")}

      <div className="oui-text-base-contrast-54">
        {timeInterval && (
          <Trans
            i18nKey={
              !open
                ? "trading.rwa.tooltip.openIn"
                : "trading.rwa.tooltip.closeIn"
            }
            values={{ timeFormat: timeInterval }}
            components={[
              // @ts-ignore
              <CountdownText key="0" />,
            ]}
          />
        )}
      </div>
      {/* TODO: wait for rwa detail page to be ready */}
      {/* <a
        href="https://paxeer-network.network/rwa"
        target="_blank"
        className="oui-flex oui-items-center oui-gap-x-1 oui-text-primary-darken oui-cursor-pointer"
        onClick={(e) => {
          e.stopPropagation();
          console.log("Link clicked in tooltip");
        }}
      >
        {t("trading.rwa.tooltip.checkDetailRules")}
        <ArrowRightShortIcon color="primary" opacity={1} />
      </a> */}
    </Flex>
  );
};

const CountdownText: React.FC<React.PropsWithChildren> = (props) => {
  const { children } = props;
  const { t } = useTranslation();
  const timeInterval = Number(children);

  // calculate days
  const days = Math.floor(timeInterval / (60 * 60 * 24));
  const daysStr = days.toString().padStart(2, "0");

  // calculate hours
  const hours = Math.floor((timeInterval % (60 * 60 * 24)) / (60 * 60));
  const hoursStr = hours.toString().padStart(2, "0");

  // calculate minutes
  const minutes = Math.floor((timeInterval % (60 * 60)) / 60);
  const minutesStr = minutes.toString().padStart(2, "0");

  // calculate seconds
  const seconds = timeInterval % 60;
  const secondsStr = seconds.toString().padStart(2, "0");

  return (
    <span className="oui-text-base-contrast oui-px-1">
      {days > 0 ? (
        <span>
          {daysStr}
          <span className="oui-text-base-contrast-54 oui-me-1 oui-ms-[2px]">
            {t("common.dayShort")}
          </span>
        </span>
      ) : (
        ""
      )}
      {hoursStr}:{minutesStr}:{secondsStr}
    </span>
  );
};
