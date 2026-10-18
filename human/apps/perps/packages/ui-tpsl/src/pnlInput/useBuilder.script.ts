import { useMemo, useRef, useState } from "react";
import { useLocalStorage } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { MenuItem } from "@paxeer-network/ui";
import type {
  InputFormatter,
  InputFormatterOptions,
} from "@paxeer-network/ui";
import { Decimal, todpIfNeed } from "@paxeer-network/utils";

export enum PnLMode {
  PnL = "PnL",
  OFFSET = "Offset",
  PERCENTAGE = "Offset%",
  OFFSET_FROM_MARK = "OffsetFromMark",
  PERCENTAGE_FROM_MARK = "PercentageFromMark",
}

export type PNL_Values = {
  PnL: string | undefined;
  Offset: string | undefined;
  "Offset%": string | undefined;
  OffsetFromMark: string | undefined;
  PercentageFromMark: string | undefined;
};

export type BuilderProps = {
  type: "TP" | "SL";
  disabled?: boolean;

  quote_dp?: number;
  onChange: (key: string, value: number | string) => void;

  values: PNL_Values;
};

export const usePNLInputBuilder = (props: BuilderProps) => {
  const { type, values } = props;
  const [mode, setMode] = useLocalStorage<PnLMode>(
    "TP/SL_Mode",
    PnLMode.PERCENTAGE,
  );
  const [focus, setFocus] = useState(true);

  const { t } = useTranslation();

  const key = useMemo(() => {
    switch (mode) {
      case PnLMode.OFFSET:
        return `${type.toLowerCase()}_offset`;
      case PnLMode.PERCENTAGE:
        return `${type.toLowerCase()}_offset_percentage`;
      case PnLMode.OFFSET_FROM_MARK:
        return `${type.toLowerCase()}_offset_from_mark`;
      case PnLMode.PERCENTAGE_FROM_MARK:
        return `${type.toLowerCase()}_offset_percentage_from_mark`;
      default:
        return `${type.toLowerCase()}_pnl`;
    }
  }, [mode]);

  const value = useMemo(() => {
    return values[mode as keyof PNL_Values];
  }, [values, mode]);

  const modes = useMemo<MenuItem[]>(() => {
    return [
      {
        label: t("tpsl.pnl"),
        value: PnLMode.PnL,
        testId: `${PnLMode.PnL}_menu_item`,
      },
      {
        label: t("tpsl.offset"),
        value: PnLMode.OFFSET,
        testId: `${PnLMode.OFFSET}_menu_item`,
      },
      {
        label: t("tpsl.offsetPercent"),
        value: PnLMode.PERCENTAGE,
        testId: `${PnLMode.PERCENTAGE}_menu_item`,
      },
      {
        label: t("tpsl.offsetMark"),
        value: PnLMode.OFFSET_FROM_MARK,
        testId: `${PnLMode.OFFSET_FROM_MARK}_menu_item`,
      },
      {
        label: t("tpsl.offsetPercentMark"),
        value: PnLMode.PERCENTAGE_FROM_MARK,
        testId: `${PnLMode.PERCENTAGE_FROM_MARK}_menu_item`,
      },
    ];
  }, [t]);

  /**
   * Use dedicated short labels for input prefix so it can diverge
   * from dropdown menu labels without coupling the two UIs.
   */
  const modeLabelMap = useMemo<Record<PnLMode, string>>(() => {
    return {
      [PnLMode.PnL]: t("tpsl.pnl"),
      [PnLMode.OFFSET]: t("tpsl.offsetHolder"),
      [PnLMode.PERCENTAGE]: t("tpsl.offsetHolder"),
      [PnLMode.OFFSET_FROM_MARK]: t("tpsl.offsetHolder"),
      [PnLMode.PERCENTAGE_FROM_MARK]: t("tpsl.offsetHolder"),
    };
  }, [t]);

  const percentageSuffix = useRef<string>("");

  const onValueChange = (value: string) => {
    if (!props.disabled) {
      props.onChange(key, value);
    }
  };

  const formatter = (options: {
    dp?: number;
    mode: PnLMode;
  }): InputFormatter => {
    const { dp = 2 } = options;
    return {
      onRenderBefore: (
        value: string | number,
        options: InputFormatterOptions,
      ) => {
        value = `${value}`; // convert to string

        if (focus) {
          // not need to add "-", because the value is already negative
          // if (type === "SL" && mode === PnLMode.PnL) {
          //   value = value.startsWith("-") ? value : "-" + value;
          // }
        }

        if (value === "" || value === "-") return "";
        // if (mode === PnLMode.PnL || mode === PnLMode.OFFSET) {
        //   return commify(value);
        // }

        if (
          mode === PnLMode.PERCENTAGE ||
          mode === PnLMode.PERCENTAGE_FROM_MARK
        ) {
          try {
            // fix value is invalid decimal string, like "-"
            return `${new Decimal(
              value.replace(
                new RegExp(percentageSuffix.current.replace(".", "\\.") + "$"),
                "",
              ),
            )
              .mul(100)
              .todp(2, 4)
              .toString()}${percentageSuffix.current}`;
          } catch {
            return "";
          }
        } else if (
          mode === PnLMode.OFFSET ||
          mode === PnLMode.OFFSET_FROM_MARK
        ) {
          value = todpIfNeed(value, dp);
        } else {
          // value = new Decimal(value).todp(2).toString();
        }

        return `${value}`;
      },
      onSendBefore: (value: string) => {
        if (/^\-?0{2,}$/.test(value)) {
          return "0";
        }

        if (
          mode === PnLMode.PERCENTAGE ||
          mode === PnLMode.PERCENTAGE_FROM_MARK
        ) {
          // console.log("value", value);
          if (value !== "") {
            // percentageSuffix.current = value.endsWith(".") ? "." : "";
            value = todpIfNeed(value, 2);
            const endStr = value.match(/\.0{0,2}$/);
            if (!!endStr) {
              percentageSuffix.current = endStr[0];
            } else {
              percentageSuffix.current = "";
            }
            // fix value is invalid decimal string, like "1-46.35"
            try {
              value = new Decimal(value).div(100).toString();
            } catch {
              value = "";
            }
            value = `${value}${percentageSuffix.current}`;
          }
        } else if (mode === PnLMode.PnL && type === "SL" && focus) {
          // not need to add "-", because the value is already negative
          value = value.startsWith("-") ? value : "-" + value;
        } else {
          value = todpIfNeed(value, dp);
        }

        if (value === "" || value === "-") return "";

        return value;
      },
    };
    // return {
    //   onRenderBefore: (
    //     value: string | number,
    //     options: InputFormatterOptions
    //   ) => {
    //     // console.log("???", options);
    //     const { isFocused } = options;
    //     value = `${value}`;
    //     if (value === "" || value === "-") return "";

    //     // if (type === "SL" && mode === PnLMode.PnL) {
    //     //   if (isFocused) {
    //     //     value = value.startsWith("-") ? value : "-" + value;
    //     //   }
    //     // }

    //     if (mode === PnLMode.PERCENTAGE) {
    //       return `${todpIfNeed(new Decimal(value).mul(100).toString(), 2)}${
    //         percentageSuffix.current
    //       }`;
    //       // return (Number(value) * 100).toFixed(2);
    //     } else if (mode === PnLMode.OFFSET) {
    //       value = todpIfNeed(value, 2);
    //     } else {
    //       // value = new Decimal(value).todp(2).toString();
    //     }

    //     return value;
    //   },
    //   onSendBefore: (value: string, options: InputFormatterOptions) => {
    //     const { isFocused } = options;

    //     if (mode === PnLMode.PERCENTAGE) {
    //       if (value !== "") {
    //         percentageSuffix.current = value.endsWith(".") ? "." : "";
    //         value = new Decimal(value).div(100).toString();
    //         value = todpIfNeed(value, 4);
    //       }
    //     } else {
    //       // value = todpIfNeed(value, quote_dp);
    //       if (isFocused) {
    //         if (type === "SL" && mode === PnLMode.PnL) {
    //           // if (
    //           //   typeof values[PnLMode.PnL] !== "undefined" &&
    //           //   values[PnLMode.PnL] !== ""
    //           // )
    //           //   return value;
    //           const num = Number(value);
    //           if (!isNaN(num) && num !== 0) {
    //             value = (Math.abs(num) * -1).toString();
    //           } else {
    //             value = "";
    //           }
    //         }
    //       }
    //     }

    //     return value;
    //   },
    // };
  };

  return {
    mode,
    modes,
    modeLabelMap,
    type: props.type,
    formatter,
    onModeChange: (mode: PnLMode) => {
      if (!props.disabled) {
        setMode(mode);
      }
    },
    disabled: props.disabled,
    value,
    pnl: values[PnLMode.PnL],
    onValueChange,
    quote_dp: props.quote_dp,
    setFocus,
  };
};

export type PNLInputState = ReturnType<typeof usePNLInputBuilder>;
