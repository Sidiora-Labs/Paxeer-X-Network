import React, { ReactNode, useMemo } from "react";
import { useTranslation, i18n } from "@paxeer-network/i18n";
import {
  Box,
  cn,
  DropdownMenuContent,
  DropdownMenuPortal,
  DropdownMenuRoot,
  DropdownMenuTrigger,
  Flex,
  Text,
  useScreen,
} from "@paxeer-network/ui";
import { CaretIcon } from "../../icons";

interface IProps {
  changeInterval: (interval: string) => void;
  interval: string;
}

const useMobileTimeIntervalMoreMap = () => {
  const { t } = useTranslation();
  const mobileTimeIntervalMoreMap = useMemo(() => {
    return [
      [
        {
          value: "3",
          label: t("tradingView.timeInterval.3m"),
        },
        {
          value: "5",
          label: t("tradingView.timeInterval.5m"),
        },

        {
          value: "30",
          label: t("tradingView.timeInterval.30m"),
        },

        {
          value: "120",
          label: t("tradingView.timeInterval.2h"),
        },
      ],
      [
        {
          value: "360",
          label: t("tradingView.timeInterval.6h"),
        },
        {
          value: "720",
          label: t("tradingView.timeInterval.12h"),
        },
        {
          value: "3d",
          label: t("tradingView.timeInterval.3d"),
        },
        {
          value: "1M",
          label: t("tradingView.timeInterval.1M"),
        },
      ],
    ];
  }, [t]);

  return { mobileTimeIntervalMoreMap };
};

export const TimeInterval: React.FC<IProps> = (props) => {
  const { isMobile } = useScreen();
  if (isMobile) {
    return <MobileTimeInterval {...props} />;
  }
  return <DesktopTimeInterval {...props} />;
};

const DesktopTimeInterval: React.FC<IProps> = (props) => {
  const { t } = useTranslation();
  const timeIntervalMap = useMemo(() => {
    return [
      {
        value: "1",
        label: t("tradingView.timeInterval.1m"),
      },
      {
        value: "3",
        label: t("tradingView.timeInterval.3m"),
      },
      {
        value: "5",
        label: t("tradingView.timeInterval.5m"),
      },
      {
        value: "15",
        label: t("tradingView.timeInterval.15m"),
      },
      {
        value: "30",
        label: t("tradingView.timeInterval.30m"),
      },

      {
        value: "60",
        label: t("tradingView.timeInterval.1h"),
      },

      {
        value: "240",
        label: t("tradingView.timeInterval.4h"),
      },

      {
        value: "720",
        label: t("tradingView.timeInterval.12h"),
      },
      {
        value: "1D",
        label: t("tradingView.timeInterval.1d"),
      },

      {
        value: "1W",
        label: t("tradingView.timeInterval.1w"),
      },
      {
        value: "1M",
        label: t("tradingView.timeInterval.1M"),
      },
    ];
  }, [t]);

  return (
    <div
      className={cn(
        "oui-text-2xs oui-text-base-contrast-36 oui-flex oui-gap-[2px] oui-items-center oui-me-3 oui-font-semibold",
        "oui-overflow-hidden",
      )}
    >
      {timeIntervalMap.map((item) => (
        <div
          key={item.value}
          className={cn(
            "oui-cursor-pointer oui-px-2",
            "hover:oui-text-base-contrast-80",
            "oui-break-normal oui-whitespace-nowrap",
            props.interval === item.value &&
              "oui-text-base-contrast-80 oui-bg-base-contrast-6 oui-rounded",
          )}
          id={item.value}
          onClick={() => props.changeInterval(item.value)}
        >
          {item.label}
        </div>
      ))}
    </div>
  );
};

