import { FC, useCallback, useEffect, useRef, MouseEvent } from "react";
import { EMPTY_LIST } from "@paxeer-network/types";
import {
  inputFormatter,
  Input,
  InputProps,
  CheckIcon,
  cn,
  Flex,
} from "@paxeer-network/ui";

export type EditableCellInputProps = {
  id?: string;
  name?: string;
  placeholder?: string;
  suffix?: React.ReactNode;
  value?: InputProps["value"];
  onChange?: (value: string) => void;
  error?: string;
  formatters?: InputProps["formatters"];
  overrideFormatters?: InputProps["formatters"];
  classNames?: InputProps["classNames"];
  readonly?: boolean;
  onClick: (e: MouseEvent) => void;
  onFocus?: (e: any) => void;
  onBlur?: (e: any) => void;
  handleKeyDown?: (e: any) => void;
};

export const EditableCellInput: FC<EditableCellInputProps> = (props) => {
  const { onClick } = props;
  const inputRef = useRef<HTMLInputElement>(null);

  const handleKeyDown = useCallback(
    (event: any) => {
      if (event.key === "Enter") {
        onClick(event);
      }
    },
    [onClick],
  );

  useEffect(() => {
    const input = inputRef.current;
    if (input) {
      const length = input.value.length;
      input.setSelectionRange(length, length);
    }
  }, [props.value]);

  return (
    <Input.tooltip
      ref={inputRef}
      tooltip={props.error}
      autoComplete="off"
      autoFocus
      type="text"
      size="sm"
      placeholder={props.readonly ? "" : props.placeholder || ""}
      id={props.id}
      name={props.name}
      color={props.error ? "danger" : undefined}
      value={props.readonly ? "" : props.value || ""}
      onValueChange={props.onChange}
      onClick={(e) => {
        e.stopPropagation();
        e.preventDefault();
      }}
      onFocus={props.onFocus}
      onBlur={props.onBlur}
      onKeyDown={props.handleKeyDown || handleKeyDown}
      formatters={
        props.overrideFormatters || [
          ...(props.formatters ?? EMPTY_LIST),
          inputFormatter.numberFormatter,
          inputFormatter.decimalPointFormatter,
        ]
      }
      classNames={{
        // TODO(theme): "oui-bg-base-700" is an invalid token (the base scale only defines 1-10) and generates no CSS. Remove it or replace with a valid token (e.g. "oui-bg-base-7") after visual comparison.
        root: "oui-bg-base-700 oui-px-2 oui-py-1 oui-rounded",
        input: "oui-pe-2",
      }}
      readOnly={props.readonly}
      suffix={
        <Flex gapX={1}>
          {props.suffix}
          <button onClick={onClick} disabled={!!props.error}>
            <CheckIcon
              size={18}
              color="white"
              opacity={1}
              className={cn(
                "oui-opacity-50",
                props.error
                  ? "oui-cursor-not-allowed"
                  : "oui-cursor-pointer hover:oui-opacity-100",
              )}
            />
          </button>
        </Flex>
      }
    />
  );
};
