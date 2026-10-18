import React, {
  useState,
  useRef,
  useCallback,
  useMemo,
  useEffect,
  forwardRef,
} from "react";
import { InputHTMLAttributes } from "react";
import { findLongestCommonSubString } from "@paxeer-network/utils";
import { InputFormatter } from "./formatter/inputFormatter";

export interface BaseInputProps<T = string> extends Omit<
  InputHTMLAttributes<HTMLInputElement>,
  "size" | "prefix" | "disabled" | "inputMode" | "color"
> {
  disabled?: boolean;

  clearable?: boolean;
  onClean?: () => void;
  fixClassName?: string;
  helpText?: string;
  loading?: boolean;
  inputMode?: "decimal" | "numeric" | "amount";
  // TODO: "containerClassName" is accepted but never applied to any DOM node, so
  // classes passed by consumers (affiliate editReferralRate, ui-share sharePnL)
  // have no effect. Implement it (e.g. forward to the root wrapper) or remove it
  // together with its usages.
  containerClassName?: string;

  formatters?: InputFormatter[];

  // dp?: number;
  // tick?: number;

  onValueChange?: (value: T) => void;
}

export const BaseInput = forwardRef<HTMLInputElement, BaseInputProps>(
  (props, ref) => {
    const {
      clearable,
      onClean,
      fixClassName,
      helpText,
      loading,
      inputMode,
      containerClassName,
      onValueChange,
      onChange,
      id,
      formatters,
      value,
      ...inputProps
    } = props;

    const [cursor, setCursor] = useState<number | null>(null);
    const innerInputRef = useRef<HTMLInputElement>(null);
    const prevInputValue = useRef<string | null>(null);

    const isFocused = useRef<boolean>(false);

    const innerFormatters = useMemo<InputFormatter[]>(() => {
      return formatters ?? [];
    }, [formatters]);

    useEffect(() => {
      if (!ref) return;
      if (typeof ref === "function") {
        ref(innerInputRef.current);
      } else {
        ref.current = innerInputRef.current;
      }
    }, [innerInputRef, ref]);

    const formatToRender = useCallback(
      (value: string) => {
        if (!Array.isArray(innerFormatters) || innerFormatters.length === 0)
          return value;
        if (value === null || value === undefined) return "";
        let index = 0;
        while (index < innerFormatters.length) {
          value = innerFormatters[index].onRenderBefore(value, {
            isFocused: isFocused.current,
          });

          index++;
        }

        return value;
      },
      [innerFormatters],
    );

    const formatToChange = useCallback(
      (value: string, originValue?: string) => {
        if (!Array.isArray(innerFormatters) || innerFormatters.length === 0)
          return value;
        if (value === null || value === undefined) return "";
        let index = innerFormatters.length - 1;
        while (index > -1) {
          value = innerFormatters[index].onSendBefore(value, {
            isFocused: isFocused.current,
            originValue,
          });
          index--;
        }

        return value;
      },
      [innerFormatters],
    );

    const formattedValue = useMemo(() => {
      if (typeof value === "undefined" || value === null) return "";
      return formatToRender(value as string);
    }, [value, formatToRender]);

    // fix cursor pointer jump to end;
    useEffect(() => {
      if (document.activeElement !== innerInputRef.current) return;
      // filter the thousands separator
      const nextValueLen = `${formattedValue}`.length;
      const prevValueLen = prevInputValue.current?.length || 0;

      const next = cursor ? cursor + (nextValueLen - prevValueLen) : 0;
      innerInputRef.current?.setSelectionRange(next, next);
    }, [formattedValue]);

    const onInputChange = (event: React.ChangeEvent<HTMLInputElement>) => {
      if (event.target.value.length < (props.value as string)?.length) {
        const currentCursor = event.target.selectionStart;
        const diffIndex = findLongestCommonSubString(
          `${props.value}`,
          event.target.value,
        );

        if (diffIndex > -1) {
          // @ts-ignore
          const diffStr = `${props.value}`.at(diffIndex);
          if (diffStr === ",") {
            event.target.value = `${event.target.value.substring(
              0,
              diffIndex - 1,
            )}${event.target.value.substring(diffIndex)}`;

            event.target.selectionStart = currentCursor ? currentCursor - 1 : 0;
          }
        }
      }

      if (typeof onChange === "function") {
        onChange(event);
      }

      if (typeof onValueChange === "function") {
        let value = event.target.value;

        value = formatToChange(value, String(props.value));

        onValueChange(value);
      }
      prevInputValue.current = event.target.value;
      setCursor(event.target.selectionStart);
    };

    const onInputFocus = (event: React.FocusEvent<HTMLInputElement>) => {
      isFocused.current = true;
      inputProps.onFocus?.(event);
    };

    const onInputBlur = (event: React.FocusEvent<HTMLInputElement>) => {
      isFocused.current = false;
      inputProps.onBlur?.(event);
    };
    return (
      <input
        type="text"
        {...inputProps}
        ref={innerInputRef}
        onBlur={onInputBlur}
        onFocus={onInputFocus}
        onChange={onInputChange}
        value={formattedValue}
        id={id}
      />
    );
  },
);

BaseInput.displayName = "BaseInput";
