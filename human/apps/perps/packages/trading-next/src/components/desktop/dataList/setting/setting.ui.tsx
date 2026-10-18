import { FC, useMemo, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  Button,
  Checkbox,
  Divider,
  DropdownMenuContent,
  DropdownMenuRoot,
  DropdownMenuTrigger,
  ExclamationFillIcon,
  Flex,
  SettingFillIcon,
  Switch,
  Text,
  Tooltip,
  useScreen,
  Sheet,
  SheetContent,
  SheetTrigger,
  modal,
} from "@paxeer-network/ui";
import { SettingState } from "./setting.script";

export const Setting: FC<SettingState> = (props) => {
  const [open, setOpen] = useState(false);
  const { t } = useTranslation();
  const { isMobile } = useScreen();

  const SettingsContent = useMemo(() => {
    return () => (
      <>
        <div className="oui-flex oui-flex-col oui-text-sm">
          <Flex
            itemAlign="center"
            justify={isMobile ? "center" : "start"}
            className="oui-w-full"
          >
            <Text className="oui-text-base oui-pb-3">
              {t("trading.portfolioSettings")}
            </Text>
          </Flex>
          <Divider />
          <Text className="oui-pb-3 oui-text-base-contrast-54 oui-mt-2">
            {t("trading.portfolioSettings.decimalPrecision")}
          </Text>
          <DecimalPrecisionCheckbox
            value={props.pnlNotionalDecimalPrecision}
            onValueChange={(e) => {
              props.setPnlNotionalDecimalPrecision(e);
              setOpen(false);
            }}
          />
          <Divider className="oui-my-3" />
          <Text className="oui-pb-3 oui-text-base-contrast-54 oui-mt-2">
            {t("trading.portfolioSettings.unrealPnlPriceBasis")}
          </Text>
          <UnPnlPriceBasisCheckBox
            value={props.unPnlPriceBasis}
            onValueChange={(e) => {
              props.setUnPnlPriceBasic(e);
              setOpen(false);
            }}
          />
        </div>
        <Divider className="oui-my-3" />
        <Flex itemAlign="center" gap={1} justify="between">
          <Flex gap={1} itemAlign="center">
            <Text size="sm" intensity={54}>
              {t("trading.portfolioSettings.reversePosition")}
            </Text>
            {isMobile ? (
              <ExclamationFillIcon
                size={14}
                className="oui-text-base-contrast-54 hover:oui-text-base-contrast-80 oui-cursor-pointer"
                onClick={() => {
                  modal.alert({
                    title: t("common.tips"),
                    message: t(
                      "trading.portfolioSettings.reversePosition.tooltip",
                    ),
                  });
                }}
              />
            ) : (
              <Tooltip
                content={t("trading.portfolioSettings.reversePosition.tooltip")}
                className="oui-max-w-[300px]"
              >
                <ExclamationFillIcon
                  size={14}
                  className="oui-text-base-contrast-54 hover:oui-text-base-contrast-80 oui-cursor-pointer"
                />
              </Tooltip>
            )}
          </Flex>
          <Switch
            checked={props.reversePosition}
            onCheckedChange={(checked: boolean) => {
              props.setReversePosition(checked);
            }}
          />
        </Flex>
      </>
    );
  }, [
    t,
    isMobile,
    props.pnlNotionalDecimalPrecision,
    props.unPnlPriceBasis,
    props.reversePosition,
  ]);

  const triggerButton = (
    <Button
      size="xs"
      type="button"
      variant="contained"
      className="oui-preference-trigger-btn oui-bg-transparent hover:oui-bg-transparent"
    >
      <SettingFillIcon
        size={16}
        color="white"
        opacity={1}
        className="oui-text-base-contrast-36 hover:oui-text-base-contrast-80"
      />
    </Button>
  );

  return (
    <Flex className="oui-preference" gap={0}>
      <Flex gap={1}>
        <Checkbox
          id="oui-checkbox-hideOtherSymbols"
          className="oui-checkbox-hideOtherSymbols"
          color="white"
          checked={props.hideOtherSymbols}
          onCheckedChange={(checked: boolean) => {
            props.setHideOtherSymbols(checked);
          }}
        />
        <label
          className="oui-text-xs oui-text-base-contrast-54 oui-cursor-pointer"
          htmlFor="oui-checkbox-hideOtherSymbols"
        >
          {t("trading.hideOtherSymbols")}
        </label>
      </Flex>

      {isMobile ? (
        <Sheet open={open} onOpenChange={setOpen}>
          <SheetTrigger asChild>{triggerButton}</SheetTrigger>
          <SheetContent side="bottom" className="oui-px-5 oui-pt-3">
            <div
              style={{
                paddingBottom: `max(32px, calc(12px + env(safe-area-inset-bottom)))`,
              }}
            >
              <SettingsContent />
            </div>
          </SheetContent>
        </Sheet>
      ) : (
        <DropdownMenuRoot open={open} onOpenChange={setOpen}>
          <DropdownMenuTrigger asChild>{triggerButton}</DropdownMenuTrigger>
          <DropdownMenuContent
            className="oui-px-5 oui-py-3 oui-w-[360px]"
            alignOffset={2}
            align="end"
          >
            <SettingsContent />
          </DropdownMenuContent>
        </DropdownMenuRoot>
      )}
    </Flex>
  );
};

