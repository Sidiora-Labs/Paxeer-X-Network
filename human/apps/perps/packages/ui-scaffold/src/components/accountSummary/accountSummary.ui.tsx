import React, { FC, PropsWithChildren, useEffect } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  Box,
  EyeCloseIcon,
  EyeIcon,
  Flex,
  Popover,
  Text,
} from "@paxeer-network/ui";

type AccountSummaryUi = {
  totalValue: number | null;
  freeCollateral: number | null;
  maxLeverage?: number | null;
  currentLeverage: number | null;
  unrealized_pnl_ROI: number | null;
  unrealPnL: number | null;
  visible?: boolean;
  onToggleVisibility?: () => void;
  // type: AccountSummaryType;
  keys: AccountSummaryList;
  elementKeys: AccountSummaryList;
  onToggleItemByKey: (key: string) => void;
  onKeyToTop: (key: string) => void;
};

//----------------- TotalValue -----------------
const TotalValue: FC<{
  totalValue: number | null;
  visible?: boolean;
  onToggleVisibility?: () => void;
  visibleAvailable?: boolean;
}> = (props) => {
  const {
    totalValue = 0,
    visible = true,
    onToggleVisibility,
    visibleAvailable = true,
  } = props;
  const { t } = useTranslation();

  return (
    <Flex
      direction={"column"}
      gap={0}
      className="oui-text-2xs oui-text-base-contrast"
      itemAlign={"start"}
    >
      <Flex gap={1} itemAlign={"center"}>
        <Text intensity={54} className="oui-whitespace-nowrap">
          {t("common.totalValue")}
        </Text>
        {visibleAvailable && (
          <button onClick={() => onToggleVisibility?.()}>
            {visible ? (
              <EyeIcon
                size={12}
                className="oui-text-primary-light"
                opacity={1}
              />
            ) : (
              <EyeCloseIcon
                size={12}
                className="oui-text-primary-light"
                opacity={1}
              />
            )}
          </button>
        )}

        <Text intensity={54}>≈</Text>
      </Flex>
      <Text.numeral
        intensity={98}
        visible={props.visible}
        unit="USDC"
        unitClassName="oui-text-base-contrast-20 oui-ms-1"
        as="div"
      >
        {totalValue ?? "-"}
      </Text.numeral>
    </Flex>
  );
};

//----------------- FreeCollateral -----------------
const FreeCollateral: FC<{
  freeCollateral?: number | null;
  visible?: boolean;
  onToggleVisibility?: () => void;
  visibleAvailable?: boolean;
}> = (props) => {
  const {
    freeCollateral,
    visible,
    onToggleVisibility,
    visibleAvailable = true,
  } = props;
  const { t } = useTranslation();

  return (
    <Flex
      direction={"column"}
      gap={0}
      className="oui-text-2xs oui-text-base-contrast"
      itemAlign={"start"}
    >
      <Flex gap={1} itemAlign={"center"}>
        <Text intensity={54} className="oui-whitespace-nowrap">
          {t("trading.asset.freeCollateral")}
        </Text>
        {visibleAvailable && (
          <button onClick={() => onToggleVisibility?.()}>
            {visible ? (
              <EyeIcon
                size={12}
                className="oui-text-primary-light"
                opacity={1}
              />
            ) : (
              <EyeCloseIcon
                size={12}
                className="oui-text-primary-light"
                opacity={1}
              />
            )}
          </button>
        )}
      </Flex>
      {/* <Box>
        <Text intensity={54} className="oui-whitespace-nowrap">
          Free collateral
        </Text>
      </Box> */}
      <Text.numeral
        intensity={98}
        unit="USDC"
        unitClassName="oui-text-base-contrast-20 oui-ms-1"
        visible={visible}
        as="div"
      >
        {freeCollateral ?? "-"}
      </Text.numeral>
    </Flex>
  );
};

//----------------- CurrentLeverage -----------------
const CurrentLeverage: FC<{
  currentLeverage: number | null;
}> = (props) => {
  const { currentLeverage } = props;
  const { t } = useTranslation();

  return (
    <Flex
      direction={"column"}
      gap={0}
      className="oui-text-2xs oui-text-base-contrast"
      itemAlign={"start"}
    >
      <Box>
        <Text intensity={54} className="oui-whitespace-nowrap">
          {t("leverage.currentLeverage")}
        </Text>
      </Box>
      <Text.numeral as={"div"} unit="x" intensity={98}>
        {currentLeverage ?? 0}
      </Text.numeral>
    </Flex>
  );
};

