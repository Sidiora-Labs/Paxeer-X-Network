import { type ReactNode, useRef } from "react";
import {
  REFERRAL_CODE_MAX_LENGTH,
  REFERRAL_CODE_MIN_LENGTH,
} from "@paxeer-network/hooks";
import { inputFormatter, TextField } from "@paxeer-network/ui";

export type ReferralCodeInputProps = {
  value: string;
  onChange: (value: string) => void;
  autoFocus: boolean;
  disabled: boolean;
  label?: ReactNode;
  placeholder?: string;
  helpText?: string;
  color?: "danger";
};

export const ReferralCodeInput = (props: ReferralCodeInputProps) => {
  const hasSetCursorToEnd = useRef(false);

  return (
    <TextField
      type="text"
      fullWidth
      label={props.label ?? ""}
      placeholder={props.placeholder}
      value={props.value}
      onChange={(e) => {
        props.onChange(e.target.value);
      }}
      onFocus={(e) => {
        if (props.autoFocus && !hasSetCursorToEnd.current) {
          hasSetCursorToEnd.current = true;
          const input = e.target as HTMLInputElement;
          const len = input.value.length;
          requestAnimationFrame(() => {
            input.setSelectionRange(len, len);
          });
        }
      }}
      formatters={[
        inputFormatter.createRegexInputFormatter((value: string | number) => {
          return String(value).replace(/[a-z]/g, (char: string) =>
            char.toUpperCase(),
          );
        }),
        inputFormatter.createRegexInputFormatter(/[^A-Z0-9]/g),
      ]}
      className="oui-w-full"
      classNames={{
        label: "oui-text-base-contrast-54 oui-text-xs",
        input: "placeholder:oui-text-base-contrast-20 placeholder:oui-text-sm",
      }}
      helpText={props.helpText}
      color={props.color}
      maxLength={REFERRAL_CODE_MAX_LENGTH}
      minLength={REFERRAL_CODE_MIN_LENGTH}
      autoComplete="off"
      disabled={props.disabled}
      autoFocus={props.autoFocus}
    />
  );
};