const UnPnlPriceBasisCheckBox = (props: {
  value: string;
  onValueChange: (value: string) => void;
}) => {
  const { value, onValueChange } = props;
  const { t } = useTranslation();

  // "markPrice" | "lastPrice"
  return (
    <Flex gap={2}>
      <RadioButton
        sel={value === "markPrice"}
        label={t("common.markPrice")}
        value={"markPrice"}
        onCheckChange={onValueChange}
      />
      <RadioButton
        sel={value === "lastPrice"}
        label={t("common.lastPrice")}
        value={"lastPrice"}
        onCheckChange={onValueChange}
      />
    </Flex>
  );
};

const DecimalPrecisionCheckbox = (props: {
  value: number;
  onValueChange: (value: number) => void;
}) => {
  const { value, onValueChange } = props;
  return (
    <Flex gap={2}>
      <RadioButton
        sel={value === 0}
        label={1}
        value={0}
        onCheckChange={onValueChange}
      />
      <RadioButton
        sel={value === 1}
        label={0.1}
        value={1}
        onCheckChange={onValueChange}
      />
      <RadioButton
        sel={value === 2}
        label={0.01}
        value={2}
        onCheckChange={onValueChange}
      />
    </Flex>
  );
};

// const InnerCheckbox = (props: {
//   sel: boolean;
//   label: any;
//   value: any;
//   onCheckChange: (value: any) => void;
// }) => {
//   const { sel, label, value, onCheckChange } = props;
//   return (
//     <Flex
//       onClick={(e) => {
//         onCheckChange(value);
//         e.stopPropagation();
//       }}
//       gap={1}
//     >
//       <Checkbox color="white" checked={sel} />
//       <Text size="xs" intensity={sel ? 98 : 54}>
//         {`${label}`}
//       </Text>
//     </Flex>
//   );
// };

const RadioButton = (props: {
  sel: boolean;
  label: any;
  value: any;
  onCheckChange: (value: any) => void;
}) => {
  const { sel, label, value, onCheckChange } = props;
  return (
    <Flex
      onClick={(e) => {
        onCheckChange(value);
        e.stopPropagation();
      }}
      gap={1}
      className="oui-cursor-pointer"
    >
      {sel ? (
        <SelIcon className="oui-fill-base-contrast" />
      ) : (
        <UnselIcon className="oui-fill-base-contrast-54" />
      )}
      <Text size="2xs" intensity={sel ? 98 : 54}>
        {label}
      </Text>
    </Flex>
  );
};

const SelIcon = (props: { className: string }) => {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 16 16"
      fill="currentColor"
      xmlns="http://www.w3.org/2000/svg"
      className={props.className}
    >
      <path d="M8.01 1.333a6.667 6.667 0 1 0 0 13.333 6.667 6.667 0 0 0 0-13.333m0 1.333a5.334 5.334 0 1 1-.001 10.667 5.334 5.334 0 0 1 0-10.667" />
      <circle cx="8" cy="8" r="3.333" />
    </svg>
  );
};

const UnselIcon = (props: { className: string }) => {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 16 16"
      fill="currentColor"
      xmlns="http://www.w3.org/2000/svg"
      className={props.className}
    >
      <path d="M8.01 1.333a6.667 6.667 0 1 0 0 13.333 6.667 6.667 0 0 0 0-13.333m0 1.333a5.334 5.334 0 1 1-.001 10.667 5.334 5.334 0 0 1 0-10.667" />
    </svg>
  );
};