//----------------- UnrealPnL -----------------
const UnrealPnL: FC<{
  unrealized_pnl_ROI: number | null;
  unrealPnL: number | null;
  visible?: boolean;
  onToggleVisibility?: () => void;
  visibleAvailable?: boolean;
}> = (props) => {
  const { visible, onToggleVisibility, visibleAvailable = true } = props;
  const { t } = useTranslation();

  return (
    <Flex
      direction={"column"}
      gap={0}
      className="oui-text-2xs oui-text-base-contrast"
      itemAlign={"start"}
    >
      <Flex gap={1} itemAlign={"center"}>
        <Text intensity={54} className="oui-whitespace-nowrap">
          {t("common.unrealizedPnl")}
        </Text>
        {visibleAvailable && (
          <button onClick={() => onToggleVisibility?.()}>
            {visible ? (
              <EyeIcon
                size={12}
                className="oui-text-primary-light"
                opacity={1}
              />
            ) : (
              <EyeCloseIcon
                size={12}
                className="oui-text-primary-light"
                opacity={1}
              />
            )}
          </button>
        )}
      </Flex>
      <Text.numeral
        as={"div"}
        coloring
        showIdentifier
        weight={"semibold"}
        visible={visible}
        intensity={98}
        suffix={
          <Text.numeral coloring prefix={"("} suffix={")"} rule={"percentages"}>
            {props.unrealized_pnl_ROI ?? "-"}
          </Text.numeral>
        }
      >
        {props.unrealPnL ?? "-"}
      </Text.numeral>
    </Flex>
  );
};

//----------------- AccountInfoPopover -----------------
const AccountInfoPopover = (props: {
  totalValue: number | null;
  freeCollateral: number | null;
  // maxLeverage?: number | null;
  currentLeverage: number | null;
  unrealPnL: number | null;
  unrealized_pnl_ROI: number | null;
  // type: AccountSummaryType;
  keys: AccountSummaryList;
  elementKeys: AccountSummaryList;
  onToggleItemByKey: (key: string) => void;
  visible?: boolean;
  onKeyToTop: (key: string) => void;
}) => {
  const { totalValue, keys, elementKeys } = props;
  const { t } = useTranslation();

  const onSetToTop = (key: SummaryKey) => (event: React.MouseEvent) => {
    event.preventDefault();
    event.stopPropagation();
    props.onKeyToTop(key);
  };

  const elements = elementKeys.map((key) => {
    switch (key) {
      case "totalValue":
        return (
          <DropdownMenu onSetTop={onSetToTop("totalValue")} key={key}>
            <Flex className={"oui-text-base-contrast-54"} gapX={2}>
              <IdentityButton
                active={keys.includes("totalValue")}
                onClick={() => props.onToggleItemByKey("totalValue")}
              />
              <span>{t("common.totalValue")}</span>
            </Flex>
            <Text.numeral
              intensity={98}
              visible={props.visible}
              unit="USDC"
              className="oui-transition-transform group-hover:-oui-translate-x-5"
              unitClassName={"oui-text-base-contrast-36 oui-ms-1"}
            >
              {totalValue ?? "-"}
            </Text.numeral>
          </DropdownMenu>
        );
      case "freeCollateral":
        return (
          <DropdownMenu onSetTop={onSetToTop("freeCollateral")} key={key}>
            <Flex className={"oui-text-base-contrast-54"} gapX={2}>
              <IdentityButton
                active={keys.includes("freeCollateral")}
                onClick={() => props.onToggleItemByKey("freeCollateral")}
              />
              <span>{t("trading.asset.freeCollateral")}</span>
            </Flex>
            <Text.numeral
              intensity={98}
              unit="USDC"
              visible={props.visible}
              className="oui-transition-transform group-hover:-oui-translate-x-5"
              unitClassName={"oui-text-base-contrast-36 oui-ms-1"}
            >
              {props.freeCollateral ?? "-"}
            </Text.numeral>
          </DropdownMenu>
        );
      case "unrealPnL":
        return (
          <DropdownMenu onSetTop={onSetToTop("unrealPnL")} key={key}>
            <Flex className={"oui-text-base-contrast-54"} gapX={2}>
              <IdentityButton
                active={keys.includes("unrealPnL")}
                onClick={() => props.onToggleItemByKey("unrealPnL")}
              />
              <span>{t("common.unrealizedPnl")}</span>
            </Flex>
            <Text.pnl
              intensity={98}
              coloring
              showIdentifier
              visible={props.visible}
              className="oui-transition-transform group-hover:-oui-translate-x-5"
              suffix={
                <Text.roi
                  coloring
                  prefix={"("}
                  visible={props.visible}
                  suffix={")"}
                  rule={"percentages"}
                >
                  {props.unrealized_pnl_ROI ?? "-"}
                </Text.roi>
              }
            >
              {props.unrealPnL ?? "-"}
            </Text.pnl>
          </DropdownMenu>
        );
      case "currentLeverage":
        return (
          <DropdownMenu onSetTop={onSetToTop("currentLeverage")} key={key}>
            <Flex className={"oui-text-base-contrast-54"} gapX={2}>
              <IdentityButton
                active={keys.includes("currentLeverage")}
                onClick={() => props.onToggleItemByKey("currentLeverage")}
              />
              <span>{t("leverage.currentLeverage")}</span>
            </Flex>
            <Text.numeral
              intensity={98}
              className="oui-transition-transform group-hover:-oui-translate-x-5"
              unit="x"
            >
              {props.currentLeverage ?? "-"}
            </Text.numeral>
          </DropdownMenu>
        );

      default:
        return null;
    }
  });

  return (
    <Flex
      className={"oui-text-2xs oui-font-semibold"}
      direction={"column"}
      gapY={1}
    >
      {elements}
    </Flex>
  );
};

