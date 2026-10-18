import { FC, ReactNode, useEffect, useMemo, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  AlertDialog,
  cn,
  EditIcon,
  Flex,
  Input,
  inputFormatter,
  Text,
  Tips,
} from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import {
  getSlippageRiskLevel,
  shouldDisableSlippageSave,
} from "./slippage.utils";

interface SlippageRiskGuidance {
  recommendedValue: number;
  tooltip: ReactNode;
  helperText: ReactNode;
  minimumError: ReactNode;
  lowWarning: ReactNode;
  highWarning: ReactNode;
}

interface SlippageProps {
  value?: number;
  onValueChange?: (value: number) => void;
  max?: number;
  min?: number;
  dp?: number;
  message?: ReactNode;
  validate?: (value: number) => string;
  riskGuidance?: SlippageRiskGuidance;
}

const options = [0.5, 1, 2];

export const Slippage: FC<SlippageProps> = (props) => {
  const { min = 0.2, max = 10, dp = 2 } = props;
  const [value, setValue] = useState<number>();
  const [customValue, setCustomValue] = useState("");
  const [open, setOpen] = useState(false);
  const { t } = useTranslation();

  useEffect(() => {
    if (props.value !== undefined && !options.includes(props.value)) {
      setValue(undefined);
      setCustomValue(props.value.toString());
    } else {
      setValue(props.value);
      setCustomValue("");
    }
  }, [props.value, open]);

  const showSlippage = () => {
    setOpen(true);
  };

  const hideSlippage = () => {
    setOpen(false);
  };

  const onClick = (val: number) => {
    setValue(val);
    setCustomValue("");
  };

  const onValueChange = (val: string) => {
    if (!val || Number(val) === 0) {
      setCustomValue(val);
      return;
    }

    const d = new Decimal(val);
    setValue(undefined);
    if (d.lt(min) && !props.riskGuidance) {
      setCustomValue(min.toString());
    } else if (d.gt(max)) {
      setCustomValue(max.toString());
    } else {
      setCustomValue(val);
    }
  };

  const getValue = () =>
    customValue ? new Decimal(customValue)?.toNumber() : value;

  const onConfirm = () => {
    const val = getValue();
    if (!val) return Promise.resolve(true);
    props.onValueChange?.(val);
    hideSlippage();
    // if custom value in options, then clear custom value
    if (options.includes(val)) {
      setCustomValue("");
    }
    return Promise.resolve(true);
  };

  const currentValue = getValue();
  const riskLevel = props.riskGuidance
    ? getSlippageRiskLevel(
        currentValue,
        min,
        props.riskGuidance.recommendedValue,
      )
    : undefined;

  const errorMessage = useMemo(() => {
    if (riskLevel === "minimum") {
      return props.riskGuidance?.minimumError;
    }
    return currentValue === undefined
      ? undefined
      : props.validate?.(currentValue);
  }, [currentValue, props.riskGuidance, props.validate, riskLevel]);

  const warningMessage =
    riskLevel === "low"
      ? props.riskGuidance?.lowWarning
      : riskLevel === "high"
        ? props.riskGuidance?.highWarning
        : undefined;
  const disabled = shouldDisableSlippageSave(currentValue, riskLevel);

  const content = (
    <div className="oui-text-2xs">
      <Flex gapX={1} itemAlign="center">
        <Text size="sm" intensity={54}>
          {t("transfer.slippage.slippageTolerance")}
        </Text>
        <Tips
          content={
            props.riskGuidance?.tooltip ??
            t("transfer.slippage.slippageTolerance.description")
          }
          title={t("common.tips")}
          classNamss={{
            trigger: "oui-mt-[2px] oui-size-4",
          }}
        />
      </Flex>

      <Flex gapX={2} mt={3}>
        {options.map((item) => {
          const isActive = value === item;
          return (
            <SlippageItem
              key={item}
              value={item}
              isActive={isActive}
              onClick={() => {
                onClick(item);
              }}
            />
          );
        })}

        <Input
          suffix="%"
          formatters={[
            inputFormatter.numberFormatter,
            inputFormatter.dpFormatter(dp),
          ]}
          value={customValue}
          onValueChange={onValueChange}
          classNames={{
            root: cn(
              "oui-rounded-md oui-bg-base-6",
              "oui-h-[40px] oui-w-[74px]",
            ),
            input: "oui-text-base-contrast",
            additional: "oui-ps-1",
            suffix: "oui-text-base-contrast-36",
          }}
        />
      </Flex>
      {(props.riskGuidance?.helperText || props.message) && (
        <Text
          as="div"
          size="2xs"
          intensity={54}
          className="oui-mt-3 oui-leading-4"
        >
          {props.riskGuidance?.helperText || props.message}
        </Text>
      )}
      {errorMessage && (
        <Flex mt={3}>
          <Text
            size="2xs"
            color="warning"
            className="oui-w-full oui-text-center"
          >
            {errorMessage}
          </Text>
        </Flex>
      )}
      {!errorMessage && warningMessage && (
        <Flex mt={3}>
          <Text as="div" size="2xs" color="warning" className="oui-leading-4">
            {warningMessage}
          </Text>
        </Flex>
      )}
    </div>
  );

  return (
    <>
      <AlertDialog
        open={open}
        onOpenChange={setOpen}
        title={t("transfer.slippage")}
        okLabel={t("common.save")}
        message={content}
        onOk={onConfirm}
        actions={{ primary: { disabled } }}
      />
      <Flex
        width="100%"
        justify="between"
        className="oui-cursor-pointer oui-select-none"
        onClick={showSlippage}
      >
        <Text intensity={36} size="2xs">
          {t("transfer.slippage")}
        </Text>
        <Flex gapX={1}>
          <Text size="2xs" color="primaryLight">
            {props.value}%
          </Text>
          <EditIcon className="oui-size-3 oui-text-primary-light" />
        </Flex>
      </Flex>
    </>
  );
};

type SlippageItemProps = {
  value: number;
  isActive: boolean;
  onClick: () => void;
};

const SlippageItem: FC<SlippageItemProps> = ({ value, isActive, onClick }) => {
  return (
    <Flex
      intensity={600}
      justify="center"
      itemAlign="center"
      r="md"
      width={74}
      height={40}
      className={cn(
        "oui-cursor-pointer oui-select-none",
        isActive
          ? "oui-bg-primary-darken oui-text-primary-contrast/80"
          : "oui-text-base-contrast-80",
      )}
      onClick={onClick}
    >
      <Text size="sm">{value}%</Text>
    </Flex>
  );
};
