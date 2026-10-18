import React, { useMemo, useState } from "react";
import { format as formatDate, isValid } from "date-fns";
import { SizeType } from "../helpers/sizeType";
import { TokenIcon } from "../icon";
import { CopyIcon } from "../icon/copy";
import { CopyableTextProps, Text, TextElement, TextProps } from "./text";
import { formatAddress } from "./utils";

export type TextRule = "date" | "address" | "symbol" | "status" | "txId";

export const isTextRule = (rule: string): rule is TextRule => {
  return ["date", "address", "symbol", "status", "txId"].includes(rule);
};

type DateText = {
  rule: "date";
  /**
   * use date-fns to format the date, default is "YYYY-MM-DD HH:mm:ss", more info:
   * @see https://date-fns.org/v3.6.0/docs/format
   */
  formatString?: string;
};
type AddressText = {
  rule: "address";
  /**
   * range of the address to show
   * the first number is the length of the start of the address, and the second number is the length of the end of the address
   * @default [6, 4]
   */
  range?: [number, number];
};

type BaseText = {
  rule: Omit<TextRule, "address" | "date">;
  /**
   * capitalize the first letter of the string
   */
  capitalize?: boolean;
};

type TxIDText = {
  rule: "txId";
  range?: [number, number];
};

type SymbolText = {
  rule: "symbol";
  // symbolElement?: "base" | "quote";
  /**
   * symbol format string, like "type-base-quote",
   * @default base-quote
   */
  formatString?: string;
  showIcon?: boolean;
  iconSize?: SizeType;
};

const DEFAULT_SYMBOL_FORMAT = "base";
const DEFAULT_DATE_FORMAT = "yyyy-MM-dd HH:mm:ss";

export type FormattedTextProps = TextProps &
  CopyableTextProps & {
    // asChildren?: boolean;
    // rule?: Omit<TextRule, "status"|'address'|'date'>;
    loading?: boolean;
    dp?: number;
    rm?: number;

    suffix?: React.ReactNode;
    prefix?: React.ReactNode;
    showIcon?: boolean;
  } & (
    | BaseText
    | DateText
    | AddressText
    | SymbolText
    | TxIDText
    | { rule?: string }
  );

export const FormattedText = React.forwardRef<TextElement, FormattedTextProps>(
  (props, ref) => {
    const {
      rule,
      children,

      prefix,
      // @ts-ignore
      symbolElement,
      // @ts-ignore
      formatString,
      // @ts-ignore
      range,
      // @ts-ignore
      capitalize,
      copyable,
      copyIconSize,
      onCopy,
      showIcon,
      //@ts-ignore
      iconSize = "xs",
      copyIconTestid,
      //@ts-ignore
      isIcon,
      ...rest
    } = props;
    // const Comp = asChildren ? Slot : "span";
    //
    const prefixElement = useMemo(() => {
      if (rule === "symbol" && showIcon) {
        return <TokenIcon symbol={children as string} size={iconSize} />;
      }

      return prefix;
    }, [prefix, showIcon, rule, iconSize, children]);

    const suffix = useMemo(() => {
      if (typeof props.suffix !== "undefined") {
        return props.suffix;
      }
      if (copyable) {
        return (
          <button
            className="oui-cursor-pointer oui-text-sm"
            data-testid={copyIconTestid}
            onClick={(e) => {
              navigator.clipboard.writeText(children as string);
              onCopy?.(e);
            }}
          >
            <CopyIcon size={copyIconSize ?? 12} color="white" />
          </button>
        );
      }
    }, [props.suffix, copyable, children]);

    const content = useMemo(() => {
      if (typeof children === "undefined") {
        return "--";
      }
      if (typeof rule === "undefined") {
        return children;
      }
      if (rule === "address" || rule === "txId") {
        return formatAddress(
          children as string,
          range ?? (rule === "address" ? [6, 4] : [6, 6]),
        );
      }
      if (rule === "date") {
        // return new Date(children as string).toLocaleString();
        const date = new Date(children as string | number | Date);
        if (!isValid(date)) {
          return "Error: Invalid Date";
        }
        return formatDate(
          new Date(children as string),
          formatString ?? DEFAULT_DATE_FORMAT,
        );
      }
      /**
       * format paxeer-network symbol
       */
      if (rule === "symbol") {
        const arr = (children as string).split("_");
        const type = arr[0];
        const base = arr[1];
        const quote = arr[2];

        return (formatString ?? DEFAULT_SYMBOL_FORMAT)
          .replace("type", type)
          .replace("base", base)
          .replace("quote", quote);
      }

      // if (rule === "status") {
      //   const status = children as string;
      //   if (status === OrderStatus.NEW || status === OrderStatus.OPEN) {
      //     return "Pending";
      //   }
      //   const text = firstLetterToUpperCase(status);

      //   return text;
      // }

      // Apply capitalization if requested
      if (capitalize && typeof children === "string") {
        return children.charAt(0).toUpperCase() + children.slice(1);
      }

      return children;
    }, [children, rule, formatString, range, symbolElement, capitalize]);

    const contentWithFix = useMemo(() => {
      if (
        typeof suffix === "undefined" &&
        typeof prefixElement === "undefined"
      ) {
        return content;
      }
      return (
        <span className="oui-flex oui-items-center oui-gap-1">
          {prefixElement}
          {content}
          {suffix}
        </span>
      );
    }, [content, suffix, prefixElement]);

    return (
      <Text {...(rest as TextProps)} ref={ref}>
        {contentWithFix}
      </Text>
    );
  },
);

FormattedText.displayName = "FormattedText";