const DropdownMenu: FC<
  PropsWithChildren<{
    onSetTop: (event: React.MouseEvent) => void;
  }>
> = (props) => {
  return (
    <div className="oui-group oui-relative oui-w-full oui-rounded oui-px-[6px] oui-py-1 hover:oui-bg-base-6">
      <Flex justify={"between"} width={"100%"}>
        {props.children}
      </Flex>
      <button
        className="oui-absolute oui-end-1 oui-top-1"
        onClick={props.onSetTop}
      >
        <svg
          className="oui-translate-x-3 oui-cursor-pointer oui-opacity-0 oui-transition-all group-hover:oui-translate-x-0 group-hover:oui-opacity-100 oui-group/icon"
          width="16"
          height="16"
          viewBox="0 0 16 16"
          fill="none"
          xmlns="http://www.w3.org/2000/svg"
        >
          <rect
            width="16"
            height="16"
            rx="2"
            // fill="rgb(var(--oui-color-primary-darken))"
            className="oui-fill-primary-darken"
          />
          <path
            // fill="rgb(var(--oui-color-primary-contrast))"
            className="oui-fill-primary-contrast/50 group-hover/icon:oui-fill-primary-contrast"
            d="M3.507 3.999a.5.5 0 1 0 0 1h9a.5.5 0 0 0 0-1zm4 8.006a.5.5 0 0 0 1 0V7.724l1.5 1.484.703-.703-2.343-2.36a.515.515 0 0 0-.72 0l-2.343 2.36.703.703 1.5-1.484z"
          />
        </svg>
      </button>
    </div>
  );
};

const IdentityButton = (props: {
  active: boolean;
  onClick: React.MouseEventHandler;
}) => {
  return (
    <button onClick={props.onClick}>
      {props.active ? (
        <svg
          xmlns="http://www.w3.org/2000/svg"
          width="12"
          height="12"
          viewBox="0 0 12 12"
          fill="none"
        >
          <path
            d="M7.506.977a.56.56 0 0 0-.365.16C6.367 1.88 6.13 2.5 6.413 3.348c-.661.506-1.078.63-1.903.63-1.127 0-1.863.137-2.364.637v.016a1.187 1.187 0 0 0 0 1.686l1.4 1.411-2.385 2.385a.513.513 0 0 0-.015.717.514.514 0 0 0 .719 0l2.394-2.395 1.403 1.396a1.19 1.19 0 0 0 1.687 0h.016c.501-.5.64-1.142.64-2.358 0-.773.16-1.319.64-1.893.924.271 1.462.012 2.22-.746a.5.5 0 0 0 .14-.36c0-.175-.044-.445-.156-.78a4 4 0 0 0-.984-1.577 4 4 0 0 0-1.578-.984C7.95 1.02 7.682.977 7.506.977"
            fill="currentcolor"
            className={"oui-text-primary-light"}
          />
        </svg>
      ) : (
        <svg
          xmlns="http://www.w3.org/2000/svg"
          width="12"
          height="12"
          viewBox="0 0 12 12"
          fill="none"
        >
          <path
            d="M7.506.977a.56.56 0 0 0-.365.16C6.367 1.88 6.13 2.5 6.413 3.348c-.661.506-1.078.63-1.903.63-1.127 0-1.863.137-2.364.637v.016a1.187 1.187 0 0 0 0 1.686l1.4 1.411-2.385 2.385a.513.513 0 0 0-.015.717.514.514 0 0 0 .719 0l2.394-2.395 1.403 1.396a1.19 1.19 0 0 0 1.687 0h.016c.501-.5.64-1.142.64-2.358 0-.773.16-1.319.64-1.893.924.271 1.462.012 2.22-.746a.5.5 0 0 0 .14-.36c0-.175-.044-.445-.156-.78a4 4 0 0 0-.984-1.577 4 4 0 0 0-1.578-.984C7.95 1.02 7.682.977 7.506.977m.193 1.027c.531.098 1.066.45 1.447.83.381.382.706.848.84 1.459-.477.458-.77.494-1.247.243a.5.5 0 0 0-.583.087c-.91.91-1.15 1.736-1.15 2.85 0 .97-.057 1.33-.36 1.655-.085.092-.203.078-.281 0L2.849 5.615a.18.18 0 0 1-.012-.255c.178-.217.576-.386 1.669-.386 1.306.001 2-.335 2.859-1.14a.5.5 0 0 0 .094-.578c-.257-.513-.217-.784.24-1.252"
            fill="currentcolor"
          />
        </svg>
      )}
    </button>
  );
};