export const MobileTimeInterval: React.FC<IProps> = (props) => {
  const { t } = useTranslation();

  const mobileTimeIntervalDefaultMap = useMemo(() => {
    return [
      {
        value: "1",
        label: t("tradingView.timeInterval.1m"),
      },
      {
        value: "15",
        label: t("tradingView.timeInterval.15m"),
      },
      {
        value: "60",
        label: t("tradingView.timeInterval.1h"),
      },
      {
        value: "240",
        label: t("tradingView.timeInterval.4h"),
      },
      {
        value: "1D",
        label: t("tradingView.timeInterval.1d"),
      },

      {
        value: "1W",
        label: t("tradingView.timeInterval.1w"),
      },
    ];
  }, [t]);

  const { mobileTimeIntervalMoreMap } = useMobileTimeIntervalMoreMap();

  const currentIntervalIsInExpand = useMemo(() => {
    for (const row of mobileTimeIntervalMoreMap) {
      for (const item of row) {
        if (item.value === props.interval) {
          return item.label;
        }
      }
    }
    return null;
  }, [props.interval, mobileTimeIntervalMoreMap]);

  return (
    <Flex
      justify="start"
      itemAlign="center"
      gap={3}
      className={cn(
        "oui-text-2xs oui-text-base-contrast-36",
        "oui-overflow-hidden",
      )}
    >
      <div className=" oui-flex oui-gap-1 oui-items-center oui-me-3 oui-font-semibold">
        {mobileTimeIntervalDefaultMap.map((item) => (
          <div
            className={cn(
              "oui-px-2",
              "oui-break-normal oui-whitespace-nowrap",
              props.interval === item.value &&
                "oui-text-base-contrast-80 oui-bg-base-contrast-6 oui-rounded",
            )}
            key={item.value}
            onClick={() => props.changeInterval(item.value)}
          >
            {item.label}
          </div>
        ))}
      </div>
      <DropDownTimeInterval {...props}>
        {currentIntervalIsInExpand ? (
          <div className="oui-text-base-contrast-80">
            {currentIntervalIsInExpand}
          </div>
        ) : (
          <Text className="oui-break-normal oui-whitespace-nowrap">
            {t("tradingView.timeInterval.more")}
          </Text>
        )}
      </DropDownTimeInterval>
    </Flex>
  );
};

const DropDownTimeInterval: React.FC<React.PropsWithChildren<IProps>> = (
  props,
) => {
  const [open, setOpen] = React.useState(false);
  const { mobileTimeIntervalMoreMap } = useMobileTimeIntervalMoreMap();

  return (
    <DropdownMenuRoot open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger asChild>
        <div className="oui-flex oui-justify-start oui-items-center oui-gap-0.5">
          {props.children}
          <CaretIcon
            className={cn(
              "oui-w-3 oui-h-3",
              open && "oui-text-base-contrast-80 oui-rotate-180",
            )}
          />
        </div>
      </DropdownMenuTrigger>
      <DropdownMenuPortal>
        <DropdownMenuContent
          onCloseAutoFocus={(e) => e.preventDefault()}
          onClick={(e) => e.stopPropagation()}
          align="start"
          alignOffset={0}
          sideOffset={0}
          className={cn(
            "oui-markets-dropdown-menu-content oui-bg-base-9 oui-w-screen oui-flex oui-flex-col oui-gap-2 oui-p-3",
          )}
        >
          {mobileTimeIntervalMoreMap.map((row, id) => (
            <div className="oui-flex oui-gap-2" key={id}>
              {row.map((item) => (
                <div
                  className={cn(
                    "oui-w-full  oui-text-2xs oui-flex oui-items-center oui-justify-center oui-h-6  oui-rounded",
                    item.value === props.interval
                      ? "oui-text-base-contrast oui-bg-primary-darken"
                      : "oui-text-base-contrast-36 oui-bg-base-5",
                  )}
                  key={item.value}
                  onClick={() => {
                    props.changeInterval(item.value);
                  }}
                >
                  <div>{item.label}</div>
                </div>
              ))}
            </div>
          ))}
        </DropdownMenuContent>
      </DropdownMenuPortal>
    </DropdownMenuRoot>
  );
};