type SummaryKey =
  | "totalValue"
  | "freeCollateral"
  | "unrealPnL"
  | "currentLeverage";

export type AccountSummaryList = Array<SummaryKey>;
const AccountSummaryItems: Record<SummaryKey, JSX.ElementType> = {
  totalValue: TotalValue,
  freeCollateral: FreeCollateral,
  unrealPnL: UnrealPnL,
  currentLeverage: CurrentLeverage,
};

export const AccountSummary: React.FC<AccountSummaryUi> = (props) => {
  const { keys, ...rest } = props;
  let canToggleIndex = 0;
  const sizeRef = React.useRef(0);

  useEffect(() => {
    const resizeObserver = new ResizeObserver((entries) => {
      if (Array.isArray(entries) && entries.length > 0) {
        const width = entries[0].contentRect.width;
        if (width > 1556) {
          sizeRef.current = 5;
        } else if (width > 1480) {
          sizeRef.current = 4;
        } else if (width > 1366) {
          sizeRef.current = 3;
        } else if (width > 1280) {
          sizeRef.current = 2;
        } else if (width > 1180) {
          sizeRef.current = 1;
        } else {
          sizeRef.current = 0;
        }
      }
    });

    resizeObserver.observe(document.body);

    return () => {
      resizeObserver.disconnect();
    };
  }, []);

  const elements = keys.slice(0, sizeRef.current).map((key, index) => {
    switch (key) {
      case "freeCollateral": {
        return (
          <FreeCollateral
            freeCollateral={props.freeCollateral}
            visible={props.visible}
            onToggleVisibility={props.onToggleVisibility}
            visibleAvailable={index === canToggleIndex}
          />
        );
      }

      case "unrealPnL":
        return (
          <UnrealPnL
            unrealPnL={props.unrealPnL}
            unrealized_pnl_ROI={props.unrealized_pnl_ROI}
            visible={props.visible}
            onToggleVisibility={props.onToggleVisibility}
            visibleAvailable={index === canToggleIndex}
          />
        );
      case "currentLeverage": {
        canToggleIndex++;
        return <CurrentLeverage currentLeverage={props.currentLeverage} />;
      }
      case "totalValue":
      default:
        return (
          <TotalValue
            totalValue={props.totalValue}
            onToggleVisibility={props.onToggleVisibility}
            visible={props.visible}
            visibleAvailable={index === canToggleIndex}
          />
        );
    }
  });

  return (
    <div className="oui-flex oui-items-center oui-gap-6">
      <Items elements={elements} />
      <Popover
        content={
          <AccountInfoPopover
            totalValue={rest.totalValue}
            freeCollateral={props.freeCollateral}
            currentLeverage={props.currentLeverage}
            unrealized_pnl_ROI={props.unrealized_pnl_ROI}
            unrealPnL={props.unrealPnL}
            keys={keys}
            elementKeys={props.elementKeys}
            onToggleItemByKey={props.onToggleItemByKey}
            onKeyToTop={props.onKeyToTop}
            visible={props.visible}
          />
        }
        contentProps={{
          onOpenAutoFocus: (event) => event.preventDefault(),
          sideOffset: 12,
          className: "oui-p-1",
        }}
        arrow
      >
        <div className="oui-cursor-pointer oui-group">
          <Dot />
        </div>
      </Popover>
    </div>
  );
};

const Items: FC<{
  elements: JSX.Element[];
}> = (props) => {
  return (
    <div className="oui-flex oui-gap-6">
      {props.elements.map((Element, index) => (
        <div key={index}>{Element}</div>
      ))}
    </div>
  );
};

const Dot = () => {
  return (
    <svg
      width="20"
      height="20"
      viewBox="0 0 20 20"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className="oui-fill-base-contrast-36 hover:oui-fill-primary-light group-data-[state=open]:oui-fill-primary-light"
    >
      <path d="M10.007 8.335a1.666 1.666 0 1 1 0 3.333 1.666 1.666 0 0 1 0-3.333m-5.84 0a1.666 1.666 0 1 1 0 3.333 1.666 1.666 0 0 1 0-3.333m11.666 0a1.666 1.666 0 1 1 0 3.333 1.666 1.666 0 0 1 0-3.333" />
    </svg>
  );
};
